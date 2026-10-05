package objects

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

// memStore 内存版 Store（复用真实 Bolt 语义：UpdateObjects 给当前全量）。
type memStore struct{ rows []repository.Obj }

func (m *memStore) ListObjects(kind string) ([]repository.Obj, error) {
	var out []repository.Obj
	for _, r := range m.rows {
		if r.Kind == kind {
			out = append(out, cloneObj(r))
		}
	}
	return out, nil
}

func (m *memStore) AllObjects() ([]repository.Obj, error) {
	out := make([]repository.Obj, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, cloneObj(r))
	}
	return out, nil
}

func (m *memStore) UpdateObjects(fn func(cur []repository.Obj) (puts, dels []repository.Obj, err error)) error {
	cur := make([]repository.Obj, 0, len(m.rows))
	for _, r := range m.rows {
		cur = append(cur, cloneObj(r))
	}
	puts, dels, err := fn(cur)
	if err != nil {
		return err
	}
	for _, d := range dels {
		for i := range m.rows {
			if m.rows[i].Kind == d.Kind && m.rows[i].ID == d.ID {
				m.rows = append(m.rows[:i], m.rows[i+1:]...)
				break
			}
		}
	}
	for _, p := range puts {
		replaced := false
		for i := range m.rows {
			if m.rows[i].Kind == p.Kind && m.rows[i].ID == p.ID {
				m.rows[i] = cloneObj(p)
				replaced = true
				break
			}
		}
		if !replaced {
			m.rows = append(m.rows, cloneObj(p))
		}
	}
	return nil
}

func cloneObj(r repository.Obj) repository.Obj {
	return repository.Obj{Kind: r.Kind, ID: r.ID, Body: append([]byte(nil), r.Body...)}
}

func newTestService(t *testing.T) (*Service, *memStore) {
	t.Helper()
	reg, err := typespec.Load()
	if err != nil {
		t.Fatalf("加载类型目录失败: %v", err)
	}
	ms := &memStore{}
	svc := New(ms, reg)
	fixed := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return fixed })
	return svc, ms
}

func specOf(m map[string]any, pairs ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i].(string)] = pairs[i+1]
	}
	return out
}

func mustCreate(t *testing.T, s *Service, kind string, m map[string]any) Object {
	t.Helper()
	o, err := s.Create(kind, m)
	if err != nil {
		t.Fatalf("创建 %s %v 失败: %v", kind, m, err)
	}
	return o
}

