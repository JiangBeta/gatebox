package handler

import (
	"errors"
	"net/http"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/plugin"
)

// RegisterComponents 注册组件与插件相关路由。
func RegisterComponents(mux *http.ServeMux, reg *component.CoreRegistry, mgr *plugin.Manager) {
	mux.HandleFunc("GET /api/v1/components", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, reg.List(r.Context()))
	})
	mux.HandleFunc("POST /api/v1/components/{id}/check", func(w http.ResponseWriter, r *http.Request) {
		info, err := reg.CheckUpdate(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"info": info, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("POST /api/v1/components/{id}/upgrade", func(w http.ResponseWriter, r *http.Request) {
		info, err := reg.Upgrade(r.Context(), r.PathValue("id"))
		if err != nil {
			if ce, ok := component.AsErr(err); ok {
				writeErrCode(w, http.StatusBadRequest, ce.Code, ce.Msg)
				return
			}
			writeErrCode(w, http.StatusInternalServerError, "UPGRADE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, info)
	})

	mux.HandleFunc("GET /api/v1/plugins", func(w http.ResponseWriter, r *http.Request) {
		list, err := mgr.List()
		if err != nil {
			writeErrCode(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("POST /api/v1/plugins/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		v, err := mgr.Install(r.Context(), r.PathValue("id"))
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, pluginCode(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("POST /api/v1/plugins/{id}/enable", func(w http.ResponseWriter, r *http.Request) {
		v, err := mgr.Enable(r.PathValue("id"))
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, pluginCode(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("POST /api/v1/plugins/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		v, err := mgr.Disable(r.PathValue("id"))
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, pluginCode(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("DELETE /api/v1/plugins/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := mgr.Remove(r.PathValue("id"))
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, pluginCode(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
}

func pluginCode(err error) string {
	switch {
	case errors.Is(err, plugin.ErrNotFound):
		return "NOT_FOUND"
	case errors.Is(err, plugin.ErrNotInstalled):
		return "NOT_INSTALLED"
	default:
		return "PLUGIN_FAILED"
	}
}

// writeErrCode 写统一错误响应 {"error":{"code","message"}}（docs/architecture.md §10）。
func writeErrCode(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
