// GateBox 控制面入口。
package main

import (
	"context"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/JiangBeta/gatebox/internal/cert"
	"github.com/JiangBeta/gatebox/internal/config"
	dockerclient "github.com/JiangBeta/gatebox/internal/docker/client"
	"github.com/JiangBeta/gatebox/internal/docker/stats"
	"github.com/JiangBeta/gatebox/internal/server"
	"github.com/JiangBeta/gatebox/internal/store"
)

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("打开存储失败: %v", err)
	}
	defer st.Close()

	// caddy 证书存储目录:$DATA_DIR/tools/caddy/data(安装脚本经 XDG_DATA_HOME 约定)
	cm := cert.NewCaddy(filepath.Join(cfg.DataDir, "tools", "caddy", "data"))

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

	handler := server.New(st, cm, dc, coll)

	log.Printf("GateBox 启动: 监听 %s,数据目录 %s", cfg.Addr, cfg.DataDir)
	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
