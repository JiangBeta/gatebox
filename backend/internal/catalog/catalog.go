// Package catalog 聚合「可安装项」：可选组件 + 插件，供扩展页的 AppStore 视图使用。
//
// 核心组件（Tier=core）不进入商店；组件与插件的安装/卸载在此按 id 分派。
package catalog

import (
	"context"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/plugin"
)

// 来源标识。
const (
	SourceComponent = "component"
	SourcePlugin    = "plugin"
)

// Item 商店条目（组件与插件的统一视图）。
type Item struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Source      string   `json:"source"` // component | plugin
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	Summary     string   `json:"summary"`
	Installed   bool     `json:"installed"`
	Status      string   `json:"status,omitempty"`
	Upgradeable bool     `json:"upgradeable"`
}

// Catalog 商店聚合器。
type Catalog struct {
	reg *component.CoreRegistry
	mgr *plugin.Manager
}

// New 构造商店聚合器。
func New(reg *component.CoreRegistry, mgr *plugin.Manager) *Catalog {
	return &Catalog{reg: reg, mgr: mgr}
}

// List 返回除核心组件外的全部可安装项。
func (c *Catalog) List(ctx context.Context) ([]Item, error) {
	out := make([]Item, 0)
	for _, info := range c.reg.List(ctx) {
		if info.Tier == "core" {
			continue
		}
		out = append(out, ComponentItem(info))
	}
	views, err := c.mgr.List()
	if err != nil {
		return nil, err
	}
	for _, v := range views {
		out = append(out, PluginItem(v))
	}
	return out, nil
}

// Install 安装指定条目。
func (c *Catalog) Install(ctx context.Context, id string) (Item, error) {
	if _, ok := c.reg.Get(ctx, id); ok {
		info, err := c.reg.Install(ctx, id)
		if err != nil {
			return Item{}, err
		}
		return ComponentItem(info), nil
	}
	v, err := c.mgr.Install(ctx, id)
	if err != nil {
		return Item{}, err
	}
	return PluginItem(v), nil
}

// Remove 卸载指定条目。
func (c *Catalog) Remove(ctx context.Context, id string) (Item, error) {
	if _, ok := c.reg.Get(ctx, id); ok {
		info, err := c.reg.Uninstall(ctx, id)
		if err != nil {
			return Item{}, err
		}
		return ComponentItem(info), nil
	}
	v, err := c.mgr.Remove(id)
	if err != nil {
		return Item{}, err
	}
	return PluginItem(v), nil
}

// ComponentItem 组件视图 → 商店条目。
func ComponentItem(info component.Info) Item {
	return Item{
		ID:          info.ID,
		Name:        info.Name,
		Kind:        string(info.Kind),
		Source:      SourceComponent,
		Tags:        info.Tags,
		Version:     info.Current,
		Summary:     info.Summary,
		Installed:   info.Installed,
		Status:      info.Status.State,
		Upgradeable: info.Upgrade == "replace",
	}
}

// PluginItem 插件视图 → 商店条目。
func PluginItem(v plugin.View) Item {
	return Item{
		ID:          v.ID,
		Name:        v.Name,
		Kind:        v.Kind,
		Source:      SourcePlugin,
		Tags:        v.Tags,
		Version:     v.Version,
		Summary:     v.Summary,
		Installed:   v.State != "available",
		Status:      v.State,
		Upgradeable: false,
	}
}
