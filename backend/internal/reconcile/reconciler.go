package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/graph"
	"github.com/JiangBeta/gatebox/internal/observe"
)

// SnapshotFunc 提供当前期望态（事实快照 + 描述符）。
type SnapshotFunc func(context.Context) graph.Snapshot

// Reconciler 声明式调和器：意图 → 消费闭包 → 拓扑排序 → 执行 → 记录。
type Reconciler struct {
	snapFn SnapshotFunc
	store  RunStore
	bus    *observe.Bus

	mu      sync.Mutex
	comps   map[string]component.Reconciler
	pending map[Intent]struct{}

	debounce time.Duration
	period   time.Duration

	wake chan struct{}
}

// New 构造调和器。
func New(snapFn SnapshotFunc, store RunStore, bus *observe.Bus) *Reconciler {
	return &Reconciler{
		snapFn:   snapFn,
		store:    store,
		bus:      bus,
		comps:    map[string]component.Reconciler{},
		pending:  map[Intent]struct{}{},
		debounce: 500 * time.Millisecond,
		period:   5 * time.Minute,
		wake:     make(chan struct{}, 1),
	}
}

// SetPeriod 设置周期兜底间隔（<=0 表示关闭周期兜底）。
func (r *Reconciler) SetPeriod(d time.Duration) {
	r.mu.Lock()
	r.period = d
	r.mu.Unlock()
}

// SetDebounce 设置意图去抖窗口。
func (r *Reconciler) SetDebounce(d time.Duration) {
	r.mu.Lock()
	if d > 0 {
		r.debounce = d
	}
	r.mu.Unlock()
}

// Register 注册一个组件的调和函数。
func (r *Reconciler) Register(id string, c component.Reconciler) {
	r.mu.Lock()
	r.comps[id] = c
	r.mu.Unlock()
}

// Trigger 提交意图（去抖合并）。
func (r *Reconciler) Trigger(i Intent) {
	r.mu.Lock()
	r.pending[i] = struct{}{}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Start 启动去抖触发循环与周期兜底，直到 ctx 结束。
func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		var ticker *time.Ticker
		var ticks <-chan time.Time
		if r.period > 0 {
			ticker = time.NewTicker(r.period)
			ticks = ticker.C
			defer ticker.Stop()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.wake:
				timer := time.NewTimer(r.debounce)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				_, _ = r.Run(ctx, "intent")
			case <-ticks:
				_, _ = r.Run(ctx, "periodic")
			}
		}
	}()
}

// Run 执行一次调和；trigger = intent | periodic | manual。
func (r *Reconciler) Run(ctx context.Context, trigger string) (Run, error) {
	if r.snapFn == nil {
		return Run{}, fmt.Errorf("reconcile: snapshot func 未配置")
	}
	if trigger == "" {
		trigger = "manual"
	}
	snap := r.snapFn(ctx)
	g := graph.Build(snap, "component")

	r.mu.Lock()
	pending := make([]Intent, 0, len(r.pending))
	for i := range r.pending {
		pending = append(pending, i)
	}
	r.pending = map[Intent]struct{}{}
	r.mu.Unlock()

	affected := affectedInfo(pending)
	ids := affectedComponents(snap.Desc, affected)

	run := Run{
		ID:        NewRunID(time.Now()),
		Trigger:   trigger,
		Intent:    describeIntents(pending),
		State:     "running",
		StartedAt: time.Now().Unix(),
	}
	if r.bus != nil {
		r.bus.Publish(observe.KindRun, "", run.ID, map[string]any{"state": "running", "trigger": trigger, "intent": run.Intent})
	}

	for _, id := range Order(ids, g.Edges) {
		c, ok := r.comps[id]
		if !ok {
			continue
		}
		step := r.runStep(ctx, snap, affected, id, c)
		run.Steps = append(run.Steps, step)
		if r.bus != nil {
			r.bus.Publish(observe.KindRun, id, run.ID, step)
		}
	}
	run.EndedAt = time.Now().Unix()
	run.State = runState(run.Steps)
	if r.store != nil {
		_ = r.store.Save(run)
	}
	if r.bus != nil {
		r.bus.Publish(observe.KindRun, "", run.ID, run)
	}
	return run, nil
}

