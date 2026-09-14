// GateBox 控制面入口。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/acme"
	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
	"github.com/JiangBeta/gatebox/internal/adapter/cert"
	dockerclient "github.com/JiangBeta/gatebox/internal/adapter/docker/client"
	"github.com/JiangBeta/gatebox/internal/adapter/docker/stats"
	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/config"
	"github.com/JiangBeta/gatebox/internal/gateway"
	"github.com/JiangBeta/gatebox/internal/plugin"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/server"
	"github.com/JiangBeta/gatebox/internal/source"
)

func main() {
	cfg := config.Load()

	st, err := repository.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("打开存储失败: %v", err)
	}
	defer st.Close()

	// 证书由独立 acme.sh 签发(DNS-01),落盘于 $DATA_DIR/tools/acme/certs/。
	acmeCertsDir := filepath.Join(cfg.DataDir, "tools", "acme", "certs")
	cm := cert.NewAcme(acmeCertsDir)
	ac := acme.New(acmeCertsDir, cfg.AcmeBin)

	// Docker 是可选外部组件:daemon 不可用时其余功能照常工作,
	// Docker 页在前端降级提示,而非让整个控制面启动失败。
	dc := dockerclient.NewUnix(cfg.DockerSocket)
	var coll *stats.Collector
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
	apiVer, pingErr := dc.Ping(pingCtx)
	pingCancel()
	if pingErr != nil {
		log.Printf("Docker 不可用(%v):Docker 页将不可用,其余功能不受影响", pingErr)
		dc = nil
	} else {
		log.Printf("Docker 已连接: %s (API %s)", cfg.DockerSocket, apiVer)
		coll = stats.New(dc)
		defer coll.Close()
	}

	caddyCli := caddy.NewClient(cfg.CaddyAdmin)
	health := gateway.NewHealthCollector(caddyCli, gateway.DefaultHealthInterval)

	// 组件运行时（制品源 + 内置组件注册表）与插件管理器。
	src := source.New(os.Getenv("GATEBOX_GITHUB_TOKEN"))
	reg := component.NewCoreRegistry(cfg.DataDir, cfg.CaddyBin, src)
	mgr := plugin.NewManager(st, src, cfg.DataDir)

	handler := server.New(st, cm, dc, coll, caddyCli, health, cfg.DaemonJSONPath, cfg.DataDir, cfg.CaddyBin, cfg.CaddyHTTPPort, cfg.CaddyHTTPSPort, cfg.CaddyHTTPSExtraPorts, ac, reg, mgr)

	log.Printf("GateBox 启动: 监听 %s,数据目录 %s", cfg.Addr, cfg.DataDir)
	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
