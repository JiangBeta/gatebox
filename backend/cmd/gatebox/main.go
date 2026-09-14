// GateBox 控制面入口。
package main

import (
	"log"
	"net/http"

	"github.com/JiangBeta/gatebox/internal/config"
	"github.com/JiangBeta/gatebox/internal/server"
)

func main() {
	cfg := config.Load()

	log.Printf("GateBox 启动: 监听 %s, 数据目录 %s", cfg.Addr, cfg.DataDir)
	if err := http.ListenAndServe(cfg.Addr, server.New()); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
