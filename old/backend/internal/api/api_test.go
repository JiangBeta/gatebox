package api

import (
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/models"
)

func TestValidateCredentialFields(t *testing.T) {
	cases := []struct {
		name string
		c    *models.DNSCredential
		ok   bool
	}{
		{"cloudflare ok", &models.DNSCredential{Provider: "cloudflare", Fields: map[string]string{"token": "x"}}, true},
		{"cloudflare miss", &models.DNSCredential{Provider: "cloudflare", Fields: map[string]string{}}, false},
		{"dnspod ok", &models.DNSCredential{Provider: "dnspod", Fields: map[string]string{"id": "i", "token": "t"}}, true},
		{"dnspod miss", &models.DNSCredential{Provider: "dnspod", Fields: map[string]string{"id": "i"}}, false},
		{"aliyun ok", &models.DNSCredential{Provider: "aliyun", Fields: map[string]string{"accessKeyId": "k", "accessKeySecret": "s"}}, true},
		{"unknown", &models.DNSCredential{Provider: "foo"}, false},
	}
	for _, tc := range cases {
		ok, _ := validateCredentialFields(tc.c)
		if ok != tc.ok {
			t.Errorf("%s: ok=%v want %v", tc.name, ok, tc.ok)
		}
	}
}

func TestAggregateDomainCertStatus(t *testing.T) {
	now := time.Now()
	ok := now.Add(30 * 24 * time.Hour)
	expiring := now.Add(5 * 24 * time.Hour)
	expired := now.Add(-1 * time.Hour)

	certs := []models.Cert{
		{FQDN: "ddnsgo.neob.cn", NotAfter: ok},
		{FQDN: "ariang.neob.cn", NotAfter: expiring},
	}
	if got := aggregateDomainCertStatus(certs, "neob.cn", now); got != "expiring" {
		t.Errorf("聚合应取最坏 expiring, got %s", got)
	}
	if got := aggregateDomainCertStatus(certs, "other.cn", now); got != "unissued" {
		t.Errorf("无证书应 unissued, got %s", got)
	}

	certs2 := []models.Cert{{FQDN: "x.neob.cn", NotAfter: expired}}
	if got := aggregateDomainCertStatus(certs2, "neob.cn", now); got != "expired" {
		t.Errorf("应 expired, got %s", got)
	}
}

func TestCertStats(t *testing.T) {
	now := time.Now()
	certs := []models.Cert{
		{NotAfter: now.Add(30 * 24 * time.Hour)},
		{NotAfter: now.Add(5 * 24 * time.Hour)},
		{NotAfter: now.Add(-1 * time.Hour)},
	}
	total, expiring, expired := certStats(certs, now)
	if total != 3 || expiring != 1 || expired != 1 {
		t.Errorf("total=%d expiring=%d expired=%d", total, expiring, expired)
	}
}

func TestSubdomainCountByRoot(t *testing.T) {
	services := []models.Service{
		{
			Enabled: true,
			Domains: []models.ProxyDomain{
				{Subdomain: "a", RootDomain: "neob.cn"},
				{Subdomain: "b", RootDomain: "neob.cn"},
				{Subdomain: "", RootDomain: "apex.cn"},
			},
		},
		{
			Enabled: false,
			Domains: []models.ProxyDomain{{Subdomain: "off", RootDomain: "neob.cn"}},
		},
		{
			Enabled: true,
			Domains: []models.ProxyDomain{{Subdomain: "docker", RootDomain: ""}}, // docker 派生,无 rootDomain
		},
	}
	got := subdomainCountByRoot(services)
	if got["neob.cn"] != 3 {
		t.Errorf("neob.cn = %d, want 3", got["neob.cn"])
	}
	if got["apex.cn"] != 1 {
		t.Errorf("apex.cn = %d, want 1", got["apex.cn"])
	}
	if _, ok := got[""]; ok {
		t.Error("不应统计空 rootDomain(docker 派生)")
	}
}

func TestDomainReferenced(t *testing.T) {
	services := []models.Service{
		{Enabled: true, Domains: []models.ProxyDomain{{Subdomain: "a", RootDomain: "neob.cn"}}},
		{Enabled: false, Domains: []models.ProxyDomain{{Subdomain: "b", RootDomain: "neob.cn"}}},
		{Enabled: true, Domains: []models.ProxyDomain{{Subdomain: "c", RootDomain: "other.cn"}}},
		{Enabled: true, Domains: []models.ProxyDomain{{Subdomain: "docker", RootDomain: ""}}},
	}
	if !domainReferenced(services, "neob.cn") {
		t.Error("neob.cn 应被引用(含停用服务)")
	}
	if domainReferenced(services, "free.cn") {
		t.Error("free.cn 不应被引用")
	}
}
