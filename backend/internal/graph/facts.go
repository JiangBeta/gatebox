// Package graph 提供 v4 的中央事实注册表与依赖图推导。
//
// 设计见 docs/v4/L1-02-graph.md 与 docs/v4/L1-03-schema.md。
// 本包只做纯推导（无副作用、无 IO），事实快照由 handler 装配。
package graph

import (
	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/model"
)

// RefKind 引用种类。
type RefKind string

const (
	RefID      RefKind = "id"      // 按目标 ID 引用
	RefName    RefKind = "name"    // 按名称引用（如 rootDomain）
	RefDerived RefKind = "derived" // 运行态派生
)

// FactReference 某事实对其他事实的引用。
type FactReference struct {
	Field      string  `json:"field"`
	TargetKind string  `json:"targetKind"`
	Kind       RefKind `json:"kind"`
}

// FactKind 事实类别（中央注册表一项）：信息映射 + 引用 + 配置契约。
type FactKind struct {
	Kind       string                  `json:"kind"`
	Info       component.InfoType      `json:"info"`
	Persisted  bool                    `json:"persisted"`
	Source     string                  `json:"source"` // "user" 或组件 id
	References []FactReference         `json:"references"`
	Schema     []component.ConfigField `json:"schema"`
	UIHint     string                  `json:"uiHint,omitempty"`
}

// factKinds 是中央事实注册表（顺序稳定）。
var factKinds = []FactKind{
	{
		Kind: "service", Info: component.InfoService, Persisted: true, Source: "user",
		References: []FactReference{
			{Field: "appId", TargetKind: "app", Kind: RefID},
			{Field: "domains[].rootDomain", TargetKind: "domain", Kind: RefName},
			{Field: "fragmentIds[]", TargetKind: "fragment", Kind: RefID},
		},
		Schema: serviceSchema(),
		UIHint: "gateway-service",
	},
	{
		Kind: "domain", Info: component.InfoDomain, Persisted: true, Source: "user",
		References: []FactReference{
			{Field: "credentialId", TargetKind: "credential", Kind: RefID},
		},
		Schema: domainSchema(),
	},
	{
		Kind: "credential", Info: component.InfoCredential, Persisted: true, Source: "user",
		Schema: credentialSchema(),
	},
	{
		Kind: "fragment", Info: component.InfoFragment, Persisted: true, Source: "user",
		Schema: fragmentSchema(),
		UIHint: "fragment-code",
	},
	{
		Kind: "variable", Info: component.InfoVariable, Persisted: true, Source: "user",
		Schema: variableSchema(),
	},
	{
		Kind: "port", Info: component.InfoPortBinding, Persisted: true, Source: "user",
		Schema: portSchema(),
	},
	{
		Kind: "app", Info: "", Persisted: true, Source: "user",
		Schema: appSchema(),
	},
	{
		Kind: "compose", Info: "", Persisted: true, Source: "user",
		Schema: composeSchema(),
		UIHint: "compose-editor",
	},
}

// FactKinds 返回全部事实类别（副本）。
func FactKinds() []FactKind {
	out := make([]FactKind, len(factKinds))
	copy(out, factKinds)
	return out
}

// FactKindOf 按 kind 查事实类别。
func FactKindOf(kind string) (FactKind, bool) {
	for _, fk := range factKinds {
		if fk.Kind == kind {
			return fk, true
		}
	}
	return FactKind{}, false
}

// InfoOf 返回事实类别映射的信息类型（默认空）。
func InfoOf(kind string) component.InfoType {
	if fk, ok := FactKindOf(kind); ok {
		return fk.Info
	}
	return ""
}

// ---- 配置契约（U3） ----

func f(key, label string, t component.ConfigType, required bool) component.ConfigField {
	return component.ConfigField{Key: key, Label: label, Type: t, Required: required}
}

func adv(f component.ConfigField) component.ConfigField {
	f.Advanced = true
	return f
}

