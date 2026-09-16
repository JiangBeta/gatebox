package handler

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/JiangBeta/gatebox/internal/catalog"
	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/plugin"
)

// RegisterComponents 注册组件、插件、扩展能力与商店相关路由。
func RegisterComponents(mux *http.ServeMux, reg *component.CoreRegistry, mgr *plugin.Manager, ext *extension.Registry) {
	cat := catalog.New(reg, mgr)

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// 插件 UI 制品静态托管（L1 iframe 宿主，ADR-039 §3）：/plugins/<id>/*
	mux.HandleFunc("GET /plugins/{id}/", servePluginUI(mgr))
	mux.HandleFunc("GET /plugins/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
	})

	// 扩展能力注册表：消费者（前端/容器/网关）只查表，不认插件身份（ADR-036 I1）。
	// 可选 ?point= 过滤（如 proxy-protocols）。
	mux.HandleFunc("GET /api/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		caps := ext.Capabilities()
		if point := r.URL.Query().Get("point"); point != "" {
			filtered := make([]extension.Capability, 0, len(caps))
			for _, c := range caps {
				if c.Point == point {
					filtered = append(filtered, c)
				}
			}
			caps = filtered
		}
		writeJSON(w, http.StatusOK, caps)
	})

	// 组件：列表 / 检查更新（单个与全部）/ 升级 / 启停 / 卸载。
	mux.HandleFunc("GET /api/v1/components", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, reg.List(r.Context()))
	})
	mux.HandleFunc("POST /api/v1/components/check", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, reg.CheckAll(r.Context()))
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
			writeComponentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("POST /api/v1/components/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		info, err := reg.Start(r.Context(), r.PathValue("id"))
		if err != nil {
			writeComponentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("POST /api/v1/components/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		info, err := reg.Stop(r.Context(), r.PathValue("id"))
		if err != nil {
			writeComponentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("POST /api/v1/components/{id}/restart", func(w http.ResponseWriter, r *http.Request) {
		info, err := reg.Restart(r.Context(), r.PathValue("id"))
		if err != nil {
			writeComponentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("DELETE /api/v1/components/{id}", func(w http.ResponseWriter, r *http.Request) {
		info, err := reg.Uninstall(r.Context(), r.PathValue("id"))
		if err != nil {
			writeComponentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})

	// 商店：可选组件 + 插件（AppStore 视图）。
	mux.HandleFunc("GET /api/v1/store", func(w http.ResponseWriter, r *http.Request) {
		items, err := cat.List(r.Context())
		if err != nil {
			writeErrCode(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("POST /api/v1/store/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		it, err := cat.Install(r.Context(), r.PathValue("id"))
		if err != nil {
			writeComponentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, it)
	})
	mux.HandleFunc("DELETE /api/v1/store/{id}", func(w http.ResponseWriter, r *http.Request) {
		it, err := cat.Remove(r.Context(), r.PathValue("id"))
		if err != nil {
			writeComponentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, it)
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

// servePluginUI 静态托管插件的 UI 制品（$DATA_DIR/tools/<id>/ui）。
//
// 找不到具体文件时回退 index.html，支持插件前端自身的客户端路由；同时阻断路径穿越。
func servePluginUI(mgr *plugin.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		dir := mgr.UIDir(id)
		if dir == "" {
			http.Error(w, "插件 UI 未安装", http.StatusNotFound)
			return
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/plugins/"+id), "/")
		if rel == "" {
			rel = "index.html"
		}
		fp := filepath.Join(dir, filepath.Clean("/"+rel))
		if st, err := os.Stat(fp); err != nil || st.IsDir() {
			fp = filepath.Join(dir, "index.html")
		}
		http.ServeFile(w, r, fp)
	}
}

// writeComponentErr 组件/商店操作的统一错误映射。
func writeComponentErr(w http.ResponseWriter, err error) {
	if ce, ok := component.AsErr(err); ok {
		writeErrCode(w, http.StatusBadRequest, ce.Code, ce.Msg)
		return
	}
	if errors.Is(err, plugin.ErrNotFound) {
		writeErrCode(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	writeErrCode(w, http.StatusInternalServerError, "OPERATION_FAILED", err.Error())
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
