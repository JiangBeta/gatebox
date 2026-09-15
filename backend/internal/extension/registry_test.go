package extension

import "testing"

// TestCoreProviderHTTPClass:核心自带 http/https 协议能力。
func TestCoreProviderHTTPClass(t *testing.T) {
	r := NewRegistry()
	r.Register(CoreProvider())
	if got := r.ClassOf("https"); got != ClassHTTP {
		t.Errorf("ClassOf(https) = %q, want %q", got, ClassHTTP)
	}
	if got := r.ClassOf("http"); got != ClassHTTP {
		t.Errorf("ClassOf(http) = %q, want %q", got, ClassHTTP)
	}
	if !r.HasClass(ClassHTTP) {
		t.Error("HasClass(http) = false")
	}
}

// TestNonHTTPProvider:注册 non-http 提供者后，非 http 协议归入该类；注销后不可代理。
func TestNonHTTPProvider(t *testing.T) {
	r := NewRegistry()
	r.Register(CoreProvider())
	if got := r.ClassOf("mqtt"); got != "" {
		t.Errorf("无提供者时 ClassOf(mqtt) = %q, want 空", got)
	}

	rd, _ := NewTemplateRenderer("t", "x")
	r.Register(Provider{
		ID:        "ext-l4",
		Renderers: map[string]Renderer{ClassNonHTTP: rd},
		Capabilities: []Capability{{
			ID: "ext-l4.proxy-protocols", Point: PointProxyProtocols, Provider: "ext-l4",
			Meta: map[string]any{"class": ClassNonHTTP, "networks": []any{"tcp", "udp"}},
		}},
	})
	if got := r.ClassOf("mqtt"); got != ClassNonHTTP {
		t.Errorf("ClassOf(mqtt) = %q, want %q", got, ClassNonHTTP)
	}
	if _, ok := r.RendererFor(ClassNonHTTP); !ok {
		t.Error("RendererFor(non-http) 应命中")
	}
	if !r.HasPoint(PointRenderer) {
		t.Error("HasPoint(renderer) 应为 true")
	}

	r.Unregister("ext-l4")
	if got := r.ClassOf("mqtt"); got != "" {
		t.Errorf("注销后 ClassOf(mqtt) = %q, want 空", got)
	}
}

// TestExactProtocolWins:精确协议优先于通配。
func TestExactProtocolWins(t *testing.T) {
	r := NewRegistry()
	r.Register(Provider{
		ID: "p1",
		Capabilities: []Capability{
			{ID: "p1.a", Point: PointProxyProtocols, Meta: map[string]any{"class": ClassHTTP, "protocols": []any{"https"}}},
			{ID: "p1.b", Point: PointProxyProtocols, Meta: map[string]any{"class": ClassNonHTTP}},
		},
	})
	if got := r.ClassOf("https"); got != ClassHTTP {
		t.Errorf("ClassOf(https) = %q, want %q", got, ClassHTTP)
	}
}
