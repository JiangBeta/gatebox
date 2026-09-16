package handler

import (
	"context"
	"errors"
	"log"
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
//
// catalogURL 为插件静态索引地址，用于核心组件配方变体重算（ADR-038）；空则跳过。
func RegisterComponents(mux *http.ServeMux, reg *component.CoreRegistry, mgr *plugin.Manager, ext *extension.Registry, catalogURL string) {
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
		id := r.PathValue("id")
		v, err := mgr.Enable(id)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, pluginCode(err), err.Error())
			return
		}
		applyVariantsFor(r.Context(), reg, ext, mgr, catalogURL, id)
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("POST /api/v1/plugins/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		v, err := mgr.Disable(id)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, pluginCode(err), err.Error())
			return
		}
		applyVariantsFor(r.Context(), reg, ext, mgr, catalogURL, id)
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("DELETE /api/v1/plugins/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		v, err := mgr.Remove(id)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, pluginCode(err), err.Error())
			return
		}
		applyVariantsFor(r.Context(), reg, ext, mgr, catalogURL, id)
		writeJSON(w, http.StatusOK, v)
	})

	// 彻底卸载（purge）：连同配置与密钥一并删除（ADR-037 §4）。
	mux.HandleFunc("DELETE /api/v1/plugins/{id}/purge", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		v, err := mgr.Purge(id)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, pluginCode(err), err.Error())
			return
		}
		applyVariantsFor(r.Context(), reg, ext, mgr, catalogURL, id)
		writeJSON(w, http.StatusOK, v)
	})

	// 插件后端（sidecar）凭据：供前端 iframe 初始化时注入（同源，ADR-039 §3）。
	mux.HandleFunc("GET /api/v1/plugins/{id}/token", func(w http.ResponseWriter, r *http.Request) {
		tok, ok := mgr.Token(r.PathValue("id"))
		if !ok {
			writeErrCode(w, http.StatusNotFound, "NOT_FOUND", "插件不存在或未安装")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"token": tok})
	})
	// 插件后端 API 反代：/api/v1/plugins/<id>/* → sidecar（ADR-039 §1/§2）。
	mux.HandleFunc("/api/v1/plugins/{id}/", mgr.Proxy)
}

// applyVariantsFor 在插件启停/卸载后重算受影响组件的配方变体（ADR-038）。
//
// 仅处理声明了 `component-variant` 贡献的插件；best-effort，失败仅告警不阻塞插件操作。
func applyVariantsFor(ctx context.Context, reg *component.CoreRegistry, ext *extension.Registry, mgr *plugin.Manager, catalogURL, pluginID string) {
	if catalogURL == "" {
		return
	}
	v, ok, err := mgr.Get(pluginID)
	if err != nil || !ok {
		return
	}
	affected := map[string]bool{}
	for _, c := range v.Contributions.Capabilities {
		if c.Point != extension.PointComponentVar {
			continue
		}
		if comp, _ := c.Data["component"].(string); comp != "" {
			affected[comp] = true
		}
	}
	for comp := range affected {
		info, ok := reg.Get(ctx, comp)
		if !ok {
			continue
		}
		if info.Current == "" {
			log.Printf("组件 %s 变体重算跳过：版本未知", comp)
			continue
		}
		features := ext.ComponentFeatures(comp)
		dest, err := reg.ApplyVariant(ctx, comp, info.Current, features, catalogURL)
		if err != nil {
			log.Printf("组件 %s 变体重算失败: %v", comp, err)
			continue
		}
		log.Printf("组件 %s 已应用变体 features=%v → %s", comp, features, dest)
		if _, err := reg.Restart(ctx, comp); err != nil {
			log.Printf("组件 %s 应用变体后重启失败: %v", comp, err)
		}
	}
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
