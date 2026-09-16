package plugin

import (
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/extension"
)

func findManifest(id string) (Manifest, bool) {
	for _, m := range builtinCatalog() {
		if m.ID == id {
			return m, true
		}
	}
	return Manifest{}, false
}

// TestCaddyL4Renderer:能力型插件贡献的 renderer 产出与旧内置 layer4 生成一致。
func TestCaddyL4Renderer(t *testing.T) {
	man, ok := findManifest("caddy-l4")
	if !ok {
		t.Fatal("caddy-l4 不在内置目录")
	}
	p, err := providerFor(man)
	if err != nil {
		t.Fatalf("providerFor: %v", err)
	}
	rd, ok := p.Renderers[extension.ClassNonHTTP]
	if !ok {
		t.Fatal("缺少 non-http renderer")
	}
	got, err := rd.Render([]extension.ProxyRule{
		{Protocol: "mqtt", Upstream: "127.0.0.1:1883", Ports: []int{1883, 8883}, Nets: []string{"tcp"}},
		{Protocol: "dns", Upstream: "10.0.0.5:53", Ports: []int{53}, Nets: []string{"tcp", "udp"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "layer4 {\n" +
		"\ttcp/:1883 {\n\t\troute {\n\t\t\tproxy tcp/127.0.0.1:1883\n\t\t}\n\t}\n" +
		"\ttcp/:8883 {\n\t\troute {\n\t\t\tproxy tcp/127.0.0.1:1883\n\t\t}\n\t}\n" +
		"\ttcp/:53 {\n\t\troute {\n\t\t\tproxy tcp/10.0.0.5:53\n\t\t}\n\t}\n" +
		"\tudp/:53 {\n\t\troute {\n\t\t\tproxy udp/10.0.0.5:53\n\t\t}\n\t}\n" +
		"}"
	if got != want {
		t.Errorf("renderer 输出不符:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestCaddyL4Capabilities:能力声明含 proxy-protocols（class=non-http）。
func TestCaddyL4Capabilities(t *testing.T) {
	man, _ := findManifest("caddy-l4")
	p, err := providerFor(man)
	if err != nil {
		t.Fatalf("providerFor: %v", err)
	}
	found := false
	for _, c := range p.Capabilities {
		if c.Point == extension.PointProxyProtocols {
			if extension.ParseProtocolClass(c.Meta).Class == extension.ClassNonHTTP {
				found = true
			}
		}
	}
	if !found {
		t.Error("caddy-l4 未声明 non-http 协议能力")
	}
}

// TestBuiltinCatalogV2:内置目录均为 v2 且制品带 role。
func TestBuiltinCatalogV2(t *testing.T) {
	for _, m := range builtinCatalog() {
		if m.APIVersion != "gatebox/v2" {
			t.Errorf("%s: apiVersion=%q", m.ID, m.APIVersion)
		}
		for _, a := range m.Artifacts {
			if a.Role == "" {
				t.Errorf("%s: 制品缺少 role", m.ID)
			}
		}
	}
	man, _ := findManifest("caddy-l4")
	if !strings.Contains(man.Contributions.Backend[0].Impl["template"], "layer4 {") {
		t.Error("caddy-l4 模板应含 layer4 指令")
	}
}
