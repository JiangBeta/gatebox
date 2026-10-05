package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/reconcile"
)

func TestSystemInfoReturnsVersionAndRestartSemantics(t *testing.T) {
	mux := http.NewServeMux()
	RegisterSystem(mux, "4.1.0", "/nonexistent-caddy", nil, nil)

	req := httptest.NewRequest("GET", "/api/v1/system", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 想要 200", rec.Code)
	}
	var got SystemInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if got.Version != "4.1.0" {
		t.Errorf("version = %q, 想要 4.1.0", got.Version)
	}
	if !got.Online {
		t.Error("online = false, 能返回响应就应为 true")
	}
	if !got.Restart.Graceful {
		t.Error("restart.graceful = false, 新版语义应为优雅停止")
	}
	if got.Restart.Estimate == "" {
		t.Error("restart.estimate 为空, 重启确认框需要它")
	}
	if got.Containers != nil {
		t.Errorf("containers = %+v, docker 不可用时应为 null 让前端显示不可达", got.Containers)
	}
}

// TestSystemInfoNoLatestVersion 守住「没有可信版本源就不编数字」。
// 顶栏「升级」角标依赖 latestVersion，编一个等于让角标永远为假。
func TestSystemInfoNoLatestVersion(t *testing.T) {
	body := mustJSON(t, SystemInfo{Version: "dev"})
	for _, k := range []string{"latestVersion", "upgradeNotes"} {
		if strings.Contains(body, k) {
			t.Errorf("响应含 %q: 升级通道未开放, 不应编造版本信息", k)
		}
	}
}

func TestCountRunningCountsOnlyRunning(t *testing.T) {
	st := &fakeRuns{runs: []reconcile.Run{
		{ID: "1", State: "running"},
		{ID: "2", State: "succeeded"},
		{ID: "3", State: "running"},
		{ID: "4", State: "failed"},
	}}
	if got := countRunning(st); got != 2 {
		t.Errorf("countRunning = %d, 想要 2", got)
	}
	if got := countRunning(&fakeRuns{err: errFakeRuns}); got != 0 {
		t.Errorf("List 出错时 countRunning = %d, 想要 0", got)
	}
}

// --- 桩 ---

type fakeRuns struct {
	runs []reconcile.Run
	err  error
}

func (f *fakeRuns) Save(reconcile.Run) error          { return nil }
func (f *fakeRuns) Get(string) (reconcile.Run, error) { return reconcile.Run{}, nil }
func (f *fakeRuns) List(int) ([]reconcile.Run, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.runs, nil
}

var errFakeRuns = &stubErr{"list failed"}

type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化: %v", err)
	}
	return string(b)
}

func TestLocalIPSkipsLoopback(t *testing.T) {
	ip := LocalIP()
	if ip == "" {
		t.Skip("本机无非回环 IPv4, 跳过")
	}
	if strings.HasPrefix(ip, "127.") {
		t.Errorf("LocalIP = %q, 不应返回回环地址", ip)
	}
}
