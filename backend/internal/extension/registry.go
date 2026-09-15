package extension

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// Provider 一个提供者的注册内容（核心或插件，同构）。
type Provider struct {
	ID           string
	Version      string
	Capabilities []Capability
	Renderers    map[string]Renderer // class → renderer
	ConfigSyncs  []ConfigSync
}

// Registry 能力注册表：核心与插件统一注册，消费者只查表。
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry 构造空注册表。
func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

// Register 注册/覆盖一个提供者。
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.ID] = p
}

// Unregister 注销一个提供者（插件停用/卸载时调用）。
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, id)
}

// Capabilities 返回全部能力（稳定排序）。
func (r *Registry) Capabilities() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Capability, 0)
	for _, p := range r.providers {
		for _, c := range p.Capabilities {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Point != out[j].Point {
			return out[i].Point < out[j].Point
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// CapabilitiesByPoint 返回某扩展点的全部能力。
func (r *Registry) CapabilitiesByPoint(point string) []Capability {
	out := make([]Capability, 0)
	for _, c := range r.Capabilities() {
		if c.Point == point {
			out = append(out, c)
		}
	}
	return out
}

// ClassOf 返回协议所属类别；无提供者支持时返回空串（不可代理）。
// 精确协议匹配优先，其次通配（class 声明但未列 protocols）能力。
func (r *Registry) ClassOf(protocol string) string {
	p := strings.ToLower(strings.TrimSpace(protocol))
	if p == "" {
		p = "https"
	}
	caps := r.CapabilitiesByPoint(PointProxyProtocols)
	for _, c := range caps {
		for _, x := range ParseProtocolClass(c.Meta).Protocols {
			if strings.EqualFold(x, p) {
				return ParseProtocolClass(c.Meta).Class
			}
		}
	}
	for _, c := range caps {
		pc := ParseProtocolClass(c.Meta)
		if len(pc.Protocols) == 0 && pc.Class != "" {
			return pc.Class
		}
	}
	return ""
}

// HasClass 是否已有提供者支持某协议类别。
func (r *Registry) HasClass(class string) bool {
	for _, c := range r.CapabilitiesByPoint(PointProxyProtocols) {
		if ParseProtocolClass(c.Meta).Class == class {
			return true
		}
	}
	return false
}

// RendererFor 返回某协议类别的渲染器。
func (r *Registry) RendererFor(class string) (Renderer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.providers {
		if rd, ok := p.Renderers[class]; ok {
			return rd, true
		}
	}
	return nil, false
}

// HasPoint 是否存在某扩展点的提供者（覆盖能力、渲染器、配置同步三类贡献）。
func (r *Registry) HasPoint(point string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.providers {
		switch point {
		case PointRenderer:
			if len(p.Renderers) > 0 {
				return true
			}
		case PointConfigSync:
			if len(p.ConfigSyncs) > 0 {
				return true
			}
		default:
			for _, c := range p.Capabilities {
				if c.Point == point {
					return true
				}
			}
		}
	}
	return false
}

// SyncConfig 调用全部 config-sync 提供者（核心在配置变更后触发）。
func (r *Registry) SyncConfig(in ProjectionInput) error {
	r.mu.RLock()
	syncs := make([]ConfigSync, 0)
	for _, p := range r.providers {
		syncs = append(syncs, p.ConfigSyncs...)
	}
	r.mu.RUnlock()
	var errs []error
	for _, s := range syncs {
		if err := s.SyncProjection(in); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CoreProvider 核心内置提供者：http/https 协议能力（HTTP 渲染为核心领域逻辑，不入表）。
//
// 注册后与插件同构，消费者只查表、不认插件身份（I1）。
func CoreProvider() Provider {
	return Provider{
		ID:      CoreProviderID,
		Version: "core",
		Capabilities: []Capability{
			{
				ID: "core.http", Point: PointProxyProtocols, Provider: CoreProviderID,
				Meta: map[string]any{
					"class": ClassHTTP, "label": "HTTP/HTTPS",
					"protocols": []string{"http", "https"}, "networks": []string{"tcp"},
					"requiresPrimaryDomain": true,
				},
			},
		},
	}
}
