package plugin

import (
	"testing"

	"github.com/JiangBeta/gatebox/internal/component"
)

func TestDescriptorForMinimal(t *testing.T) {
	man := Manifest{
		APIVersion: "gatebox/v2", Kind: "caddy-module", ID: "caddy-l4", Name: "Caddy L4", Version: "0.1.0",
		Contributions: Contributions{
			Capabilities: []Contribution{{Point: "proxy-protocols", Data: map[string]any{"class": "non-http"}}},
		},
	}
	d := DescriptorFor(man)
	if d.ID != "caddy-l4" || d.Kind != component.KindCaddyModule {
		t.Fatalf("bad identity: %+v", d)
	}
	if len(d.Functions) != 1 || d.Functions[0].ID != "reverse-proxy" {
		t.Fatalf("expected derived reverse-proxy function, got %+v", d.Functions)
	}
	if len(d.Consumes) != 0 || len(d.Produces) != 0 {
		t.Fatalf("minimal descriptor should have no info contract")
	}
	_ = component.InfoService
}

func TestDescriptorForFullContracts(t *testing.T) {
	man := Manifest{
		APIVersion: "gatebox/v2", Kind: "process", ID: "ddns-go", Name: "ddns-go", Version: "1.0.0",
		Provides:      []string{"ddns-publish"},
		Consumes:      []string{"domain", "credential"},
		Produces:      []string{"ip"},
		Observability: &ObservabilityDecl{State: true, Logs: true, Metrics: []string{"publish_total"}},
		Effect: []EffectDecl{
			{On: "domain", Do: []string{"update-config", "restart", "bogus"}},
		},
	}
	d := DescriptorFor(man)
	if len(d.Functions) != 1 || d.Functions[0].ID != "ddns-publish" {
		t.Fatalf("bad functions: %+v", d.Functions)
	}
	if len(d.Consumes) != 2 {
		t.Fatalf("want 2 consumes, got %+v", d.Consumes)
	}
	// domain 应带 effect，且非法动作被忽略。
	var domain *component.InfoPort
	for i := range d.Consumes {
		if d.Consumes[i].Info == component.InfoDomain {
			domain = &d.Consumes[i]
		}
	}
	if domain == nil || len(domain.Effect) != 2 {
		t.Fatalf("domain effect wrong: %+v", domain)
	}
	if len(d.Produces) != 1 || d.Produces[0].Info != component.InfoIP {
		t.Fatalf("bad produces: %+v", d.Produces)
	}
	if !d.Observable.State || len(d.Observable.Metrics) != 1 {
		t.Fatalf("bad observability: %+v", d.Observable)
	}
}
