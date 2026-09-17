package graph

import (
	"sort"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/model"
)

// FactRef 事实实例引用（kind + id）。
type FactRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Fact 事实实例。
type Fact struct {
	Ref    FactRef
	Info   component.InfoType
	Origin string // "user" 或组件 id
	Value  any    // model.*（供引用提取）
}

// Snapshot 图构建输入：描述符（类型骨架）+ 事实快照（实例绑定）。
type Snapshot struct {
	Desc  []component.Descriptor
	Facts []Fact
}

// RefEdge 事实→事实引用边。
type RefEdge struct {
	From  FactRef `json:"from"`
	To    FactRef `json:"to"`
	Field string  `json:"field"`
}

// InfoCount 信息计数（按 Origin 分组）。
type InfoCount struct {
	Info    component.InfoType `json:"info"`
	Count   int                `json:"count"`
	Origins map[string]int     `json:"origins,omitempty"`
}

// Node 图节点（组件视角或功能视角）。
type Node struct {
	ID           string      `json:"id"`
	Kind         string      `json:"kind"` // component | function
	Label        string      `json:"label"`
	Tier         string      `json:"tier,omitempty"`
	Functions    []string    `json:"functions,omitempty"`
	Implementors []string    `json:"implementors,omitempty"`
	Consumes     []InfoCount `json:"consumes,omitempty"`
	Produces     []InfoCount `json:"produces,omitempty"`
}

// Edge 组件/功能间的信息流边（生产者→消费者）。
type Edge struct {
	From      string             `json:"from"`
	To        string             `json:"to"`
	Info      component.InfoType `json:"info"`
	Instances []FactRef          `json:"instances"`
	Cycle     bool               `json:"cycle"`
}

// Graph 依赖图。
type Graph struct {
	View     string    `json:"view"`
	Nodes    []Node    `json:"nodes"`
	Edges    []Edge    `json:"edges"`
	FactRefs []RefEdge `json:"factRefs"`
}

// ---- 事实构造器（ID 提取的唯一来源） ----

// ServiceFact 构造服务事实；origin = user（manual）/ docker（派生）。
func ServiceFact(s model.Service, origin string) Fact {
	return Fact{Ref: FactRef{Kind: "service", ID: s.ID}, Info: component.InfoService, Origin: origin, Value: s}
}

// DomainFact 根域名事实。
func DomainFact(d model.Domain) Fact {
	return Fact{Ref: FactRef{Kind: "domain", ID: d.ID}, Info: component.InfoDomain, Origin: "user", Value: d}
}

// CredentialFact DNS 凭证事实。
func CredentialFact(c model.DNSCredential) Fact {
	return Fact{Ref: FactRef{Kind: "credential", ID: c.ID}, Info: component.InfoCredential, Origin: "user", Value: c}
}

// FragmentFact Caddy 片段事实。
func FragmentFact(f model.Fragment) Fact {
	return Fact{Ref: FactRef{Kind: "fragment", ID: f.ID}, Info: component.InfoFragment, Origin: "user", Value: f}
}

// VariableFact 变量事实（主键为 key）。
func VariableFact(v model.Variable) Fact {
	return Fact{Ref: FactRef{Kind: "variable", ID: v.Key}, Info: component.InfoVariable, Origin: "user", Value: v}
}

// PortFact 端口事实（主键为 protocol）。
func PortFact(p model.PortBinding) Fact {
	return Fact{Ref: FactRef{Kind: "port", ID: p.Protocol}, Info: component.InfoPortBinding, Origin: "user", Value: p}
}

// AppFact 应用分组事实（无信息类别，不进组件边）。
func AppFact(a model.App) Fact {
	return Fact{Ref: FactRef{Kind: "app", ID: a.ID}, Info: "", Origin: "user", Value: a}
}

// ComposeFact 编排事实（无信息类别，不进图节点）。
func ComposeFact(c model.ComposeInstance) Fact {
	return Fact{Ref: FactRef{Kind: "compose", ID: c.ProjectName}, Info: "", Origin: "user", Value: c}
}

