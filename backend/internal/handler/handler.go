// Package handler 提供 HTTP 翻译层：解析请求、调用 service、写响应。
//
// 硬规则：只做翻译，禁止业务判断与外部调用（见 docs/architecture.md §8.2）。
package handler

import (
	"encoding/json"
	"net/http"
)

// Register 注册全部 HTTP 路由。
func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/health", health)
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON 写 JSON 响应（成功直接返回资源；错误用 writeErr）。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr 写统一错误响应：{"error":{"code":..,"message":..}}（docs/architecture.md §10）。
func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
