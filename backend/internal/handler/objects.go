package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/JiangBeta/gatebox/internal/objects"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

// RegisterObjects 注册 V4.1 通用对象层端点（ADR-043 §2）。
//
//	GET    /api/v1/objects                     → 全部对象（按 kind 分组，供任务中心/拓扑消费）
//	GET    /api/v1/objects/{kind}              → 列出某 kind
//	POST   /api/v1/objects/{kind}              → 新建
//	GET    /api/v1/objects/{kind}/{id}         → 读取
//	PUT    /api/v1/objects/{kind}/{id}         → 更新源字段
//	PATCH  /api/v1/objects/{kind}/{id}/key     → 改主键（级联改引用）
//	DELETE /api/v1/objects/{kind}/{id}         → 删除（?force=1 连带删引用方）
//	GET    /api/v1/objects/{kind}/{id}/refs    → 谁引用了我 / 我引用了谁
//
// 写操作成功后由 Trigger 钩子接 reconcile（ADR-043 §4：落库 → 未生效 → Run → 回写状态）。
func RegisterObjects(mux *http.ServeMux, svc *objects.Service, spec *typespec.Registry) {
	o := &objectAPI{svc: svc, spec: spec}
	mux.HandleFunc("GET /api/v1/objects", o.listAll)
	mux.HandleFunc("GET /api/v1/objects/{kind}", o.list)
	mux.HandleFunc("POST /api/v1/objects/{kind}", o.create)
	mux.HandleFunc("GET /api/v1/objects/{kind}/{id}", o.get)
	mux.HandleFunc("PUT /api/v1/objects/{kind}/{id}", o.update)
	mux.HandleFunc("PATCH /api/v1/objects/{kind}/{id}/key", o.rename)
	mux.HandleFunc("DELETE /api/v1/objects/{kind}/{id}", o.del)
	mux.HandleFunc("GET /api/v1/objects/{kind}/{id}/refs", o.refs)
}

type objectAPI struct {
	svc  *objects.Service
	spec *typespec.Registry
}

// SetAfterWrite 注入写后钩子（server 层接 reconcile.Trigger）。
var afterObjectWrite func(kind, id, op string)

func (o *objectAPI) notify(kind, id, op string) {
	if afterObjectWrite != nil {
		afterObjectWrite(kind, id, op)
	}
}

// objectBody 请求体：源字段平铺在一个对象里。
type objectBody struct {
	Spec map[string]any `json:"spec"`
}

// readSpec 兼容两种写法：{"spec":{...}} 与直接把源字段铺在顶层。
// readSpec 解析请求体，返回对象字段与只写字段（当前只有 user 的 password）。
//
// 只写字段必须在这里摘掉：它不属于对象文本，混进 spec 会被当成普通字段存进 Bolt
// （明文密码落库 = 泄露）。前端两种写法都支持：`{"spec":{...},"password":"x"}`（推荐）
// 与拍平的 `{"name":"x","password":"x"}`。
func readSpec(r *http.Request) (spec map[string]any, writeOnly map[string]any, err error) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, nil, err
	}
	// 先从外层摘只写字段：包封式写法里 password 是 spec 的兄弟字段，
	// 换 raw 之前不摘就丢了。
	writeOnly = map[string]any{}
	for _, k := range writeOnlyKeys {
		if v, ok := raw[k]; ok {
			writeOnly[k] = v
			delete(raw, k)
		}
	}
	if sv, ok := raw["spec"]; ok {
		s, ok := sv.(map[string]any)
		if !ok {
			// spec 存在但不是对象（客户端把字段形状搞错了）：直接报 400。
			// 早前这里落进 else 分支，把 {"spec":"x"} 整个当 spec 用，
			// 后面报「服务名 为空 / 500」，排查成本极高。
			return nil, nil, fmt.Errorf("spec 必须是对象（收到 %s）", jsonKind(sv))
		}
		raw = s
	} else {
		delete(raw, "status")
	}
	return raw, writeOnly, nil
}

// jsonKind 描述 JSON 值的形状，给前端/用户一句人话。
func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "字符串"
	case bool:
		return "布尔"
	case float64, int, json.Number:
		return "数字"
	case []any:
		return "数组"
	default:
		return "非对象"
	}
}

// writeOnlyKeys 只写字段白名单（不进对象文本、不落 spec）。
var writeOnlyKeys = []string{"password"}

// passwordOf 取请求里的明文密码（非字符串一律当没传）。
func passwordOf(writeOnly map[string]any) string {
	s, _ := writeOnly["password"].(string)
	return s
}

