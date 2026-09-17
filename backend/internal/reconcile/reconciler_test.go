package reconcile

import (
	"context"
	"errors"
	"testing"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/graph"
	"github.com/JiangBeta/gatebox/internal/observe"
)

func testSnapshot() graph.Snapshot {
	return graph.Snapshot{
		Desc: []component.Descriptor{
			{ID: "acme",
				Consumes: []component.InfoPort{{Info: component.InfoDomain}, {Info: component.InfoCredential}},
				Produces: []component.InfoPort{{Info: component.InfoCert}}},
			{ID: "caddy",
				Consumes: []component.InfoPort{{Info: component.InfoService}, {Info: component.InfoCert}, {Info: component.InfoDomain}}},
			{ID: "docker",
				Consumes: []component.InfoPort{{Info: component.InfoVariable}},
				Produces: []component.InfoPort{{Info: component.InfoService}}},
		},
		Facts: []graph.Fact{
			{Ref: graph.FactRef{Kind: "cert", ID: "x"}, Info: component.InfoCert, Origin: "acme"},
			{Ref: graph.FactRef{Kind: "service", ID: "s1"}, Info: component.InfoService, Origin: "docker"},
		},
	}
}

type stub struct {
	id    string
	calls *[]string
	fail  bool
}

func (s stub) Reconcile(_ context.Context, _ component.ReconcileInput) (component.ReconcileResult, error) {
	*s.calls = append(*s.calls, s.id)
	if s.fail {
		return component.ReconcileResult{State: "error"}, errors.New("boom")
	}
	return component.ReconcileResult{State: "success"}, nil
}

func TestRunOrdersAndFiltersByIntent(t *testing.T) {
	var calls []string
	r := New(func(context.Context) graph.Snapshot { return testSnapshot() }, nil, observe.NewBus(16))
	r.Register("acme", stub{id: "acme", calls: &calls})
	r.Register("caddy", stub{id: "caddy", calls: &calls})
	r.Register("docker", stub{id: "docker", calls: &calls})

	r.Trigger(Intent{Op: "update", Kind: "domain", ID: "d1"})
	run, err := r.Run(context.Background(), "intent")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "success" {
		t.Fatalf("run state=%s", run.State)
	}
	// domain 消费者 = acme + caddy；d不包含 docker。
	if len(calls) != 2 {
		t.Fatalf("want acme+caddy, got %v", calls)
	}
	if calls[0] != "acme" || calls[1] != "caddy" {
		t.Fatalf("acme 应在 caddy 前: %v", calls)
	}
}

func TestRunFailureDegradedButContinues(t *testing.T) {
	var calls []string
	r := New(func(context.Context) graph.Snapshot { return testSnapshot() }, nil, observe.NewBus(16))
	r.Register("acme", stub{id: "acme", calls: &calls, fail: true})
	r.Register("caddy", stub{id: "caddy", calls: &calls})

	r.Trigger(Intent{Op: "update", Kind: "domain", ID: "d1"})
	run, _ := r.Run(context.Background(), "intent")
	if run.State != "error" {
		t.Fatalf("want error, got %s", run.State)
	}
	if len(calls) != 2 {
		t.Fatalf("失败不应中断其他分支: %v", calls)
	}
}

func TestRunWithoutIntentAffectsAll(t *testing.T) {
	var calls []string
	r := New(func(context.Context) graph.Snapshot { return testSnapshot() }, nil, observe.NewBus(16))
	r.Register("acme", stub{id: "acme", calls: &calls})
	r.Register("caddy", stub{id: "caddy", calls: &calls})
	r.Register("docker", stub{id: "docker", calls: &calls})

	run, _ := r.Run(context.Background(), "manual")
	if len(run.Steps) != 3 {
		t.Fatalf("want 3 steps, got %d", len(run.Steps))
	}
}
