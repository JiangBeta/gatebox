// Package server 装配 HTTP 处理器：API 路由 + 内嵌前端。
//
// 装配层：只负责把各层组装起来，不写业务逻辑（见 docs/architecture.md §8）。
package server

import (
	"io/fs"
	"net/http"

	"github.com/JiangBeta/gatebox/internal/handler"
	"github.com/JiangBeta/gatebox/internal/web"
)

// New 构造完整 HTTP handler。
func New() http.Handler {
	mux := http.NewServeMux()

	handler.Register(mux)

	// 内嵌前端（若已构建）；缺失时仅提供 API。
	if distFS, err := fs.Sub(web.Dist, "dist"); err == nil {
		mux.Handle("/", http.FileServer(http.FS(distFS)))
	}

	return mux
}
