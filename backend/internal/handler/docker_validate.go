package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/JiangBeta/gatebox/internal/adapter/docker/client"
	"gopkg.in/yaml.v3"
)

// 本文件承载部署前的三项本地校验(docs §4.1 Q22)。
//
// `config -q` 只能查出语法与 schema 错误,查不出「镜像不存在/端口占用/
// 挂载路径缺失」。这三项纯本地、毫秒级、零网络依赖,却能拦掉最常见的部署失败。
// 结果是非阻断的警告——不联网校验镜像/tag 是否存在,让 pull 阶段去报更诚实。

// localValidate 返回部署前的警告列表(空 = 无警告)。
func (d *dockerAPI) localValidate(ctx context.Context, yamlStr, dir string) []string {
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(yamlStr), &doc); err != nil {
		return nil // 语法错误由 config -q 处理,这里不重复报
	}
	if len(doc.Services) == 0 {
		return nil
	}

	// 现有容器端口映射(端口占用检查)
	containers, _ := d.cli.ListContainers(ctx, client.ListContainersOptions{All: true})
	usedPorts := map[int]bool{}
	for _, ct := range containers {
		for _, p := range ct.Ports {
			if p.PublicPort > 0 {
				usedPorts[int(p.PublicPort)] = true
			}
		}
	}

	// 本地镜像集合(镜像本地检查)
	images, _ := d.cli.ListImages(ctx, client.ListImagesOptions{})
	localTags := map[string]bool{}
	for _, img := range images {
		for _, tag := range img.RepoTags {
			localTags[tag] = true
		}
	}

	var warns []string
	for _, svc := range doc.Services {
		if svc == nil {
			continue
		}
		// 1. 镜像本地
		if image, ok := svc["image"].(string); ok && image != "" && !imageExistsLocally(image, localTags) {
			warns = append(warns, fmt.Sprintf("镜像 %s 本地不存在,部署时将拉取", image))
		}
		// 2. 端口占用
		for _, p := range portStrings(svc["ports"]) {
			if hp := parseHostPort(p); hp > 0 && usedPorts[hp] {
				warns = append(warns, fmt.Sprintf("宿主机端口 %d 已被其他容器占用", hp))
			}
		}
		// 3. 挂载路径(bind 源)
		for _, v := range volumeStrings(svc["volumes"]) {
			if src := parseBindSource(v); src != "" {
				abs := src
				if !filepath.IsAbs(abs) {
					abs = filepath.Join(dir, abs)
				}
				if _, err := os.Stat(abs); os.IsNotExist(err) {
					warns = append(warns, fmt.Sprintf("挂载路径 %s 不存在", src))
				}
			}
		}
	}
	return dedupe(warns)
}

// imageExistsLocally 判断镜像是否已在本地(含无 tag 时匹配 :latest)。
func imageExistsLocally(image string, localTags map[string]bool) bool {
	if localTags[image] {
		return true
	}
	if !strings.Contains(image, ":") && !strings.Contains(image, "@") {
		return localTags[image+":latest"]
	}
	return false
}

// portStrings 提取 ports 的短形式字符串列表(长形式对象跳过)。
func portStrings(v any) []string { return scalarList(v) }

// volumeStrings 提取 volumes 的短形式字符串列表。
func volumeStrings(v any) []string { return scalarList(v) }

// scalarList 从 any(可能是 []any 或单个 string)提取字符串列表。
func scalarList(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		out = append(out, t)
	}
	return out
}

// parseHostPort 从 "8080:80" / "127.0.0.1:8080:80" 提取宿主机端口;未映射返回 0。
func parseHostPort(s string) int {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return 0 // "80" 只映射容器端口
	}
	// 末段是容器端口,倒数第二段是宿主机端口(可能被 IP 抢占为倒数第三段)
	hostPart := parts[len(parts)-2]
	n, err := strconv.Atoi(hostPart)
	if err != nil {
		return 0
	}
	return n
}

// parseBindSource 从 "./config:/config" 提取 bind 源;非 bind 或未映射返回空。
func parseBindSource(s string) string {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	src, dst := parts[0], parts[1]
	// 绝对路径源(如 /media)或相对路径源(如 ./config)才算 bind;
	// 卷名(:/data)是命名卷,不属于「宿主机路径」校验范围。
	if strings.HasPrefix(src, "/") || strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../") || strings.HasPrefix(src, "~/") {
		if strings.HasPrefix(dst, "/") {
			return src
		}
	}
	return ""
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// varRefRe 匹配 ${VAR} / ${VAR:-default} / ${VAR?err} 三种 compose 插值形式。
var varRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)([:-][^}]*)?\}`)

// detectUndefinedVars 检测 YAML 里引用但未定义的变量(docs §4.1)。
//
// compose 对未定义变量**静默替换为空串**(实测),而变量名敲错导致的
// 「部署成功但配置是空串」是最阴的一类失败。这里把 .env 与宿主环境
// 里都没有的变量揪出来告警。
func (d *dockerAPI) detectUndefinedVars(yamlStr, dir string) []string {
	refs := map[string]bool{}
	for _, m := range varRefRe.FindAllStringSubmatch(yamlStr, -1) {
		refs[m[1]] = true
	}
	if len(refs) == 0 {
		return nil
	}

	defined := map[string]bool{}
	if data, err := os.ReadFile(filepath.Join(dir, ".env")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if eq := strings.IndexByte(line, '='); eq > 0 {
				defined[strings.TrimSpace(line[:eq])] = true
			}
		}
	}

	var undefined []string
	for name := range refs {
		if defined[name] {
			continue
		}
		if _, ok := os.LookupEnv(name); ok {
			continue // 宿主环境变量,compose 也能读到
		}
		undefined = append(undefined, name)
	}
	sort.Strings(undefined)
	if len(undefined) == 0 {
		return nil
	}
	return []string{"未定义变量(将被替换为空串): " + strings.Join(undefined, ", ")}
}