// mustEntrypoint 建入口点对象（route.entrypoint 是对象引用，必须先存在）。
func mustEntrypoint(t *testing.T, s *Service, protocol string, ports ...float64) Object {
	t.Helper()
	ps := make([]any, 0, len(ports))
	for _, p := range ports {
		ps = append(ps, p)
	}
	return mustCreate(t, s, typespec.KindEntrypoint, specOf(nil,
		"protocol", protocol, "ports", ps, "network", "tcp", "enabled", true))
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"aria":            "aria",
		"aria-web":        "aria-web",
		"  GateBox 路由 ":   "gatebox-路由",
		"https":           "https",
		"a/b":             "a-b",
		"--a--":           "a",
		"Flare Home 5005": "flare-home-5005",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestCreateReadUpdate(t *testing.T) {
	s, _ := newTestService(t)

	mw := mustCreate(t, s, typespec.KindMiddleware, specOf(nil,
		"name", "GZIP", "type", "encode", "formats", []any{"gzip", "zstd"}))
	if mw.ID != "gzip" || mw.Key != "GZIP" {
		t.Errorf("id/key 异常: %+v", mw)
	}
	if mw.CreatedAt.IsZero() || mw.UpdatedAt.IsZero() {
		t.Error("时间戳应被填充")
	}
	// JSON 出来时 spec/status 不得为 null。
	b, _ := json.Marshal(mw)
	if !jsonHas(b, `"spec":{`) || !jsonHas(b, `"status":{`) {
		t.Errorf("序列化应保证 spec/status 非 null: %s", b)
	}

	// 重复主键被拒。
	if _, err := s.Create(typespec.KindMiddleware, specOf(nil, "name", "GZIP", "type", "encode")); err == nil {
		t.Error("重复主键应报错")
	}

	// 更新源字段。
	up, err := s.Update(typespec.KindMiddleware, mw.ID, specOf(nil,
		"name", "GZIP", "type", "encode", "formats", []any{"zstd"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(up.Strings("formats")) != 1 || up.Strings("formats")[0] != "zstd" {
		t.Errorf("更新未生效: %v", up.Spec)
	}

	// 校验：type 不在目录。
	var ve *ValidationError
	_, err = s.Create(typespec.KindMiddleware, specOf(nil, "name", "x", "type", "nope"))
	if !errors.As(err, &ve) {
		t.Errorf("目录外类型应报 ValidationError，实际 %v", err)
	}
}

func TestRouteBackendRequiredAndServiceOptional(t *testing.T) {
	s, _ := newTestService(t)
	mustEntrypoint(t, s, "https", 443)
	svc := mustCreate(t, s, typespec.KindService, specOf(nil,
		"name", "aria-web", "type", "reverse_proxy",
		"backend", map[string]any{"host": "nas", "port": 6880.0},
	))

	// service 引用 + 自持 backend。
	rt := mustCreate(t, s, typespec.KindRoute, specOf(nil,
		"name", "aria", "service", svc.ID,
		"backend", map[string]any{"host": "192.168.1.20", "port": 6880.0},
		"entrypoint", "https", "port", 443.0, "tls", "auto", "enabled", true))
	if rt.ID != "aria" {
		t.Errorf("路由 id 应为 aria: %+v", rt)
	}
	if rt.String("service") != "aria-web" {
		t.Errorf("service 应存 id: %q", rt.String("service"))
	}

	// L4 路由：service 留空（类型由入口协议派生）。
	l4 := mustCreate(t, s, typespec.KindRoute, specOf(nil,
		"name", "ssh-fwd",
		"backend", map[string]any{"host": "192.168.1.10", "port": 22.0},
		"entrypoint", "https", "tls", "off"))
	if l4.String("service") != "" {
		t.Error("L4 路由的 service 应留空")
	}

	// 缺 backend → 必填。
	var ve *ValidationError
	if _, err := s.Create(typespec.KindRoute, specOf(nil, "name", "bad", "entrypoint", "https")); !errors.As(err, &ve) {
		t.Errorf("缺 backend 应报校验错，实际 %v", err)
	}

	// 悬空 service 引用。
	_, err := s.Create(typespec.KindRoute, specOf(nil,
		"name", "bad2", "service", "no-such",
		"backend", map[string]any{"host": "h", "port": 1.0}, "entrypoint", "https"))
	if !errors.As(err, &ve) || ve.Details()[0].Field != "service" {
		t.Errorf("悬空引用应定位到 service 字段: %v", err)
	}
}

func TestRenameCascadesReferences(t *testing.T) {
	s, _ := newTestService(t)
	mustEntrypoint(t, s, "https", 443)
	svc := mustCreate(t, s, typespec.KindService, specOf(nil,
		"name", "aria-web", "type", "reverse_proxy",
		"backend", map[string]any{"host": "nas", "port": 6880.0}))
	mw := mustCreate(t, s, typespec.KindMiddleware, specOf(nil, "name", "GZIP", "type", "encode"))
	rt := mustCreate(t, s, typespec.KindRoute, specOf(nil,
		"name", "aria", "service", svc.ID,
		"backend", map[string]any{"host": "192.168.1.20", "port": 6880.0},
		"entrypoint", "https", "middlewares", []any{mw.ID}))
	// remark 里恰好写着旧 id：级联不得改它。
	rt2 := mustCreate(t, s, typespec.KindRoute, specOf(nil,
		"name", "echo", "service", svc.ID,
		"backend", map[string]any{"host": "h", "port": 1.0},
		"entrypoint", "https", "remark", "aria-web 是旧名"))

	got, cascaded, err := s.Rename(typespec.KindService, svc.ID, "aria-edge")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "aria-edge" || cascaded != 2 {
		t.Errorf("重命名结果异常: id=%q cascaded=%d", got.ID, cascaded)
	}
	if _, err := s.Get(typespec.KindService, svc.ID); !errors.Is(err, ErrNotFound) {
		t.Error("旧 id 应消失")
	}
	// 引用被级联。
	after, _ := s.Get(typespec.KindRoute, rt.ID)
	if after.String("service") != "aria-edge" {
		t.Errorf("route.service 未级联: %q", after.String("service"))
	}
	// 数组引用不变（中间件没改名）。
	if after.Strings("middlewares")[0] != mw.ID {
		t.Error("无关的数组引用被误改")
	}
	// remark 未被误改。
	echo, _ := s.Get(typespec.KindRoute, rt2.ID)
	if echo.String("remark") != "aria-web 是旧名" {
		t.Errorf("remark 被误改: %q", echo.String("remark"))
	}
	// 引用索引同步。
	refs, err := s.ReferencedBy(typespec.KindService, "aria-edge")
	if err != nil || len(refs) != 2 {
		t.Errorf("引用索引应回报 2 条: %v %v", refs, err)
	}
}

func TestDeleteRefusesWhenReferenced(t *testing.T) {
	s, _ := newTestService(t)
	mustEntrypoint(t, s, "https", 443)
	svc := mustCreate(t, s, typespec.KindService, specOf(nil,
		"name", "aria-web", "type", "reverse_proxy",
		"backend", map[string]any{"host": "nas", "port": 6880.0}))
	mustCreate(t, s, typespec.KindRoute, specOf(nil,
		"name", "aria", "service", svc.ID,
		"backend", map[string]any{"host": "h", "port": 1.0}, "entrypoint", "https"))

	var iu *InUseError
	if _, err := s.Delete(typespec.KindService, svc.ID, false); !errors.As(err, &iu) {
		t.Fatalf("被引用时应拒绝删除: %v", err)
	}
	if len(iu.Refs) != 1 || iu.Refs[0].From != typespec.KindRoute {
		t.Errorf("InUseError 应回报来源: %+v", iu.Refs)
	}

	// 强制删除连带删掉引用方，避免悬空。
	cascade, err := s.Delete(typespec.KindService, svc.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cascade) != 1 || cascade[0] != typespec.KindRoute {
		t.Errorf("连带删除应回报 route: %v", cascade)
	}
	if _, err := s.Get(typespec.KindRoute, "aria"); !errors.Is(err, ErrNotFound) {
		t.Error("引用方应被连带删除")
	}
}

func TestCatalogIsReadonly(t *testing.T) {
	s, _ := newTestService(t)
	if _, err := s.Create(typespec.KindCatalog, specOf(nil, "name", "x")); err == nil {
		t.Fatal("catalog 应只读")
	}
}

func TestSetStatus(t *testing.T) {
	s, _ := newTestService(t)
	mw := mustCreate(t, s, typespec.KindMiddleware, specOf(nil, "name", "GZIP", "type", "encode"))
	if err := s.SetStatus(typespec.KindMiddleware, mw.ID, map[string]any{"refCount": 3.0}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(typespec.KindMiddleware, mw.ID)
	if got.Status["refCount"] != 3.0 {
		t.Errorf("status 未写入: %v", got.Status)
	}
	// SetStatus 不得动源字段。
	if got.String("name") != "GZIP" {
		t.Error("SetStatus 污染了 spec")
	}
}

func TestListSortedByKey(t *testing.T) {
	s, _ := newTestService(t)
	for _, n := range []string{"zeta", "alpha", "mid"} {
		mustCreate(t, s, typespec.KindMiddleware, specOf(nil, "name", n, "type", "encode"))
	}
	list, err := s.List(typespec.KindMiddleware)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{list[0].Key, list[1].Key, list[2].Key}
	want := []string{"alpha", "mid", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("排序异常: %v", got)
		}
	}
}

func jsonHas(b []byte, sub string) bool {
	return json.Valid(b) && strings.Contains(string(b), sub)
}
