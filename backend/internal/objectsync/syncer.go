// Package objectsync 实现 V4.1 对象 → Caddy 的生效链路（ADR-043 §4）。
//
//	保存（落库 + status.state=未生效）→ MarkPending → 同步器（去抖）→
//	建 Run → 取对象快照 → 渲染最小 Caddyfile → caddy validate → Admin /load →
//	回写每个对象的 status.state（已生效 / 错误 / 已停用）→ 发 SSE
//
// 与旧的 component.Reconciler 并存但互不依赖：旧链路服务旧对象（model.Service 等），
// 本链路服务 V4.1 对象（objects.Object）。等迁移执行、验收通过后再决定是否合并。
package objectsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/caddyrender"
	"github.com/JiangBeta/gatebox/internal/objects"
	"github.com/JiangBeta/gatebox/internal/observe"
	"github.com/JiangBeta/gatebox/internal/reconcile"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

// Loader 是 caddy 侧的应用能力（生产实现 = *caddy.Client）。
type Loader interface {
	// Load 通过 Admin API 原子应用 Caddyfile。
	Load(ctx context.Context, caddyfile string) error
}

// Validator 是可选的前置校验（生产实现 = caddy.Validate，二进制缺失时返回
// reconcile.ErrValidateUnavailable 之类的不可用错误 → 降级跳过而非硬失败）。
type Validator interface {
	Validate(ctx context.Context, caddyfile string) error
}

// Options 同步器配置。
type Options struct {
	// Render caddyrender.Options（DataDir / 端口覆盖 / 扩展全局片段）。
	Render caddyrender.Options
	// Debounce 合并窗口：连续保存多个对象只跑一次同步。<=0 取 500ms。
	Debounce time.Duration
	// Period 周期兜底间隔（对齐一次外部漂移）。<=0 关闭。
	Period time.Duration
}

// Syncer 对象生效同步器。
type Syncer struct {
	objs      *objects.Service
	loader    Loader
	validator Validator
	runs      reconcile.RunStore
	bus       *observe.Bus
	opt       Options

	mu      sync.Mutex
	pending map[string]struct{} // 待生效对象 "<kind>/<id>"
	// lastCaddyfile 上一次成功应用的配置（用于判断是否真的变了 / 供 diff 用）。
	lastCaddyfile string

	wake chan struct{}
	done chan struct{}
	// running 防止并发跑两次（去抖唤醒与周期兜底可能撞车）。
	running bool
}

// New 构造同步器。
func New(objs *objects.Service, loader Loader, validator Validator, runs reconcile.RunStore, bus *observe.Bus, opt Options) *Syncer {
	if opt.Debounce <= 0 {
		opt.Debounce = 500 * time.Millisecond
	}
	return &Syncer{
		objs:      objs,
		loader:    loader,
		validator: validator,
		runs:      runs,
		bus:       bus,
		opt:       opt,
		pending:   map[string]struct{}{},
		wake:      make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
}

// Start 启动后台循环（去抖 + 周期兜底）。ctx 结束时停止。
func (s *Syncer) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Syncer) loop(ctx context.Context) {
	defer close(s.done)
	var tick <-chan time.Time
	if s.opt.Period > 0 {
		t := time.NewTicker(s.opt.Period)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			// 去抖：等一小会儿，让连着来的几个保存合并成一次。
			if !s.sleepCtx(ctx, s.opt.Debounce) {
				return
			}
			if !s.DrainPending(ctx) {
				return
			}
		case <-tick:
			// 周期兜底：即使没人保存，也对齐一次外部漂移。
			if _, err := s.SyncNow(ctx, "periodic"); err != nil {
				s.publish("error", "sync", "", map[string]any{"error": err.Error()})
			}
		}
	}
}

func (s *Syncer) sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// MarkPending 由对象 API 的写后钩子调用（ADR-043 §4）：
// 先把该对象标成「未生效」，再唤醒同步器。
func (s *Syncer) MarkPending(ctx context.Context, kind, id, op string) {
	if s.objs == nil {
		return
	}
	// 对象可能已被删（op=delete），标状态会失败——那不是错误，删了就不该再有状态。
	_ = s.objs.SetStatus(kind, id, map[string]any{"state": objects.StatePending, "pendingOp": op})
	s.mu.Lock()
	s.pending[kind+"/"+id] = struct{}{}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default: // 已有一次唤醒在排队
	}
}