func serviceSchema() []component.ConfigField {
	return []component.ConfigField{
		{Key: "appId", Label: "归属应用", Type: component.ConfigReference, Required: true,
			Reference: &component.ReferenceSpec{Types: []string{"app"}}},
		f("name", "服务名", component.ConfigText, true),
		{Key: "type", Label: "代理类型", Type: component.ConfigSelect, Required: true,
			Options: []component.FieldOption{
				{Label: "反向代理", Value: model.RouteTypeReverseProxy},
				{Label: "静态文件", Value: model.RouteTypeFileServer},
			}},
		{Key: "domains", Label: "发布域名", Type: component.ConfigArray, Required: true,
			SummaryFields: []string{"rootDomain", "subdomain"},
			Item: &component.ConfigField{Type: component.ConfigObject, Fields: []component.ConfigField{
				{Key: "protocol", Label: "协议", Type: component.ConfigSelect,
					Options: []component.FieldOption{
						{Label: "HTTPS", Value: model.DomainProtoHTTPS},
						{Label: "HTTP", Value: model.DomainProtoHTTP},
					}},
				f("subdomain", "二级域名", component.ConfigText, false),
				{Key: "rootDomain", Label: "根域名", Type: component.ConfigReference,
					Reference: &component.ReferenceSpec{Types: []string{"domain"}}},
				f("customPort", "自定义端口", component.ConfigSwitch, false),
				f("port", "端口", component.ConfigNumber, false),
			}}},
		{Key: "upstream", Label: "后端地址", Type: component.ConfigArray,
			Placeholder: "127.0.0.1:8080",
			Item:        &component.ConfigField{Type: component.ConfigText}},
		adv(component.ConfigField{Key: "upstreamProto", Label: "后端协议", Type: component.ConfigSelect,
			Options: []component.FieldOption{{Label: "http", Value: "http"}, {Label: "https", Value: "https"}}}),
		adv(f("root", "静态根目录", component.ConfigText, false)),
		adv(f("healthUri", "健康检查路径", component.ConfigText, false)),
		adv(component.ConfigField{Key: "fragmentIds", Label: "Caddy 片段", Type: component.ConfigArray,
			Item: &component.ConfigField{Type: component.ConfigReference,
				Reference: &component.ReferenceSpec{Types: []string{"fragment"}}}}),
		f("enabled", "启用", component.ConfigSwitch, false),
	}
}

func domainSchema() []component.ConfigField {
	return []component.ConfigField{
		f("name", "根域名", component.ConfigText, true),
		{Key: "credentialId", Label: "DNS 凭证", Type: component.ConfigReference,
			Reference: &component.ReferenceSpec{Types: []string{"credential"}}},
	}
}

func credentialSchema() []component.ConfigField {
	// 动态：providerId + 供应商字段由 handler 按 dns-provider 能力追加。
	return []component.ConfigField{
		{Key: "providerId", Label: "供应商", Type: component.ConfigSelect, Required: true},
		f("name", "名称", component.ConfigText, false),
	}
}

func fragmentSchema() []component.ConfigField {
	return []component.ConfigField{
		f("name", "名称", component.ConfigText, false),
		adv(component.ConfigField{Key: "description", Label: "说明", Type: component.ConfigTextarea}),
		{Key: "code", Label: "片段代码", Type: component.ConfigTextarea,
			Description: "遵循 Caddyfile 语法的片段体，可含 <%VAR%> 变量"},
		f("defaultEnabled", "默认启用", component.ConfigSwitch, false),
		f("defaultHidden", "默认隐藏", component.ConfigSwitch, false),
	}
}

func variableSchema() []component.ConfigField {
	return []component.ConfigField{
		f("key", "键", component.ConfigText, true),
		{Key: "value", Label: "值", Type: component.ConfigPassword},
		adv(component.ConfigField{Key: "description", Label: "说明", Type: component.ConfigText}),
	}
}

func portSchema() []component.ConfigField {
	return []component.ConfigField{
		f("protocol", "协议", component.ConfigText, true),
		{Key: "ports", Label: "端口", Type: component.ConfigArray,
			Item: &component.ConfigField{Type: component.ConfigNumber}},
		{Key: "network", Label: "传输网络", Type: component.ConfigSelect,
			Options: []component.FieldOption{
				{Label: "TCP", Value: model.NetTCP},
				{Label: "UDP", Value: model.NetUDP},
				{Label: "TCP+UDP", Value: model.NetBoth},
			}},
		adv(component.ConfigField{Key: "description", Label: "说明", Type: component.ConfigText}),
		f("enabled", "启用", component.ConfigSwitch, false),
	}
}

func appSchema() []component.ConfigField {
	return []component.ConfigField{
		f("name", "应用名", component.ConfigText, true),
		adv(component.ConfigField{Key: "description", Label: "说明", Type: component.ConfigTextarea}),
	}
}

func composeSchema() []component.ConfigField {
	return []component.ConfigField{
		f("projectName", "项目名", component.ConfigText, true),
		f("displayName", "展示名", component.ConfigText, false),
	}
}
