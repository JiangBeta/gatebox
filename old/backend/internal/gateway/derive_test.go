package gateway

import (
	"testing"

	"github.com/JiangBeta/gatebox/internal/models"
)

func TestDeriveRoutes(t *testing.T) {
	containers := []ProxyableContainer{
		{
			Project: "jellyfin", Service: "web", DisplayName: "Jellyfin", State: "running",
			Ports:    []ProxyablePort{{Internal: 8096, Host: 8097}},
			HostPort: 8097,
			Labels: map[string]string{
				"caddy":               "jellyfin.neob.cn",
				"gatebox.description": "家庭影音",
			},
		},
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	if len(routes) != 1 {
		t.Fatalf("route 数 = %d, want 1", len(routes))
	}
	r := routes[0]
	if len(r.Domains) != 1 || r.Domains[0].Host() != "jellyfin.neob.cn" {
		t.Errorf("domains = %+v", r.Domains)
	}
	if len(r.Upstream) != 1 || r.Upstream[0] != "127.0.0.1:8097" {
		t.Errorf("upstream = %v", r.Upstream)
	}
	if r.Name != "Jellyfin/web" {
		t.Errorf("name = %q, want Jellyfin/web", r.Name)
	}
	if r.Description != "家庭影音" {
		t.Errorf("description = %q", r.Description)
	}
	if r.Type != models.RouteTypeReverseProxy {
		t.Errorf("type = %q", r.Type)
	}
	if r.HealthURI != "/" {
		t.Errorf("healthUri = %q, want /", r.HealthURI)
	}
	if r.ID != "" {
		t.Errorf("派生 service 不应有 ID: %q", r.ID)
	}
}

func TestDeriveRoutesMultipleDomains(t *testing.T) {
	// 每个站点派生独立 Service(ADR-026 修订):同容器多域名 → 各自的 Service。
	containers := []ProxyableContainer{
		{
			Project: "jellyfin", Service: "web", State: "running",
			Ports: []ProxyablePort{{Internal: 8096, Host: 8096}},
			Labels: map[string]string{
				"caddy":   "jellyfin.neob.cn",
				"caddy_1": "files.neob.cn",
			},
		},
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	if len(routes) != 2 {
		t.Fatalf("service 数 = %d, want 2(每站点独立)", len(routes))
	}
	if len(routes[0].Domains) != 1 || len(routes[1].Domains) != 1 {
		t.Fatalf("每个派生 service 应只有单个域名行: %+v", routes)
	}
	h0, h1 := routes[0].Domains[0].Host(), routes[1].Domains[0].Host()
	if h0 != "jellyfin.neob.cn" || h1 != "files.neob.cn" {
		t.Errorf("domain 顺序错误: %q, %q", h0, h1)
	}
	if routes[0].Name == routes[1].Name {
		t.Errorf("多站点派生 service 名应区分域名: %q", routes[0].Name)
	}
}

func TestDeriveRoutesRowLevelOverride(t *testing.T) {
	// 行级覆盖继承(ADR-026 修订):caddy_1.reverse_proxy 覆盖共享模板;gatebox.fragments_1 覆盖共享片段。
	containers := []ProxyableContainer{
		{
			Project: "svc", Service: "app", State: "running",
			Ports: []ProxyablePort{
				{Internal: 3000, Host: 9000},
				{Internal: 5000, Host: 9001},
				{Internal: 9000, Host: 9002},
			},
			Labels: map[string]string{
				"caddy":                 "api.neob.cn",
				"caddy_1":               "web.neob.cn",
				"caddy_1.reverse_proxy": "{{upstreams 5000}}",
				"caddy.reverse_proxy":   "{{upstreams 9000}}",
				"gatebox.fragments":     "共享甲",
				"gatebox.fragments_1":   "展示乙",
			},
		},
	}
	fragments := []models.Fragment{
		{ID: "fa", Name: "共享甲"},
		{ID: "fb", Name: "展示乙"},
	}
	routes := DeriveRoutes(containers, nil, fragments, nil)
	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(routes))
	}
	// 站点0:caddy.reverse_proxy → {{upstreams 9000}} → 宿主 9002
	a := routes[0]
	if a.Domains[0].Host() != "api.neob.cn" || len(a.Upstream) != 1 || a.Upstream[0] != "127.0.0.1:9002" {
		t.Errorf("站点0 upstream = %v, want 127.0.0.1:9002", a.Upstream)
	}
	if !hasID(a.FragmentIDs, "fa") || hasID(a.FragmentIDs, "fb") {
		t.Errorf("站点0 片段应继承共享甲而非展示乙: %v", a.FragmentIDs)
	}
	// 站点1:caddy_1.reverse_proxy → {{upstreams 5000}} → 宿主 9001
	b := routes[1]
	if b.Domains[0].Host() != "web.neob.cn" || len(b.Upstream) != 1 || b.Upstream[0] != "127.0.0.1:9001" {
		t.Errorf("站点1 upstream = %v, want 127.0.0.1:9001", b.Upstream)
	}
	if !hasID(b.FragmentIDs, "fb") || hasID(b.FragmentIDs, "fa") {
		t.Errorf("站点1 片段应覆盖为展示乙而非共享甲: %v", b.FragmentIDs)
	}
	// 站点1 https? 站点0 应为 http({{upstreams 9000}} 无 https 前缀)
	if a.UpstreamProto == "https" || b.UpstreamProto != "http" {
		t.Errorf("upstream proto 错误: a=%q b=%q", a.UpstreamProto, b.UpstreamProto)
	}
}

