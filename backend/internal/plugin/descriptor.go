package plugin

import (
	"log"
	"sort"

	"github.com/JiangBeta/gatebox/internal/component"
)

// ObservabilityDecl manifest 的观测契约声明（v4 addit）。
type ObservabilityDecl struct {
	State    bool     `json:"state,omitempty"`
	Activity bool     `json:"activity,omitempty"`
	Logs     bool     `json:"logs,omitempty"`
	Metrics  []string `json:"metrics,omitempty"`
}

// EffectDecl manifest 的生效契约声明（v4 addit）：收到 on 信息变更时执行 do。
type EffectDecl struct {
	On string   `json:"on"`
	Do []string `json:"do"`
}

// DescriptorFor 把 manifest 编译为组件描述符（ADR-041 §4.2）。
//
// 缺失 v4 契约字段时编译为「最小描述符」：仅身份 + 由 contributions 派生的功能，
// 保证旧插件零改动加载。
func DescriptorFor(man Manifest) component.Descriptor {
	d := component.Descriptor{
		ID:             man.ID,
		Name:           man.Name,
		Summary:        man.Summary,
		Kind:           kindOf(man.Kind),
		Tier:           "optional",
		Tags:           man.Tags,
		Provision:      "managed",
		Runtime:        "manage",
		Upgrade:        "replace",
		Capabilities:   capabilitiesOf(man),
		DefaultEnabled: false,
		Removable:      true,
		Bundled:        false,
	}

	// 功能：显式 provides 优先；否则由 contributions.capabilities 派生。
	d.Functions = functionsOf(man)

	// 信息契约：consumes 带 effect（按 EffectDecl 映射），produces 无 effect。
	for _, info := range man.Consumes {
		port := component.InfoPort{Info: component.InfoType(info)}
		for _, e := range man.Effect {
			if e.On != info {
				continue
			}
			for _, act := range e.Do {
				a := component.EffectAction(act)
				if !component.ValidEffect(a) {
					log.Printf("plugin %s: 未知 effect 动作 %q，已忽略", man.ID, act)
					continue
				}
				port.Effect = append(port.Effect, a)
			}
		}
		d.Consumes = append(d.Consumes, port)
	}
	for _, info := range man.Produces {
		d.Produces = append(d.Produces, component.InfoPort{Info: component.InfoType(info)})
	}

	// 观测契约。
	if o := man.Observability; o != nil {
		d.Observable = component.Observability{State: o.State, Activity: o.Activity, Logs: o.Logs}
		for _, m := range o.Metrics {
			d.Observable.Metrics = append(d.Observable.Metrics, component.MetricSpec{Name: m, Label: m})
		}
	}
	return d
}

// kindOf 把 manifest kind 映射为组件 Kind。
func kindOf(kind string) component.Kind {
	switch kind {
	case "caddy-module":
		return component.KindCaddyModule
	case "process":
		return component.KindProcess
	case "config-only":
		return component.KindConfigOnly
	default:
		return component.KindConfigOnly
	}
}

// capabilitiesOf 由 manifest 贡献推导旧式能力列表（供既有 UI 门控）。
func capabilitiesOf(man Manifest) []string {
	set := map[string]bool{}
	for _, b := range man.Contributions.Backend {
		switch b.Point {
		case "config-sync", "reconcile":
			set["config"] = true
		case "renderer":
			set["config"] = true
		}
	}
	if man.Kind == "process" {
		set["runnable"] = true
		set["logs"] = true
		set["restartable"] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// functionsOf 输出功能清单：显式 provides，否则由 capabilities 派生。
func functionsOf(man Manifest) []component.Function {
	out := make([]component.Function, 0, len(man.Provides))
	for _, p := range man.Provides {
		out = append(out, component.Function{ID: p, Label: p})
	}
	if len(out) == 0 {
		// 最小描述符：proxy-protocols 能力 → reverse-proxy 功能。
		for _, c := range man.Contributions.Capabilities {
			if c.Point == "proxy-protocols" {
				out = append(out, component.Function{ID: "reverse-proxy", Label: "反向代理"})
				break
			}
		}
	}
	return out
}

// Descriptors 返回已启用插件的描述符（稳定排序）。
func (m *Manager) Descriptors() []component.Descriptor {
	list, err := m.List()
	if err != nil {
		return nil
	}
	out := make([]component.Descriptor, 0)
	for _, v := range list {
		if v.State != "enabled" {
			continue
		}
		out = append(out, DescriptorFor(v.Manifest))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
