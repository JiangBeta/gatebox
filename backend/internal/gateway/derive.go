package gateway

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/JiangBeta/gatebox/internal/model"
)

// ProxyableContainer 网关单位消费的容器视图(对应 GET /docker/proxyable,ADR-016/019/026)。
// 不含容器 ID 与 IP,从结构层面杜绝下游依赖易变值。
type ProxyableContainer struct {
	Project       string            `json:"project"`
	Service       string            `json:"service"`
	ContainerName string            `json:"containerName"`
	DisplayName   string            `json:"displayName"` // ComposeInstance 展示名,可为空
	HostPort      int               `json:"hostPort"`    // 首个宿主映射端口(兼容保留)
	Ports         []ProxyablePort   `json:"ports"`       // 全量 tcp 端口映射(ADR-026)
	Labels        map[string]string `json:"labels"`      // 仅 caddy.* / gatebox.*
	State         string            `json:"state"`
}

// ProxyablePort 一条 tcp 端口映射。Host 为 0 表示未映射到宿主机。
type ProxyablePort struct {
	Internal uint16 `json:"internal"`
	Host     uint16 `json:"host"`
}

// label 键常量(ADR-026)。
const (
	LabelReverseProxy = "caddy.reverse_proxy" // 服务级(=站点 0)共享反代指令
	LabelUpstreamPort = "gatebox.upstream_port"
	LabelFragments    = "gatebox.fragments" // 服务级(=站点 0)共享片段
	LabelDescription  = "gatebox.description"
)

// siteKeyRE 拆 caddy / caddy_N / caddy_N.reverse_proxy 等键。
var siteKeyRE = regexp.MustCompile(`^caddy(?:_(\d+))?$`)

// protoNameRE 非 http/https 的协议名(小写字母/数字,如 mqtt),由能力注册表决定可否代理。
var protoNameRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)
var siteRPRE = regexp.MustCompile(`^caddy(?:_(\d+))?\.reverse_proxy$`)
var siteFragRE = regexp.MustCompile(`^gatebox\.fragments(?:_(\d+))?$`)

// globalOptionDirectives caddy-docker-proxy 中属于全局 options 的指令名
// (ADR-026 §4:忽略 + 告警,全局配置归网关页)。
var globalOptionDirectives = map[string]bool{
	"email": true, "admin": true, "http_port": true, "https_port": true,
	"servers": true, "ocsp_interval": true, "local_certs": true, "debug": true,
	"grace_period": true, "default_sni": true, "storage": true, "auto_https": true,
	"skip_install_trust": true, "log": true, "tls": true,
}

// upstreamsRegex 匹配 {{upstreams [proto] [port]}} 模板(ADR-026 §2)。
var upstreamsRegex = regexp.MustCompile(`\{\{\s*upstreams(?:\s+(https?))?(?:\s+(\d+))?\s*\}\}`)

// DeriveRoutes 从容器 caddy label 派生 docker 自动 Service(ADR-018/019/026)。
//
// 粒度(ADR-026 修订):**每站点(站点地址)派生一个独立 Service**——同一容器可经不同
// 域名发布不同端口/不同片段的服务。旧「多域名共享」label(caddy.reverse_proxy /
// gatebox.fragments 无序号)作为服务级共享,被行级 caddy_N.reverse_proxy /
// gatebox.fragments_N 覆盖继承(向后兼容)。
//
// 规则:每个可代理站点 = 独立 Service。可代理 = running + 至少一个站点
// (ADR-016 寻址:upstream = 127.0.0.1:<宿主映射端口>)。
// 异常保留:非 running 的带 label 容器也派生进列表(gateway.md §9.3)——Enabled=false、
// 不含 upstream,仅展示「容器未运行」,不生成 site block、不上报 ddns。
// 派生 Service 不落库、ID 为空、AppID=compose 项目名(只读自动分组)。
//
// 参数:
//   - domains:受管根域名列表,派生时最长后缀匹配 → 拆 Subdomain+RootDomain
//     (命中的域名进入 ddns/acme 闭环,ADR-026 §6)。可为 nil。
//   - fragments:用户片段,用于 gatebox.fragments 名→ID 解析。可为 nil。
//   - defaultFragmentIDs:默认启用的片段 ID 集(调用方按 seedDefaultFragments(nil,nil)
//     计算,含内置 toggle 覆盖)。派生自动带上。
func DeriveRoutes(containers []ProxyableContainer, domains []model.Domain, fragments []model.Fragment, defaultFragmentIDs []string) []model.Service {
	fragIDByName := make(map[string]string, len(fragments))
	for _, f := range fragments {
		if n := strings.TrimSpace(f.Name); n != "" {
			if _, ok := fragIDByName[n]; !ok {
				fragIDByName[n] = f.ID // 首个同名片段生效
			}
		}
	}

	var out []model.Service
	for _, c := range containers {
		st := parseContainerLabels(c.Labels)
		sites := st.sites
		if len(sites) == 0 {
			continue // 无 caddy 站点:不派生
		}
		for _, s := range sites {
			svc := deriveSite(c, s, st, domains, fragIDByName, defaultFragmentIDs)
			if svc != nil {
				out = append(out, *svc)
			}
		}
	}
	return out
}

