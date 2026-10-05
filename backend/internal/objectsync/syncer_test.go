package objectsync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/objects"
	"github.com/JiangBeta/gatebox/internal/observe"
	"github.com/JiangBeta/gatebox/internal/reconcile"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

type fakeLoader struct {
	mu       sync.Mutex
	got      string
	calls    int
	err      error
	validate string
	verr     error
	// gate 非 nil 时 Load 阻塞到该 channel 关闭（用于并发测试）。
	gate chan struct{}
}

func (f *fakeLoader) block(ch chan struct{}) {
	f.mu.Lock()
	f.gate = ch
	f.mu.Unlock()
}

func (f *fakeLoader) unblock() {
	f.mu.Lock()
	g := f.gate
	f.gate = nil
	f.mu.Unlock()
	if g != nil {
		select {
		case <-g:
		default:
			close(g)
		}
	}
}

func (f *fakeLoader) Load(_ context.Context, caddyfile string) error {
	f.mu.Lock()
	gate, err, caddyfile := f.gate, f.err, caddyfile
	if gate != nil {
		f.calls++
		f.mu.Unlock()
		<-gate
		f.mu.Lock()
		f.got = caddyfile
		f.mu.Unlock()
		return err
	}
	f.calls++
	f.got = caddyfile
	f.mu.Unlock()
	return err
}

func (f *fakeLoader) Validate(_ context.Context, caddyfile string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.validate = caddyfile
	return f.verr
}

type memRuns struct {
	mu   sync.Mutex
	runs []reconcile.Run
}

func (m *memRuns) Save(r reconcile.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs = append(m.runs, r)
	return nil
}
func (m *memRuns) Get(string) (reconcile.Run, error) { return reconcile.Run{}, nil }
func (m *memRuns) List(int) ([]reconcile.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs, nil
}

func newSyncer(t *testing.T) (*Syncer, *objects.Service, *fakeLoader, *memRuns) {
	t.Helper()
	dir := t.TempDir()
	repo, err := repository.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	specs, err := typespec.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc := objects.New(repo, specs).WithSecrets(repo)
	loader := &fakeLoader{}
	runs := &memRuns{}
	bus := observe.NewBus(16)
	s := New(svc, loader, loader, runs, bus, Options{Debounce: 10 * time.Millisecond})
	return s, svc, loader, runs
}

// seed 建一条可渲染的最小链路：入口点 + 域名 + 路由。
func seed(t *testing.T, svc *objects.Service, host string, port float64, enabled bool) objects.Object {
	t.Helper()
	mustCreate(t, svc, typespec.KindEntrypoint, map[string]any{
		"protocol": "https", "ports": []any{443.0}, "network": "tcp", "enabled": true})
	mustCreate(t, svc, typespec.KindDomain, map[string]any{"name": "neob.cn"})
	return mustCreate(t, svc, typespec.KindRoute, map[string]any{
		"name": "aria", "subdomain": "aria", "roots": []any{"neob-cn"},
		"entrypoint": "https", "tls": "auto", "enabled": enabled,
		"backend": map[string]any{"host": host, "port": port}})
}

func mustCreate(t *testing.T, svc *objects.Service, kind string, m map[string]any) objects.Object {
	t.Helper()
	o, err := svc.Create(kind, m)
	if err != nil {
		t.Fatalf("创建 %s 失败: %v", kind, err)
	}
	return o
}

