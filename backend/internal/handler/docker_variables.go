package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/repository"
	"gopkg.in/yaml.v3"
)

// dockerVariableInput 容器变量创建/更新请求体(与网关变量独立)。
type dockerVariableInput struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

// 容器页内建变量(运行时按编排项目解析,只读)。
// 系统变量现仅保留 GB_PROJ_FILE(项目默认地址);GB_PROJECT/GB_DISPLAY 已移除。
const (
	ProjectVarName = "GB_PROJ_NAME" // 项目名称
	ProjectVarFile = "GB_PROJ_FILE" // 项目默认地址:<dataDir>/appData/<项目名>/
)

// listContainerVariables 列出容器页用户变量(内建由前端展示)。
func (d *dockerAPI) listContainerVariables(w http.ResponseWriter, r *http.Request) {
	vs, err := d.s.ListContainerVariables()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

func (d *dockerAPI) createContainerVariable(w http.ResponseWriter, r *http.Request) {
	var in dockerVariableInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if !validVariableKey(in.Key) {
		writeErr(w, http.StatusBadRequest, "变量 key 不合法(不得以 GB_ 开头)")
		return
	}
	key := strings.TrimSpace(in.Key)
	if _, err := d.s.GetContainerVariable(key); err == nil {
		writeErr(w, http.StatusConflict, "变量已存在")
		return
	}
	v := &model.Variable{Key: key, Value: in.Value, Description: in.Description, CreatedAt: time.Now()}
	if err := d.s.SaveContainerVariable(v); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (d *dockerAPI) updateContainerVariable(w http.ResponseWriter, r *http.Request) {
	v, err := d.s.GetContainerVariable(r.PathValue("key"))
	if errors.Is(err, repository.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "变量不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in dockerVariableInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	v.Value = in.Value
	v.Description = in.Description
	if err := d.s.SaveContainerVariable(v); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (d *dockerAPI) deleteContainerVariable(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if _, err := d.s.GetContainerVariable(key); errors.Is(err, repository.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "变量不存在")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := d.s.DeleteContainerVariable(key); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

var containerVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// interpolateContainerVars 把 compose YAML 中的 ${KEY} 占位替换为容器变量值。
//
// 内建:GB_PROJ_NAME=项目名,GB_PROJ_FILE=项目默认地址;其余查用户容器变量。
// 已定义键的替换;**未定义的 ${KEY} 原样保留**,交由 docker compose / 宿主环境
// 解释——因为 ${…} 是 docker compose 原生变量语法,不能像网关 <%%…%%> 那样对
// 未命中即报错。${KEY:-def} / ${KEY:?err} 等带修饰形态正则不匹配,原样透传。
// 网关变量(<%…%>)与容器变量相互独立。
func (d *dockerAPI) interpolateContainerVars(project, _displayName, yaml string) (string, error) {
	vals := map[string]string{}
	// 项目默认地址:表单默认挂载路径引用 ${GB_PROJ_FILE}/<name>/),需保证以 / 或以 ./
	// 开头,否则 docker compose 会把它当命名卷而非 bind mount。值**不含结尾 /**,
	// 便于 `${GB_PROJ_FILE}/<子目录>/` 组合表达。
	projFile := filepath.Join(d.dataDir, "appData", project)
	if !filepath.IsAbs(projFile) {
		projFile = "./" + projFile
	}
	vals[ProjectVarName] = project
	vals[ProjectVarFile] = projFile

	// 服务端口系统变量:GB_SER_<服务No>_PORT_<发布端口No>(No/No 均 1 起)。
	// 仅填有发布端口的项;无发布端口的服务不生成变量(健康检查默认引用
	// 第一个发布端口,服务未映射端口时其 healthcheck 由前端省略)。
	for k, v := range composePublishedPorts(yaml) {
		vals[k] = v
	}

	if userVars, err := d.s.ListContainerVariables(); err == nil {
		for _, v := range userVars {
			vals[v.Key] = v.Value
		}
	}
	return containerVarRe.ReplaceAllStringFunc(yaml, func(m string) string {
		key := m[2 : len(m)-1] // 去掉 ${ }
		if v, ok := vals[key]; ok {
			return v
		}
		return m
	}), nil
}

// composePublishedPorts 解析 compose YAML 的 services(保持键顺序)各服务的发布端口,
// 生成系统变量映射 GB_SER_<No>_PORT_<N> → 发布端口(No=服务序号 1 起,N=发布端口序号 1 起,
// 仅统计有 host 映射的端口项)。
func composePublishedPorts(yamlStr string) map[string]string {
	out := map[string]string{}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlStr), &doc); err != nil || len(doc.Content) == 0 {
		return out
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return out
	}
	var services *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "services" {
			services = root.Content[i+1]
			break
		}
	}
	if services == nil || services.Kind != yaml.MappingNode {
		return out
	}
	svcNo := 0
	for i := 0; i+1 < len(services.Content); i += 2 {
		svcNo++
		svc := services.Content[i+1]
		if svc.Kind != yaml.MappingNode {
			continue
		}
		var ports *yaml.Node
		for j := 0; j+1 < len(svc.Content); j += 2 {
			if svc.Content[j].Value == "ports" {
				ports = svc.Content[j+1]
				break
			}
		}
		if ports == nil || ports.Kind != yaml.SequenceNode {
			continue
		}
		portNo := 0
		for _, item := range ports.Content {
			host := ""
			switch item.Kind {
			case yaml.ScalarNode:
				host = publishedPortFromString(item.Value)
			case yaml.MappingNode:
				host = publishedPortFromMapping(item)
			}
			if host == "" {
				continue
			}
			portNo++
			out[fmt.Sprintf("GB_SER_%d_PORT_%d", svcNo, portNo)] = host
		}
	}
	return out
}

// publishedPortFromString 从短形式端口 "8080:80" / "127.0.0.1:8080:80" / "8080:80/tcp"
// 提取发布端口;纯容器端口("80")无发布端口返回空。
func publishedPortFromString(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

// publishedPortFromMapping 从长形式端口映射 {published: 8080, target: 80} 提取发布端口;
// 未声明 published(纯容器端口)返回空。
func publishedPortFromMapping(n *yaml.Node) string {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "published" {
			return n.Content[i+1].Value
		}
	}
	return ""
}
