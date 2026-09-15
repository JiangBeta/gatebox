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
		"output file /data/logs/caddy/access.log",
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
	if !strings.Contains(got, "encode zstd gzip") {
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
	appNames := map[string]string{"app1": "demo"}
	d := model.ProxyDomain{Protocol: model.DomainProtoHTTPS, Subdomain: "jellyfin", RootDomain: "neob.cn"}
	varEnv := map[string]string{"<%BACKEND%>": "10.0.0.9"}
	const staticRoot = "/srv/www"
	const dataDir = "/data"

	cases := []struct {
		in, want string
	}{
		{"<%GB_APP%>", "demo"},
		{"<%GB_SERVICE%>", "jellyfin"},
		{"<%GB_HOST_PORT%>", "8096"},
		{"<%GB_DATA_DIR%>/foo", "/data/foo"},
		{"<%GB_SUB_DOMAIN%>", "jellyfin.neob.cn"},
		{"<%GB_ACME_FILE%>/certs", "/data/tools/acme/certs"},
		{"<%GB_SSL_FILE%>", "/data/tools/acme/certs/jellyfin.neob.cn/fullchain.pem"},
		{"<%BACKEND%>", "10.0.0.9"},
		{`{$STATIC_WEB_DIR}`, `{$STATIC_WEB_DIR}`}, // caddy env 透传
		{"<%GB_STATIC_ROOT%>/index.html", "/srv/www/index.html"},
		{"<%GB_LOG_FILE%>", "/data/logs/caddy/demo_jellyfin.log"},
	}
	for _, c := range cases {
		got, err := interpolateVars(c.in, svc, appNames, d, varEnv, staticRoot, dataDir)
		if err != nil {
			t.Fatalf("interpolate(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("interpolate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := interpolateVars("..<%BAD_VAR%>..", svc, appNames, d, varEnv, staticRoot, dataDir); err == nil {
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

// TestGenerateReverseProxyFragmentMerge: reverse_proxy 包裹片段合并进受控反代块;
// https 上游条件应用 skip-verify,且不再注入默认 tls transport(片段优先)。
func TestGenerateReverseProxyFragmentMerge(t *testing.T) {
	builtins := BuiltinFragmentCatalog()
	services := []model.Service{
		{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "a.neob.cn"}},
			Upstream: []string{"10.0.0.1:443"}, UpstreamProto: "https",
			Enabled:     true,
			FragmentIDs: []string{model.FragmentWebsocket},
		},
	}
	got, err := Generate(services, nil, nil, nil, builtins, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Count(got, "reverse_proxy 10.0.0.1:443 {") != 1 {
		t.Errorf("应只有一个受控反代块:\n%s", got)
	}
	for _, want := range []string{"flush_interval -1", "tls_insecure_skip_verify"} {
		if !strings.Contains(got, want) {
			t.Errorf("合并块缺少 %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\ttransport http {\n\t\ttls\n\t}") {
		t.Errorf("片段已提供 transport,生成器不应再注入默认 tls:\n%s", got)
	}
}

// TestGenerateSkipVerifyConditional: 非 https 上游时 skip-verify 即使被显式引用也应被丢弃。
func TestGenerateSkipVerifyConditional(t *testing.T) {
	builtins := BuiltinFragmentCatalog()
	services := []model.Service{
		{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "a.neob.cn"}},
			Upstream: []string{"127.0.0.1:8080"}, UpstreamProto: "http",
			Enabled:     true,
			FragmentIDs: []string{model.FragmentSkipVerify},
		},
	}
	got, err := Generate(services, nil, nil, nil, builtins, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Contains(got, "tls_insecure_skip_verify") || strings.Contains(got, "transport http") {
		t.Errorf("http 上游不应出现跳过校验/transport:\n%s", got)
	}
}

// TestGenerateFragmentErrors: 带参数 reverse_proxy 片段 / 混合顶层与 reverse_proxy 均报错。
func TestGenerateFragmentErrors(t *testing.T) {
	svc := func() []model.Service {
		return []model.Service{{
			ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "a.neob.cn"}},
			Upstream: []string{"127.0.0.1:8080"}, Enabled: true,
			FragmentIDs: []string{"f1"},
		}}
	}
	bad := []model.Fragment{{ID: "f1", Code: "reverse_proxy 1.2.3.4:80 {\n\tflush_interval -1\n}"}}
	if _, err := Generate(svc(), nil, bad, nil, nil, testDataDir, nil); err == nil {
		t.Error("带参数 reverse_proxy 片段应报错")
	}
	mixed := []model.Fragment{{ID: "f1", Code: "encode zstd gzip\nreverse_proxy {\n\tflush_interval -1\n}"}}
	if _, err := Generate(svc(), nil, mixed, nil, nil, testDataDir, nil); err == nil {
		t.Error("混合顶层与 reverse_proxy 的片段应报错")
	}
}

// TestGenerateHandleNestedReverseProxyOk: handle 块内部的 reverse_proxy 不算「混用」,按顶层片段写入。
func TestGenerateHandleNestedReverseProxyOk(t *testing.T) {
	frags := []model.Fragment{{ID: "f1", Code: "handle /api/* {\n\treverse_proxy 10.0.0.9:9000\n}"}}
	services := []model.Service{{
		ID: "s1", Name: "app", Type: model.RouteTypeReverseProxy,
		Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "a.neob.cn"}},
		Upstream: []string{"127.0.0.1:8080"}, Enabled: true, FragmentIDs: []string{"f1"},
	}}
	got, err := Generate(services, nil, frags, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("handle 内嵌 reverse_proxy 应被接受: %v", err)
	}
	if !strings.Contains(got, "handle /api/* {") {
		t.Errorf("缺少 handle 片段:\n%s", got)
	}
}

// TestGenerateStaticServiceIgnoresRPFragment: 静态服务上的 reverse_proxy 包裹片段静默跳过。
func TestGenerateStaticServiceIgnoresRPFragment(t *testing.T) {
	builtins := BuiltinFragmentCatalog()
	services := []model.Service{
		{
			ID: "s1", Name: "files", Type: model.RouteTypeFileServer,
			Domains: []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "files.neob.cn"}},
			Root:    "/srv/www", Enabled: true,
			FragmentIDs: []string{model.FragmentWebsocket},
		},
	}
	got, err := Generate(services, nil, nil, nil, builtins, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Contains(got, "flush_interval") || strings.Contains(got, "reverse_proxy") {
		t.Errorf("静态服务不应生成 reverse_proxy 片段:\n%s", got)
	}
}

