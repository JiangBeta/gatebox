package handler

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/JiangBeta/gatebox/internal/gateway"
	"gopkg.in/yaml.v3"
)

// 容器内置变量(运行时按编排项目解析,只读,不入库)。
// 用户变量已与网关统一到 bucket `variables`(ADR-035,见设置 → 变量)。
const (
	ProjectVarName = "GB_PROJ_NAME" // 项目名称
	ProjectVarFile = "GB_PROJ_FILE" // 项目默认地址:<GB_DATA_DIR>/appData/<GB_PROJ_NAME>(不含结尾 /)
	ProjectVarData = "GB_DATA_DIR"  // 运行时数据根目录(与网关 <%GB_DATA_DIR%> 同值)

	SubDomainVar = "GB_SUB_DOMAIN" // 项目首个 caddy 站点的完整域名
	ACMEFileVar  = "GB_ACME_FILE"  // acme.sh 工作目录(= <GB_DATA_DIR>/tools/acme)
	SSLFileVar   = "GB_SSL_FILE"   // <GB_ACME_FILE>/certs/<GB_SUB_DOMAIN>/fullchain.pem
)

// containerSystemVariables 返回容器侧系统变量描述符(ADR-035 §1,供统一「变量」页展示)。
// dataDir 为运行时数据根目录(绝对路径),用于展示 GB_DATA_DIR 实际值。
func containerSystemVariables(dataDir string) []systemVariableView {
	return []systemVariableView{
		{Key: ProjectVarName, Placeholder: "${" + ProjectVarName + "}", Value: "${" + ProjectVarName + "}", Description: "当前编排项目名(按项目注入)", Context: "container"},
		{Key: ProjectVarData, Placeholder: "${" + ProjectVarData + "}", Value: dataDir, Description: "运行时数据根目录(与网关 <%GB_DATA_DIR%> 同值)", Context: "container"},
		{Key: ProjectVarFile, Placeholder: "${" + ProjectVarFile + "}", Value: dataDir + "/appData/<项目名>", Description: "项目默认地址 = <GB_DATA_DIR>/appData/<GB_PROJ_NAME>(按项目注入,不含结尾 /)", Context: "container"},
		{Key: SubDomainVar, Placeholder: "${" + SubDomainVar + "}", Value: "${" + SubDomainVar + "}", Description: "当前编排项目首个 caddy 站点的完整域名(按项目 caddy label 解析;无条件则不注入)", Context: "container"},
		{Key: ACMEFileVar, Placeholder: "${" + ACMEFileVar + "}", Value: filepath.Join(dataDir, "tools", "acme"), Description: "acme.sh 工作目录/证书根(= <GB_DATA_DIR>/tools/acme)", Context: "container"},
		{Key: SSLFileVar, Placeholder: "${" + SSLFileVar + "}", Value: filepath.Join(dataDir, "tools", "acme", "certs", "${"+SubDomainVar+"}", "fullchain.pem"), Description: "证书链路径 = <GB_ACME_FILE>/certs/<GB_SUB_DOMAIN>/fullchain.pem", Context: "container"},
		{Key: "GB_SER_<n>_PORT_<m>", Placeholder: "${GB_SER_<n>_PORT_<m>}", Value: "${GB_SER_<n>_PORT_<m>}", Description: "第 n 个服务的第 m 个发布端口(派生模式,按编排 YAML 端口映射生成)", Context: "container"},
	}
}

var containerVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// interpolateContainerVars 把 compose YAML 中的 ${KEY} 占位替换为变量值。
//
// 内建:GB_PROJ_NAME=项目名,GB_DATA_DIR=数据根,GB_PROJ_FILE=项目默认地址(=<GB_DATA_DIR>/appData/<GB_PROJ_NAME>,不含结尾 /);其余查统一用户变量(ADR-035)。
// 已定义键的替换;**未定义的 ${KEY} 原样保留**,交由 docker compose / 宿主环境
// 解释——因为 ${…} 是 docker compose 原生变量语法,不能像网关 <%%…%%> 那样对
// 未命中即报错。${KEY:-def} / ${KEY:?err} 等带修饰形态正则不匹配,原样透传。
// 用户变量与网关共用同一存储,仅引用写法不同。
func (d *dockerAPI) interpolateContainerVars(project, _displayName, yaml string) (string, error) {
	vals := map[string]string{}
	// 项目默认地址:表单默认挂载路径引用 ${GB_PROJ_FILE}/<name>/),需保证以 / 或以 ./
	// 开头,否则 docker compose 会把它当命名卷而非 bind mount。值**不含结尾 /**,
	// 便于 `${GB_PROJ_FILE}/<子目录>/` 组合表达。
	// 路径来自 compose.ManagedDir(唯一真源),与 GB_DATA_DIR/appData/项目名 同源。
	projFile := d.cmp.ManagedDir(project)
	if !filepath.IsAbs(projFile) {
		projFile = "./" + projFile
	}
	vals[ProjectVarName] = project
	vals[ProjectVarData] = d.dataDir
	vals[ProjectVarFile] = projFile

	// ACME/SSL 路径系统变量:acme.sh 工作目录固定在 <GB_DATA_DIR>/tools/acme,
	// GB_SUB_DOMAIN 取编排 YAML 中首个 caddy 站点的完整域名(无条件则不注入)。
	acme := filepath.Join(d.dataDir, "tools", "acme")
	vals[ACMEFileVar] = acme
	if host := composeFirstCaddyHost(yaml); host != "" {
		vals[SubDomainVar] = host
		vals[SSLFileVar] = filepath.Join(acme, "certs", host, "fullchain.pem")
	}

	// 服务端口系统变量:GB_SER_<服务No>_PORT_<发布端口No>(No/No 均 1 起)。
	// 仅填有发布端口的项;无发布端口的服务不生成变量(健康检查默认引用
	// 第一个发布端口,服务未映射端口时其 healthcheck 由前端省略)。
	for k, v := range composePublishedPorts(yaml) {
		vals[k] = v
	}

	if userVars, err := d.s.ListVariables(); err == nil {
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

// composeFirstCaddyHost 从 compose YAML 中取首个带 caddy 站点的服务主机名。
// labels 支持 map 与 "k=v" 列表两种写法;跨服务按声明顺序取第一个命中。
func composeFirstCaddyHost(yamlStr string) string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlStr), &doc); err != nil || len(doc.Content) == 0 {
		return ""
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return ""
	}
	var services *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "services" {
			services = root.Content[i+1]
			break
		}
	}
	if services == nil || services.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		svc := services.Content[i+1]
		if svc.Kind != yaml.MappingNode {
			continue
		}
		var labels *yaml.Node
		for j := 0; j+1 < len(svc.Content); j += 2 {
			if svc.Content[j].Value == "labels" {
				labels = svc.Content[j+1]
				break
			}
		}
		if labels == nil {
			continue
		}
		if host := gateway.FirstSiteHost(yamlLabelsToMap(labels)); host != "" {
			return host
		}
	}
	return ""
}

// yamlLabelsToMap 把 compose labels 节点(map 或 "k=v" 序列)转成 map。
func yamlLabelsToMap(n *yaml.Node) map[string]string {
	out := map[string]string{}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			out[n.Content[i].Value] = n.Content[i+1].Value
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode {
				continue
			}
			if k, v, ok := strings.Cut(item.Value, "="); ok {
				out[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
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
