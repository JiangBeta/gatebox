package caddy

import (
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/model"
)

const testDataDir = "/data"

func TestGenerateReverseProxy(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "jellyfin", Type: model.RouteTypeReverseProxy,
			Domains:   []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "jellyfin", RootDomain: "neob.cn"}},
			Upstream:  []string{"127.0.0.1:8096"},
			HealthURI: "/health", Enabled: true,
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	checks := []string{
		"output file /data/tools/caddy/logs/access.log",
		"format json",
		"jellyfin.neob.cn {",
		"reverse_proxy 127.0.0.1:8096 {",
		"health_uri /health",
		"import /data/tools/caddy/user/*",
	}
	for _, c := range checks {
		if !strings.Contains(got, c) {
			t.Errorf("缺少 %q:\n%s", c, got)
		}
	}
	if strings.Contains(got, "gatebox-default") {
		t.Errorf("无默认 handler 时不应有 gatebox-default:\n%s", got)
	}
}

func TestGenerateFileServer(t *testing.T) {
	services := []model.Service{
		{
			ID: "s2", Name: "static", Type: model.RouteTypeFileServer,
			Domains: []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "files", RootDomain: "neob.cn"}},
			Root:    "/srv/www/files", Enabled: true,
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "files.neob.cn {") {
		t.Errorf("缺少 site block:\n%s", got)
	}
	if !strings.Contains(got, "root * /srv/www/files") {
		t.Errorf("缺少 root:\n%s", got)
	}
	if !strings.Contains(got, "file_server") {
		t.Errorf("缺少 file_server:\n%s", got)
	}
	if strings.Contains(got, "reverse_proxy") {
		t.Errorf("静态文件不应有 reverse_proxy:\n%s", got)
	}
}

func TestGenerateSkipsDisabled(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "on", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "on.neob.cn"}},
			Upstream: []string{"127.0.0.1:1"}, Enabled: true,
		},
		{
			ID: "s2", Name: "off", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "off.neob.cn"}},
			Upstream: []string{"127.0.0.1:2"}, Enabled: false,
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "on.neob.cn {") {
		t.Errorf("启用服务应生成:\n%s", got)
	}
	if strings.Contains(got, "off.neob.cn") {
		t.Errorf("停用服务不应生成:\n%s", got)
	}
}

func TestGenerateMultiDomainAndProtocol(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains: []model.ProxyDomain{
				{Protocol: model.DomainProtoHTTPS, Subdomain: "www", RootDomain: "neob.cn"},
				{Protocol: model.DomainProtoHTTP, Subdomain: "lan", RootDomain: "neob.cn"},
			},
			Upstream: []string{"127.0.0.1:80"}, Enabled: true,
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "www.neob.cn {") {
		t.Errorf("缺少 https 域名行:\n%s", got)
	}
	if !strings.Contains(got, "http://lan.neob.cn {") {
		t.Errorf("缺少 http 域名行(应带 http:// 前缀):\n%s", got)
	}
}

func TestGenerateMultiUpstream(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains: []model.ProxyDomain{
				{Protocol: model.DomainProtoHTTPS, Subdomain: "a", RootDomain: "neob.cn"},
			},
			Upstream:  []string{"127.0.0.1:8081", "127.0.0.1:8082", "127.0.0.1:8083"},
			HealthURI: "/health", Enabled: true,
		},
		{
			ID: "s2", Name: "single", Type: model.RouteTypeReverseProxy,
			Domains: []model.ProxyDomain{
				{Protocol: model.DomainProtoHTTPS, Subdomain: "b", RootDomain: "neob.cn"},
			},
			Upstream: []string{"127.0.0.1:9000"}, Enabled: true, HealthURI: "/health",
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "reverse_proxy 127.0.0.1:8081 127.0.0.1:8082 127.0.0.1:8083 {") {
		t.Errorf("多上游应合并写为块形式:\n%s", got)
	}
	if !strings.Contains(got, "health_uri /health") {
		t.Errorf("多上游块中应保留 health_uri:\n%s", got)
	}
}

func TestGenerateCustomPort(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains: []model.ProxyDomain{
				{Protocol: model.DomainProtoHTTPS, Subdomain: "a", RootDomain: "neob.cn", CustomPort: true, Port: 8443},
			},
			Upstream: []string{"127.0.0.1:80"}, Enabled: true,
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "a.neob.cn:8443 {") {
		t.Errorf("缺少自定义端口 site 地址:\n%s", got)
	}
}

func TestGenerateFragmentApply(t *testing.T) {
	fragments := []model.Fragment{
		{ID: "frag-gzip", Name: "Gzip", Code: "encode gzip zstd"},
	}
	services := []model.Service{
		{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "a.neob.cn"}},
			Upstream: []string{"127.0.0.1:80"}, Enabled: true, FragmentIDs: []string{"frag-gzip"},
		},
	}
	got, err := Generate(services, nil, fragments, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "encode gzip zstd") {
		t.Errorf("缺少片段内容:\n%s", got)
	}
}

