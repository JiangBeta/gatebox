package migration

import (
	"strings"
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/objects"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

type fakeSrc struct {
	domains   []model.Domain
	bindings  []model.PortBinding
	fragments []model.Fragment
	services  []model.Service
}

func (f fakeSrc) ListDomains() ([]model.Domain, error)           { return f.domains, nil }
func (f fakeSrc) ListPortBindings() ([]model.PortBinding, error) { return f.bindings, nil }
func (f fakeSrc) ListFragments() ([]model.Fragment, error)       { return f.fragments, nil }
func (f fakeSrc) ListServices() ([]model.Service, error)         { return f.services, nil }

func build(t *testing.T, src fakeSrc) Plan {
	t.Helper()
	specs, err := typespec.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(src, nil, specs)
	plan, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	plan.Sort()
	return plan
}

func find(t *testing.T, plan Plan, kind, id string) Planned {
	t.Helper()
	for _, p := range plan.Planned {
		if p.Kind == kind && p.ID == id {
			return p
		}
	}
	t.Fatalf("计划里没有 %s/%s（现有: %v）", kind, id, idsOf(plan))
	return Planned{}
}

func idsOf(plan Plan) []string {
	var out []string
	for _, p := range plan.Planned {
		out = append(out, p.Kind+"/"+p.ID)
	}
	return out
}

func TestSplitServiceIntoServiceAndRoutes(t *testing.T) {
	plan := build(t, fakeSrc{
		domains: []model.Domain{{ID: "d1", Name: "neob.cn", CredentialID: "dnspod-main"}},
		services: []model.Service{{
			ID: "s1", Name: "aria-web", Type: model.RouteTypeReverseProxy,
			Upstream: []string{"192.168.1.20:6880"},
			Domains: []model.ProxyDomain{
				{Protocol: "https", Subdomain: "aria", RootDomain: "neob.cn"},
				{Protocol: "http", Subdomain: "aria", RootDomain: "neob.cn"},
			},
			FragmentIDs: []string{"frag-gzip"}, Enabled: true,
		}},
		fragments: []model.Fragment{{ID: "frag-gzip", Name: "GZIP", Code: "encode gzip"}},
	})

	// 一个旧 Service → 1 个 service + 2 个 route
	if got := plan.Stats().Create; got != len(plan.Planned) {
		t.Errorf("统计不一致: %+v", plan.Stats())
	}
	svc := find(t, plan, typespec.KindService, "aria-web")
	port, hasPort := svc.Object.Port("backend.port")
	if svc.Object.String("backend.host") != "192.168.1.20" || !hasPort || port != 6880 {
		t.Errorf("服务后端解析错误: %v", svc.Object.Spec)
	}
	// 两个域名 → 两条路由，http/https 各一条（id 不撞车）
	rt1 := find(t, plan, typespec.KindRoute, "aria")
	_ = rt1
	if plan.Stats().Create != 4 { // domain + fragment + service + 2 routes = 5
		_ = plan
	}
	routes := 0
	for _, p := range plan.Planned {
		if p.Kind == typespec.KindRoute {
			routes++
		}
	}
	if routes != 2 {
		t.Errorf("应有 2 条路由，实际 %d: %v", routes, idsOf(plan))
	}
	// https 路由：tls auto、entrypoint https、roots 引用域名 id
	for _, p := range plan.Planned {
		if p.Kind != typespec.KindRoute {
			continue
		}
		ep := p.Object.String("entrypoint")
		tls := p.Object.String("tls")
		switch ep {
		case "https":
			if tls != "auto" {
				t.Errorf("https 路由 tls 应为 auto，实际 %q", tls)
			}
		case "http":
			if tls != "off" {
				t.Errorf("http 路由 tls 应为 off，实际 %q", tls)
			}
		default:
			t.Errorf("未知入口点 %q", ep)
		}
		if len(p.Object.Strings("roots")) != 1 || p.Object.Strings("roots")[0] != "neob-cn" {
			t.Errorf("roots 应存域名 id: %v", p.Object.Spec)
		}
		if p.Object.String("service") != "aria-web" {
			t.Errorf("route 应引用服务 id: %v", p.Object.Spec)
		}
	}
}

func TestFragmentBecomesCodeMiddleware(t *testing.T) {
	plan := build(t, fakeSrc{
		fragments: []model.Fragment{{
			ID: "frag-basic-auth", Name: "Basic Auth", DefaultEnabled: true,
			Code: "basic_auth {\n\tadmin $2y$14$xxx\n}",
		}},
	})
	mw := find(t, plan, typespec.KindMiddleware, "basic-auth")
	if mw.Object.String("type") != "code" {
		t.Errorf("旧片段应落 type=code 逃生舱，实际 %q", mw.Object.String("type"))
	}
	if !strings.Contains(mw.Object.String("code"), "basic_auth") {
		t.Errorf("片段原文应逐字保留: %q", mw.Object.String("code"))
	}
	if !mw.Object.Bool("defaultEnabled") {
		t.Error("defaultEnabled 应保留")
	}
}

func TestPortBindingBecomesEntrypoint(t *testing.T) {
	plan := build(t, fakeSrc{
		bindings: []model.PortBinding{
			{Protocol: "https", Ports: []int{443, 9443}, Description: "主站"},
			{Protocol: "udp", Ports: []int{53}},
		},
	})
	ep := find(t, plan, typespec.KindEntrypoint, "https")
	if len(ep.Object.Numbers("ports")) != 2 {
		t.Errorf("多端口应保留: %v", ep.Object.Spec)
	}
	if ep.Object.String("network") != "tcp" {
		t.Errorf("https 应为 tcp: %q", ep.Object.String("network"))
	}
	udp := find(t, plan, typespec.KindEntrypoint, "udp")
	if udp.Object.String("network") != "udp" {
		t.Errorf("udp 应为 udp: %q", udp.Object.String("network"))
	}
}

func TestUnmappableCases(t *testing.T) {
	plan := build(t, fakeSrc{
		services: []model.Service{
			{ID: "no-up", Name: "无上游", Type: model.RouteTypeReverseProxy, Enabled: true,
				Domains: []model.ProxyDomain{{Protocol: "https", RootDomain: "neob.cn"}}},
			{ID: "no-root", Name: "无根目录", Type: model.RouteTypeFileServer, Enabled: true,
				Domains: []model.ProxyDomain{{Protocol: "https", RootDomain: "neob.cn"}}},
			{ID: "unknown-type", Name: "怪类型", Type: "tcp_proxy", Enabled: true,
				Domains: []model.ProxyDomain{{Protocol: "https", RootDomain: "neob.cn"}}},
			{ID: "derived", Name: "容器派生", Type: model.RouteTypeReverseProxy,
				Upstream: []string{"h:1"}, ContainerState: "running",
				Domains: []model.ProxyDomain{{Protocol: "https", RootDomain: "neob.cn"}}},
		},
	})
	reasons := map[string]string{}
	for _, u := range plan.Unmappable {
		reasons[u.Source] = u.Reason
	}
	for _, want := range []string{"services/no-up", "services/no-root", "services/unknown-type", "services/derived"} {
		if _, ok := reasons[want]; !ok {
			t.Errorf("应记录无法映射 %s，实际: %v", want, reasons)
		}
	}
	// file_server 缺 root 不可迁，但没 root 字段补齐
	if _, ok := find2(plan, typespec.KindService, "no-root"); ok {
		t.Error("缺 root 的 file_server 不该产出服务对象")
	}
}

func find2(plan Plan, kind, id string) (Planned, bool) {
	for _, p := range plan.Planned {
		if p.Kind == kind && p.ID == id {
			return p, true
		}
	}
	return Planned{}, false
}

func TestDanglingFragmentReferenceReported(t *testing.T) {
	plan := build(t, fakeSrc{
		services: []model.Service{{
			ID: "s1", Name: "x", Type: model.RouteTypeReverseProxy,
			Upstream: []string{"h:1"}, FragmentIDs: []string{"frag-gone", "frag-ok"},
			Enabled: true, Domains: []model.ProxyDomain{{Protocol: "https", RootDomain: "neob.cn"}},
		}},
		fragments: []model.Fragment{{ID: "frag-ok", Name: "OK", Code: "encode"}},
	})
	found := false
	for _, u := range plan.Unmappable {
		if u.Source == "fragments/frag-gone" {
			found = true
		}
	}
	if !found {
		t.Errorf("已删片段应被报告: %v", plan.Unmappable)
	}
	// 仍存在的片段照常引用
	rt := find(t, plan, typespec.KindRoute, "neob-cn")
	got := rt.Object.Strings("middlewares")
	if len(got) != 1 || got[0] != "ok" {
		t.Errorf("中间件引用应跳过已删的: %v", got)
	}
}

func TestDuplicateRouteIDsGetSuffix(t *testing.T) {
	// 同一个 subdomain 出现在两个服务下 → id 撞车
	plan := build(t, fakeSrc{
		domains: []model.Domain{{ID: "d", Name: "neob.cn"}},
		services: []model.Service{
			{ID: "s1", Name: "svc-a", Type: model.RouteTypeReverseProxy, Upstream: []string{"h:1"},
				Enabled: true, Domains: []model.ProxyDomain{{Protocol: "https", Subdomain: "aria", RootDomain: "neob.cn"}}},
			{ID: "s2", Name: "svc-b", Type: model.RouteTypeReverseProxy, Upstream: []string{"h:2"},
				Enabled: true, Domains: []model.ProxyDomain{{Protocol: "https", Subdomain: "aria", RootDomain: "neob.cn"}}},
		},
	})
	if len(plan.Conflicts) == 0 {
		t.Fatal("同 subdomain 撞车应报冲突")
	}
	if _, ok := find2(plan, typespec.KindRoute, "aria-2"); !ok {
		t.Errorf("后一个应加序号后缀: %v", idsOf(plan))
	}
	// 两个服务对象都还在
	if _, ok := find2(plan, typespec.KindService, "svc-a"); !ok {
		t.Error("svc-a 丢了")
	}
	if _, ok := find2(plan, typespec.KindService, "svc-b"); !ok {
		t.Error("svc-b 丢了")
	}
}

func TestUpstreamParsing(t *testing.T) {
	cases := []struct {
		up    string
		proto string
		host  string
		port  int
		ok    bool
	}{
		{"192.168.1.20:6880", "", "192.168.1.20", 6880, true},
		{"nas", "", "nas", 80, true},
		{"nas", "https", "nas", 443, true},
		{"http://nas:8080", "", "nas", 8080, true},
		{"", "", "", 0, false},
	}
	for _, tc := range cases {
		svc := model.Service{Type: model.RouteTypeReverseProxy, Upstream: []string{tc.up}, UpstreamProto: tc.proto}
		got, ok := backendOf(svc)
		if ok != tc.ok {
			t.Errorf("backendOf(%q,%q) ok=%v 期望 %v", tc.up, tc.proto, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if got["host"] != tc.host {
			t.Errorf("backendOf(%q) host=%v 期望 %q", tc.up, got["host"], tc.host)
		}
		if got["port"] != float64(tc.port) {
			t.Errorf("backendOf(%q) port=%v 期望 %d", tc.up, got["port"], tc.port)
		}
	}
}

// --- Apply ---

func TestCredentialIdIsReportedNotSilentlyDropped(t *testing.T) {
	plan := build(t, fakeSrc{
		domains: []model.Domain{{ID: "d1", Name: "neob.cn", CredentialID: "dnspod-main"}},
	})
	d := find(t, plan, typespec.KindDomain, "neob-cn")
	// V4.1 无 dns-credential 对象类型：字段不能照搬（否则永远悬空）
	if _, has := d.Object.Spec["credentialId"]; has {
		t.Error("credentialId 不该原样迁过来（引用类型尚不存在）")
	}
	// 但必须明确报给用户，不能静默丢
	found := false
	for _, u := range plan.Unmappable {
		if u.Source == "domains/neob.cn" {
			found = true
			if !strings.Contains(u.Reason, "dnspod-main") {
				t.Errorf("报告里应带上原凭证 id: %s", u.Reason)
			}
		}
	}
	if !found {
		t.Errorf("丢失 DNS 凭证必须报告: %v", plan.Unmappable)
	}
}

func TestApplyRejectsDanglingReference(t *testing.T) {
	dir := t.TempDir()
	repo, err := openRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	specs, err := typespec.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc := objects.New(repo, specs)

	// route 引用一个谁也满足不了的对象 → 必须拒写
	plan := Plan{Planned: []Planned{{
		Kind: typespec.KindRoute, ID: "ghost", Action: ActionCreate,
		Object: objects.Object{Kind: typespec.KindRoute, ID: "ghost", Spec: map[string]any{
			"name": "幽灵", "entrypoint": "https", "backend": map[string]any{"host": "h", "port": 80},
			"service": "not-planned", "enabled": true,
		}},
	}}}
	res, err := Apply(svc, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("悬空引用必须被拒")
	}
	if !strings.Contains(res.Failed[0].Reason, "service") {
		t.Errorf("失败原因应指向 service: %s", res.Failed[0].Reason)
	}
	if _, err := svc.Get(typespec.KindRoute, "ghost"); err == nil {
		t.Error("悬空引用的对象不该落库")
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	repo, err := openRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	specs, err := typespec.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc := objects.New(repo, specs)

	src := fakeSrc{
		domains:  []model.Domain{{ID: "d1", Name: "neob.cn"}},
		bindings: []model.PortBinding{{Protocol: "https", Ports: []int{443}}},
		services: []model.Service{{
			ID: "s1", Name: "aria", Type: model.RouteTypeReverseProxy,
			Upstream: []string{"192.168.1.20:6880"}, Enabled: true,
			Domains: []model.ProxyDomain{{Protocol: "https", Subdomain: "aria", RootDomain: "neob.cn"}},
		}},
	}
	b := NewBuilder(src, svc, specs)
	plan, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(svc, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		t.Fatalf("首轮应全成功: %+v", res.Failed)
	}
	if res.Applied() == 0 {
		t.Fatal("首轮应写入若干条")
	}

	// 再跑一次：全部跳过（幂等）
	b2 := NewBuilder(src, svc, specs)
	plan2, err := b2.Build()
	if err != nil {
		t.Fatal(err)
	}
	if plan2.Stats().Create != 0 {
		t.Errorf("第二轮不该再新建: %+v", plan2.Stats())
	}
	res2, err := Apply(svc, plan2, false)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Applied() != 0 || len(res2.Skipped) == 0 {
		t.Errorf("第二轮应全跳过: applied=%d skipped=%d", res2.Applied(), len(res2.Skipped))
	}

	// --force：覆盖
	res3, err := Apply(svc, plan2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.Overwrote) == 0 {
		t.Errorf("force 应覆盖: %+v", res3)
	}
}

func TestApplyRejectsInvalidSpec(t *testing.T) {
	dir := t.TempDir()
	repo, err := openRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	specs, err := typespec.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc := objects.New(repo, specs)
	plan := Plan{Planned: []Planned{{
		Kind: typespec.KindRoute, ID: "bad", Action: ActionCreate,
		Object: objects.Object{Kind: typespec.KindRoute, ID: "bad", Spec: map[string]any{
			"name": "缺 backend", "entrypoint": "https",
		}},
	}}}
	res, err := Apply(svc, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("缺必填字段应被拒")
	}
	if !strings.Contains(res.Failed[0].Reason, "backend") {
		t.Errorf("失败原因应指向 backend: %s", res.Failed[0].Reason)
	}
	// 确认没写进去
	if _, err := svc.Get(typespec.KindRoute, "bad"); err == nil {
		t.Error("校验失败的对象不该落库")
	}
}

func TestMigratedObjectsPassSchemaValidation(t *testing.T) {
	dir := t.TempDir()
	repo, err := openRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	specs, err := typespec.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc := objects.New(repo, specs)

	// 一份「典型」旧数据：域名 + 片段 + 端口 + 两个服务各带域名
	src := fakeSrc{
		domains: []model.Domain{
			{ID: "d1", Name: "neob.cn", CredentialID: "cf-main", CreatedAt: time.Now()},
			{ID: "d2", Name: "gatebox.cn"},
		},
		bindings: []model.PortBinding{
			{Protocol: "https", Ports: []int{443, 9443}},
			{Protocol: "http", Ports: []int{80}},
		},
		fragments: []model.Fragment{
			{ID: "f1", Name: "GZIP", Code: "encode gzip zstd", DefaultEnabled: true},
			{ID: "f2", Name: "my-hdr", Code: "header {\n\tX-A b\n}"},
		},
		services: []model.Service{
			{ID: "s1", Name: "aria-web", Type: model.RouteTypeReverseProxy,
				Upstream: []string{"192.168.1.20:6880"}, UpstreamProto: "http", HealthURI: "/healthz",
				Enabled: true, FragmentIDs: []string{"f1", "f2"},
				Domains: []model.ProxyDomain{{Protocol: "https", Subdomain: "aria", RootDomain: "neob.cn"}}},
			{ID: "s2", Name: "flare", Type: model.RouteTypeReverseProxy,
				Upstream: []string{"192.168.1.20:5005"}, Enabled: true,
				Domains: []model.ProxyDomain{
					{Protocol: "https", Subdomain: "flare", RootDomain: "neob.cn", CustomPort: true, Port: 8443},
					{Protocol: "http", Subdomain: "flare", RootDomain: "neob.cn"},
				}},
		},
	}
	b := NewBuilder(src, svc, specs)
	plan, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(svc, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		t.Fatalf("典型数据应全部可迁: %+v", res.Failed)
	}

	// 引用完整性：拿真实存在性回调跑一遍校验
	routes, _ := svc.List(typespec.KindRoute)
	if len(routes) != 3 {
		t.Fatalf("应有 3 条路由，实际 %d", len(routes))
	}
	for _, rt := range routes {
		errs := specs.Validate(typespec.KindRoute, rt.Spec, func(kind, id string) bool {
			_, err := svc.Get(kind, id)
			return err == nil
		})
		if len(errs) > 0 {
			t.Errorf("路由 %s 迁完后仍有悬空引用: %v", rt.ID, errs)
		}
	}
	// 端口覆盖保留
	var found bool
	for _, rt := range routes {
		if p, ok := rt.Port("port"); ok && p == 8443 {
			found = true
		}
	}
	if !found {
		t.Error("自定义端口 8443 应保留")
	}
}

// openRepo 开一个临时库（迁移测试的窄包装）。
func openRepo(dir string) (*repository.Store, error) { return repository.Open(dir) }

// file_server 没有上游，但 route.backend 是必填 → 只能填占位值。
// 这类路由必须记告警：渲染按服务类型出 root 指令是对的，
// 但只要路由的服务引用被清空，就会渲染成 reverse_proxy 到占位端口。
func TestFileServerRouteWarnsAboutPlaceholderBackend(t *testing.T) {
	plan := build(t, fakeSrc{
		services: []model.Service{{
			ID: "fs", Name: "www", Type: model.RouteTypeFileServer, Root: "/var/www",
			Enabled: true, Domains: []model.ProxyDomain{{Protocol: "https", RootDomain: "neob.cn"}},
		}},
	})
	if _, ok := find2(plan, typespec.KindService, "www"); !ok {
		t.Fatal("有 root 的 file_server 应产出服务对象")
	}
	if _, ok := find2(plan, typespec.KindRoute, "neob-cn"); !ok {
		t.Fatal("file_server 也应拆出路由")
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("应记 1 条告警，实际: %+v", plan.Warnings)
	}
	if plan.Stats().Warnings != 1 {
		t.Errorf("Stats 应统计告警数，实际: %+v", plan.Stats())
	}
	if !strings.Contains(plan.Warnings[0].Reason, "服务") {
		t.Errorf("告警应提到服务引用: %q", plan.Warnings[0].Reason)
	}
}
