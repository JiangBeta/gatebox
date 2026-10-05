// Package caddyrender 把 V4.1 对象渲染成最小 Caddyfile（ADR-043 §4）。
//
// 明确的完成度边界（ADR-043 §4 原话）：本轮只覆盖
//
//	reverse_proxy 单上游（取自 route.backend）+ middlewares 列表 + tls + entrypoint
//
// 的基本形态。复杂中间件（如 rewrite.body 的嵌套对象）留 TODO，渲染不出来时
// 返回带上下文的错误 → 上层把对象 status.state 写成「错误」。
// 换句话说：本轮保存后「可能编译失败」是真实用例，不是 bug。
//
// 不复用 internal/adapter/caddy 的旧片段生成器：那套围绕 model.Service +
// Fragment 模板，属于旧数据模型；V4.1 的对象是 schema 驱动的，形状不同。
package caddyrender

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JiangBeta/gatebox/internal/objects"
)

// Options 渲染选项。
type Options struct {
	// DataDir 数据目录，决定 access log 落点。
	DataDir string
	// HTTPPort / HTTPSPort 非 0 时写进全局块（与同机 80/443 占用者共存，ADR-001）。
	HTTPPort  int
	HTTPSPort int
	// GlobalSnippets 扩展注册表产出的全局片段（能力型插件接入点，ADR-036）。
	GlobalSnippets []string
}

// Input 渲染输入：一次 reconcile 拿到的对象快照。
type Input struct {
	Routes []objects.Object
	// Services 路由引用的服务对象：决定上游指令的形态（file_server 没有上游）。
	Services    []objects.Object
	Middlewares []objects.Object
	Entrypoints []objects.Object
	Domains     []objects.Object
	Users       []objects.Object
}

// Result 渲染结果。
type Result struct {
	Caddyfile string
	// SiteAddrs 渲染出的站点地址（调试 / diff 用）。
	SiteAddrs []string
	// Skipped 未渲染的路由及原因（停用 / 依赖未装）。
	Skipped []Skip
}

// Skip 一条被跳过的路由。
type Skip struct {
	RouteID string `json:"routeId"`
	Reason  string `json:"reason"`
}

// Resolver 渲染器需要的引用解析能力（由调用方注入，避免包间循环依赖）。
type Resolver interface {
	// PasswordHash 返回 user 对象的 bcrypt 哈希（basic_auth 渲染用）。
	PasswordHash(userID string) (string, error)
}

// Render 渲染完整 Caddyfile。
func Render(in Input, opt Options, hashes Resolver) (Result, error) {
	mwIndex := indexByID(in.Middlewares)
	svcIndex := indexByID(in.Services)
	epIndex := indexByID(in.Entrypoints)
	domIndex := indexByID(in.Domains)
	userIndex := indexByID(in.Users)

	var b strings.Builder
	writeGlobalBlock(&b, opt)

	var res Result
	// 站点地址 → 占用它的路由 id（检测同址重复）。
	siteOwner := map[string]string{}

	// 先按稳定顺序遍历路由，保证输出确定。
	routes := append([]objects.Object(nil), in.Routes...)
	objects.SortObjects(routes)

	for _, rt := range routes {
		if !rt.Bool("enabled") {
			res.Skipped = append(res.Skipped, Skip{RouteID: rt.ID, Reason: "已停用"})
			continue
		}
		epID := rt.String("entrypoint")
		ep, ok := epIndex[epID]
		if !ok {
			return Result{}, fmt.Errorf("路由 %s 的入口点 %q 不存在", rt.ID, epID)
		}
		if !protocolInstalled(ep.String("protocol")) {
			return Result{}, fmt.Errorf("路由 %s 依赖协议 %q，其组件未安装（该协议在类型目录里 installed=false）",
				rt.ID, ep.String("protocol"))
		}

		lines, err := routeLines(rt, mwIndex, svcIndex, domIndex, userIndex, hashes)
		if err != nil {
			return Result{}, fmt.Errorf("路由 %s: %w", rt.ID, err)
		}
		addr, err := siteAddr(rt, ep, domIndex)
		if err != nil {
			return Result{}, fmt.Errorf("路由 %s: %w", rt.ID, err)
		}
		// 同址重复 = 配置错误（一条主机名+入口点只能有一条路由）。
		// 不在这里拦的话，caddy validate 只会报
		//「ambiguous site definition: http://x:8081」，用户完全不知道是哪两条路由撞了。
		if prev, dup := siteOwner[addr]; dup {
			return Result{}, fmt.Errorf("路由 %s 与 %s 抢同一个站点地址 %s"+
				"（同一域名 + 同一入口点只能有一条路由；停用其中一条，或换一个入口点/子域名）",
				prev, rt.ID, addr)
		}
		siteOwner[addr] = rt.ID
		res.SiteAddrs = append(res.SiteAddrs, addr)
		b.WriteString(addr + " {\n")
		for _, ln := range lines {
			if ln == "" {
				b.WriteString("\n")
				continue
			}
			b.WriteString("\t" + ln + "\n")
		}
		b.WriteString("}\n\n")
	}

	res.Caddyfile = b.String()
	return res, nil
}