func TestGenerateFragmentExcludeAndBuiltin(t *testing.T) {
	builtins := BuiltinFragmentCatalog()
	services := []model.Service{
		{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "a.neob.cn"}},
			Upstream: []string{"127.0.0.1:80"}, Enabled: true,
			// 默认启用内置(如 frag-block-common)由调用方 seed;此处显示引用 + 排除
			FragmentIDs:        []string{model.FragmentBlockCommon, model.FragmentGzip},
			ExcludeFragmentIDs: []string{model.FragmentBlockCommon},
		},
	}
	got, err := Generate(services, nil, nil, nil, builtins, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "encode gzip zstd") {
		t.Errorf("未被排除的片段应生成:\n%s", got)
	}
	if strings.Contains(got, "X-Content-Type-Options") {
		t.Errorf("被排除的片段不应生成:\n%s", got)
	}
}

func TestGenerateVariableAndUnresolvedError(t *testing.T) {
	fragments := []model.Fragment{
		{ID: "f1", Code: "header {\n\tX-Custom <%MY_VAR%>\n}"},
	}
	variables := []model.Variable{{Key: "MY_VAR", Value: "hello"}}
	services := []model.Service{
		{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "a.neob.cn"}},
			Upstream: []string{"127.0.0.1:80"}, Enabled: true, FragmentIDs: []string{"f1"},
		},
	}
	got, err := Generate(services, nil, fragments, variables, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate with var: %v", err)
	}
	if !strings.Contains(got, "X-Custom hello") {
		t.Errorf("变量未替换:\n%s", got)
	}

	// 未命中变量应报错
	services[0].FragmentIDs = []string{"f1"}
	bad := []model.Fragment{{ID: "f1", Code: "header {\n\tX <%NOPE%>\n}"}}
	if _, err := Generate(services, nil, bad, nil, nil, testDataDir, nil); err == nil {
		t.Fatal("引用未定义变量应报错")
	}
}

func TestInterpolateVars(t *testing.T) {
	svc := model.Service{AppID: "app1", Name: "jellyfin", Type: model.RouteTypeReverseProxy, Upstream: []string{"127.0.0.1:8096"}}
	appNames := map[string]string{"app1": "媒体"}
	d := model.ProxyDomain{Protocol: model.DomainProtoHTTPS, Subdomain: "jellyfin", RootDomain: "neob.cn"}
	varEnv := map[string]string{"<%BACKEND%>": "10.0.0.9", "<%GB_STATIC_ROOT%>": "/srv/www"}

	cases := []struct {
		in, want string
	}{
		{"<%GB_APP%>", "媒体"},
		{"<%GB_SERVICE%>", "jellyfin"},
		{"<%GB_HOST_PORT%>", "8096"},
		{"<%BACKEND%>", "10.0.0.9"},
		{`${STATIC_WEB_DIR}`, `${STATIC_WEB_DIR}`}, // caddy env 透传
		{"<%GB_STATIC_ROOT%>/index.html", "/srv/www/index.html"},
	}
	for _, c := range cases {
		got, err := interpolateVars(c.in, svc, appNames, d, varEnv)
		if err != nil {
			t.Fatalf("interpolate(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("interpolate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := interpolateVars("..<%BAD_VAR%>..", svc, appNames, d, varEnv); err == nil {
		t.Fatal("未定义变量应报错")
	}
}

func TestGenerateExtraDirectivesPassthrough(t *testing.T) {
	// docker 派生 Escape(ADR-026 §5):片段 → 透传指令 → 受控反代;无 upstream 时受控段跳过。
	services := []model.Service{
		{
			ID: "s1", Name: "derived", Type: model.RouteTypeReverseProxy,
			Domains: []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "a", RootDomain: "neob.cn"}},
			Enabled: true,
			// caddy.reverse_proxy 非模板透传:受控反代不生成
			ExtraDirectives: []string{"encode zstd gzip", "reverse_proxy localhost:8081"},
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "encode zstd gzip") {
		t.Errorf("缺少透传指令:\n%s", got)
	}
	if !strings.Contains(got, "reverse_proxy localhost:8081") {
		t.Errorf("缺少透传 reverse_proxy:\n%s", got)
	}
	if strings.Contains(got, "reverse_proxy\n") || strings.Count(got, "reverse_proxy") != 1 {
		t.Errorf("无 upstream 时不应另生成受控反代:\n%s", got)
	}
}

func TestDefaultEnabledFragmentIDs(t *testing.T) {
	builtins := BuiltinFragmentCatalog()
	ids := DefaultEnabledFragmentIDs(nil, builtins)
	// frag-block-common 是唯一 DefaultEnabled=true 的内置
	want := true
	for _, b := range builtins {
		if b.DefaultEnabled && b.ID != model.FragmentBlockCommon {
			want = false
		}
	}
	_ = want
	found := false
	for _, id := range ids {
		if id == model.FragmentBlockCommon {
			found = true
		}
	}
	if !found {
		t.Errorf("默认启用应含 frag-block-common, got %v", ids)
	}
}
