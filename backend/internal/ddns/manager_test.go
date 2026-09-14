package ddns

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/JiangBeta/gatebox/internal/models"
)

func TestBuildEntries(t *testing.T) {
	creds := []models.DNSCredential{
		{ID: "cf1", Provider: "cloudflare", Fields: map[string]string{"token": "tok-cf"}},
		{ID: "dp1", Provider: "dnspod", Fields: map[string]string{"id": "id-dp", "token": "tok-dp"}},
		{ID: "ali1", Provider: "aliyun", Fields: map[string]string{"accessKeyId": "ak", "accessKeySecret": "sk"}},
		{ID: "bad", Provider: "dnspod", Fields: map[string]string{"id": "id-bad"}}, // 缺 token → 跳过
	}
	domains := []models.Domain{
		{ID: "d1", Name: "neob.cn", CredentialID: "cf1"},
		{ID: "d2", Name: "dom2.cn", CredentialID: "dp1"},
		{ID: "d3", Name: "ali.cn", CredentialID: "ali1"},
		{ID: "d4", Name: "nobind.cn", CredentialID: ""},
		{ID: "d5", Name: "bad-cred.cn", CredentialID: "bad"},
	}
	services := []models.Service{
		{
			ID: "s1", Enabled: true,
			Domains: []models.ProxyDomain{
				{Subdomain: "a", RootDomain: "neob.cn"},
				{Subdomain: "b", RootDomain: "neob.cn"},
			},
		},
		{
			ID: "s2", Enabled: true,
			Domains: []models.ProxyDomain{
				{Subdomain: "a", RootDomain: "neob.cn"}, // 与 s1 去重
				{Subdomain: "", RootDomain: "dom2.cn"},  // apex
				{Subdomain: "x", RootDomain: "ali.cn"},
				{Subdomain: "y", RootDomain: "ali.cn"},
				{Subdomain: "z", RootDomain: "nobind.cn"},   // 无凭证 → 跳过
				{Subdomain: "w", RootDomain: "bad-cred.cn"}, // 凭证残缺 → 跳过
			},
		},
		{
			ID: "s3", Enabled: false,
			Domains: []models.ProxyDomain{{Subdomain: "off", RootDomain: "neob.cn"}}, // 停用 → 跳过
		},
		{
			ID: "s4", Enabled: true,
			Domains: []models.ProxyDomain{{Subdomain: "docker", RootDomain: ""}}, // docker 派生,无 rootDomain → 跳过
		},
	}

	entries := BuildEntries(services, domains, creds)
	if len(entries) != 3 {
		t.Fatalf("条目数 = %d, want 3(cloudflare/dnspod/aliyun): %+v", len(entries), entries)
	}

	byProv := map[string]Entry{}
	for _, e := range entries {
		byProv[e.Provider] = e
	}

	cf := byProv["cloudflare"]
	if cf.ID != "" || cf.Secret != "tok-cf" {
		t.Errorf("cloudflare ID/Secret 不对: %+v", cf)
	}
	if len(cf.Domains) != 2 || cf.Domains[0] != "a.neob.cn" || cf.Domains[1] != "b.neob.cn" {
		t.Errorf("cloudflare domains = %v", cf.Domains)
	}

	dp := byProv["dnspod"]
	if dp.ID != "id-dp" || dp.Secret != "tok-dp" {
		t.Errorf("dnspod ID/Secret 不对: %+v", dp)
	}
	if len(dp.Domains) != 1 || dp.Domains[0] != "dom2.cn" {
		t.Errorf("dnspod 应只有 apex dom2.cn, got %v", dp.Domains)
	}

	ali := byProv["aliyun"]
	if ali.ID != "ak" || ali.Secret != "sk" {
		t.Errorf("aliyun ID/Secret 不对: %+v", ali)
	}
	if len(ali.Domains) != 2 || ali.Domains[0] != "x.ali.cn" || ali.Domains[1] != "y.ali.cn" {
		t.Errorf("aliyun domains = %v", ali.Domains)
	}
}

func TestBuildEntriesEmpty(t *testing.T) {
	entries := BuildEntries(nil, nil, nil)
	if entries == nil {
		t.Fatal("空输入应返回空切片而非 nil")
	}
	if len(entries) != 0 {
		t.Fatalf("空输入 returns %d entries", len(entries))
	}
}

func TestManagerSync(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "tools", "ddnsgo", ".ddns_go_config.yaml"))

	entries := []Entry{
		{Provider: "cloudflare", ID: "", Secret: "tok", Domains: []string{"a.neob.cn"}},
		{Provider: "aliyun", ID: "ak", Secret: "sk", Domains: []string{"b.neob.cn"}},
	}
	if err := m.Sync(entries); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	data, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		t.Fatalf("读配置: %v", err)
	}
	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("落盘 YAML 无法解析: %v", err)
	}
	if len(cfg.DnsConf) != 2 {
		t.Fatalf("dnsconf 数 = %d, want 2", len(cfg.DnsConf))
	}

	// 残留临时文件应被清理/更名
	if _, err := os.Stat(filepath.Join(dir, "tools", "ddnsgo", ".ddns_go_config.yaml.tmp")); !os.IsNotExist(err) {
		t.Error("残留 .tmp 文件")
	}

	// 空条目也应覆盖写盘(删除联动:清空记录)
	if err := m.Sync(nil); err != nil {
		t.Fatalf("Sync(nil): %v", err)
	}
	data2, _ := os.ReadFile(m.ConfigPath())
	var cfg2 config
	if err := yaml.Unmarshal(data2, &cfg2); err != nil {
		t.Fatal(err)
	}
	if len(cfg2.DnsConf) != 0 {
		t.Fatalf("清空后 dnsconf 数 = %d, want 0", len(cfg2.DnsConf))
	}
}
