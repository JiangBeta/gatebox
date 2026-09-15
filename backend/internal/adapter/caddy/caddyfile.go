// Package caddy 负责 Caddyfile 生成与 Caddy Admin API 交互。
package caddy

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JiangBeta/gatebox/internal/model"
)

// GenerateOptions 生成附加选项(可变参数,缺省零值)。
type GenerateOptions struct {
	// HTTPPort/HTTPSPort 覆盖 caddy 站点监听的 http/https 端口并写入全局块
	// (http_port/https_port)。0 = 标准 80/443。用于与同机 80/443 占用者
	// (如 traefik)共存时把 caddy 挪到高可用端口(ADR-001 控制面共存)。
	HTTPPort  int
	HTTPSPort int
	// ExtraHTTPSPorts 额外的 https 监听端口(与主 https 端口并存):
	// 为每个 https 站点额外生成 host:<port> 重复 site block(Caddy 按端口分 server,
	// 证书同 host 复用一次)。如 [9443] 使 443 与 9443 都可 https 访问。
	ExtraHTTPSPorts []int
	// ExtraHTTPPorts 额外的 http 监听端口(与主 http 端口并存):
	// 为每个 http 站点额外生成 http://host:<port> 重复 site block。
	ExtraHTTPPorts []int
	// StaticRoot 全局静态根目录,填充内建变量 <%GB_STATIC_ROOT%>(ADR-033)。
	StaticRoot string
	// GlobalSnippets 由扩展注册表 renderer 产出的全局块片段(原样插入全局块)。
	// 核心不解析其内容——能力型插件(如 Caddy-L4)经此接入(ADR-036)。
	GlobalSnippets []string
}