func indexByID(list []objects.Object) map[string]objects.Object {
	m := make(map[string]objects.Object, len(list))
	for _, o := range list {
		m[o.ID] = o
	}
	return m
}

// writeGlobalBlock 全局选项块。
func writeGlobalBlock(b *strings.Builder, opt Options) {
	b.WriteString("{\n")
	if opt.HTTPPort > 0 {
		fmt.Fprintf(b, "\thttp_port %d\n", opt.HTTPPort)
	}
	if opt.HTTPSPort > 0 {
		fmt.Fprintf(b, "\thttps_port %d\n", opt.HTTPSPort)
	}
	if opt.DataDir != "" {
		accessLog := filepath.Join(opt.DataDir, "logs", "caddy", "access.log")
		fmt.Fprintf(b, "\tlog {\n\t\toutput file %s {\n\t\t\troll_size 100mb\n\t\t\troll_keep 5\n\t\t}\n\t\tformat json\n\t}\n", accessLog)
	}
	for _, snip := range opt.GlobalSnippets {
		writeIndented(b, snip, "\t")
	}
	b.WriteString("}\n\n")
}

func writeIndented(b *strings.Builder, snippet, indent string) {
	snippet = strings.TrimRight(snippet, "\n")
	if snippet == "" {
		return
	}
	for _, ln := range strings.Split(snippet, "\n") {
		if ln == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent + ln + "\n")
	}
}

// siteAddr 由 subdomain + 根域名 + 入口协议/端口拼出 Caddy 站点地址。
func siteAddr(rt objects.Object, ep objects.Object, domIndex map[string]objects.Object) (string, error) {
	proto := ep.String("protocol")
	host := rt.String("subdomain")

	roots := rt.Strings("roots")
	if len(roots) == 0 {
		// 无根域名：L4 / 固定响应等场景，允许裸主机名或空。
		if host == "" {
			// 没有域名也没有主机名 → 只能监听端口（如 :8080）。
			if port := routePort(rt, ep); port > 0 {
				return ":" + strconv.Itoa(port), nil
			}
			return "", fmt.Errorf("既无域名也无端口，无法确定监听地址")
		}
		return host, nil
	}
	if host == "" {
		d, ok := domIndex[roots[0]]
		if !ok {
			return "", fmt.Errorf("根域名 %q 不存在", roots[0])
		}
		host = d.Key
	} else {
		d, ok := domIndex[roots[0]]
		if !ok {
			return "", fmt.Errorf("根域名 %q 不存在", roots[0])
		}
		host = host + "." + d.Key
	}

	// http 入口点显式写 http://，避免 caddy 把它当 https。
	if proto == "http" {
		if port := routePort(rt, ep); port > 0 && !isDefaultPort(proto, port) {
			return fmt.Sprintf("http://%s:%d", host, port), nil
		}
		return "http://" + host, nil
	}
	if port := routePort(rt, ep); port > 0 && !isDefaultPort(proto, port) {
		return fmt.Sprintf("%s:%d", host, port), nil
	}
	return host, nil
}

