package caddy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/model"
)

// findCaddy 定位 caddy 二进制:env GATEBOX_CADDY_BIN > PATH > 跳过。
func findCaddy(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GATEBOX_CADDY_BIN"); p != "" {
		return p
	}
	if p, err := exec.LookPath("caddy"); err == nil {
		return p
	}
	t.Skip("caddy 未安装,跳过 validate 集成测试")
	return ""
}

// TestCaddyValidate 若环境装有 caddy 二进制,验证生成的 Caddyfile(覆盖全部内置片段)
// 能通过 adapt 校验。没有 caddy 时跳过(与 docker 集成测试同策略)。
func TestCaddyValidate(t *testing.T) {
	caddyBin := findCaddy(t)

	dataDir := t.TempDir()
	// import 的 user 目录须存在且有至少一个文件,否则 glob 空匹配报错。
	userDir := filepath.Join(dataDir, "tools", "caddy", "user")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "example.caddy"), []byte("# gatebox user 扩展占位\n"), 0o644); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}

	builtins := BuiltinFragmentCatalog()
	all := make([]string, 0, len(builtins))
	for _, b := range builtins {
		all = append(all, b.ID)
	}
	apps := []model.App{{ID: "app1", Name: "demo"}}
	services := []model.Service{
		{
			// https 上游 + 全部内置片段:覆盖 skip-verify 合并、websocket flush、basic-auth、静态缓存、方法拦截、按服务日志。
			ID: "s1", AppID: "app1", Name: "app", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "app", RootDomain: "neob.cn"}},
			Upstream: []string{"10.0.0.1:443"}, UpstreamProto: "https", HealthURI: "/health", Enabled: true,
			FragmentIDs: all,
		},
		{
			// 静态文件:reverse_proxy 包裹片段应被忽略,其余 site 片段生效。
			ID: "s2", AppID: "app1", Name: "files", Type: model.RouteTypeFileServer,
			Domains: []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "files", RootDomain: "neob.cn"}},
			Root:    "<%GB_STATIC_ROOT%>/<%GB_APP%>/", Enabled: true,
			FragmentIDs: all,
		},
	}
	out, err := Generate(services, apps, nil, nil, builtins, dataDir, nil, GenerateOptions{StaticRoot: filepath.Join(dataDir, "www")})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cfgFile := filepath.Join(dataDir, "Caddyfile")
	if err := os.WriteFile(cfgFile, []byte(out), 0o600); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}
	cmd := exec.Command(caddyBin, "validate", "--config", cfgFile, "--adapter", "caddyfile")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("caddy validate 失败: %v\n输出:\n%s\nCaddyfile:\n%s", err, b, out)
	}
}

// TestCaddyValidateL4 仅当 caddy 含 caddy-l4 模块时,校验 L4(TCP/UDP)生成配置。
func TestCaddyValidateL4(t *testing.T) {
	caddyBin := findCaddy(t)
	out, err := exec.Command(caddyBin, "list-modules").Output()
	if err != nil || !strings.Contains(string(out), "layer4.handlers.proxy") {
		t.Skip("caddy 无 caddy-l4 模块,跳过 L4 validate")
	}
	dataDir := t.TempDir()
	services := []model.Service{
		{
			ID: "s1", Name: "mqtt", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: "mqtt"}},
			Upstream: []string{"127.0.0.1:1883"}, UpstreamProto: "tcp", Enabled: true,
		},
		{
			ID: "s2", Name: "dns", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: "dns"}},
			Upstream: []string{"10.0.0.5:53"}, UpstreamProto: "udp", Enabled: true,
		},
	}
	cfg, err := Generate(services, nil, nil, nil, nil, dataDir, nil,
		GenerateOptions{GlobalSnippets: []string{"layer4 {\n\ttcp/:1883 {\n\t\troute {\n\t\t\tproxy tcp/127.0.0.1:1883\n\t\t}\n\t}\n\tudp/:53 {\n\t\troute {\n\t\t\tproxy udp/10.0.0.5:53\n\t\t}\n\t}\n}"}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfgFile := filepath.Join(dataDir, "Caddyfile")
	if err := os.WriteFile(cfgFile, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmd := exec.Command(caddyBin, "validate", "--config", cfgFile, "--adapter", "caddyfile")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("caddy validate(L4) 失败: %v\n%s\nCaddyfile:\n%s", err, b, cfg)
	}
}
