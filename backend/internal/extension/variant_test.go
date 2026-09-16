package extension

import "testing"

// TestVariantKeyMatchesStore 变体键必须与 GateBoxStore 的 gbx-store 实现一致。
// 期望值由 `gbx-store variant-key -component caddy -version 2.10.0 -os linux -arch amd64
// -features github.com/mholt/caddy-l4,github.com/corazawaf/coraza-caddy/v2` 产出。
func TestVariantKeyMatchesStore(t *testing.T) {
	got := VariantKey("caddy", "2.10.0", "linux", "amd64", []string{
		"github.com/mholt/caddy-l4",
		"github.com/corazawaf/coraza-caddy/v2",
	})
	const want = "sha256:a544e359b338b7f7"
	if got != want {
		t.Fatalf("变体键不一致: got %s want %s", got, want)
	}
}

// TestVariantKeyOrderInsensitive 特征顺序与重复不影响结果。
func TestVariantKeyOrderInsensitive(t *testing.T) {
	a := VariantKey("caddy", "2.10.0", "linux", "amd64", []string{"b", "a"})
	b := VariantKey("caddy", "2.10.0", "linux", "amd64", []string{"a", "b", "a"})
	if a != b {
		t.Fatalf("顺序/重复不应影响: %s vs %s", a, b)
	}
}

// TestComponentFeatures 特征并集来自注册表的能力表。
func TestComponentFeatures(t *testing.T) {
	r := NewRegistry()
	r.Register(Provider{ID: "p1", Capabilities: []Capability{
		{ID: "p1.f1", Point: PointComponentVar, Meta: map[string]any{"component": "caddy", "feature": "mod.b"}},
	}})
	r.Register(Provider{ID: "p2", Capabilities: []Capability{
		{ID: "p2.f1", Point: PointComponentVar, Meta: map[string]any{"component": "caddy", "feature": "mod.a"}},
		{ID: "p2.f2", Point: PointComponentVar, Meta: map[string]any{"component": "other", "feature": "mod.x"}},
	}})
	got := r.ComponentFeatures("caddy")
	if len(got) != 2 || got[0] != "mod.a" || got[1] != "mod.b" {
		t.Fatalf("特征并集错误: %v", got)
	}
}