// TestGenerateStaticRootVar: <%GB_STATIC_ROOT%> 由 GenerateOptions.StaticRoot 注入。
func TestGenerateStaticRootVar(t *testing.T) {
	apps := []model.App{{ID: "app1", Name: "demo"}}
	services := []model.Service{
		{
			ID: "s1", AppID: "app1", Name: "files", Type: model.RouteTypeFileServer,
			Domains: []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "files.neob.cn"}},
			Root:    "<%GB_STATIC_ROOT%>/<%GB_APP%>/", Enabled: true,
		},
	}
	got, err := Generate(services, apps, nil, nil, nil, testDataDir, nil, GenerateOptions{StaticRoot: "/data/www"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "root * /data/www/demo/") {
		t.Errorf("静态根变量未注入:\n%s", got)
	}
}

// TestGeneratePerServiceLog: 内置「按服务日志」片段落到 <dataDir>/logs/caddy/<app>_<svc>.log。
func TestGeneratePerServiceLog(t *testing.T) {
	builtins := BuiltinFragmentCatalog()
	services := []model.Service{
		{
			ID: "s1", Name: "jellyfin", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "a.neob.cn"}},
			Upstream: []string{"127.0.0.1:8096"}, Enabled: true,
			FragmentIDs: []string{model.FragmentLogConfig},
		},
	}
	got, err := Generate(services, nil, nil, nil, builtins, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "output file /data/logs/caddy/default_jellyfin.log") {
		t.Errorf("按服务日志路径错误:\n%s", got)
	}
}