// ---- 构建 ----

// Build 由快照推导依赖图。view = "component" | "function"。
func Build(snap Snapshot, view string) Graph {
	byRef := make(map[FactRef]Fact, len(snap.Facts))
	domainByName := make(map[string]FactRef)
	for _, f := range snap.Facts {
		byRef[f.Ref] = f
		if f.Ref.Kind == "domain" {
			if d, ok := f.Value.(model.Domain); ok {
				domainByName[d.Name] = f.Ref
			}
		}
	}

	refEdges := buildRefEdges(snap.Facts, byRef, domainByName)

	nodeIDs := make([]string, 0, len(snap.Desc))
	for _, d := range snap.Desc {
		nodeIDs = append(nodeIDs, d.ID)
	}

	edges := buildEdges(snap, byRef)

	if view == "function" {
		return Graph{View: "function", Nodes: buildFunctionNodes(snap, edges), Edges: buildFunctionEdges(snap, edges), FactRefs: refEdges}
	}
	return Graph{View: "component", Nodes: buildComponentNodes(snap, byRef), Edges: edges, FactRefs: refEdges}
}

func buildRefEdges(facts []Fact, byRef map[FactRef]Fact, domainByName map[string]FactRef) []RefEdge {
	out := make([]RefEdge, 0)
	for _, f := range facts {
		fk, ok := FactKindOf(f.Ref.Kind)
		if !ok {
			continue
		}
		for _, ref := range fk.References {
			for _, target := range extractTargets(f, ref) {
				to := FactRef{Kind: ref.TargetKind, ID: target}
				if _, ok := byRef[to]; !ok && ref.TargetKind == "domain" {
					if r, ok := domainByName[target]; ok {
						to = r
					}
				}
				if _, ok := byRef[to]; !ok {
					continue
				}
				out = append(out, RefEdge{From: f.Ref, To: to, Field: ref.Field})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return refLess(out[i].From, out[j].From)
		}
		return refLess(out[i].To, out[j].To)
	})
	return out
}

func refLess(a, b FactRef) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID < b.ID
}

func extractTargets(f Fact, ref FactReference) []string {
	switch v := f.Value.(type) {
	case model.Service:
		switch ref.Field {
		case "appId":
			if v.AppID != "" {
				return []string{v.AppID}
			}
		case "domains[].rootDomain":
			out := make([]string, 0, len(v.Domains))
			for _, d := range v.Domains {
				if d.RootDomain != "" {
					out = append(out, d.RootDomain)
				}
			}
			return out
		case "fragmentIds[]":
			return v.FragmentIDs
		}
	case model.Domain:
		if ref.Field == "credentialId" && v.CredentialID != "" {
			return []string{v.CredentialID}
		}
	}
	return nil
}

// buildEdges 类型骨架（produces × consumes）+ 实例绑定。
func buildEdges(snap Snapshot, byRef map[FactRef]Fact) []Edge {
	producers := map[component.InfoType][]string{}
	consumers := map[component.InfoType][]string{}
	for _, d := range snap.Desc {
		for _, p := range d.Produces {
			if p.Info != "" {
				producers[p.Info] = append(producers[p.Info], d.ID)
			}
		}
		for _, c := range d.Consumes {
			if c.Info != "" {
				consumers[c.Info] = append(consumers[c.Info], d.ID)
			}
		}
	}
	out := make([]Edge, 0)
	for info, ps := range producers {
		cs := consumers[info]
		if len(cs) == 0 {
			continue
		}
		for _, p := range ps {
			inst := instancesOf(snap.Facts, info, p)
			for _, c := range cs {
				if p == c {
					continue
				}
				out = append(out, Edge{From: p, To: c, Info: info, Instances: inst})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].Info < out[j].Info
	})
	return out
}