// DrainPending 消费待生效集合并跑一次同步。返回 false 表示 ctx 已取消。
func (s *Syncer) DrainPending(ctx context.Context) bool {
	s.mu.Lock()
	n := len(s.pending)
	s.mu.Unlock()
	if n == 0 {
		return true
	}
	if _, err := s.SyncNow(ctx, "intent"); err != nil {
		s.publish("error", "sync", "", map[string]any{"error": err.Error()})
	}
	return true
}

// PendingCount 当前待生效对象数（前端「有 N 项待生效」提示用）。
func (s *Syncer) PendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// SyncNow 立即跑一次完整生效链路，返回该次 Run。
func (s *Syncer) SyncNow(ctx context.Context, trigger string) (reconcile.Run, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return reconcile.Run{}, errors.New("同步已在进行中")
	}
	s.running = true
	// 取走待生效集合：本次要处理的对象。
	batch := make([]string, 0, len(s.pending))
	for k := range s.pending {
		batch = append(batch, k)
	}
	s.pending = map[string]struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	run := s.execute(ctx, trigger, batch)
	if s.runs != nil {
		if err := s.runs.Save(run); err != nil {
			return run, err
		}
	}
	return run, nil
}

// execute 按步骤跑链路，边跑边记录 Step。
func (s *Syncer) execute(ctx context.Context, trigger string, batch []string) reconcile.Run {
	now := time.Now()
	run := reconcile.Run{
		ID:        reconcile.NewRunID(now),
		Trigger:   trigger,
		Intent:    strings.Join(batch, ","),
		State:     "running",
		StartedAt: now.UnixMilli(),
	}
	s.publish("state", "sync", run.ID, map[string]any{"state": "running", "batch": batch})

	// 步骤 1：取对象快照。
	in, err := s.snapshot()
	if err != nil {
		return s.fail(run, "snapshot", err, nil)
	}
	run.Steps = append(run.Steps, step("sync", "snapshot", "success",
		fmt.Sprintf("路由 %d / 中间件 %d / 入口点 %d / 域名 %d / 用户 %d",
			len(in.Routes), len(in.Middlewares), len(in.Entrypoints), len(in.Domains), len(in.Users)),
		now))

	// 步骤 2：渲染。
	res, err := caddyrender.Render(in, s.opt.Render, s.objs)
	if err != nil {
		return s.fail(run, "render", err, in.Routes)
	}
	detail := fmt.Sprintf("%d 个站点", len(res.SiteAddrs))
	if len(res.Skipped) > 0 {
		detail += fmt.Sprintf("，跳过 %d", len(res.Skipped))
	}
	run.Steps = append(run.Steps, step("sync", "render", "success", detail, now))

	// 步骤 3：前置校验（不可用则降级，不硬失败）。
	if s.validator != nil {
		if err := s.validator.Validate(ctx, res.Caddyfile); err != nil {
			if isUnavailable(err) {
				run.Steps = append(run.Steps, step("caddy", "validate", "degraded",
					"caddy 二进制不可用，跳过前置校验", now))
			} else {
				return s.fail(run, "validate", err, in.Routes)
			}
		} else {
			run.Steps = append(run.Steps, step("caddy", "validate", "success", "配置校验通过", now))
		}
	}

	// 步骤 4：应用。
	if s.loader == nil {
		return s.fail(run, "load", errors.New("未配置 caddy loader"), in.Routes)
	}
	if err := s.loader.Load(ctx, res.Caddyfile); err != nil {
		return s.fail(run, "load", err, in.Routes)
	}
	run.Steps = append(run.Steps, step("caddy", "load", "success",
		fmt.Sprintf("已应用 %d 字节配置", len(res.Caddyfile)), now))

	s.mu.Lock()
	s.lastCaddyfile = res.Caddyfile
	s.mu.Unlock()

	// 步骤 5：回写状态。
	s.writeBack(in.Routes, res, "")
	run.Steps = append(run.Steps, step("sync", "status", "success", "已回写对象生效状态", now))

	run.State = "success"
	run.EndedAt = time.Now().UnixMilli()
	s.publish("state", "sync", run.ID, map[string]any{"state": run.State})
	return run
}

