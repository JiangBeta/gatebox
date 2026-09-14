package caddy

import (
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/model"
)

// TestGenerateGlobalPorts 全局 http_port/https_port 覆盖写入(与 traefik 共存用)。
func TestGenerateGlobalPorts(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "jellyfin", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "jellyfin", RootDomain: "neob.cn"}},
			Upstream: []string{"127.0.0.1:8096"}, Enabled: true,
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil,
		GenerateOptions{HTTPPort: 8081, HTTPSPort: 8443})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, want := range []string{"http_port 8081", "https_port 8443"} {
		if !strings.Contains(got, want) {
			t.Errorf("全局块应含 %q, got:\n%s", want, got)
		}
	}
	// 零值(默认)不写端口,保持标准 80/443 行为。
	gotDefault, err := Generate(services, nil, nil, nil, nil, testDataDir, nil)
	if err != nil {
		t.Fatalf("generate default: %v", err)
	}
	if strings.Contains(gotDefault, "http_port") || strings.Contains(gotDefault, "https_port") {
		t.Errorf("默认不应写端口覆盖, got:\n%s", gotDefault)
	}
}

// TestGenerateExtraHTTPSPorts 额外 https 端口:每个 https 站点额外生成 host:<port> 重复块;
// http 站点不生成(ADR-026 多 https 端口,如 443+9443)。
func TestGenerateExtraHTTPSPorts(t *testing.T) {
	services := []model.Service{
		{
			ID: "s1", Name: "jellyfin", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "jellyfin", RootDomain: "neob.cn"}},
			Upstream: []string{"127.0.0.1:8096"}, Enabled: true,
		},
		{
			ID: "s2", Name: "lan", Type: model.RouteTypeReverseProxy,
			Domains:  []model.ProxyDomain{{Protocol: model.DomainProtoHTTP, Subdomain: "lan", RootDomain: "neob.cn"}},
			Upstream: []string{"127.0.0.1:8080"}, Enabled: true,
		},
	}
	got, err := Generate(services, nil, nil, nil, nil, testDataDir, nil,
		GenerateOptions{HTTPSPort: 443, ExtraHTTPSPorts: []int{9443}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(got, "jellyfin.neob.cn {") || !strings.Contains(got, "jellyfin.neob.cn:9443 {") {
		t.Errorf("https 站点应生成主块与 :9443 重复块:\n%s", got)
	}
	if strings.Contains(got, "lan.neob.cn:9443") {
		t.Errorf("http 站点不应生成额外 https 端口块:\n%s", got)
	}
	if strings.Count(got, "jellyfin.neob.cn") != 2 {
		t.Errorf("jellyfin 应恰好 2 个块(主+9443), got:\n%s", got)
	}
}
