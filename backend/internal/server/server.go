// Package server 装配 HTTP 处理器:API + 内嵌前端 + CORS。
package server

import (
	"io/fs"
	"net/http"

	"github.com/JiangBeta/gatebox/internal/api"
	"github.com/JiangBeta/gatebox/internal/cert"
	"github.com/JiangBeta/gatebox/internal/docker/client"
	"github.com/JiangBeta/gatebox/internal/docker/stats"
	"github.com/JiangBeta/gatebox/internal/store"
	"github.com/JiangBeta/gatebox/internal/web"
)

// New 构造完整 HTTP handler。
func New(s *store.Store, cm cert.CertManager, dc *client.Client, coll *stats.Collector, daemonJSON, dataDir string) http.Handler {
	mux := http.NewServeMux()
	api.Register(mux, s, cm)
	if dc != nil {
		api.RegisterDocker(mux, dc, coll, s, daemonJSON, dataDir)
	}

	// 内嵌前端(若已构建);缺失时仅提供 API。
	if distFS, err := fs.Sub(web.Dist, "dist"); err == nil {
		mux.Handle("/", http.FileServer(http.FS(distFS)))
	}

	return withCORS(mux)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket 升级请求不能被 CORS 中间件加工:Accept 会接管连接,
		// 提前写入响应头会破坏握手。
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
