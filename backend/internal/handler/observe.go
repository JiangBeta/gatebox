package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/observe"
	"github.com/JiangBeta/gatebox/internal/reconcile"
	"github.com/JiangBeta/gatebox/internal/repository"
)

// RegisterObserve 注册 v4 观测与调和端点（ADR-041 §7/§8）。
func RegisterObserve(mux *http.ServeMux, bus *observe.Bus, reg *component.CoreRegistry, store *repository.Store, runs reconcile.RunStore, rec *reconcile.Reconciler) {
	// SSE：观测事件流（state/activity/run）。
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		serveEvents(w, r, bus)
	})

	// 组件级观测。
	mux.HandleFunc("GET /api/v1/components/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		info, ok := reg.Get(r.Context(), r.PathValue("id"))
		if !ok {
			writeErrCode(w, http.StatusNotFound, "COMPONENT_NOT_FOUND", "组件不存在")
			return
		}
		writeJSON(w, http.StatusOK, info.Status)
	})
	mux.HandleFunc("GET /api/v1/components/{id}/activity", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, activityOf(r.Context(), store, r.PathValue("id")))
	})
	mux.HandleFunc("GET /api/v1/components/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		// L2 占位：返回空文本流；后续接组件日志来源。
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(""))
	})

	// 运行记录（只读）。
	mux.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		limit := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit = n
			}
		}
		items, err := runs.List(limit)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		run, err := runs.Get(r.PathValue("id"))
		if err != nil {
			writeErrCode(w, http.StatusNotFound, "RUN_NOT_FOUND", "运行记录不存在")
			return
		}
		writeJSON(w, http.StatusOK, run)
	})

	// 手动全量调和。
	if rec != nil {
		mux.HandleFunc("POST /api/v1/reconcile", func(w http.ResponseWriter, r *http.Request) {
			run, err := rec.Run(r.Context(), "manual")
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"runId": run.ID, "state": run.State})
		})
	}
}

// ObserveProviders 构造组件级观测来源（state 用现有探测，activity 复用 CertLog）。
func ObserveProviders(reg *component.CoreRegistry, store *repository.Store) []observe.Provider {
	out := make([]observe.Provider, 0)
	for _, d := range reg.Descriptors() {
		id := d.ID
		out = append(out, observe.Provider{
			ID: id,
			Status: func(ctx context.Context) (component.Status, error) {
				info, ok := reg.Get(ctx, id)
				if !ok {
					return component.Status{State: "unknown"}, nil
				}
				return info.Status, nil
			},
			Activity: func(ctx context.Context) (component.Activity, error) {
				return activityOf(ctx, store, id), nil
			},
		})
	}
	return out
}

// activityOf 返回组件当前活动。acme 复用证书日志（首个「执行到哪一步」样板）。
func activityOf(_ context.Context, store *repository.Store, id string) component.Activity {
	if id == "acme" && store != nil {
		if logs, err := store.ListCertLogs(1); err == nil && len(logs) > 0 {
			l := logs[0]
			return component.Activity{
				Task:  "cert-" + l.Action,
				Step:  l.Status,
				Actor: "acme",
				Since: l.CreatedAt.Unix(),
			}
		}
	}
	return component.Activity{Task: "idle"}
}

// serveEvents 以 SSE 推送观测事件：先回放 Last-Event-ID 之后的事件，再持续推送。
func serveEvents(w http.ResponseWriter, r *http.Request, bus *observe.Bus) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	lastID := uint64(0)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			lastID = n
		}
	}
	for _, ev := range bus.Recent() {
		if ev.Seq > lastID {
			writeSSE(w, ev)
		}
	}
	flusher.Flush()

	ch, cancel := bus.Subscribe(64)
	defer cancel()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			writeSSE(w, ev)
			flusher.Flush()
		case <-ping.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev observe.Event) {
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Kind, string(ev.Data))
}
