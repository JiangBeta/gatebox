// Package server 装配 HTTP 处理器:API + 内嵌前端 + CORS。
package server

import (
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/acme"
	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
	"github.com/JiangBeta/gatebox/internal/adapter/cert"
	"github.com/JiangBeta/gatebox/internal/adapter/docker/client"
	"github.com/JiangBeta/gatebox/internal/adapter/docker/stats"
	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/gateway"
	"github.com/JiangBeta/gatebox/internal/handler"
	"github.com/JiangBeta/gatebox/internal/plugin"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/web"
)

// New 构造完整 HTTP handler。
func New(s *repository.Store, cm cert.CertManager, dc *client.Client, coll *stats.Collector, caddyCli *caddy.Client, health *gateway.HealthCollector, daemonJSON, dataDir, caddyBin, staticRoot string, httpPort, httpsPort int, extraHTTPSPorts []int, ac *acme.Issuer, reg *component.CoreRegistry, mgr *plugin.Manager, ext *extension.Registry) http.Handler {
	mux := http.NewServeMux()
	apiH := handler.Register(mux, s, cm, ac, ext)
	gw := handler.RegisterGateway(mux, s, dc, caddyCli, health, dataDir, caddyBin, staticRoot, httpPort, httpsPort, extraHTTPSPorts, ac, ext)
	// 域名页二级域名统计需含 docker 派生(编排)服务。
	apiH.SetExtraServices(gw.DerivedServices)
	if dc != nil {
		// docker 单位回调 = 网关派生+重载(ADR-026 §7:编排动作自动同步/手动「同步到网关」)。
		handler.RegisterDocker(mux, dc, coll, s, daemonJSON, dataDir, gw.SyncDockerLabels)
	}
	// 组件运行时 + 插件 + 扩展平台（v3）。
	handler.RegisterComponents(mux, reg, mgr, ext)
	// 插件投影 API（token 鉴权 + scope + 长轮询，ADR-039 §2）。
	handler.RegisterProjection(mux, mgr, s, ext, gw.DerivedServices)

	// 内嵌前端(若已构建);缺失时仅提供 API。
	if distFS, err := fs.Sub(web.Dist, "dist"); err == nil {
		mux.Handle("/", http.FileServer(http.FS(distFS)))
	}

	return withAccessLog(withCORS(mux))
}

// withAccessLog 请求访问日志(受 GATEBOX_ACCESS_LOG=1 门控;WS 经 CORS 内层先走,
// 日志层仅包普通 HTTP,不影响 hijack)。
func withAccessLog(next http.Handler) http.Handler {
	if os.Getenv("GATEBOX_ACCESS_LOG") != "1" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		log.Printf("access %s %s → %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
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
