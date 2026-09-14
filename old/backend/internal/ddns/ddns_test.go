package ddns

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildConfig(t *testing.T) {
	data, err := BuildConfig([]Entry{
		{Provider: "cloudflare", ID: "tok", Secret: "", Domains: []string{"a.neob.cn", "b.neob.cn"}},
		{Provider: "aliyun", ID: "ak", Secret: "sk", Domains: []string{"c.neob.cn"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("生成的 YAML 无法反序列化: %v", err)
	}
	if len(cfg.DnsConf) != 2 {
		t.Fatalf("dnsconf 数 = %d, want 2", len(cfg.DnsConf))
	}
	if cfg.DnsConf[0].DNS.Name != "cloudflare" {
		t.Errorf("cloudflare 供应商名 = %q", cfg.DnsConf[0].DNS.Name)
	}
	if cfg.DnsConf[1].DNS.Name != "alidns" {
		t.Errorf("aliyun 应映射为 alidns, got %q", cfg.DnsConf[1].DNS.Name)
	}
	if cfg.DnsConf[0].Ipv4.Enable != true || cfg.DnsConf[0].Ipv4.GetType != "url" {
		t.Errorf("ipv4 配置不对: %+v", cfg.DnsConf[0].Ipv4)
	}
	if len(cfg.DnsConf[0].Ipv4.Domains) != 2 {
		t.Errorf("ipv4 domains 数 = %d, want 2", len(cfg.DnsConf[0].Ipv4.Domains))
	}
	if cfg.DnsConf[0].DNS.ID != "tok" {
		t.Errorf("凭证 ID = %q", cfg.DnsConf[0].DNS.ID)
	}
}