// routePort 取路由端口：route.port 覆盖，否则取入口点主端口。
func routePort(rt, ep objects.Object) int {
	if n, ok := rt.Port("port"); ok && n > 0 {
		return n
	}
	for _, p := range ep.Numbers("ports") {
		n := int(p)
		if n > 0 {
			return n
		}
	}
	return 0
}

func isDefaultPort(proto string, port int) bool {
	switch proto {
	case "http":
		return port == 80
	case "https":
		return port == 443
	}
	return false
}

// routeLines 渲染一个路由的指令行。
func routeLines(rt objects.Object, mwIndex, svcIndex, domIndex, userIndex map[string]objects.Object, res Resolver) ([]string, error) {
	var lines []string

	for _, id := range rt.Strings("middlewares") {
		mw, ok := mwIndex[id]
		if !ok {
			return nil, fmt.Errorf("中间件 %q 不存在", id)
		}
		ml, err := middlewareLines(mw, userIndex, res)
		if err != nil {
			return nil, err
		}
		lines = append(lines, ml...)
	}

	// tls
	switch rt.String("tls") {
	case "off":
		// Caddy 在裸 host（非 http://）默认走 TLS；关 TLS 要显式写。
		lines = append(lines, "tls internal")
	case "auto", "":
		// 站点地址不带 scheme 时 caddy 自动签发，毋须再写。
	default:
		lines = append(lines, "tls "+rt.String("tls"))
	}

	up, err := upstreamLines(rt, svcIndex)
	if err != nil {
		return nil, err
	}
	lines = append(lines, up...)
	return lines, nil
}

// upstreamLines 产出路由的响应指令。
//
// 路由自持后端（ADR-043 §3），所以默认形态只用 route.backend 的 reverse_proxy；
// 但 file_server / respond / redirect 这类服务类型根本没有上游，
// 必须按 **服务类型** 分派，否则会把静态站点渲染成 reverse_proxy 127.0.0.1:1
// （这是迁移器历史行为，已在 migration/v41.go 记为告警）。
// 类型目录里已列出但渲染未实现的类型一律返回错误：上层把 status.state 写成
// 「错误」，而不是悄悄渲染出一个错的站点（ADR-043 §4 的完成度边界）。
func upstreamLines(rt objects.Object, svcIndex map[string]objects.Object) ([]string, error) {
	svcType := ""
	if id := rt.String("service"); id != "" {
		svc, ok := svcIndex[id]
		if !ok {
			return nil, fmt.Errorf("服务 %q 不存在", id)
		}
		svcType = svc.String("type")
	}

	switch svcType {
	case "", "reverse_proxy":
		host := rt.String("backend.host")
		port, hasPort := rt.Port("backend.port")
		if host == "" || !hasPort {
			// 校验层已保证必填；到这里说明数据被绕过 API 直改了。
			return []string{fmt.Sprintf("respond %q 500", "backend 缺失")}, nil
		}
		return []string{"reverse_proxy " + host + ":" + strconv.Itoa(port)}, nil

	case "file_server":
		svc := svcIndex[rt.String("service")]
		root := svc.String("root")
		if root == "" {
			return nil, errors.New("file_server 服务缺 root")
		}
		lines := []string{"root " + root}
		if svc.Bool("browse") {
			lines = append(lines, "file_server browse")
		}
		return lines, nil

	case "respond":
		svc := svcIndex[rt.String("service")]
		body := svc.String("body")
		if body == "" {
			return nil, errors.New("respond 服务缺 body")
		}
		if status, ok := svc.Number("status"); ok && status != 0 && status != 200 {
			return []string{fmt.Sprintf("respond /%d %q", int(status), body)}, nil
		}
		// 200 是 caddy 的默认状态码，不必显式写。
		return []string{fmt.Sprintf("respond %q", body)}, nil

	case "redirect":
		to := svcIndex[rt.String("service")].String("to")
		if to == "" {
			return nil, errors.New("redirect 服务缺 to")
		}
		return []string{"redir " + to}, nil

	default:
		return nil, fmt.Errorf("服务类型 %q 渲染未实现（类型目录已列出，需渲染器支持）", svcType)
	}
}

