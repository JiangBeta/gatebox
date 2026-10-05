package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JiangBeta/gatebox/internal/typespec"
)

func newTypespecMux(t *testing.T) *http.ServeMux {
	t.Helper()
	reg, err := typespec.Load()
	if err != nil {
		t.Fatalf("加载类型目录失败: %v", err)
	}
	mux := http.NewServeMux()
	RegisterTypespec(mux, reg)
	return mux
}

func TestTypespecCatalogEndpoint(t *testing.T) {
	mux := newTypespecMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var m typespec.Manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Catalog.ServiceTypes) != 5 {
		t.Errorf("服务类型应为 5（4 内置 + l4_proxy），实际 %d", len(m.Catalog.ServiceTypes))
	}
	if len(m.Catalog.MiddlewareTypes) != 6 {
		t.Errorf("中间件类型应为 6，实际 %d", len(m.Catalog.MiddlewareTypes))
	}
	if len(m.Catalog.EntrypointProtocols) == 0 {
		t.Error("入口协议表为空")
	}
	if len(m.Catalog.Abilities) == 0 {
		t.Error("能力表为空")
	}
	// kinds 应含批次 4 三个对象 + 被引用的三类。
	for _, k := range []string{"route", "service", "middleware", "entrypoint", "domain", "user"} {
		if !containsStr(m.Kinds, k) {
			t.Errorf("kinds 缺 %s: %v", k, m.Kinds)
		}
	}
	if m.Groups["服务"] == nil {
		t.Errorf("分组「服务」为空: %v", m.Groups)
	}
}

func TestTypespecSpecsEndpoint(t *testing.T) {
	mux := newTypespecMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/specs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	var specs []*typespec.Spec
	if err := json.Unmarshal(rec.Body.Bytes(), &specs); err != nil {
		t.Fatal(err)
	}
	if len(specs) < 10 {
		t.Fatalf("对象类型过少: %d", len(specs))
	}
}

func TestTypespecSpecDetail(t *testing.T) {
	mux := newTypespecMux(t)

	// route + 无 type：字段应为 schema 本身。
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/specs/route", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var v struct {
		Kind  string               `json:"kind"`
		Title string               `json:"title"`
		Key   string               `json:"keyField"`
		Refs  []typespec.RefTarget `json:"refs"`
		Spec  *typespec.Spec       `json:"-"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Kind != "route" || v.Title != "路由" {
		t.Errorf("kind/title 异常: %+v", v)
	}
	if v.Key != "name" {
		t.Errorf("主键字段应为 name，实际 %q", v.Key)
	}
	if len(v.Refs) != 4 {
		t.Errorf("route 应有 4 个引用字段（service/roots/entrypoint/middlewares），实际 %d", len(v.Refs))
	}

	// service + type=file_server：应追加类型参数。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/specs/service?type=file_server", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var v2 struct {
		Fields []typespec.Field `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v2); err != nil {
		t.Fatal(err)
	}
	if !fieldKeyExists(v2.Fields, "backend.host") {
		t.Errorf("backend 子字段应被扁平化: %+v", v2.Fields)
	}

	// 未知 kind → 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/specs/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知 kind 应 404，实际 %d", rec.Code)
	}
}

func TestTypespecFieldsEndpoint(t *testing.T) {
	mux := newTypespecMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/specs/middleware/fields?type=basic_auth", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var v struct {
		Kind      string           `json:"kind"`
		TypeField string           `json:"typeField"`
		Fields    []typespec.Field `json:"fields"`
		Status    []typespec.Field `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.TypeField != "type" {
		t.Errorf("middleware 的类型字段应为 type，实际 %q", v.TypeField)
	}
	if !fieldKeyExists(v.Fields, "users") {
		t.Error("basic_auth 应展开 users 参数")
	}
	if !fieldKeyExists(v.Fields, "code") {
		t.Error("code 逃生舱应在基础 schema 里")
	}
	if !fieldKeyExists(v.Status, "refCount") {
		t.Error("status 字段应返回")
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func fieldKeyExists(list []typespec.Field, key string) bool {
	for _, f := range list {
		if f.Key == key {
			return true
		}
	}
	return false
}
