package catalog

import (
	"testing"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/plugin"
)

func TestComponentItem(t *testing.T) {
	info := component.Info{
		Descriptor: component.Descriptor{
			ID: "ddns-go", Name: "ddns-go", Kind: component.KindProcess,
			Tags: []string{"独立进程"}, Summary: "动态 DNS", Upgrade: "replace",
		},
		Current:   "1.2.3",
		Installed: true,
		Status:    component.Status{State: "running"},
	}
	it := ComponentItem(info)
	if it.Source != SourceComponent || it.ID != "ddns-go" || !it.Installed || !it.Upgradeable {
		t.Fatalf("组件映射异常: %+v", it)
	}
	if it.Version != "1.2.3" || it.Status != "running" || len(it.Tags) != 1 {
		t.Fatalf("组件字段映射异常: %+v", it)
	}
}

func TestPluginItem(t *testing.T) {
	v := plugin.View{
		Manifest: plugin.Manifest{ID: "coraza", Name: "Coraza WAF", Kind: "caddy-module",
			Version: "0.1.0", Summary: "WAF", Tags: []string{"Caddy插件"}},
		State: "available",
	}
	it := PluginItem(v)
	if it.Source != SourcePlugin || it.Installed {
		t.Fatalf("未安装插件应 installed=false: %+v", it)
	}
	if it.Upgradeable || it.Kind != "caddy-module" || len(it.Tags) != 1 {
		t.Fatalf("插件字段映射异常: %+v", it)
	}

	v.State = "enabled"
	if !PluginItem(v).Installed {
		t.Fatal("已启用插件应 installed=true")
	}
}
