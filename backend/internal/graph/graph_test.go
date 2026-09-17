package graph

import (
	"encoding/json"
	"testing"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/model"
)

func descriptors() []component.Descriptor {
	return []component.Descriptor{
		{
			ID: "acme", Name: "acme.sh", Tier: "core",
			Functions: []component.Function{{ID: "cert-issue"}},
			Consumes: []component.InfoPort{
				{Info: component.InfoDomain}, {Info: component.InfoCredential},
			},
			Produces: []component.InfoPort{{Info: component.InfoCert}},
		},
		{
			ID: "caddy", Name: "Caddy", Tier: "core",
			Functions: []component.Function{{ID: "reverse-proxy"}},
			Consumes: []component.InfoPort{
				{Info: component.InfoService}, {Info: component.InfoCert}, {Info: component.InfoDomain},
			},
		},
		{
			ID: "docker", Name: "Docker", Tier: "core",
			Functions: []component.Function{{ID: "label-publish"}},
			Consumes:  []component.InfoPort{{Info: component.InfoVariable}},
			Produces:  []component.InfoPort{{Info: component.InfoLabel}, {Info: component.InfoService}},
		},
	}
}

func snapshot() Snapshot {
	return Snapshot{
		Desc: descriptors(),
		Facts: []Fact{
			DomainFact(model.Domain{ID: "d1", Name: "neob.cn", CredentialID: "c1"}),
			CredentialFact(model.DNSCredential{ID: "c1", Provider: "cloudflare"}),
			ServiceFact(model.Service{ID: "s1", Type: "reverse_proxy",
				Domains:     []model.ProxyDomain{{Protocol: "https", Subdomain: "app", RootDomain: "neob.cn"}},
				FragmentIDs: []string{"f1"}}, "user"),
			ServiceFact(model.Service{ID: "derived1", Type: "reverse_proxy"}, "docker"),
			{Ref: FactRef{Kind: "cert", ID: "app.neob.cn"}, Info: component.InfoCert, Origin: "acme"},
		},
	}
}

func TestFactRegistry(t *testing.T) {
	if _, ok := FactKindOf("service"); !ok {
		t.Fatal("service fact kind missing")
	}
	for _, k := range []string{"service", "domain", "credential", "fragment", "variable", "port"} {
		fk, ok := FactKindOf(k)
		if !ok || !fk.Persisted || fk.Info == "" {
			t.Errorf("%s: expected persisted fact with info mapping, got %+v", k, fk)
		}
		if len(fk.Schema) == 0 {
			t.Errorf("%s: schema empty", k)
		}
	}
}

func TestBuildComponentEdges(t *testing.T) {
	g := Build(snapshot(), "component")
	if g.View != "component" {
		t.Fatalf("view=%s", g.View)
	}
	var acmeToCaddy, dockerToCaddy *Edge
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.From == "acme" && e.To == "caddy" && e.Info == component.InfoCert {
			acmeToCaddy = e
		}
		if e.From == "docker" && e.To == "caddy" && e.Info == component.InfoService {
			dockerToCaddy = e
		}
	}
	if acmeToCaddy == nil || len(acmeToCaddy.Instances) != 1 {
		t.Fatalf("acme->caddy cert edge wrong: %+v", acmeToCaddy)
	}
	if dockerToCaddy == nil || len(dockerToCaddy.Instances) != 1 || dockerToCaddy.Instances[0].ID != "derived1" {
		t.Fatalf("docker->caddy service edge wrong: %+v", dockerToCaddy)
	}
}

func TestRefEdgesResolveDomainByName(t *testing.T) {
	g := Build(snapshot(), "component")
	found := false
	for _, re := range g.FactRefs {
		if re.From.Kind == "service" && re.To.Kind == "domain" && re.To.ID == "d1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("service->domain ref by name not resolved: %+v", g.FactRefs)
	}
}

func TestBuildDeterministic(t *testing.T) {
	a, _ := json.Marshal(Build(snapshot(), "component"))
	b, _ := json.Marshal(Build(snapshot(), "component"))
	if string(a) != string(b) {
		t.Fatal("build not deterministic")
	}
}

func TestFunctionView(t *testing.T) {
	g := Build(snapshot(), "function")
	if len(g.Nodes) == 0 {
		t.Fatal("function view has no nodes")
	}
	hasCertIssue := false
	for _, n := range g.Nodes {
		if n.ID == "cert-issue" {
			hasCertIssue = true
			if len(n.Implementors) != 1 || n.Implementors[0] != "acme" {
				t.Errorf("cert-issue implementors wrong: %+v", n.Implementors)
			}
		}
	}
	if !hasCertIssue {
		t.Error("function node cert-issue missing")
	}
}