// Generate 根据服务与 Caddy 片段生成完整 Caddyfile(ADR-002)。
//
// services 为 manual 落库服务(调用方可并入 docker 派生)。
// fragments 为用户片段;variables 为用户变量;builtins 为只读内置清单。
// dataDir 用于日志与 user 扩展目录;dns 是 rootDomain → 凭证(生成 tls dns)。
func Generate(services []model.Service, apps []model.App, fragments []model.Fragment, variables []model.Variable, builtins []model.FragmentTemplate, dataDir string, dns map[string]model.DNSCredential, opts ...GenerateOptions) (string, error) {
	var opt GenerateOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	fragMap := make(map[string]fragmentBody, len(fragments)+len(builtins))
	for _, f := range fragments {
		fragMap[f.ID] = fragmentBody{code: f.Code}
	}
	for _, b := range builtins {
		fragMap[b.ID] = fragmentBody{code: b.Code}
	}
	varEnv := make(map[string]string, len(variables))
	for _, v := range variables {
		varEnv["<%"+v.Key+"%>"] = v.Value
	}
	appNames := make(map[string]string, len(apps))
	for _, a := range apps {
		appNames[a.ID] = a.Name
	}

	var b strings.Builder
	writeGlobalBlock(&b, dataDir, opt)

	for _, svc := range services {
		if !svc.Enabled {
			continue
		}
		applied := serviceFragments(svc, fragMap)
		if err := writeServiceBlocks(&b, svc, appNames, applied, fragMap, varEnv, dataDir, opt.StaticRoot, dns, opt.ExtraHTTPSPorts, opt.ExtraHTTPPorts); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

// collectL4Servers 已移除：非 HTTP 协议的代理块由扩展注册表中的 renderer 产出
// （核心只提供中性规则，不认识 layer4 语法，ADR-036）。

// fragmentBody 片段渲染体(内置或用户,形式一致)。
type fragmentBody struct {
	code string
}

// serviceFragments 计算某服务实际应用的片段 ID:
// = FragmentIDs(API 层已 seed 默认启用) ∖ ExcludeFragmentIDs,并剔除不存在的 ID;
// 另按 UpstreamProto 条件应用「忽略证书校验」(https,ADR-033 §2)。
func serviceFragments(svc model.Service, fragMap map[string]fragmentBody) []string {
	set := map[string]bool{}
	for _, id := range svc.FragmentIDs {
		if _, ok := fragMap[id]; ok {
			set[id] = true
		}
	}
	for _, ex := range svc.ExcludeFragmentIDs {
		delete(set, ex)
	}
	// 忽略证书校验:仅后端协议 = https 时应用,与落库 FragmentIDs 解耦(避免协议变更残留)。
	if svc.UpstreamProto == "https" {
		if _, ok := fragMap[model.FragmentSkipVerify]; ok {
			set[model.FragmentSkipVerify] = true
		}
	} else {
		delete(set, model.FragmentSkipVerify)
	}
	var ids []string
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// writeGlobalBlock 写全局选项块:http/https 端口覆盖(可选) + access log + 扩展全局片段(可选)。
func writeGlobalBlock(b *strings.Builder, dataDir string, opt GenerateOptions) {
	b.WriteString("{\n")
	if opt.HTTPPort > 0 {
		fmt.Fprintf(b, "\thttp_port %d\n", opt.HTTPPort)
	}
	if opt.HTTPSPort > 0 {
		fmt.Fprintf(b, "\thttps_port %d\n", opt.HTTPSPort)
	}
	accessLog := filepath.Join(dataDir, "logs", "caddy", "access.log")
	fmt.Fprintf(b, "\tlog {\n\t\toutput file %s {\n\t\t\troll_size 100mb\n\t\t\troll_keep 5\n\t\t}\n\t\tformat json\n\t}\n", accessLog)
	for _, snip := range opt.GlobalSnippets {
		writeIndented(b, snip, "\t")
	}
	b.WriteString("}\n\n")
}

// writeIndented 把多行片段整体缩进后写入(空行不缩进)。
func writeIndented(b *strings.Builder, snippet, indent string) {
	snippet = strings.TrimRight(snippet, "\n")
	for _, ln := range strings.Split(snippet, "\n") {
		if ln == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent)
		b.WriteString(ln)
		b.WriteString("\n")
	}
}

// writeServiceBlocks 为一个服务(可能多个域名行)生成多个 site block。
// https 域名行额外为 ExtraHTTPSPorts 各生成一个 host:<port> 重复块;http 域名行
// 额外为 ExtraHTTPPorts 各生成一个 http://host:<port> 重复块(多端口并存,ADR-026)。
func writeServiceBlocks(b *strings.Builder, svc model.Service, appNames map[string]string, applied []string, fragMap map[string]fragmentBody, varEnv map[string]string, dataDir, staticRoot string, dns map[string]model.DNSCredential, extraHTTPSPorts, extraHTTPPorts []int) error {
	if len(svc.Domains) == 0 {
		return fmt.Errorf("服务 %s 没有域名行", svc.Name)
	}
	for _, d := range svc.Domains {
		if !model.IsHTTPProto(d.Protocol) {
			continue // 非 HTTP 协议由全局扩展片段处理(核心不生成)
		}
		if err := writeSiteBlockAt(b, siteAddress(d), svc, appNames, d, applied, fragMap, varEnv, dataDir, staticRoot, dns); err != nil {
			return err
		}
		switch d.Protocol {
		case model.DomainProtoHTTPS:
			// 额外 https 端口:host:<port> 重复块。Caddy 会为不同端口各建一个 https server,
			// 但自动 HTTPS 对同一 host 只签发一次证书(按存储缓存),两个端口都能用。
			for _, p := range extraHTTPSPorts {
				if err := writeSiteBlockAt(b, fmt.Sprintf("%s:%d", d.Host(), p), svc, appNames, d, applied, fragMap, varEnv, dataDir, staticRoot, dns); err != nil {
					return err
				}
			}
		case model.DomainProtoHTTP:
			// 额外 http 端口:http://host:<port> 重复块。
			for _, p := range extraHTTPPorts {
				if err := writeSiteBlockAt(b, fmt.Sprintf("http://%s:%d", d.Host(), p), svc, appNames, d, applied, fragMap, varEnv, dataDir, staticRoot, dns); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// siteAddress 计算 site block 的地址(协议 + 端口 → caddy site address)。
func siteAddress(d model.ProxyDomain) string {
	host := d.Host()
	proto := d.Protocol
	if proto == "" {
		proto = model.DomainProtoHTTPS
	}
	if d.CustomPort && d.Port > 0 {
		if proto == model.DomainProtoHTTP {
			return fmt.Sprintf("http://%s:%d", host, d.Port)
		}
		return fmt.Sprintf("%s:%d", host, d.Port)
	}
	if proto == model.DomainProtoHTTP {
		return "http://" + host
	}
	return host
}

// writeSiteBlockAt 写单个域名行、指定 site 地址的 block(主端口与额外 https 端口共用)。
//
// 片段按作用域分流(ADR-033 §1):顶层片段直接写 site block;`reverse_proxy { ... }`
// 包裹片段收集内部行,随后合并进受控反代块。
func writeSiteBlockAt(b *strings.Builder, addr string, svc model.Service, appNames map[string]string, d model.ProxyDomain, applied []string, fragMap map[string]fragmentBody, varEnv map[string]string, dataDir, staticRoot string, dns map[string]model.DNSCredential) error {
	fmt.Fprintf(b, "%s {\n", addr)

	if d.Protocol == model.DomainProtoHTTPS {
		if _, ok := rootDomainOf(d.RootDomain, dns); ok && tlsFilesExist(dataDir, d.Host()) {
			b.WriteString(renderTLSDNS(dataDir, d.Host()))
		}
	}

	var rpLines []string
	for _, id := range applied {
		body := fragMap[id]
		site, rp, err := parseFragment(body.code, svc, appNames, d, varEnv, staticRoot, dataDir)
		if err != nil {
			return fmt.Errorf("片段 %s: %w", id, err)
		}
		for _, ln := range site {
			fmt.Fprintf(b, "\t%s\n", ln)
		}
		rpLines = append(rpLines, rp...)
	}

	// 透传的 caddy.* 子指令(docker 派生 Escape,ADR-026 §5 中段)。
	// 已在派生解析时装好缩进,原样写入。
	for _, ln := range svc.ExtraDirectives {
		fmt.Fprintf(b, "\t%s\n", ln)
	}

	switch svc.Type {
	case model.RouteTypeReverseProxy:
		// 受控反代段(ADR-026 §5 末段):仅当存在 upstream 时生成。
		// 无 upstream 的派生行 = caddy.reverse_proxy 已透传(ExtraDirectives 承载)。
		if len(svc.Upstream) > 0 {
			if err := writeReverseProxy(b, svc, rpLines); err != nil {
				return err
			}
		}
	case model.RouteTypeFileServer:
		if err := writeFileServer(b, svc, appNames, d, varEnv, staticRoot, dataDir); err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知服务类型: %q", svc.Type)
	}

	userGlob := filepath.Join(dataDir, "tools", "caddy", "user", "*")
	fmt.Fprintf(b, "\timport %s\n", userGlob)

	b.WriteString("}\n\n")
	return nil
}

// writeReverseProxy 写 reverse_proxy 指令(片段内部行 / healthUri / 目标 https 走 tls transport)。
// 多个 upstream(负载均衡,ADR-018「字段数组化向后兼容」)写块形式,
// caddy 默认按 round-robin 分摊;单 upstream 且无附加选项时保持单行(输出稳定)。
// 片段已提供 transport 时,生成器不再注入默认 tls transport(片段优先,ADR-033 §1)。
func writeReverseProxy(b *strings.Builder, svc model.Service, rpLines []string) error {
	if len(svc.Upstream) == 0 {
		return fmt.Errorf("反向代理 %s 缺少 upstream", svc.Name)
	}
	ups := strings.Join(svc.Upstream, " ")
	inner := make([]string, 0, len(rpLines)+2)
	inner = append(inner, rpLines...)
	if svc.HealthURI != "" && !hasReverseProxyDirective(rpLines, "health_uri") {
		inner = append(inner, "health_uri "+svc.HealthURI)
	}
	if svc.UpstreamProto == "https" && !hasReverseProxyDirective(rpLines, "transport") {
		inner = append(inner, "transport http {", "\ttls", "}")
	}
	if len(inner) > 0 || len(svc.Upstream) > 1 {
		fmt.Fprintf(b, "\treverse_proxy %s {\n", ups)
		for _, ln := range inner {
			fmt.Fprintf(b, "\t%s\n", ln)
		}
		b.WriteString("\t}\n")
		return nil
	}
	fmt.Fprintf(b, "\treverse_proxy %s\n", ups)
	return nil
}

// writeFileServer 写 file_server 指令(root 可含 <%VAR%>,browse 开关)。
func writeFileServer(b *strings.Builder, svc model.Service, appNames map[string]string, d model.ProxyDomain, varEnv map[string]string, staticRoot, dataDir string) error {
	root, err := interpolateVars(svc.Root, svc, appNames, d, varEnv, staticRoot, dataDir)
	if err != nil {
		return err
	}
	if root != "" {
		fmt.Fprintf(b, "\troot * %s\n", root)
	}
	if svc.Browse {
		fmt.Fprintf(b, "\tfile_server browse\n")
	} else {
		b.WriteString("\tfile_server\n")
	}
	return nil
}
