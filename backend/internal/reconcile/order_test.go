package reconcile

import (
	"testing"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/graph"
)

func TestOrderProducerBeforeConsumer(t *testing.T) {
	edges := []graph.Edge{
		{From: "acme", To: "caddy", Info: component.InfoCert},
		{From: "docker", To: "caddy", Info: component.InfoService},
	}
	got := Order([]string{"caddy", "acme", "docker"}, edges)
	if index(got, "acme") > index(got, "caddy") {
		t.Fatalf("acme 应在 caddy 前: %v", got)
	}
	if index(got, "docker") > index(got, "caddy") {
		t.Fatalf("docker 应在 caddy 前: %v", got)
	}
}

func TestOrderCycleDoesNotHang(t *testing.T) {
	edges := []graph.Edge{
		{From: "a", To: "b", Info: component.InfoService},
		{From: "b", To: "a", Info: component.InfoCert},
	}
	got := Order([]string{"a", "b"}, edges)
	if len(got) != 2 {
		t.Fatalf("cycle nodes missing: %v", got)
	}
}

func TestOrderDeterministic(t *testing.T) {
	ids := []string{"c", "a", "b"}
	edges := []graph.Edge{{From: "b", To: "c", Info: component.InfoService}}
	if index(Order(ids, edges), "x") != -1 {
		t.Fatal("unexpected")
	}
	first := Order(ids, edges)
	second := Order(ids, edges)
	for i := range first {
		if first[i] != second[i] {
			t.Fatal("not deterministic")
		}
	}
}

func index(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