func (r *Reconciler) runStep(ctx context.Context, snap graph.Snapshot, affected map[component.InfoType]bool, id string, c component.Reconciler) Step {
	step := Step{Component: id, Action: "reconcile", State: "running", StartedAt: time.Now().Unix()}
	in := component.ReconcileInput{Facts: FactViews(snap.Facts), Affected: infoList(affected)}
	res, err := c.Reconcile(ctx, in)
	step.EndedAt = time.Now().Unix()
	if err != nil {
		step.State = "error"
		step.Detail = err.Error()
		return step
	}
	switch res.State {
	case "success", "degraded", "skipped":
		step.State = res.State
	default:
		step.State = "success"
	}
	step.Detail = res.Detail
	step.Events = res.Events
	return step
}

// FactViews 把 graph.Fact 转为 component.FactView。
func FactViews(facts []graph.Fact) []component.FactView {
	out := make([]component.FactView, 0, len(facts))
	for _, f := range facts {
		out = append(out, component.FactView{Kind: f.Ref.Kind, ID: f.Ref.ID, Info: f.Info, Origin: f.Origin, Value: f.Value})
	}
	return out
}

// kindInfo 意图类别 → 受影响的信息类型（含派生关系）。
//
// service 携带域名行，故变更服务也影响 domain（acme 需要重签）；
// compose 产出 label→派生 service，故同样影响 service/domain。
var kindInfo = map[string][]component.InfoType{
	"service":    {component.InfoService, component.InfoDomain},
	"domain":     {component.InfoDomain},
	"credential": {component.InfoCredential},
	"fragment":   {component.InfoFragment},
	"variable":   {component.InfoVariable},
	"port":       {component.InfoPortBinding},
	"compose":    {component.InfoService, component.InfoDomain},
}

func affectedInfo(pending []Intent) map[component.InfoType]bool {
	out := map[component.InfoType]bool{}
	for _, i := range pending {
		// 空 kind = 全量意图（网关写操作）：合并时按全量处理。
		if i.Kind == "" {
			return map[component.InfoType]bool{}
		}
		if infos, ok := kindInfo[i.Kind]; ok {
			for _, info := range infos {
				out[info] = true
			}
			continue
		}
		if info := graph.InfoOf(i.Kind); info != "" {
			out[info] = true
		}
	}
	return out
}

// affectedComponents 求「消费了受影响信息类型」的组件（消费闭包）。
//
// 注意：不能只看图边——源事实（如 domain/credential）由用户产出，没有生产者组件，
// 因此对应的 info 类型不会出现在任何组件边上；须按描述符的 Consumes 判定。
func affectedComponents(desc []component.Descriptor, affected map[component.InfoType]bool) []string {
	all := make([]string, 0, len(desc))
	for _, d := range desc {
		all = append(all, d.ID)
	}
	if len(affected) == 0 {
		return all
	}
	out := make([]string, 0, len(desc))
	for _, d := range desc {
		for _, c := range d.Consumes {
			if affected[c.Info] {
				out = append(out, d.ID)
				break
			}
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

func infoList(m map[component.InfoType]bool) []component.InfoType {
	out := make([]component.InfoType, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func describeIntents(pending []Intent) string {
	if len(pending) == 0 {
		return "full"
	}
	parts := make([]string, 0, len(pending))
	for _, i := range pending {
		parts = append(parts, fmt.Sprintf("%s:%s", i.Kind, i.Op))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func runState(steps []Step) string {
	if len(steps) == 0 {
		return "success"
	}
	state := "success"
	for _, s := range steps {
		switch s.State {
		case "error":
			return "error"
		case "degraded":
			state = "degraded"
		}
	}
	return state
}
