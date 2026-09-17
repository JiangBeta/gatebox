package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/graph"
	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/plugin"
	"github.com/JiangBeta/gatebox/internal/repository"
)

// RegisterGraph 注册 v4 依赖图端点（功能地图数据源）。
//
// derived 提供 docker 派生服务（可为 nil）；快照在此装配，graph 包只做纯推导。
func RegisterGraph(mux *http.ServeMux, core *component.CoreRegistry, mgr *plugin.Manager, store *repository.Store, derived func(context.Context) []model.Service) {
	mux.HandleFunc("GET /api/v1/graph", func(w http.ResponseWriter, r *http.Request) {
		snap := graph.Snapshot{Desc: allDescriptors(core, mgr)}
		snap.Facts = snapshotFacts(r.Context(), store, derived)

		view := r.URL.Query().Get("view")
		if view == "" {
			view = "component"
		}
		writeJSON(w, http.StatusOK, graph.Build(snap, view))
	})
}

// snapshotFacts 从持久事实 + docker 派生服务装配事实快照。
func snapshotFacts(ctx context.Context, store *repository.Store, derived func(context.Context) []model.Service) []graph.Fact {
	facts := make([]graph.Fact, 0)
	if store != nil {
		if xs, err := store.ListServices(); err == nil {
			for _, x := range xs {
				facts = append(facts, graph.ServiceFact(x, "user"))
			}
		} else {
			log.Printf("graph: ListServices: %v", err)
		}
		if xs, err := store.ListDomains(); err == nil {
			for _, x := range xs {
				facts = append(facts, graph.DomainFact(x))
			}
		}
		if xs, err := store.ListCredentials(); err == nil {
			for _, x := range xs {
				facts = append(facts, graph.CredentialFact(x))
			}
		}
		if xs, err := store.ListFragments(); err == nil {
			for _, x := range xs {
				facts = append(facts, graph.FragmentFact(x))
			}
		}
		if xs, err := store.ListVariables(); err == nil {
			for _, x := range xs {
				facts = append(facts, graph.VariableFact(x))
			}
		}
		if xs, err := store.ListPortBindings(); err == nil {
			for _, x := range xs {
				facts = append(facts, graph.PortFact(x))
			}
		}
		if xs, err := store.ListApps(); err == nil {
			for _, x := range xs {
				facts = append(facts, graph.AppFact(x))
			}
		}
		if xs, err := store.ListComposeInstances(); err == nil {
			for _, x := range xs {
				facts = append(facts, graph.ComposeFact(x))
			}
		}
	}
	if derived != nil {
		for _, s := range derived(ctx) {
			facts = append(facts, graph.ServiceFact(s, "docker"))
		}
	}
	return facts
}

// RegisterSchema 注册 v4 事实配置契约端点（驱动前端表单）。
func RegisterSchema(mux *http.ServeMux, ext *extension.Registry) {
	mux.HandleFunc("GET /api/v1/schema/{factKind}", func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("factKind")
		fk, ok := graph.FactKindOf(kind)
		if !ok {
			writeErrCode(w, http.StatusNotFound, "UNKNOWN_FACT_KIND", "未知事实类别")
			return
		}
		fields := fk.Schema
		if kind == "credential" {
			fields = credentialSchema(ext, r.URL.Query().Get("provider"))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"factKind": kind,
			"uiHint":   fk.UIHint,
			"fields":   fields,
		})
	})
}

// credentialSchema 生成 DNS 凭证 schema：providerId 选项来自扩展注册表；
// 指定 provider 时追加该供应商字段（ProviderField → ConfigField）。
func credentialSchema(ext *extension.Registry, provider string) []component.ConfigField {
	fk, _ := graph.FactKindOf("credential")
	fields := make([]component.ConfigField, 0, len(fk.Schema)+4)
	for _, f := range fk.Schema {
		if f.Key == "providerId" && ext != nil {
			for _, s := range ext.DNSProviders() {
				label := s.Label
				if label == "" {
					label = s.ID
				}
				f.Options = append(f.Options, component.FieldOption{Label: label, Value: s.ID})
			}
		}
		fields = append(fields, f)
	}
	if provider == "" || ext == nil {
		return fields
	}
	spec, ok := ext.DNSProvider(provider)
	if !ok {
		return fields
	}
	for _, pf := range spec.Fields {
		t := component.ConfigText
		if pf.Type == "password" || pf.Secret {
			t = component.ConfigPassword
		}
		fields = append(fields, component.ConfigField{
			Key: pf.Name, Label: pf.Label, Type: t, Required: pf.Required,
		})
	}
	return fields
}
