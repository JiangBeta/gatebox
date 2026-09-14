package caddy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JiangBeta/gatebox/internal/models"
)

// TestCaddyValidate 若环境装有 caddy 二进制,验证生成的 Caddyfile 能通过 adapt 校验。
// 没有 caddy 时跳过(与 docker 集成测试同策略)。
func TestCaddyValidate(t *testing.T) {
	caddyBin, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("caddy 未安装,跳过 validate 集成测试")
	}

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
	services := []models.Service{
		{
			ID: "s1", Name: "app", Type: models.RouteTypeReverseProxy,
			Domains:  []models.ProxyDomain{{Protocol: models.DomainProtoHTTPS, Subdomain: "app", RootDomain: "neob.cn"}},
			Upstream: []string{"127.0.0.1:8096"}, HealthURI: "/health", Enabled: true,
			FragmentIDs: DefaultEnabledFragmentIDs(nil, builtins),
		},
		{
			ID: "s2", Name: "files", Type: models.RouteTypeFileServer,
			Domains: []models.ProxyDomain{{Protocol: models.DomainProtoHTTPS, Subdomain: "files", RootDomain: "neob.cn"}},
			Root:    "/srv/www", Enabled: true,
		},
	}
	out, err := Generate(services, nil, nil, nil, builtins, dataDir, nil)
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