// writeBack 回写路由状态。errMsg 非空表示整体失败（所有路由标「错误」）。
func (s *Syncer) writeBack(routes []objects.Object, res caddyrender.Result, errMsg string) {
	skipped := map[string]string{}
	for _, sk := range res.Skipped {
		skipped[sk.RouteID] = sk.Reason
	}
	for _, rt := range routes {
		patch := map[string]any{}
		switch {
		case errMsg != "":
			patch["state"] = objects.StateError
			patch["error"] = errMsg
			patch["skipReason"] = nil
		default:
			// 不在跳过表里的路由都已渲染进 Caddyfile。
			if reason, ok := skipped[rt.ID]; ok {
				patch["state"] = objects.StateDisabled
				patch["skipReason"] = reason
			} else {
				patch["state"] = objects.StateApplied
			}
			// 本轮成功了，上一轮的错误/跳过原因必须跟着消失。
			patch["error"] = nil
			if _, ok := patch["skipReason"]; !ok {
				patch["skipReason"] = nil
			}
		}
		// pendingOp 消费掉（nil = 删除该键），否则界面会一直显示"待生效"。
		patch["pendingOp"] = nil
		_ = s.objs.SetStatus(rt.Kind, rt.ID, patch)
	}
}

// snapshot 取出渲染所需的对象快照。
func (s *Syncer) snapshot() (caddyrender.Input, error) {
	var in caddyrender.Input
	for _, kind := range []string{
		typespec.KindRoute, typespec.KindService, typespec.KindMiddleware,
		typespec.KindEntrypoint, typespec.KindDomain, typespec.KindUser,
	} {
		list, err := s.objs.List(kind)
		if err != nil {
			return in, fmt.Errorf("读取 %s 列表失败: %w", kind, err)
		}
		switch kind {
		case typespec.KindRoute:
			in.Routes = list
		case typespec.KindService:
			// 渲染要按服务类型分派（file_server 没有上游），所以服务必须进快照。
			in.Services = list
		case typespec.KindMiddleware:
			in.Middlewares = list
		case typespec.KindEntrypoint:
			in.Entrypoints = list
		case typespec.KindDomain:
			in.Domains = list
		case typespec.KindUser:
			in.Users = list
		}
	}
	return in, nil
}

// fail 记失败步并把相关对象标成「错误」（ADR-043 §4：失败回写 错误）。
// routes 为 nil（快照阶段就失败）时无从回写，跳过。
func (s *Syncer) fail(run reconcile.Run, action string, err error, routes []objects.Object) reconcile.Run {
	run.Steps = append(run.Steps, step("sync", action, "error", err.Error(), time.Now()))
	if routes != nil {
		s.writeBack(routes, caddyrender.Result{}, err.Error())
	}
	run.State = "error"
	run.EndedAt = time.Now().UnixMilli()
	s.publish("state", "sync", run.ID, map[string]any{"state": "error", "error": err.Error()})
	return run
}

func step(component, action, state, detail string, now time.Time) reconcile.Step {
	return reconcile.Step{
		Component: component,
		Action:    action,
		State:     state,
		Detail:    detail,
		StartedAt: now.UnixMilli(),
		EndedAt:   time.Now().UnixMilli(),
	}
}

// publish 发事件（bus 可为 nil：单测与迁移期不接 SSE）。
func (s *Syncer) publish(kind, component, runID string, data any) {
	if s.bus == nil {
		return
	}
	raw, _ := json.Marshal(data)
	s.bus.Publish(observe.Kind(kind), component, runID, raw)
}

// isUnavailable 判断错误是否属于「能力不可用」而非「配置非法」。
func isUnavailable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "不可用") ||
		strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "no such file or directory")
}
