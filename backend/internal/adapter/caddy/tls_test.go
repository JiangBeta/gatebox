package caddy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/model"
)

func TestGenerateTLS(t *testing.T) {
	dataDir := t.TempDir()
	// 预置 acme 证书文件(jellyfin.neob.cn),使 tls 指令被渲染。
	dir := filepath.Join(dataDir, "tools", "acme", "certs", "jellyfin.neob.cn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"fullchain.pem", "key.pem"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	services := []model.Service{
		{
			ID: "s1", Name: "jellyfin", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "jellyfin", RootDomain: "neob.cn"}},
			Upstream: []string{"127.0.0.1:8096"}, Enabled: true,
		},
	}
	dns := map[string]model.DNSCredential{
		"neob.cn": {Provider: model.ProviderCloudflare, Fields: map[string]string{"token": "tok123"}},
	}
	got, err := Generate(services, nil, nil, nil, nil, dataDir, dns)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "tls "+dataDir+"/tools/acme/certs/jellyfin.neob.cn/fullchain.pem") {
		t.Errorf("缺少引用 acme 证书的 tls 指令:\n%s", got)
	}
}

func TestGenerateTLSNoMatch(t *testing.T) {
	dataDir := t.TempDir()
	services := []model.Service{
		{
			ID: "s1", Name: "other", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, RootDomain: "other.example.com"}},
			Upstream: []string{"127.0.0.1:1"}, Enabled: true,
		},
	}
	dns := map[string]model.DNSCredential{
		"neob.cn": {Provider: model.ProviderCloudflare, Fields: map[string]string{"token": "tok123"}},
	}
	got, err := Generate(services, nil, nil, nil, nil, dataDir, dns)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Contains(got, "tls ") {
		t.Errorf("无匹配 rootDomain 时不应有 tls 指令:\n%s", got)
	}
}

func TestRenderTLSDNS(t *testing.T) {
	got := renderTLSDNS("/data", "jellyfin.neob.cn")
	want := "/data/tools/acme/certs/jellyfin.neob.cn/fullchain.pem"
	if !strings.Contains(got, want) {
		t.Errorf("renderTLSDNS = %q, 不含 %q", got, want)
	}
}

func TestTLSFilesExist(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "tools", "acme", "certs", "ok.neob.cn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"fullchain.pem", "key.pem"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !tlsFilesExist(dataDir, "ok.neob.cn") {
		t.Error("证书存在时 tlsFilesExist 应为 true")
	}
	if tlsFilesExist(dataDir, "missing.neob.cn") {
		t.Error("证书缺失时 tlsFilesExist 应为 false")
	}
}