// TestGenerateGlobalSnippets:扩展 renderer 产出的片段原样插入全局块,非 HTTP 协议不生成 site block。
func TestGenerateGlobalSnippets(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "mqtt", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: "mqtt"}},
			Upstream: []string{"127.0.0.1:1883"}, Enabled: true,
		},
	}
	snippet := "layer4 {\n\ttcp/:1883 {\n\t\troute {\n\t\t\tproxy tcp/127.0.0.1:1883\n\t\t}\n\t}\n}"
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil,
		GenerateOptions{GlobalSnippets: []string{snippet}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, c := range []string{"\tlayer4 {", "\ttcp/:1883 {", "\t\tproxy tcp/127.0.0.1:1883"} {
		if !strings.Contains(got, c) {
			t.Errorf("缺少 %q:\n%s", c, got)
		}
	}
	if strings.Contains(got, "mqtt {") {
		t.Errorf("非 HTTP 协议不应生成 HTTP site block:\n%s", got)
	}
}

// TestGenerateGlobalSnippetsMixed:HTTP 域名行仍生成 site,非 HTTP 域名行交给扩展片段。
func TestGenerateGlobalSnippetsMixed(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "mix", Type: model.RouteTypeReverseProxy,
			Domains: []model.ProxyDomain{
				{Protocol: model.DomainProtoHTTPS, Subdomain: "app", RootDomain: "neob.cn"},
				{Protocol: "mqtt"},
			},
			Upstream: []string{"127.0.0.1:8080"}, Enabled: true,
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil,
		GenerateOptions{GlobalSnippets: []string{"layer4 {\n\ttcp/:1883 {\n\t\troute {\n\t\t\tproxy tcp/127.0.0.1:8080\n\t\t}\n\t}\n}"}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "app.neob.cn {") {
		t.Errorf("HTTP 域名行应生成 site block:\n%s", got)
	}
	if !strings.Contains(got, "tcp/:1883 {") {
		t.Errorf("非 HTTP 域名行应交给全局片段:\n%s", got)
	}
}

// TestSystemVariables:系统变量视图——物理路径解析,服务上下文保留占位。
func TestSystemVariables(t *testing.T) {
	vs := SystemVariables("/home/u/data", "/home/u/data/www")
	want := map[string]string{
		"GB_APP":         "<%GB_APP%>",
		"GB_SERVICE":     "<%GB_SERVICE%>",
		"GB_HOST_PORT":   "<%GB_HOST_PORT%>",
		"GB_SUB_DOMAIN":  "<%GB_SUB_DOMAIN%>",
		"GB_DATA_DIR":    "/home/u/data",
		"GB_ACME_FILE":   "/home/u/data/tools/acme",
		"GB_SSL_FILE":    "<%GB_ACME_FILE%>/certs/<%GB_SUB_DOMAIN%>/fullchain.pem",
		"GB_STATIC_ROOT": "<%GB_DATA_DIR%>/www",
		"GB_LOG_FILE":    "<%GB_DATA_DIR%>/logs/caddy/<%GB_APP%>_<%GB_SERVICE%>.log",
	}
	if len(vs) != len(want) {
		t.Fatalf("系统变量数量 = %d, want %d", len(vs), len(want))
	}
	for _, v := range vs {
		if want[v.Key] != v.Value {
			t.Errorf("%s = %q, want %q", v.Key, v.Value, want[v.Key])
		}
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

// （原 TestGenerateL4Both 已迁至 plugin 包：layer4 渲染现由扩展 renderer 负责。）