// FirstSiteHost 返回 label 集合中首个 caddy 站点的主机名(不含端口),无则空。
// 供容器变量 GB_SUB_DOMAIN 以编排项目的首个站点域名解析(单域名场景最常用)。
func FirstSiteHost(labels map[string]string) string {
	st := parseContainerLabels(labels)
	for _, s := range st.sites {
		if s.spec.host != "" {
			return s.spec.host
		}
	}
	return ""
}

// siteSpec 一条解析后的站点地址(caddy-docker-proxy 风格)。
type siteSpec struct {
	proto  string // https | http
	host   string // host[:port] 剥掉协议后的主机名,不含端口
	port   int    // 显式端口 >0
	custom bool   // 显式指定 :port
}

// siteParsed 一个站点的 label 解析结果。
type siteParsed struct {
	idx     int // caddy=0, caddy_N=N
	spec    siteSpec
	ownRP   string // caddy_N.reverse_proxy 原始值(行级)
	ownFrag string // gatebox.fragments_N 原始值(行级)
}

// derivedState 一个容器的 label 解析结果。
type derivedState struct {
	sites   []siteParsed // 有序站点
	rp0     string       // caddy.reverse_proxy 原始值(服务级共享)
	frag0   string       // gatebox.fragments 原始值(服务级共享)
	upPort  string       // gatebox.upstream_port(服务级逃生舱)
	extras0 []string     // 服务级 caddy.* 子指令(继承到每个站点)
	warns   []string     // 派生告警(温和降级)
}

// parseContainerLabels 把容器的 caddy.*/gatebox.* label 解析为按站点组织的派生输入。
func parseContainerLabels(labels map[string]string) derivedState {
	var st derivedState
	for k, v := range labels {
		switch {
		case k == "caddy":
			// 用 siteByIndex 复用占位(行级覆盖可能先于站点 key 出现),避免重复站点丢覆盖。
			siteByIndex(&st, 0).spec = firstSite(v)
		case siteKeyRE.MatchString(k) && k != "caddy":
			if n, err := strconv.Atoi(strings.TrimPrefix(k, "caddy_")); err == nil {
				siteByIndex(&st, n).spec = firstSite(v)
			}
		case siteRPRE.MatchString(k):
			m := siteRPRE.FindStringSubmatch(k)
			if m[1] == "" {
				st.rp0 = v
			} else if n, err := strconv.Atoi(m[1]); err == nil {
				if s := siteByIndex(&st, n); s != nil {
					s.ownRP = v
				}
			}
		case siteFragRE.MatchString(k):
			m := siteFragRE.FindStringSubmatch(k)
			if m[1] == "" {
				st.frag0 = v
			} else if n, err := strconv.Atoi(m[1]); err == nil {
				if s := siteByIndex(&st, n); s != nil {
					s.ownFrag = v
				}
			}
		case k == LabelReverseProxy:
			// 已被 siteRPRE 捕获(无序号)
		case k == LabelUpstreamPort:
			st.upPort = v
		case strings.HasPrefix(k, "caddy."):
			if lines, wasGlobal := expandDirective(strings.TrimPrefix(k, "caddy."), v); lines != nil {
				st.extras0 = append(st.extras0, lines...)
			} else if wasGlobal {
				st.warns = append(st.warns, fmt.Sprintf("忽略全局级 label %s(全局配置走网关页)", k))
			}
		}
	}
	sort.Slice(st.sites, func(i, j int) bool { return st.sites[i].idx < st.sites[j].idx })
	sort.Strings(st.extras0)
	return st
}

// siteByIndex 按序号定位站点;不存在则新建占位(行级指令先于站点声明出现的场景)。
func siteByIndex(st *derivedState, n int) *siteParsed {
	for i := range st.sites {
		if st.sites[i].idx == n {
			return &st.sites[i]
		}
	}
	st.sites = append(st.sites, siteParsed{idx: n})
	return &st.sites[len(st.sites)-1]
}

