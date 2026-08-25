// GateBox 控制面入口。
package main

import (
	"log"
	"net/http"
	"path/filepath"

	"github.com/JiangBeta/gatebox/internal/cert"
	"github.com/JiangBeta/gatebox/internal/config"
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

	handler := server.New(st, cm)

	log.Printf("GateBox 启动: 监听 %s,数据目录 %s", cfg.Addr, cfg.DataDir)
	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
