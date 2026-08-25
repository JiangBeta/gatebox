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