// firstSite 取站点地址值的首个站点(caddy-docker-proxy:逗号/空格后多余忽略为主站点)。
func firstSite(v string) siteSpec {
	for _, part := range strings.Split(v, ",") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		if s, ok := parseSiteAddr(fields[0]); ok {
			return s
		}
	}
	return siteSpec{}
}

// deriveSite 派生一个站点的独立 Service;站点无有效 spec 时返回 nil。
func deriveSite(c ProxyableContainer, s siteParsed, st derivedState, domains []model.Domain, fragIDByName map[string]string, defaultFragmentIDs []string) *model.Service {
	// 非 HTTP 站点无 host;HTTP 站点必须有 host。
	if s.spec.host == "" && model.IsHTTPProto(s.spec.proto) {
		return nil
	}
	// 该站点的行级/服务级值
	rp := s.ownRP
	if rp == "" {
		rp = st.rp0
	}
	fragRaw := s.ownFrag
	if fragRaw == "" {
		fragRaw = st.frag0
	}
	extras := append([]string{}, st.extras0...)

	if c.State != "running" {
		// 异常保留:非 running 依旧展示,标记状态、禁用代理。
		svc := derivedService(c, s.spec, domains, nil, "", false, extras, fragRaw, fragIDByName, defaultFragmentIDs)
		svc.Name = derivedName(c, len(st.sites), s.spec.host)
		svc = withWarn(svc, st.warns)
		return &svc
	}

	upstream, upProto, passthrough, ok, warn := resolveSiteUpstream(c, rp, st.upPort, s.idx, s.spec)
	if !ok {
		// 端口无法解析:保留但禁用,便于用户看到原因(网关分组行展示告警)。
		st.warns = append(st.warns, warn)
		svc := derivedService(c, s.spec, domains, nil, "", false, extras, fragRaw, fragIDByName, defaultFragmentIDs)
		svc.Name = derivedName(c, len(st.sites), s.spec.host)
		svc = withWarn(svc, st.warns)
		return &svc
	}
	if passthrough && rp != "" {
		// 非模板 reverse_proxy:该站点透传指令,不生成受控反代。
		extras = append(extras, "reverse_proxy "+strings.TrimSpace(rp))
		sort.Strings(extras)
	}
	var ups []string
	if !passthrough {
		ups = []string{upstream}
	}
	svc := derivedService(c, s.spec, domains, ups, upProto, true, extras, fragRaw, fragIDByName, defaultFragmentIDs)
	svc.Name = derivedName(c, len(st.sites), s.spec.host)
	svc = withWarn(svc, st.warns)
	return &svc
}

// resolveSiteUpstream 按行级/服务级解析一个站点的反代目标(ADR-026 §2/修订:行级覆盖继承)。
// 返回:上游地址、上游协议、是否透传(非模板 reverse_proxy)、是否可代理、告警。
func resolveSiteUpstream(c ProxyableContainer, rp, upPort string, idx int, spec siteSpec) (upstream, proto string, passthrough, ok bool, warn string) {
	if rp != "" {
		proto, internalPort, isTpl := parseUpstreamsTemplate(rp)
		if isTpl {
			hp, found := c.hostForUpstream(internalPort)
			if !found {
				return "", "", false, false, fmt.Sprintf("站点 %s:%s:{{upstreams}} 无法解析宿主映射端口(容器内部端口 %d 未映射或多端口未标注)", spec.host, c.Service, internalPort)
			}
			if proto == "" {
				proto = model.DomainProtoHTTP
			}
			return fmt.Sprintf("127.0.0.1:%d", hp), proto, false, true, ""
		}
		// 非模板:作为透传指令,不生成受控反代(ADR-026 §4)。
		return "", proto, true, true, ""
	}
	if upPort != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(upPort)); err == nil && n > 0 && n <= 65535 {
			return fmt.Sprintf("127.0.0.1:%d", n), model.DomainProtoHTTP, false, true, ""
		}
		return "", "", false, false, fmt.Sprintf("容器 %s:%s 非法,忽略", LabelUpstreamPort, upPort)
	}
	hp, found := c.uniqueHostPort()
	if !found {
		return "", "", false, false, fmt.Sprintf("容器 %s:映射了多个宿主端口且站点 %s 未指定反代目标(用 caddy.reverse_proxy: \"{{upstreams <容器内部端口>}}\" 或 %s: <宿主端口> 指定)", c.Service, spec.host, LabelUpstreamPort)
	}
	return fmt.Sprintf("127.0.0.1:%d", hp), model.DomainProtoHTTP, false, true, ""
}

