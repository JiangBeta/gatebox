package ddns

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/JiangBeta/gatebox/internal/model"
)

// Manager ddns-go 配置管理器:把网关服务的二级域名聚合为 ddns-go 配置并落盘。
//
// ddns-go 周期性(默认 5 分钟)重读配置文件并执行更新,故无需重启进程——
// 改写配置即生效,符合「GateBox 只改写配置、不接管生命周期」原则
// (docs/domain.md §4、docs/adr/ADR-021 §2)。
type Manager struct {
	configPath string
}

// NewManager 构造 Manager。configPath 为 ddns-go 配置文件完整路径。
func NewManager(configPath string) *Manager {
	return &Manager{configPath: configPath}
}

// ConfigPath 返回配置落盘路径。
func (m *Manager) ConfigPath() string { return m.configPath }

// credentialIDSecret 按 provider 提取 ddns-go 所需的 ID/Secret。
// cloudflare 仅使用 Secret(Bearer Token),ID 置空——见 ddns-go dns/cloudflare.go。
func credentialIDSecret(c model.DNSCredential) (id, secret string, ok bool) {
	switch c.Provider {
	case model.ProviderCloudflare:
		tok := c.Fields["token"]
		return "", tok, tok != ""
	case model.ProviderDNSPod:
		id := c.Fields["id"]
		sec := c.Fields["token"]
		return id, sec, id != "" && sec != ""
	case model.ProviderAliyun:
		id := c.Fields["accessKeyId"]
		sec := c.Fields["accessKeySecret"]
		return id, sec, id != "" && sec != ""
	default:
		return "", "", false
	}
}

// BuildEntries 从落库服务聚合 ddns-go 上报条目(纯函数,便于单测)。
//
// 规则:只上报「已启用服务」中 rootDomain 已登记且凭证完整的二级域名;
// 同一凭证下的域名去重合并;凭证字段缺失 / provider 不支持则跳过。
// 二级域名取 d.Host()(subdomain 空则为主域名本身,apex 合法,DNS 记录同样需要)。
func BuildEntries(services []model.Service, domains []model.Domain, creds []model.DNSCredential) []Entry {
	domBy := make(map[string]model.Domain, len(domains))
	for _, d := range domains {
		domBy[d.Name] = d
	}
	credBy := make(map[string]model.DNSCredential, len(creds))
	for _, c := range creds {
		credBy[c.ID] = c
	}

	type acc struct {
		cred model.DNSCredential
		id   string
		sec  string
		set  map[string]bool
	}
	byCred := map[string]*acc{}
	for _, svc := range services {
		if !svc.Enabled {
			continue
		}
		for _, d := range svc.Domains {
			dom, ok := domBy[d.RootDomain]
			if !ok || dom.CredentialID == "" {
				continue
			}
			cred, ok := credBy[dom.CredentialID]
			if !ok {
				continue
			}
			a, ok := byCred[dom.CredentialID]
			if !ok {
				id, sec, ok := credentialIDSecret(cred)
				if !ok {
					continue
				}
				a = &acc{cred: cred, id: id, sec: sec, set: map[string]bool{}}
				byCred[dom.CredentialID] = a
			}
			a.set[d.Host()] = true
		}
	}

	out := make([]Entry, 0, len(byCred))
	for _, a := range byCred {
		doms := make([]string, 0, len(a.set))
		for dn := range a.set {
			doms = append(doms, dn)
		}
		sort.Strings(doms)
		out = append(out, Entry{Provider: a.cred.Provider, ID: a.id, Secret: a.sec, Domains: doms})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Secret < out[j].Secret
	})
	return out
}

// Sync 把上报条目渲染为 ddns-go 配置并原子写盘(临时文件 + rename,
// 避免半截配置被 ddns-go 读到)。空条目也会写(清空记录,删除联动后置)。
func (m *Manager) Sync(entries []Entry) error {
	data, err := BuildConfig(entries)
	if err != nil {
		return err
	}
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp := m.configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.configPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