func instancesOf(facts []Fact, info component.InfoType, origin string) []FactRef {
	out := make([]FactRef, 0)
	for _, f := range facts {
		if f.Info == info && (origin == "" || f.Origin == origin) {
			out = append(out, f.Ref)
		}
	}
	sort.Slice(out, func(i, j int) bool { return refLess(out[i], out[j]) })
	return out
}

func buildComponentNodes(snap Snapshot, byRef map[FactRef]Fact) []Node {
	out := make([]Node, 0, len(snap.Desc))
	for _, d := range snap.Desc {
		n := Node{ID: d.ID, Kind: "component", Label: d.Name, Tier: d.Tier}
		for _, f := range d.Functions {
			n.Functions = append(n.Functions, f.ID)
		}
		sort.Strings(n.Functions)
		for _, c := range d.Consumes {
			if c.Info == "" {
				continue
			}
			total, origins := countOf(snap.Facts, c.Info, "")
			n.Consumes = append(n.Consumes, InfoCount{Info: c.Info, Count: total, Origins: origins})
		}
		for _, p := range d.Produces {
			if p.Info == "" {
				continue
			}
			total, origins := countOf(snap.Facts, p.Info, d.ID)
			n.Produces = append(n.Produces, InfoCount{Info: p.Info, Count: total, Origins: origins})
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func countOf(facts []Fact, info component.InfoType, origin string) (int, map[string]int) {
	total := 0
	origins := map[string]int{}
	for _, f := range facts {
		if f.Info != info {
			continue
		}
		if origin != "" && f.Origin != origin {
			continue
		}
		total++
		origins[f.Origin]++
	}
	if len(origins) == 0 {
		return total, nil
	}
	return total, origins
}

// buildFunctionNodes 由组件节点聚合为功能节点。
func buildFunctionNodes(snap Snapshot, edges []Edge) []Node {
	type agg struct {
		label        string
		implementors map[string]bool
		consumes     map[component.InfoType]bool
		produces     map[component.InfoType]bool
	}
	byFunc := map[string]*agg{}
	byComp := map[string]component.Descriptor{}
	for _, d := range snap.Desc {
		byComp[d.ID] = d
		for _, f := range d.Functions {
			a := byFunc[f.ID]
			if a == nil {
				label := f.Label
				if label == "" {
					label = f.ID
				}
				a = &agg{label: label, implementors: map[string]bool{}, consumes: map[component.InfoType]bool{}, produces: map[component.InfoType]bool{}}
				byFunc[f.ID] = a
			}
			a.implementors[d.ID] = true
			for _, c := range d.Consumes {
				if c.Info != "" {
					a.consumes[c.Info] = true
				}
			}
			for _, p := range d.Produces {
				if p.Info != "" {
					a.produces[p.Info] = true
				}
			}
		}
	}
	out := make([]Node, 0, len(byFunc))
	for id, a := range byFunc {
		n := Node{ID: id, Kind: "function", Label: a.label}
		for c := range a.implementors {
			n.Implementors = append(n.Implementors, c)
		}
		sort.Strings(n.Implementors)
		for info := range a.consumes {
			n.Consumes = append(n.Consumes, InfoCount{Info: info})
		}
		for info := range a.produces {
			n.Produces = append(n.Produces, InfoCount{Info: info})
		}
		sort.Slice(n.Consumes, func(i, j int) bool { return n.Consumes[i].Info < n.Consumes[j].Info })
		sort.Slice(n.Produces, func(i, j int) bool { return n.Produces[i].Info < n.Produces[j].Info })
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// buildFunctionEdges 把组件边展开为功能边。
func buildFunctionEdges(snap Snapshot, edges []Edge) []Edge {
	byComp := map[string]component.Descriptor{}
	for _, d := range snap.Desc {
		byComp[d.ID] = d
	}
	out := make([]Edge, 0)
	for _, e := range edges {
		from := byComp[e.From]
		to := byComp[e.To]
		for _, ff := range from.Functions {
			for _, tf := range to.Functions {
				out = append(out, Edge{From: ff.ID, To: tf.ID, Info: e.Info, Instances: e.Instances})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].Info < out[j].Info
	})
	return out
}