// parseSiteAddr 解析单个站点地址:[proto://]host[:port]。路径不支持(ADR-026 §1)。
func parseSiteAddr(tok string) (siteSpec, bool) {
	tok = strings.TrimSpace(tok)
	if tok == "" || strings.ContainsAny(tok, " \t") {
		return siteSpec{}, false
	}
	proto := model.DomainProtoHTTPS
	rest := tok
	explicit := false
	if i := strings.Index(tok, "://"); i >= 0 {
		scheme := tok[:i]
		switch scheme {
		case "http":
			proto = model.DomainProtoHTTP
		case "https":
			// 保持 https
		default:
			// 非 http/https scheme → 协议名(如 `mqtt://`);须为小写字母/数字。
			if !protoNameRE.MatchString(scheme) {
				return siteSpec{}, false
			}
			proto = scheme
		}
		rest = tok[i+3:]
		explicit = true
	}
	if rest == "" {
		if explicit && !model.IsHTTPProto(proto) {
			return siteSpec{proto: proto}, true // 如 mqtt://(无 host,端口由端口页驱动)
		}
		return siteSpec{}, false
	}
	if strings.ContainsAny(rest, "/ \t") {
		return siteSpec{}, false
	}
	host := rest
	port := 0
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		if n, err := strconv.Atoi(rest[i+1:]); err == nil && n > 0 && n <= 65535 {
			host = rest[:i]
			port = n
		} else {
			host = rest // 带冒号但非数字端口:整体当 host(如无端口域)
		}
	}
	if host == "" {
		return siteSpec{}, false
	}
	return siteSpec{proto: proto, host: host, port: port, custom: port > 0}, true
}

// parseUpstreamsTemplate 解析 {{upstreams [proto] [port]}}。返回 proto、内部端口(0=自动)、是否模板。
func parseUpstreamsTemplate(v string) (proto string, port int, ok bool) {
	m := upstreamsRegex.FindStringSubmatch(v)
	if m == nil {
		return "", 0, false
	}
	proto = m[1]
	if m[2] != "" {
		if n, err := strconv.Atoi(m[2]); err == nil {
			port = n
		}
	}
	return proto, port, true
}

// hostForUpstream 按内部端口查宿主映射;port=0 时用唯一宿主端口。
func (c ProxyableContainer) hostForUpstream(internalPort int) (int, bool) {
	if internalPort > 0 {
		for _, p := range c.Ports {
			if p.Internal == uint16(internalPort) && p.Host > 0 {
				return int(p.Host), true
			}
		}
		return 0, false
	}
	return c.uniqueHostPort()
}

// uniqueHostPort 返回唯一的宿主映射端口;多个不同宿主端口返回 false。
func (c ProxyableContainer) uniqueHostPort() (int, bool) {
	seen := 0
	for _, p := range c.Ports {
		if p.Host <= 0 {
			continue
		}
		if seen != 0 && seen != int(p.Host) {
			return 0, false
		}
		seen = int(p.Host)
	}
	if seen == 0 {
		seen = c.HostPort
	}
	return seen, seen > 0
}

// derivedService 构造一个 docker 派生 Service 行(单站点)。
// upstream 为空(或 enabled=false)表示仅展示;upstreamProto https 走 tls transport。
func derivedService(c ProxyableContainer, spec siteSpec, domains []model.Domain, upstream []string, upstreamProto string, enabled bool, extras []string, fragRaw string, fragIDByName map[string]string, defaultFragmentIDs []string) model.Service {
	svc := model.Service{
		// 稳定 ID:供前端操作列(停止/启动/重启/日志)与本地启停覆盖引用,不落库。
		// 用 ~ 分隔且不含 /,保证可作为 URL 路径段直接传递。
		ID:              fmt.Sprintf("docker:%s~%s~%s~%d~%s", c.Project, c.Service, spec.host, spec.port, spec.proto),
		AppID:           c.Project,
		Description:     c.Labels[LabelDescription],
		Type:            model.RouteTypeReverseProxy,
		Domains:         []model.ProxyDomain{spec.toDomain(domains)},
		HealthURI:       "/",
		Enabled:         enabled,
		ContainerState:  c.State,
		Upstream:        upstream,
		UpstreamProto:   upstreamProto,
		ExtraDirectives: extras,
		FragmentIDs:     resolveFragmentIDs(fragRaw, fragIDByName, defaultFragmentIDs, upstreamProto),
	}
	return svc
}

// derivedName 派生服务展示名:仅服务名;多站点时追加 · 域名 以区分。
// 归属(compose 展示名)不再拼进名称,改由前端「服务名称」下方以 <icon>/<归属> 呈现。
func derivedName(c ProxyableContainer, siteCount int, host string) string {
	base := c.Service
	if siteCount > 1 && host != "" {
		// 域名去掉通配符 `*`?,保留原样即可
		return base + " · " + host
	}
	return base
}

