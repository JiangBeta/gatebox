// Package extension 提供扩展平台：扩展点、能力注册表、投影与扩展契约。
//
// 设计见 docs/adr/ADR-036 与 docs/architecture.md §4。三个不变式：
//   - I1 核心无插件身份：只认抽象扩展点，不出现插件 ID；
//   - I2 核心 → 插件：只经投影（只读）；
//   - I3 插件 → 核心：只经贡献声明 + 契约调用。
package extension

import (
	"strings"
	"text/template"

	"github.com/JiangBeta/gatebox/internal/model"
)

// 扩展点（point）。
const (
	PointProxyProtocols = "proxy-protocols"   // 可代理的协议类别
	PointRenderer       = "renderer"          // 中性规则 → Caddyfile 片段
	PointValidator      = "validator"         // 校验器
	PointConfigSync     = "config-sync"       // 从投影渲染插件配置并落盘
	PointReconcile      = "reconcile"         // 通知侧车自行收敛
	PointDNSProvider    = "dns-provider"      // DNS 凭证供应商（ADR-039 §5）
	PointComponentVar   = "component-variant" // 核心组件配方特征（ADR-038）
)

// CoreProviderID 核心内置提供者的伪插件 ID（与插件同构注册）。
const CoreProviderID = "_core"

// 协议类别。
const (
	ClassHTTP    = "http"
	ClassNonHTTP = "non-http"
)

// Capability 注册表中的一条能力。
type Capability struct {
	ID       string         `json:"id"`
	Point    string         `json:"point"`
	Provider string         `json:"provider"`
	Version  string         `json:"version,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
}

// ProtocolClass 从 proxy-protocols 能力 meta 解析出的协议类别。
type ProtocolClass struct {
	Class                 string   `json:"class"`
	Label                 string   `json:"label,omitempty"`
	Protocols             []string `json:"protocols,omitempty"` // 空 = 通配（该类下任意协议）
	Networks              []string `json:"networks,omitempty"`
	RequiresPrimaryDomain bool     `json:"requiresPrimaryDomain,omitempty"`
}

// ParseProtocolClass 从能力 meta 解析协议类别（纯函数）。
func ParseProtocolClass(meta map[string]any) ProtocolClass {
	pc := ProtocolClass{}
	if meta == nil {
		return pc
	}
	if v, ok := meta["class"].(string); ok {
		pc.Class = v
	}
	if v, ok := meta["label"].(string); ok {
		pc.Label = v
	}
	pc.Protocols = stringSlice(meta["protocols"])
	pc.Networks = stringSlice(meta["networks"])
	if v, ok := meta["requiresPrimaryDomain"].(bool); ok {
		pc.RequiresPrimaryDomain = v
	}
	return pc
}

// ProxyRule 中性代理规则（核心只产出这个，不解析插件语法）。
type ProxyRule struct {
	Protocol string   `json:"protocol"`
	Upstream string   `json:"upstream"`
	Ports    []int    `json:"ports"`
	Nets     []string `json:"nets"`
}

// ProjectionInput 配置同步契约的输入：核心状态快照（进程内，含凭证）。
type ProjectionInput struct {
	Services []model.Service
	Domains  []model.Domain
	Creds    []model.DNSCredential
}

// ConfigSync 扩展契约：从投影渲染插件配置并落盘（如 ddns-go）。
type ConfigSync interface {
	SyncProjection(in ProjectionInput) error
}

// ProviderField DNS 凭证字段描述（驱动前端动态表单与后端校验）。
type ProviderField struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type,omitempty"` // text | password
	Required bool   `json:"required,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
}

// DDNSMapping 供应商到 ddns-go 的字段映射。
type DDNSMapping struct {
	Provider    string `json:"provider"`          // ddns-go 供应商名（如 alidns）
	IDField     string `json:"idField,omitempty"` // 取 ID 的字段名
	SecretField string `json:"secretField"`       // 取 Secret 的字段名
}

// DNSProviderSpec dns-provider 能力的解析结果（纯数据，供应商无关）。
type DNSProviderSpec struct {
	ID       string            `json:"id"`
	Label    string            `json:"label,omitempty"`
	ACMEHook string            `json:"acmeHook,omitempty"` // acme.sh dnsapi hook 名（如 dns_cf）
	Fields   []ProviderField   `json:"fields,omitempty"`
	EnvMap   map[string]string `json:"envMap,omitempty"` // 凭证字段 → acme.sh 环境变量
	DDNS     *DDNSMapping      `json:"ddns,omitempty"`   // ddns-go 映射（可选）
}

// ParseDNSProvider 从能力 meta 解析 DNS 供应商描述（纯函数）。
func ParseDNSProvider(meta map[string]any) DNSProviderSpec {
	spec := DNSProviderSpec{}
	if meta == nil {
		return spec
	}
	spec.ID = stringField(meta, "id")
	spec.Label = stringField(meta, "label")
	spec.ACMEHook = stringField(meta, "acmeHook")
	if fs, ok := meta["fields"].([]any); ok {
		for _, f := range fs {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			spec.Fields = append(spec.Fields, ProviderField{
				Name:     stringField(fm, "name"),
				Label:    stringField(fm, "label"),
				Type:     stringField(fm, "type"),
				Required: boolField(fm, "required"),
				Secret:   boolField(fm, "secret"),
			})
		}
	}
	if em, ok := meta["envMap"].(map[string]any); ok {
		spec.EnvMap = make(map[string]string, len(em))
		for k, v := range em {
			if s, ok := v.(string); ok {
				spec.EnvMap[k] = s
			}
		}
	}
	if dm, ok := meta["ddns"].(map[string]any); ok {
		spec.DDNS = &DDNSMapping{
			Provider:    stringField(dm, "provider"),
			IDField:     stringField(dm, "idField"),
			SecretField: stringField(dm, "secretField"),
		}
	}
	return spec
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func boolField(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// Renderer 扩展契约：中性规则 → Caddyfile 全局块片段。
type Renderer interface {
	Render(rules []ProxyRule) (string, error)
}

// TemplateRenderer 声明式 renderer：插件随包携带的 text/template。
type TemplateRenderer struct {
	id string
	t  *template.Template
}

// NewTemplateRenderer 解析模板；模板数据为 struct{ Rules []ProxyRule }。
func NewTemplateRenderer(id, text string) (*TemplateRenderer, error) {
	t, err := template.New(id).Parse(text)
	if err != nil {
		return nil, err
	}
	return &TemplateRenderer{id: id, t: t}, nil
}

// Render 执行模板。
func (r *TemplateRenderer) Render(rules []ProxyRule) (string, error) {
	var b strings.Builder
	if err := r.t.Execute(&b, struct{ Rules []ProxyRule }{Rules: rules}); err != nil {
		return "", err
	}
	return b.String(), nil
}

func stringSlice(v any) []string {
	switch xs := v.(type) {
	case []string:
		return xs
	case []any:
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
