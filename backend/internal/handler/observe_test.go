package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/observe"
	"github.com/JiangBeta/gatebox/internal/reconcile"
	"github.com/JiangBeta/gatebox/internal/repository"
)

func newObserveMux(t *testing.T) (*http.ServeMux, *observe.Bus) {
	t.Helper()
	repo, err := repository.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := component.NewCoreRegistry(t.TempDir(), "/tmp/caddy", "http://127.0.0.1:2019", nil)
	bus := observe.NewBus(16)
	mux := http.NewServeMux()
	RegisterObserve(mux, bus, reg, repo, reconcile.NewBoltRunStore(repo), nil, t.TempDir())
	return mux, bus
}

func TestObserveActivityAndRuns(t *testing.T) {
	mux, _ := newObserveMux(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/components/caddy/activity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("activity status=%d", rec.Code)
	}
	var act component.Activity
	if err := json.Unmarshal(rec.Body.Bytes(), &act); err != nil {
		t.Fatal(err)
	}
	if act.Task != "idle" {
		t.Fatalf("want idle, got %+v", act)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("runs status=%d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("want empty runs, got %s", rec.Body.String())
	}
}

func TestEventsSSE(t *testing.T) {
	mux, bus := newObserveMux(t)
	bus.Publish(observe.KindState, "caddy", "", map[string]any{"state": "running"})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type=%s", ct)
	}
	buf := make([]byte, 256)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), "event: state") {
		t.Fatalf("no state event in SSE: %q", string(buf[:n]))
	}
}