// resolveFragmentIDs 计算派生站点的片段集合(ADR-026 §3):
// = 默认启用集 ∪ gatebox.fragments[_N] 解析的 ID。
// 忽略证书校验不在此写入:由生成器按 UpstreamProto=https 条件应用(ADR-033 §2)。
func resolveFragmentIDs(fragRaw string, fragIDByName map[string]string, defaults []string, upstreamProto string) []string {
	set := make(map[string]bool, len(defaults)+2)
	for _, id := range defaults {
		set[id] = true
	}
	if fragRaw != "" {
		for _, name := range strings.Split(fragRaw, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if id, ok := fragIDByName[name]; ok {
				set[id] = true
			}
			// 引用缺失:忽略 + 温和降级(ADR-026 §3,不阻断代理)
		}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// toDomain 把站点地址转为网关域名行(ADR-026 §1/§6)。
func (s siteSpec) toDomain(domains []model.Domain) model.ProxyDomain {
	// 非 HTTP:无 host/子域,协议名即端口页协议;监听端口由端口页驱动。
	if !model.IsHTTPProto(s.proto) {
		return model.ProxyDomain{Protocol: s.proto}
	}
	d := model.ProxyDomain{
		Protocol: s.proto,
		Port:     defaultPort(s.proto),
	}
	if s.custom {
		d.CustomPort = true
		d.Port = s.port
	}
	if sub, root, ok := matchRootDomain(s.host, domains); ok {
		d.Subdomain = sub
		d.RootDomain = root
	} else {
		d.Subdomain = s.host
	}
	return d
}

// defaultPort 协议默认端口(https=443 / http=80)。
func defaultPort(proto string) int {
	if proto == model.DomainProtoHTTP {
		return 80
	}
	return 443
}

// matchRootDomain 做最长后缀匹配:host 是某受管根域的域名或子域 → 拆出 Subdomain+RootDomain。
func matchRootDomain(host string, domains []model.Domain) (sub, root string, ok bool) {
	best := ""
	for _, d := range domains {
		name := d.Name
		if name == "" {
			continue
		}
		if host == name || strings.HasSuffix(host, "."+name) {
			if len(name) > len(best) {
				best = name
			}
		}
	}
	if best == "" {
		return "", "", false
	}
	if host == best {
		return "", best, true
	}
	return strings.TrimSuffix(host, "."+best), best, true
}

// expandDirective 把 caddy.X.Y 子指令展开为 site block 指令块(caddy-docker-proxy 风格,
// ADR-026 §4)。返回 nil 表示该键被忽略;second 为 true 时表示命中全局级忽略。
func expandDirective(suffix, value string) (lines []string, wasGlobal bool) {
	segs := strings.Split(suffix, ".")
	if len(segs) == 0 || segs[0] == "" {
		return nil, false
	}
	if globalOptionDirectives[stripOrderPrefix(segs[0])] {
		return nil, true
	}
	indent := 0
	for i, seg := range segs {
		name := stripOrderPrefix(seg)
		if name == "" {
			return nil, false
		}
		if i == len(segs)-1 {
			args := strings.TrimSpace(value)
			if args != "" {
				lines = append(lines, strings.Repeat("\t", indent)+name+" "+args)
			} else {
				lines = append(lines, strings.Repeat("\t", indent)+name)
			}
		} else {
			lines = append(lines, strings.Repeat("\t", indent)+name+" {")
			indent++
		}
	}
	for i := 1; i < len(segs); i++ {
		indent--
		lines = append(lines, strings.Repeat("\t", indent)+"}")
	}
	return lines, false
}

// stripOrderPrefix 剥掉 caddy-docker-proxy 的 N_ 排序/隔离前缀(如 "0_reverse_proxy"→"reverse_proxy")。
func stripOrderPrefix(seg string) string {
	if len(seg) < 3 || seg[0] < '0' || seg[0] > '9' {
		return seg
	}
	for i := 1; i < len(seg); i++ {
		if seg[i] < '0' || seg[i] > '9' {
			if seg[i] == '_' {
				return seg[i+1:]
			}
			break
		}
	}
	return seg
}

// withWarn 把派生告警合并进 DerivedWarning 单行文本(ADR-026)。
func withWarn(svc model.Service, warns []string) model.Service {
	if joined := strings.Join(warns, "; "); joined != "" {
		svc.DerivedWarning = joined
	}
	return svc
}
