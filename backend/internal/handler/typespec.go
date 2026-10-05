package handler

import (
	"net/http"

	"github.com/JiangBeta/gatebox/internal/typespec"
)

// RegisterTypespec 注册 V4.1 类型目录与对象 schema 端点（ADR-043 §1）。
//
//	GET /api/v1/catalog          → 类型目录（服务类型 / 中间件类型 / 入口协议 / 能力）
//	GET /api/v1/specs            → 全部对象类型定义（表单据此自绘）
//	GET /api/v1/specs/{kind}     → 单个对象类型定义；?type=xxx 时返回展开类型参数后的字段
//
// 全部只读：目录与模型均随二进制内嵌（typespec.Models），本轮无写接口。
func RegisterTypespec(mux *http.ServeMux, reg *typespec.Registry) {
	mux.HandleFunc("GET /api/v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, reg.Manifest())
	})

	mux.HandleFunc("GET /api/v1/specs", func(w http.ResponseWriter, r *http.Request) {
		out := make([]*typespec.Spec, 0)
		for _, kind := range reg.Kinds() {
			sp, _ := reg.Spec(kind)
			out = append(out, sp)
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/v1/specs/{kind}", func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("kind")
		sp, ok := reg.Spec(kind)
		if !ok {
			writeErrCode(w, http.StatusNotFound, "SPEC_NOT_FOUND", "对象类型不存在: "+kind)
			return
		}
		typeID := r.URL.Query().Get("type")
		keyField, _ := reg.KeyField(kind)
		writeJSON(w, http.StatusOK, specView{
			Spec:   sp,
			Type:   typeID,
			Fields: reg.EffectiveFields(kind, typeID),
			Status: sp.Status,
			Refs:   reg.ReferenceTargets(kind),
			Key:    keyField.Key,
		})
	})

	mux.HandleFunc("GET /api/v1/specs/{kind}/fields", func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("kind")
		if _, ok := reg.Spec(kind); !ok {
			writeErrCode(w, http.StatusNotFound, "SPEC_NOT_FOUND", "对象类型不存在: "+kind)
			return
		}
		typeID := r.URL.Query().Get("type")
		writeJSON(w, http.StatusOK, map[string]any{
			"kind":      kind,
			"type":      typeID,
			"fields":    reg.EffectiveFields(kind, typeID),
			"status":    statusFields(reg, kind),
			"typeField": dynamicSelectKey(reg, kind),
		})
	})
}

// specView 对象定义 + 展开后的字段序列 + 引用清单。
type specView struct {
	*typespec.Spec
	Type   string               `json:"type,omitempty"`
	Fields []typespec.Field     `json:"fields"`
	Status []typespec.Field     `json:"status,omitempty"`
	Refs   []typespec.RefTarget `json:"refs,omitempty"`
	Key    string               `json:"keyField,omitempty"`
}

// statusFields 返回某 kind 的 status 字段定义。
func statusFields(reg *typespec.Registry, kind string) []typespec.Field {
	sp, ok := reg.Spec(kind)
	if !ok {
		return nil
	}
	return sp.Status
}

// dynamicSelectKey 返回该 kind 里承担「类型」语义的字段键（service/middleware 是 type，entrypoint 是 protocol）。
func dynamicSelectKey(reg *typespec.Registry, kind string) string {
	for _, f := range reg.SchemaFields(kind) {
		if f.Dynamic && f.Type == typespec.FieldSelect {
			return f.Key
		}
	}
	return ""
}
