package handler

import (
	"context"
	"net/http"

	"github.com/JiangBeta/gatebox/internal/objectsync"
)

// objectSync 是 V4.1 对象生效同步器（server 注入；为 nil 时对象写操作只落库不生效）。
var objectSync *objectsync.Syncer

// SetObjectSync 注入对象生效同步器，并把对象 API 的写后钩子接上
// （落库 → status.state=未生效 → 去抖同步，ADR-043 §4）。
func SetObjectSync(s *objectsync.Syncer) {
	objectSync = s
	afterObjectWrite = func(kind, id, op string) {
		if s == nil {
			return
		}
		// 用请求外的后台 ctx：写操作已返回，同步是异步的。
		s.MarkPending(context.Background(), kind, id, op)
	}
}

// RegisterObjectSync 注册同步器的运维端点。
//
//	GET  /api/v1/objects/sync/pending   → 当前待生效对象数
//	POST /api/v1/objects/sync           → 立即同步一次（不等去抖窗口）
func RegisterObjectSync(mux *http.ServeMux, s *objectsync.Syncer) {
	mux.HandleFunc("GET /api/v1/objects/sync/pending", func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			writeJSON(w, http.StatusOK, map[string]any{"pending": 0, "enabled": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"pending": s.PendingCount(), "enabled": true})
	})
	mux.HandleFunc("POST /api/v1/objects/sync", func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "SYNC_DISABLED", "对象生效链路未启用")
			return
		}
		run, err := s.SyncNow(r.Context(), "manual")
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, run)
	})
}