func (o *objectAPI) listAll(w http.ResponseWriter, r *http.Request) {
	out := map[string][]objects.Object{}
	for _, kind := range o.spec.Kinds() {
		list, err := o.svc.List(kind)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(list) > 0 {
			out[kind] = list
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (o *objectAPI) list(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := o.spec.Spec(kind); !ok {
		writeErrCode(w, http.StatusNotFound, "SPEC_NOT_FOUND", "对象类型不存在: "+kind)
		return
	}
	list, err := o.svc.List(kind)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (o *objectAPI) create(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	spec, writeOnly, err := readSpec(r)
	if err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_JSON", "请求体解析失败: "+err.Error())
		return
	}
	var obj objects.Object
	if kind == typespec.KindUser {
		// 建用户必须带密码：否则会留下一个永远登录不了的账户。
		obj, err = o.svc.CreateUser(spec, passwordOf(writeOnly))
	} else {
		obj, err = o.svc.Create(kind, spec)
	}
	if err != nil {
		writeObjectErr(w, err)
		return
	}
	o.notify(kind, obj.ID, "create")
	writeJSON(w, http.StatusCreated, obj)
}

func (o *objectAPI) get(w http.ResponseWriter, r *http.Request) {
	obj, err := o.svc.Get(r.PathValue("kind"), r.PathValue("id"))
	if err != nil {
		writeObjectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, obj)
}

func (o *objectAPI) update(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	spec, writeOnly, err := readSpec(r)
	if err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_JSON", "请求体解析失败: "+err.Error())
		return
	}
	// 密码先校验：太弱就别让源字段先落库。
	password := passwordOf(writeOnly)
	if kind == typespec.KindUser && password != "" {
		if err := objects.CheckPassword(password); err != nil {
			writeObjectErr(w, err)
			return
		}
	}
	obj, err := o.svc.Update(kind, id, spec)
	if err != nil {
		writeObjectErr(w, err)
		return
	}
	// 留空 = 不改密码（model/user.yaml：编辑时留空表示不修改）。
	if kind == typespec.KindUser && password != "" {
		if err := o.svc.SetPassword(id, password); err != nil {
			writeObjectErr(w, err)
			return
		}
		obj, err = o.svc.Get(kind, id)
		if err != nil {
			writeObjectErr(w, err)
			return
		}
	}
	o.notify(kind, id, "update")
	writeJSON(w, http.StatusOK, obj)
}

func (o *objectAPI) rename(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_JSON", "请求体解析失败: "+err.Error())
		return
	}
	obj, cascaded, err := o.svc.Rename(kind, id, body.Key)
	if err != nil {
		writeObjectErr(w, err)
		return
	}
	o.notify(kind, obj.ID, "update")
	writeJSON(w, http.StatusOK, map[string]any{
		"object":       obj,
		"oldId":        id,
		"cascadedRefs": cascaded,
	})
}

func (o *objectAPI) del(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	cascade, err := o.svc.Delete(kind, id, force)
	if err != nil {
		writeObjectErr(w, err)
		return
	}
	o.notify(kind, id, "delete")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": kind + "/" + id, "cascaded": cascade})
}

func (o *objectAPI) refs(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	if _, err := o.svc.Get(kind, id); err != nil {
		writeObjectErr(w, err)
		return
	}
	inbound, err := o.svc.ReferencedBy(kind, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	idx, err := o.svc.RefIndex()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":     kind,
		"id":       id,
		"inbound":  inbound,
		"outbound": idx.To(kind, id),
	})
}

// writeObjectErr 把对象层错误翻译成 HTTP 语义。
func writeObjectErr(w http.ResponseWriter, err error) {
	var ve *objects.ValidationError
	if errors.As(err, &ve) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":   ve.Error(),
			"code":    "VALIDATION_FAILED",
			"details": ve.Details(),
		})
		return
	}
	var iu *objects.InUseError
	if errors.As(err, &iu) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": iu.Error(),
			"code":  "IN_USE",
			"refs":  iu.Refs,
		})
		return
	}
	if errors.Is(err, objects.ErrNotFound) {
		writeErrCode(w, http.StatusNotFound, "NOT_FOUND", "对象不存在")
		return
	}
	if errors.Is(err, objects.ErrReadonly) {
		writeErrCode(w, http.StatusForbidden, "READONLY", err.Error())
		return
	}
	if errors.Is(err, objects.ErrKeyChange) {
		writeErrCode(w, http.StatusConflict, "KEY_CHANGE_REQUIRES_RENAME", err.Error()+
			"（PATCH /api/v1/objects/{kind}/{id}/key）")
		return
	}
	switch {
	case errors.Is(err, objects.ErrPasswordRequired):
		writeErrCode(w, http.StatusUnprocessableEntity, "PASSWORD_REQUIRED", err.Error())
		return
	case errors.Is(err, objects.ErrPasswordWeak):
		writeErrCode(w, http.StatusUnprocessableEntity, "PASSWORD_WEAK", err.Error())
		return
	case errors.Is(err, objects.ErrDuplicate):
		writeErrCode(w, http.StatusConflict, "DUPLICATE", err.Error())
		return
	}
	msg := err.Error()
	code := http.StatusInternalServerError
	switch {
	case strings.Contains(msg, "未定义"), strings.Contains(msg, "只读"), strings.Contains(msg, "已存在"):
		code = http.StatusUnprocessableEntity
	}
	writeErr(w, code, msg)
}