func TestSyncAppliesAndMarksStatus(t *testing.T) {
	s, svc, loader, runs := newSyncer(t)
	rt := seed(t, svc, "192.168.1.20", 6880, true)

	run, err := s.SyncNow(context.Background(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "success" {
		t.Fatalf("run 状态 %s: %+v", run.State, run.Steps)
	}
	if loader.calls != 1 {
		t.Errorf("应调用一次 /load，实际 %d", loader.calls)
	}
	if !strings.Contains(loader.got, "reverse_proxy 192.168.1.20:6880") {
		t.Errorf("配置未含上游:\n%s", loader.got)
	}
	if !strings.Contains(loader.validate, "aria.neob.cn") {
		t.Errorf("前置校验应收到同一份配置:\n%s", loader.validate)
	}
	// 步骤齐全且顺序正确
	wantActions := []string{"snapshot", "render", "validate", "load", "status"}
	if len(run.Steps) != len(wantActions) {
		t.Fatalf("步骤数 %d，期望 %d: %+v", len(run.Steps), len(wantActions), run.Steps)
	}
	for i, want := range wantActions {
		if run.Steps[i].Action != want {
			t.Errorf("步骤 %d = %q，期望 %q", i, run.Steps[i].Action, want)
		}
		if run.Steps[i].State != "success" {
			t.Errorf("步骤 %q 状态 %q", want, run.Steps[i].State)
		}
	}
	// 对象状态回写
	got, err := svc.Get(typespec.KindRoute, rt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status["state"] != objects.StateApplied {
		t.Errorf("状态应回写为已生效: %v", got.Status)
	}
	if _, leaked := got.Status["pendingOp"]; leaked {
		t.Errorf("pendingOp 应被清掉: %v", got.Status)
	}
	// Run 已落库
	if len(runs.runs) != 1 {
		t.Errorf("Run 应落库 1 条，实际 %d", len(runs.runs))
	}
}

func TestSyncDisabledRouteStaysDisabled(t *testing.T) {
	s, svc, loader, _ := newSyncer(t)
	rt := seed(t, svc, "h", 1, false)

	run, err := s.SyncNow(context.Background(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "success" {
		t.Fatalf("停用不该让 run 失败: %+v", run.Steps)
	}
	if strings.Contains(loader.got, "aria.neob.cn") {
		t.Errorf("停用路由不该渲染出站点:\n%s", loader.got)
	}
	got, _ := svc.Get(typespec.KindRoute, rt.ID)
	if got.Status["state"] != objects.StateDisabled {
		t.Errorf("状态应为已停用: %v", got.Status)
	}
	if got.Status["skipReason"] == nil {
		t.Errorf("应记录跳过原因: %v", got.Status)
	}
}

func TestSyncLoadFailureMarksError(t *testing.T) {
	s, svc, loader, _ := newSyncer(t)
	rt := seed(t, svc, "h", 1, true)
	loader.err = errors.New("caddy /load 失败(500): invalid config")

	run, err := s.SyncNow(context.Background(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "error" {
		t.Errorf("run 应为 error: %s", run.State)
	}
	last := run.Steps[len(run.Steps)-1]
	if last.Action != "load" || last.State != "error" {
		t.Errorf("末步应为 load/error: %+v", last)
	}
	if !strings.Contains(last.Detail, "invalid config") {
		t.Errorf("应保留 caddy 原文: %s", last.Detail)
	}
	got, _ := svc.Get(typespec.KindRoute, rt.ID)
	if got.Status["state"] != objects.StateError {
		t.Errorf("状态应为错误: %v", got.Status)
	}
	if !strings.Contains(got.Status["error"].(string), "invalid config") {
		t.Errorf("对象上应记下错误原因: %v", got.Status)
	}
}

func TestSyncValidateFailureBlocksLoad(t *testing.T) {
	s, svc, loader, _ := newSyncer(t)
	seed(t, svc, "h", 1, true)
	loader.verr = errors.New("caddy: 语法错误")

	run, _ := s.SyncNow(context.Background(), "manual")
	if run.State != "error" {
		t.Errorf("校验失败应 error: %s", run.State)
	}
	if loader.calls != 0 {
		t.Errorf("校验失败不应调用 /load，实际 %d 次", loader.calls)
	}
}

func TestSyncValidateUnavailableIsDegraded(t *testing.T) {
	s, svc, loader, _ := newSyncer(t)
	seed(t, svc, "h", 1, true)
	loader.verr = errors.New("caddy validate 不可用: exec: \"caddy\": executable file not found in $PATH")

	run, err := s.SyncNow(context.Background(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "success" {
		t.Errorf("校验不可用应放行（降级），实际 %s: %+v", run.State, run.Steps)
	}
	var degraded bool
	for _, st := range run.Steps {
		if st.Action == "validate" {
			degraded = st.State == "degraded"
		}
	}
	if !degraded {
		t.Errorf("validate 步应为 degraded: %+v", run.Steps)
	}
	if loader.calls != 1 {
		t.Errorf("降级后仍应 /load，实际 %d 次", loader.calls)
	}
}

func TestSyncRenderErrorFailsBeforeValidate(t *testing.T) {
	s, svc, loader, _ := newSyncer(t)
	// rewrite.body 属目录内类型但渲染器未覆盖（ADR-043 §4 边界）→ render 步失败
	mustCreate(t, svc, typespec.KindEntrypoint, map[string]any{
		"protocol": "https", "ports": []any{443.0}, "network": "tcp", "enabled": true})
	mustCreate(t, svc, typespec.KindMiddleware, map[string]any{
		"name": "rw", "type": "rewrite",
		"body": map[string]any{"method": "replace", "from": "a", "to": "b"}})
	mustCreate(t, svc, typespec.KindRoute, map[string]any{
		"name": "aria", "subdomain": "aria", "entrypoint": "https", "enabled": true,
		"middlewares": []any{"rw"},
		"backend":     map[string]any{"host": "h", "port": 1.0}})

	run, _ := s.SyncNow(context.Background(), "manual")
	if run.State != "error" {
		t.Fatalf("应 error: %+v", run.Steps)
	}
	if run.Steps[len(run.Steps)-1].Action != "render" {
		t.Errorf("应在 render 步失败: %+v", run.Steps)
	}
	if !strings.Contains(run.Steps[len(run.Steps)-1].Detail, "rewrite.body") {
		t.Errorf("应说明未覆盖的原因: %s", run.Steps[len(run.Steps)-1].Detail)
	}
	if loader.calls != 0 {
		t.Errorf("渲染失败不应触碰 caddy")
	}
	// 失败也应把对象标成「错误」，而不是留在「未生效」
	got, _ := svc.Get(typespec.KindRoute, "aria")
	if got.Status["state"] != objects.StateError {
		t.Errorf("状态应为错误: %v", got.Status)
	}
}

func TestMarkPendingSetsStateAndDrains(t *testing.T) {
	s, svc, loader, _ := newSyncer(t)
	rt := seed(t, svc, "h", 1, true)

	s.MarkPending(context.Background(), typespec.KindRoute, rt.ID, "update")
	got, _ := svc.Get(typespec.KindRoute, rt.ID)
	if got.Status["state"] != objects.StatePending {
		t.Fatalf("应先标未生效: %v", got.Status)
	}
	if got.Status["pendingOp"] != "update" {
		t.Errorf("应记下待生效操作: %v", got.Status)
	}
	if s.PendingCount() != 1 {
		t.Errorf("待生效计数应为 1，实际 %d", s.PendingCount())
	}

	if _, err := s.SyncNow(context.Background(), "intent"); err != nil {
		t.Fatal(err)
	}
	if s.PendingCount() != 0 {
		t.Errorf("同步后待生效应清空，实际 %d", s.PendingCount())
	}
	got, _ = svc.Get(typespec.KindRoute, rt.ID)
	if got.Status["state"] != objects.StateApplied {
		t.Errorf("同步后应为已生效: %v", got.Status)
	}
	// pendingOp 必须真被清掉：只「加字段」的合并会让界面一直显示待生效。
	if _, still := got.Status["pendingOp"]; still {
		t.Errorf("pendingOp 应被消费掉: %v", got.Status)
	}
	if loader.calls != 1 {
		t.Errorf("应 /load 一次，实际 %d", loader.calls)
	}
}

func TestWriteBackClearsPreviousErrorAndSkip(t *testing.T) {
	s, svc, loader, _ := newSyncer(t)
	rt := seed(t, svc, "h", 1, true)

	// 先攒上一轮的脏状态：错误 + 跳过原因 + 待生效。
	_ = svc.SetStatus(typespec.KindRoute, rt.ID, map[string]any{
		"error": "上一轮渲染炸了", "skipReason": "上一轮被跳过",
	})
	s.MarkPending(context.Background(), typespec.KindRoute, rt.ID, "update")

	if _, err := s.SyncNow(context.Background(), "intent"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(typespec.KindRoute, rt.ID)
	if got.Status["state"] != objects.StateApplied {
		t.Fatalf("本轮成功应为已生效: %v", got.Status)
	}
	if _, still := got.Status["error"]; still {
		t.Errorf("上一轮的错误应被清除: %v", got.Status)
	}
	if _, still := got.Status["skipReason"]; still {
		t.Errorf("上一轮的跳过原因应被清除: %v", got.Status)
	}
	if _, still := got.Status["pendingOp"]; still {
		t.Errorf("pendingOp 应被清除: %v", got.Status)
	}

	// 反向：这一轮失败要写上 error，且不残留 pendingOp。
	loader.failLoad()
	s.MarkPending(context.Background(), typespec.KindRoute, rt.ID, "update")
	if _, err := s.SyncNow(context.Background(), "intent"); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Get(typespec.KindRoute, rt.ID)
	if got.Status["state"] != objects.StateError {
		t.Errorf("失败应标错误: %v", got.Status)
	}
	if got.Status["error"] == nil || got.Status["error"] == "" {
		t.Errorf("失败原因应记录: %v", got.Status)
	}
	if _, still := got.Status["skipReason"]; still {
		t.Errorf("失败轮次不该残留跳过原因: %v", got.Status)
	}
}

func TestMarkPendingOnDeletedObjectIsHarmless(t *testing.T) {
	s, _, _, _ := newSyncer(t)
	// 对象不存在（刚被删）不该 panic，也不该让状态写入冒泡
	s.MarkPending(context.Background(), typespec.KindRoute, "ghost", "delete")
	if s.PendingCount() != 1 {
		t.Errorf("仍应记入待生效批次（下次同步会自然落空）")
	}
	if _, err := s.SyncNow(context.Background(), "intent"); err != nil {
		t.Fatal(err)
	}
}

func TestSyncConcurrentCallRejected(t *testing.T) {
	s, svc, loader, _ := newSyncer(t)
	seed(t, svc, "h", 1, true)
	release := make(chan struct{})
	loader.block(release)
	defer loader.unblock()

	done := make(chan struct{})
	go func() {
		_, _ = s.SyncNow(context.Background(), "manual")
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	if _, err := s.SyncNow(context.Background(), "manual"); err == nil {
		t.Error("并发第二次同步应被拒")
	}
	loader.unblock()
	<-done
}

// failLoad 让后续 Load 一律失败（用于验证失败回写路径）。
func (f *fakeLoader) failLoad() {
	f.mu.Lock()
	f.err = errors.New("加载炸了")
	f.mu.Unlock()
}
