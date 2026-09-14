// Package ddns 生成 ddns-go 的配置文件(ADR-004:改写 YAML + 重启生效)。
package ddns

import (
	"gopkg.in/yaml.v3"
)

// Entry 一条 DDNS 上报配置:一个 DNS 凭证 + 一组待上报的二级域名。
type Entry struct {
	Provider string   // GateBox 内部供应商名:cloudflare | dnspod | aliyun
	ID       string   // 凭证 ID(token / SecretId / AccessKey ID)
	Secret   string   // 凭证 Secret
	Domains  []string // IPv4 待上报的二级域名
}

// providerName 将 GateBox 供应商名映射为 ddns-go 的供应商名。
func providerName(p string) string {
	switch p {
	case "aliyun":
		return "alidns"
	default:
		return p
	}
}

// BuildConfig 生成 ddns-go 的 .ddns_go_config.yaml 内容。
func BuildConfig(entries []Entry) ([]byte, error) {
	cfg := config{}
	for _, e := range entries {
		cfg.DnsConf = append(cfg.DnsConf, dnsConf{
			Name: e.Provider,
			Ipv4: ipv4Config{
				Enable:  true,
				GetType: "url",
				URL:     "https://api.ipify.org",
				Domains: e.Domains,
			},
			Ipv6: ipv6Config{Enable: false},
			DNS: dnsConfig{
				Name:   providerName(e.Provider),
				ID:     e.ID,
				Secret: e.Secret,
			},
		})
	}
	return yaml.Marshal(cfg)
}

// ddns-go 配置结构(字段名与其 YAML 键一致,均为小写)。
type config struct {
	DnsConf []dnsConf `yaml:"dnsconf"`
}

type dnsConf struct {
	Name          string     `yaml:"name"`
	Ipv4          ipv4Config `yaml:"ipv4"`
	Ipv6          ipv6Config `yaml:"ipv6"`
	DNS           dnsConfig  `yaml:"dns"`
	TTL           string     `yaml:"ttl,omitempty"`
	HttpInterface string     `yaml:"httpinterface,omitempty"`
}

type dnsConfig struct {
	Name     string `yaml:"name"`
	ID       string `yaml:"id"`
	Secret   string `yaml:"secret"`
	ExtParam string `yaml:"extparam,omitempty"`
}

type ipv4Config struct {
	Enable       bool     `yaml:"enable"`
	GetType      string   `yaml:"gettype"`
	URL          string   `yaml:"url,omitempty"`
	NetInterface string   `yaml:"netinterface,omitempty"`
	Cmd          string   `yaml:"cmd,omitempty"`
	Domains      []string `yaml:"domains,omitempty"`
}

type ipv6Config struct {
	Enable       bool     `yaml:"enable"`
	GetType      string   `yaml:"gettype,omitempty"`
	URL          string   `yaml:"url,omitempty"`
	NetInterface string   `yaml:"netinterface,omitempty"`
	Cmd          string   `yaml:"cmd,omitempty"`
	Domains      []string `yaml:"domains,omitempty"`
	Ipv6Reg      string   `yaml:"ipv6reg,omitempty"`
}