func TestDeriveRoutesCommaFirstSite(t *testing.T) {
	// 单 label 逗号多站点:caddy-docker-proxy 兼容语义,取首个作主站点;其余用 caddy_N。
	containers := []ProxyableContainer{
		{
			Project: "demo", Service: "web", State: "running",
			Ports:  []ProxyablePort{{Internal: 80, Host: 18080}},
			Labels: map[string]string{"caddy": "a.neob.cn, b.neob.cn"},
		},
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	if len(routes) != 1 || routes[0].Domains[0].Host() != "a.neob.cn" {
		t.Errorf("逗号多个站点应取首个为主站点: %+v", routes)
	}
}

func TestDeriveRoutesSkips(t *testing.T) {
	containers := []ProxyableContainer{
		{State: "running", Labels: map[string]string{"caddy": "a.neob.cn"}},                                            // 未映射端口 → 保留禁用(可代理失败但展示原因)
		{State: "running", Ports: []ProxyablePort{{Internal: 80, Host: 8080}}, Labels: map[string]string{}},            // 无 caddy label
		{State: "running", Ports: []ProxyablePort{{Internal: 80, Host: 8080}}, Labels: map[string]string{"caddy": ""}}, // 空 domain
		{State: "running", Labels: map[string]string{}},                                                                // 无端口无 label
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1(无 label/空域跳过,无端口保留禁用)", len(routes))
	}
	if routes[0].Enabled {
		t.Error("未映射端口且带 caddy 的容器应保留禁用(Enabled=false)")
	}
}

// TestDeriveRoutesAbnormalRetention 异常保留:非 running 的带 label 容器保留展示
// 但禁用(ErrorRetention,gateway.md §9.3),不生成 site block、无 upstream。
func TestDeriveRoutesAbnormalRetention(t *testing.T) {
	containers := []ProxyableContainer{
		{
			Project: "aria2", Service: "web", DisplayName: "Aria2", State: "exited",
			Labels: map[string]string{"caddy": "aria2.neob.cn"},
		},
		{State: "exited", Labels: map[string]string{}}, // 无 caddy label:不展示
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	if len(routes) != 1 {
		t.Fatalf("service 数 = %d, want 1(仅保留带 label 的异常容器)", len(routes))
	}
	r := routes[0]
	if r.Enabled {
		t.Error("异常容器派生 service 应 Enabled=false,不生成 site block")
	}
	if len(r.Upstream) != 0 {
		t.Errorf("异常容器不应有 upstream, got %v", r.Upstream)
	}
	if r.ContainerState != "exited" {
		t.Errorf("containerState = %q, want exited", r.ContainerState)
	}
	if len(r.Domains) != 1 || r.Domains[0].Host() != "aria2.neob.cn" {
		t.Errorf("domains = %+v", r.Domains)
	}
	if r.AppID != "aria2" {
		t.Errorf("appId = %q, want aria2(项目归属)", r.AppID)
	}
}

func TestDeriveRoutesSiteAddr(t *testing.T) {
	containers := []ProxyableContainer{
		{
			Project: "demo", Service: "web", State: "running",
			Ports:  []ProxyablePort{{Internal: 80, Host: 18080}},
			Labels: map[string]string{"caddy": "http://demo.neob.cn:8080"},
		},
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	r := routes[0]
	if len(r.Domains) != 1 {
		t.Fatalf("domains = %d", len(r.Domains))
	}
	d := r.Domains[0]
	if d.Protocol != models.DomainProtoHTTP || d.CustomPort != true || d.Port != 8080 || d.Subdomain != "demo.neob.cn" {
		t.Errorf("domains[0] = %+v, want http/8080 custom", d)
	}
}

func TestDeriveRoutesRootDomainMatch(t *testing.T) {
	domains := []models.Domain{{ID: "1", Name: "neob.cn"}, {ID: "2", Name: "sub.neob.cn"}}
	containers := []ProxyableContainer{
		{
			Project: "demo", Service: "web", State: "running",
			Ports:  []ProxyablePort{{Internal: 80, Host: 18080}},
			Labels: map[string]string{"caddy": "jellyfin.sub.neob.cn"},
		},
	}
	routes := DeriveRoutes(containers, domains, nil, nil)
	d := routes[0].Domains[0]
	if d.RootDomain != "sub.neob.cn" || d.Subdomain != "jellyfin" {
		t.Errorf("匹配到最长根域: %+v, want sub.neob.cn/jellyfin", d)
	}
}

func TestDeriveRoutesUpstreamPriority(t *testing.T) {
	// 一级:{{upstreams <容器内部端口>}} 反向查宿主映射
	containers := []ProxyableContainer{
		{
			Project: "demo", Service: "a", State: "running",
			Ports:  []ProxyablePort{{Internal: 3000, Host: 9000}, {Internal: 5000, Host: 9001}},
			Labels: map[string]string{"caddy": "a.neob.cn", "caddy.reverse_proxy": "{{upstreams 5000}}"},
		},
		// 一级 + https
		{
			Project: "demo", Service: "b", State: "running",
			Ports:  []ProxyablePort{{Internal: 443, Host: 8443}},
			Labels: map[string]string{"caddy": "b.neob.cn", "caddy.reverse_proxy": "{{upstreams https 443}}"},
		},
		// 二级:gatebox.upstream_port
		{
			Project: "demo", Service: "c", State: "running",
			Ports:  []ProxyablePort{{Internal: 80, Host: 18080}, {Internal: 443, Host: 18443}},
			Labels: map[string]string{"caddy": "c.neob.cn", "gatebox.upstream_port": "18443"},
		},
		// 三级:唯一宿主端口
		{
			Project: "demo", Service: "d", State: "running",
			Ports:  []ProxyablePort{{Internal: 80, Host: 18080}},
			Labels: map[string]string{"caddy": "d.neob.cn"},
		},
		// 多宿主端口未标注:不可代理(保留展示)
		{
			Project: "demo", Service: "e", State: "running",
			Ports:  []ProxyablePort{{Internal: 80, Host: 18080}, {Internal: 90, Host: 18090}},
			Labels: map[string]string{"caddy": "e.neob.cn"},
		},
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	wantUp := map[string]string{
		"demo/a": "127.0.0.1:9001",
		"demo/b": "127.0.0.1:8443",
		"demo/c": "127.0.0.1:18443",
		"demo/d": "127.0.0.1:18080",
	}
	for _, r := range routes {
		if u, ok := wantUp[r.Name]; ok {
			if len(r.Upstream) != 1 || r.Upstream[0] != u {
				t.Errorf("%s upstream = %v, want %s", r.Name, r.Upstream, u)
			}
			if r.Name == "demo/b" && r.UpstreamProto != "https" {
				t.Errorf("demo/b upstreamProto = %q, want https", r.UpstreamProto)
			}
		}
		if r.Name == "demo/e" {
			if r.Enabled || len(r.Upstream) != 0 {
				t.Errorf("demo/e 应禁用(多端口未标注): enabled=%v upstream=%v", r.Enabled, r.Upstream)
			}
		}
	}
}

func TestDeriveRoutesPassthrough(t *testing.T) {
	containers := []ProxyableContainer{
		{
			Project: "demo", Service: "web", State: "running",
			Ports:  []ProxyablePort{{Internal: 80, Host: 18080}},
			Labels: map[string]string{"caddy": "w.neob.cn", "caddy.reverse_proxy": "localhost:8081"},
		},
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	r := routes[0]
	if !r.Enabled {
		t.Error("透传 reverse_proxy 应可代理(Enabled=true)")
	}
	if len(r.Upstream) != 0 {
		t.Errorf("透传场景不应有受控 upstream: %v", r.Upstream)
	}
	found := false
	for _, ln := range r.ExtraDirectives {
		if ln == "reverse_proxy localhost:8081" {
			found = true
		}
	}
	if !found {
		t.Errorf("ExtraDirectives 应含透传 reverse_proxy: %v", r.ExtraDirectives)
	}
}

func TestDeriveRoutesExtrasAndGlobals(t *testing.T) {
	containers := []ProxyableContainer{
		{
			Project: "demo", Service: "web", State: "running",
			Ports: []ProxyablePort{{Internal: 80, Host: 18080}},
			Labels: map[string]string{
				"caddy":                        "w.neob.cn",
				"caddy.encode":                 "zstd gzip",
				"caddy.handle.0_reverse_proxy": "127.0.0.1:8096",
				"caddy.email":                  "admin@neob.cn", // 全局级:忽略
			},
		},
	}
	routes := DeriveRoutes(containers, nil, nil, nil)
	r := routes[0]
	joined := ""
	for _, ln := range r.ExtraDirectives {
		joined += ln + "\n"
	}
	if !contains(joined, "encode zstd gzip") {
		t.Errorf("缺少透传 encode 指令:\n%s", joined)
	}
	if !contains(joined, "handle {\n") || !contains(joined, "\treverse_proxy 127.0.0.1:8096\n") {
		t.Errorf("handle 嵌套展开错误:\n%s", joined)
	}
	if contains(joined, "email") {
		t.Errorf("全局级 caddy.email 应被忽略:\n%s", joined)
	}
}

func TestDeriveRoutesFragments(t *testing.T) {
	fragments := []models.Fragment{
		{ID: "f1", Name: "Basic Auth"},
		{ID: "f2", Name: "日志"},
	}
	defaults := []string{"frag-block-common"}
	containers := []ProxyableContainer{
		{
			Project: "demo", Service: "web", State: "running",
			Ports:  []ProxyablePort{{Internal: 80, Host: 18080}},
			Labels: map[string]string{"caddy": "w.neob.cn", "gatebox.fragments": "Basic Auth, 日志, 不存在"},
		},
		{
			Project: "demo", Service: "tls", State: "running",
			Ports:  []ProxyablePort{{Internal: 443, Host: 8443}},
			Labels: map[string]string{"caddy": "t.neob.cn", "caddy.reverse_proxy": "{{upstreams https 443}}"},
		},
	}
	routes := DeriveRoutes(containers, nil, fragments, defaults)
	ids := routes[0].FragmentIDs
	// 默认集 + 显式引用(按名解析),缺失的「不存在」忽略
	if !hasID(ids, "frag-block-common") || !hasID(ids, "f1") || !hasID(ids, "f2") {
		t.Errorf("fragmentIDs = %v, want 默认集+f1+f2", ids)
	}
	if !hasID(routes[1].FragmentIDs, models.FragmentSkipVerify) {
		t.Errorf("https 上游应自动补 frag-skip-verify: %v", routes[1].FragmentIDs)
	}
}

func TestParseSiteAddr(t *testing.T) {
	cases := []struct {
		in      string
		host    string
		proto   string
		port    int
		custom  bool
		wantErr bool
	}{
		{"jellyfin.neob.cn", "jellyfin.neob.cn", "https", 0, false, false},
		{"http://jellyfin.neob.cn", "jellyfin.neob.cn", "http", 0, false, false},
		{"example.com:8443", "example.com", "https", 8443, true, false},
		{"http://example.com:8080", "example.com", "http", 8080, true, false},
		{"*.neob.cn", "*.neob.cn", "https", 0, false, false},
		{"example.com/api", "", "", 0, false, true}, // 路径不支持
	}
	for _, tc := range cases {
		s, ok := parseSiteAddr(tc.in)
		if tc.wantErr {
			if ok {
				t.Errorf("parseSiteAddr(%q) 应失败", tc.in)
			}
			continue
		}
		if !ok {
			t.Errorf("parseSiteAddr(%q) 失败", tc.in)
			continue
		}
		if s.host != tc.host || s.proto != tc.proto || s.port != tc.port || s.custom != tc.custom {
			t.Errorf("parseSiteAddr(%q) = %+v, want host=%s proto=%s port=%d custom=%v", tc.in, s, tc.host, tc.proto, tc.port, tc.custom)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func hasID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
