package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/docker/client"
	"github.com/JiangBeta/gatebox/internal/models"
)

// 本文件承载「游离容器转编排」(docs §4.3)。
//
// 破坏性操作,风险等级高于「接管」:inspect 反推 compose → 预览 → 用户确认后
// 落盘 appData/<project>/ → 删除原容器(down+up 重建,数据风险)。UI 上必须与
// 「接管」区分开。

// convertPreview 反推 compose YAML,不落盘、不删容器。
func (d *dockerAPI) convertPreview(w http.ResponseWriter, r *http.Request) {
	det, err := d.cli.InspectContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	yaml := inspectToCompose(det)
	writeJSON(w, http.StatusOK, map[string]any{
		"projectName": suggestProjectName(det.TrimmedName()),
		"yaml":        yaml,
	})
}

// convertContainer 确认转换:落盘 + 删除原容器 + 落库。
func (d *dockerAPI) convertContainer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if !models.ValidateProjectName(in.Project) {
		writeErr(w, http.StatusBadRequest, "projectName 不合法")
		return
	}

	det, err := d.cli.InspectContainer(r.Context(), id)
	if err != nil {
		writeDockerErr(w, err)
		return
	}

	// 1. 落盘
	dir := d.cmp.ManagedDir(in.Project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	yaml := inspectToCompose(det)
	if err := os.WriteFile(d.cmp.ManagedFile(in.Project), []byte(yaml), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 2. 删除原游离容器(否则 up 时 container_name 冲突)。停止的才能删。
	if det.State.Running {
		if err := d.cli.StopContainer(r.Context(), id, nil); err != nil {
			writeErr(w, http.StatusInternalServerError, "停止原容器失败: "+err.Error())
			return
		}
	}
	if err := d.cli.RemoveContainer(r.Context(), id, client.RemoveContainerOptions{}); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除原容器失败: "+err.Error())
		return
	}

	// 3. 落库
	inst := &models.ComposeInstance{
		ProjectName: in.Project,
		DisplayName: in.Project,
		HostID:      "local",
		Managed:     true,
		ConfigFiles: d.cmp.ManagedFile(in.Project),
		Editable:    true,
		CreatedAt:   time.Now(),
	}
	if err := d.s.SaveComposeInstance(inst); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"projectName": in.Project})
}

// suggestProjectName 把容器名规范化为合法的 projectName。
func suggestProjectName(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "converted"
	}
	if out[0] == '-' || out[0] == '_' {
		out = "app" + out
	}
	return out
}

// inspectToCompose 从容器详情反推一个可部署的 compose YAML(尽力而为)。
//
// 反推不追求完整还原(运行时信息无法精确回溯),目标是生成一个
// image / container_name / ports / env / volumes / labels 齐全、
// up 后行为接近原容器的 compose。
func inspectToCompose(det *client.ContainerDetail) string {
	var b strings.Builder
	svc := suggestProjectName(det.TrimmedName())

	fmt.Fprintf(&b, "services:\n  %s:\n", svc)
	fmt.Fprintf(&b, "    image: %s\n", det.Config.Image)
	fmt.Fprintf(&b, "    container_name: %s\n", det.TrimmedName())

	if det.HostConfig != nil {
		if rp := det.HostConfig.RestartPolicy.Name; rp != "" {
			fmt.Fprintf(&b, "    restart: %s\n", rp)
		}
		if nm := det.HostConfig.NetworkMode; nm != "" && nm != "default" && nm != "bridge" {
			fmt.Fprintf(&b, "    network_mode: %s\n", nm)
		}
		if det.Config.User != "" {
			fmt.Fprintf(&b, "    user: \"%s\"\n", det.Config.User)
		}
		if len(det.HostConfig.PortBindings) > 0 {
			writePorts(&b, det.HostConfig.PortBindings)
		}
	}

	if len(det.Config.Env) > 0 {
		b.WriteString("    environment:\n")
		for _, e := range det.Config.Env {
			fmt.Fprintf(&b, "      - %s\n", quoteScalar(e))
		}
	}

	if len(det.Mounts) > 0 {
		b.WriteString("    volumes:\n")
		for _, m := range det.Mounts {
			switch m.Type {
			case "bind":
				fmt.Fprintf(&b, "      - %s:%s\n", m.Source, m.Destination)
			case "volume":
				fmt.Fprintf(&b, "      - %s:%s\n", m.Name, m.Destination)
			}
		}
	}

	var labels []string
	for k, v := range det.Config.Labels {
		if strings.HasPrefix(k, "caddy") || strings.HasPrefix(k, "gatebox") {
			labels = append(labels, fmt.Sprintf("      %s: %s", k, quoteScalar(v)))
		}
	}
	if len(labels) > 0 {
		sort.Strings(labels)
		b.WriteString("    labels:\n")
		for _, l := range labels {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}

	return b.String()
}

func writePorts(b *strings.Builder, bindings map[string][]client.PortBinding) {
	// 键形如 "80/tcp",按端口排序保证稳定输出
	keys := make([]string, 0, len(bindings))
	for k := range bindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString("    ports:\n")
	for _, k := range keys {
		for _, pb := range bindings[k] {
			entry := ""
			if pb.HostIP != "" && pb.HostIP != "0.0.0.0" && pb.HostIP != "::" {
				entry = pb.HostIP + ":"
			}
			if pb.HostPort != "" {
				entry += pb.HostPort + ":"
			}
			entry += k
			fmt.Fprintf(b, "      - %s\n", quoteScalar(entry))
		}
	}
}

// quoteScalar 给含特殊字符的标量加双引号(YAML 安全)。
func quoteScalar(s string) string {
	needsQuote := strings.ContainsAny(s, ": #{}[]&*!|>'\"%@`,") || strings.TrimSpace(s) != s || s == ""
	if !needsQuote {
		return s
	}
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}