// middlewareLines 渲染一个中间件的指令行。
func middlewareLines(mw objects.Object, userIndex map[string]objects.Object, res Resolver) ([]string, error) {
	switch mw.String("type") {
	case "websocket":
		// Caddy 的 reverse_proxy 原生透传 WebSocket 升级，无需指令。
		return nil, nil

	case "encode":
		formats := mw.Strings("formats")
		if len(formats) == 0 {
			return nil, nil // 未选格式 = 不启用
		}
		out := []string{"encode " + strings.Join(formats, " ")}
		if n, ok := mw.Port("minLength"); ok {
			out = append(out, "minimum_length "+strconv.Itoa(n))
		}
		return out, nil

	case "headers":
		set := mw.Strings("set")
		del := mw.Strings("delete")
		if len(set) == 0 && len(del) == 0 {
			return nil, nil
		}
		out := []string{"header {"}
		for _, kv := range set {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return nil, fmt.Errorf("中间件 %s 的响应头 %q 缺少 '='（应形如 X-Frame-Options=DENY）", mw.ID, kv)
			}
			out = append(out, "\t"+strings.TrimSpace(k)+" "+strings.TrimSpace(v))
		}
		for _, k := range del {
			out = append(out, "\t"+strings.TrimSpace(k)+"-")
		}
		out = append(out, "}")
		return out, nil

	case "basic_auth":
		users := mw.Strings("users")
		if len(users) == 0 {
			return nil, fmt.Errorf("中间件 %s（basic_auth）没有勾选用户", mw.ID)
		}
		out := []string{"basic_auth {"}
		for _, uid := range users {
			u, ok := userIndex[uid]
			if !ok {
				return nil, fmt.Errorf("中间件 %s 引用了不存在的用户 %q", mw.ID, uid)
			}
			if res == nil {
				return nil, fmt.Errorf("未配置密码解析器，无法渲染 basic_auth")
			}
			hash, err := res.PasswordHash(uid)
			if err != nil {
				return nil, fmt.Errorf("用户 %s（%s）: %w", uid, u.Key, err)
			}
			out = append(out, "\t"+u.Key+" "+hash)
		}
		out = append(out, "}")
		return out, nil

	case "rewrite":
		var out []string
		if uri := strings.TrimSpace(mw.String("uri")); uri != "" {
			out = append(out, "rewrite "+uri)
		}
		if _, ok := mw.Get("body"); ok {
			return nil, fmt.Errorf("中间件 %s 的 rewrite.body 尚未支持（ADR-043 §4 边界）", mw.ID)
		}
		return out, nil

	case "code":
		code := strings.TrimRight(mw.String("code"), "\n")
		if strings.TrimSpace(code) == "" {
			return nil, nil
		}
		return strings.Split(code, "\n"), nil

	default:
		// 类型目录里新增的类型：不是 bug，是「本轮渲染器还不认识」。
		return nil, fmt.Errorf("中间件 %s 的类型 %q 尚无渲染实现（ADR-043 §4 只覆盖内置最小集）", mw.ID, mw.String("type"))
	}
}

// protocolInstalled 查类型目录里该入口协议是否已装。
func protocolInstalled(protocol string) bool {
	return builtinProtocols[protocol]
}

// builtinProtocols 已装协议（与 catalog.yaml entrypoint_protocols 的 installed=true 对齐）。
var builtinProtocols = map[string]bool{
	"http":      true,
	"https":     true,
	"grpc":      true,
	"websocket": true,
}

// TODO(ADR-043 §4): 以下能力本轮不覆盖，落地时逐条销账
//   - rewrite.body（响应体改写）
//   - encode.minimum_length 之外的压缩调优
//   - 多上游 / upstreamProto=https 的 tls_insecure_skip_verify
//   - l4_proxy / file_server / redirect / respond 服务类型（类型目录已列，渲染未实现）
//   - 蓝图 revision / diff / rollback（批次 7）
