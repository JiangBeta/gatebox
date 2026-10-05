package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/objects"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/typespec"
	"golang.org/x/crypto/bcrypt"
)

func newObjectsMux(t *testing.T) (*http.ServeMux, *objects.Service) {
	t.Helper()
	reg, err := typespec.Load()
	if err != nil {
		t.Fatalf("加载类型目录失败: %v", err)
	}
	dir := t.TempDir()
	st, err := repository.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("打开仓储失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := objects.New(st, reg)
	mux := http.NewServeMux()
	RegisterObjects(mux, svc, reg)
	return mux, svc
}

func objJSON(t *testing.T, mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func objDecode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("解析响应失败 %s: %v", rec.Body.String(), err)
	}
	return v
}

func TestObjectsCRUDOverHTTP(t *testing.T) {
	mux, _ := newObjectsMux(t)

	// 建入口点。
	rec := objJSON(t, mux, http.MethodPost, "/api/v1/objects/entrypoint",
		map[string]any{"protocol": "https", "ports": []any{443.0}, "network": "tcp", "enabled": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("建入口点 %d: %s", rec.Code, rec.Body.String())
	}
	ep := objDecode[objects.Object](t, rec)
	if ep.ID != "https" {
		t.Errorf("入口点 id: %q", ep.ID)
	}

	// 建服务。
	rec = objJSON(t, mux, http.MethodPost, "/api/v1/objects/service", map[string]any{
		"name": "aria-web", "type": "reverse_proxy",
		"backend": map[string]any{"host": "nas", "port": 6880.0},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("建服务 %d: %s", rec.Code, rec.Body.String())
	}

	// 建路由（引用入口点）。
	rec = objJSON(t, mux, http.MethodPost, "/api/v1/objects/route", map[string]any{
		"name": "aria", "service": "aria-web", "entrypoint": "https",
		"backend":   map[string]any{"host": "192.168.1.20", "port": 6880.0},
		"subdomain": "aria", "port": 443.0, "tls": "auto", "enabled": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("建路由 %d: %s", rec.Code, rec.Body.String())
	}
	rt := objDecode[objects.Object](t, rec)
	if rt.Status == nil {
		t.Error("status 不得为 null")
	}

	// 列表。
	rec = objJSON(t, mux, http.MethodGet, "/api/v1/objects/route", nil)
	list := objDecode[[]objects.Object](t, rec)
	if len(list) != 1 || list[0].ID != "aria" {
		t.Fatalf("路由列表异常: %s", rec.Body.String())
	}

	// 聚合列表按 kind 分组。
	rec = objJSON(t, mux, http.MethodGet, "/api/v1/objects", nil)
	all := objDecode[map[string][]objects.Object](t, rec)
	if len(all["route"]) != 1 || len(all["service"]) != 1 || len(all["entrypoint"]) != 1 {
		t.Errorf("聚合列表异常: %v", all)
	}

	// 更新。
	rec = objJSON(t, mux, http.MethodPut, "/api/v1/objects/route/aria", map[string]any{
		"name": "aria", "service": "aria-web", "entrypoint": "https",
		"backend": map[string]any{"host": "192.168.1.21", "port": 6881.0},
		"tls":     "auto", "enabled": false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新 %d: %s", rec.Code, rec.Body.String())
	}
	if objDecode[objects.Object](t, rec).String("backend.host") != "192.168.1.21" {
		t.Error("更新未落库")
	}
}

func TestObjectsRenameAndDeleteConflict(t *testing.T) {
	mux, _ := newObjectsMux(t)
	seed := func(kind, path string, body map[string]any) {
		rec := objJSON(t, mux, http.MethodPost, path, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed %s %d: %s", kind, rec.Code, rec.Body.String())
		}
	}
	seed("entrypoint", "/api/v1/objects/entrypoint",
		map[string]any{"protocol": "https", "ports": []any{443.0}, "network": "tcp", "enabled": true})
	seed("service", "/api/v1/objects/service",
		map[string]any{"name": "aria-web", "type": "reverse_proxy",
			"backend": map[string]any{"host": "nas", "port": 6880.0}})
	seed("route", "/api/v1/objects/route",
		map[string]any{"name": "aria", "service": "aria-web", "entrypoint": "https",
			"backend": map[string]any{"host": "192.168.1.20", "port": 6880.0}, "tls": "auto"})

	// 改主键 → 级联引用。
	rec := objJSON(t, mux, http.MethodPatch, "/api/v1/objects/service/aria-web/key",
		map[string]any{"key": "aria-edge"})
	if rec.Code != http.StatusOK {
		t.Fatalf("改名 %d: %s", rec.Code, rec.Body.String())
	}
	res := objDecode[struct {
		Object       objects.Object `json:"object"`
		OldID        string         `json:"oldId"`
		CascadedRefs int            `json:"cascadedRefs"`
	}](t, rec)
	if res.Object.ID != "aria-edge" || res.OldID != "aria-web" || res.CascadedRefs != 1 {
		t.Errorf("改名结果异常: %s", rec.Body.String())
	}
	rec = objJSON(t, mux, http.MethodGet, "/api/v1/objects/route/aria", nil)
	if got := objDecode[objects.Object](t, rec).String("service"); got != "aria-edge" {
		t.Errorf("引用未级联: %q", got)
	}

	// 被引用的服务删除 → 409 + 引用来源。
	rec = objJSON(t, mux, http.MethodDelete, "/api/v1/objects/service/aria-edge", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("应冲突，实际 %d: %s", rec.Code, rec.Body.String())
	}
	conf := objDecode[struct {
		Code string        `json:"code"`
		Refs []objects.Ref `json:"refs"`
	}](t, rec)
	if conf.Code != "IN_USE" || len(conf.Refs) != 1 || conf.Refs[0].From != "route" {
		t.Errorf("冲突响应异常: %s", rec.Body.String())
	}

	// 强制删除连带删引用方。
	rec = objJSON(t, mux, http.MethodDelete, "/api/v1/objects/service/aria-edge?force=1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("强制删除 %d: %s", rec.Code, rec.Body.String())
	}
	if rec := objJSON(t, mux, http.MethodGet, "/api/v1/objects/route/aria", nil); rec.Code != http.StatusNotFound {
		t.Errorf("引用方应被连带删除，实际 %d", rec.Code)
	}
}

func TestObjectsValidationErrorShape(t *testing.T) {
	mux, _ := newObjectsMux(t)
	// 缺必填 backend。
	rec := objJSON(t, mux, http.MethodPost, "/api/v1/objects/route",
		map[string]any{"name": "bad", "entrypoint": "https"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("期望 422，实际 %d: %s", rec.Code, rec.Body.String())
	}
	det := objDecode[struct {
		Code    string                `json:"code"`
		Details []typespec.FieldError `json:"details"`
	}](t, rec)
	if det.Code != "VALIDATION_FAILED" || len(det.Details) == 0 {
		t.Fatalf("校验响应异常: %s", rec.Body.String())
	}

	// 悬空引用定位到字段。
	rec = objJSON(t, mux, http.MethodPost, "/api/v1/objects/route",
		map[string]any{"name": "bad2", "entrypoint": "nope",
			"backend": map[string]any{"host": "h", "port": 1.0}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("期望 422，实际 %d: %s", rec.Code, rec.Body.String())
	}
	det2 := objDecode[struct {
		Details []typespec.FieldError `json:"details"`
	}](t, rec)
	if det2.Details[0].Field != "entrypoint" {
		t.Errorf("应定位到 entrypoint: %s", rec.Body.String())
	}
}

func TestObjectsUnknownKindAndReadonly(t *testing.T) {
	mux, _ := newObjectsMux(t)
	if rec := objJSON(t, mux, http.MethodGet, "/api/v1/objects/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("未知 kind 应 404，实际 %d", rec.Code)
	}
	rec := objJSON(t, mux, http.MethodPost, "/api/v1/objects/catalog", map[string]any{"name": "x"})
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusForbidden {
		t.Errorf("catalog 只读应被拒，实际 %d: %s", rec.Code, rec.Body.String())
	}
}

func TestObjectsRefsEndpoint(t *testing.T) {
	mux, _ := newObjectsMux(t)
	seed := func(path string, body map[string]any) {
		if rec := objJSON(t, mux, http.MethodPost, path, body); rec.Code != http.StatusCreated {
			t.Fatalf("seed %d: %s", rec.Code, rec.Body.String())
		}
	}
	seed("/api/v1/objects/entrypoint",
		map[string]any{"protocol": "https", "ports": []any{443.0}, "network": "tcp"})
	seed("/api/v1/objects/middleware", map[string]any{"name": "GZIP", "type": "encode"})
	seed("/api/v1/objects/route",
		map[string]any{"name": "aria", "entrypoint": "https", "middlewares": []any{"gzip"},
			"backend": map[string]any{"host": "h", "port": 1.0}})

	rec := objJSON(t, mux, http.MethodGet, "/api/v1/objects/middleware/gzip/refs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("refs %d: %s", rec.Code, rec.Body.String())
	}
	out := objDecode[struct {
		Inbound []objects.Ref `json:"inbound"`
	}](t, rec)
	if len(out.Inbound) != 1 || out.Inbound[0].From != "route" || out.Inbound[0].Field != "middlewares" {
		t.Errorf("反查异常: %s", rec.Body.String())
	}
}

// newUserMux 带秘密存储的对象 API（建用户要写 bcrypt 哈希）。
func newUserMux(t *testing.T) (*http.ServeMux, *objects.Service, *repository.Store) {
	t.Helper()
	reg, err := typespec.Load()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	st, err := repository.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := objects.New(st, reg).WithSecrets(st)
	mux := http.NewServeMux()
	RegisterObjects(mux, svc, reg)
	return mux, svc, st
}

func TestUserCreateRequiresPassword(t *testing.T) {
	mux, _, _ := newUserMux(t)
	rec := objJSON(t, mux, http.MethodPost, "/api/v1/objects/user",
		map[string]any{"name": "admin", "displayName": "管理员", "enabled": true})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("无密码建用户应 422，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PASSWORD_REQUIRED") {
		t.Errorf("错误码不对: %s", rec.Body.String())
	}
}

func TestUserPasswordNeverEchoedOrStored(t *testing.T) {
	mux, svc, st := newUserMux(t)
	const pwd = "sup3r-secret-pw"
	rec := objJSON(t, mux, http.MethodPost, "/api/v1/objects/user", map[string]any{
		"spec":     map[string]any{"name": "admin", "displayName": "管理员", "enabled": true},
		"password": pwd,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("建用户 %d: %s", rec.Code, rec.Body.String())
	}
	// 响应里绝不能出现明文或哈希
	if strings.Contains(rec.Body.String(), pwd) || strings.Contains(rec.Body.String(), "$2a$") {
		t.Fatalf("响应泄露了密码: %s", rec.Body.String())
	}
	u := objDecode[objects.Object](t, rec)
	if u.Status["passwordSet"] != true {
		t.Errorf("应标记已设密码: %v", u.Status)
	}
	if _, leaked := u.Spec["password"]; leaked {
		t.Errorf("密码混进了 spec: %v", u.Spec)
	}
	// 原始落库文本同样不能有明文
	raw, err := st.ListObjects(typespec.KindUser)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range raw {
		if strings.Contains(string(o.Body), pwd) {
			t.Fatalf("落库文本泄露明文密码: %s", string(o.Body))
		}
		if strings.Contains(string(o.Body), "password\":") {
			t.Fatalf("落库文本不该有 password 字段: %s", string(o.Body))
		}
	}
	// 内部链路能取到可用的 bcrypt 哈希（渲染 basic_auth 用）
	h, err := svc.PasswordHash("admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(h), []byte(pwd)); err != nil {
		t.Errorf("哈希与密码不匹配: %v", err)
	}
}

func TestUserWeakPasswordRejected(t *testing.T) {
	mux, _, _ := newUserMux(t)
	rec := objJSON(t, mux, http.MethodPost, "/api/v1/objects/user",
		map[string]any{"name": "bob", "password": "short"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("弱密码应 422，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PASSWORD_WEAK") {
		t.Errorf("错误码不对: %s", rec.Body.String())
	}
}

func TestUserUpdatePasswordOptional(t *testing.T) {
	mux, svc, _ := newUserMux(t)
	rec := objJSON(t, mux, http.MethodPost, "/api/v1/objects/user",
		map[string]any{"name": "admin", "password": "first-password"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("建用户 %d: %s", rec.Code, rec.Body.String())
	}
	before, err := svc.PasswordHash("admin")
	if err != nil {
		t.Fatal(err)
	}

	// 留空 = 不改密码
	rec = objJSON(t, mux, http.MethodPut, "/api/v1/objects/user/admin",
		map[string]any{"name": "admin", "displayName": "改了显示名", "enabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新 %d: %s", rec.Code, rec.Body.String())
	}
	after, err := svc.PasswordHash("admin")
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Error("留空不该改密码哈希")
	}
	if objDecode[objects.Object](t, rec).String("displayName") != "改了显示名" {
		t.Error("源字段没更新")
	}

	// 给了新密码 = 改密码
	rec = objJSON(t, mux, http.MethodPut, "/api/v1/objects/user/admin",
		map[string]any{"name": "admin", "enabled": true, "password": "second-password"})
	if rec.Code != http.StatusOK {
		t.Fatalf("改密码 %d: %s", rec.Code, rec.Body.String())
	}
	h, err := svc.PasswordHash("admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(h), []byte("second-password")); err != nil {
		t.Errorf("新密码没生效: %v", err)
	}

	// 弱密码不许先落库：源字段也不该变
	rec = objJSON(t, mux, http.MethodPut, "/api/v1/objects/user/admin",
		map[string]any{"name": "admin", "displayName": "不该写进来", "password": "no"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("弱密码应 422，实际 %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := svc.Get(typespec.KindUser, "admin")
	if got.String("displayName") == "不该写进来" {
		t.Error("密码没过就不该落源字段")
	}
}

func TestUpdateCannotChangeKey(t *testing.T) {
	mux, _ := newObjectsMux(t)
	rec := objJSON(t, mux, http.MethodPost, "/api/v1/objects/entrypoint",
		map[string]any{"protocol": "https", "ports": []any{443.0}, "network": "tcp", "enabled": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("建入口点 %d: %s", rec.Code, rec.Body.String())
	}
	// 改 protocol = 改主键 → 必须让路给重命名接口，否则会留下 id≠Slug(key) 的对象
	rec = objJSON(t, mux, http.MethodPut, "/api/v1/objects/entrypoint/https", map[string]any{
		"protocol": "http", "ports": []any{443.0}, "network": "tcp", "enabled": true,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("改主键应 409，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "KEY_CHANGE_REQUIRES_RENAME") {
		t.Errorf("错误码不对: %s", rec.Body.String())
	}
}

// TestSpecMustBeObject 客户端把 spec 形状搞错时必须报 400，而不是被兜成 500。
//
// 回归背景：{"spec":"字符串"} 早前会落进 readSpec 的 else 分支，整个当 spec 用，
// 之后报「服务名 为空」+ HTTP 500 —— 用户看到的是服务端故障，其实是自己请求体错了。
func TestSpecMustBeObject(t *testing.T) {
	mux, _ := newObjectsMux(t)
	for _, body := range []string{
		`{"spec":"我不是对象"}`,
		`{"spec":[1,2,3]}`,
		`{"spec":42}`,
		`{"spec":null}`,
	} {
		req := httptest.NewRequest("POST", "/api/v1/objects/service", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s 期望 400，实际 %d（%s）", body, rec.Code, rec.Body.String())
		}
		// writeErrCode 的 error 是对象：{code, message}
		var got struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("解析错误响应失败: %v", err)
		}
		if got.Error.Code != "BAD_JSON" || !strings.Contains(got.Error.Message, "spec 必须是对象") {
			t.Errorf("body=%s 错误文案应点明 spec 形状，实际 %+v", body, got.Error)
		}
	}
}

// TestKeyFieldProblemsAre422 主键字段缺失/类型错/空白属于客户端输入错误，必须 422，
// 不能是 500（早前 KeyOf 返回裸 fmt.Errorf，被兜成了 500）。
func TestKeyFieldProblemsAre422(t *testing.T) {
	mux, _ := newObjectsMux(t)
	cases := []struct{ name, spec string }{
		{"主键缺失", `{"spec":{"type":"reverse_proxy","backend":{"host":"127.0.0.1","port":80}}}`},
		{"主键非文本", `{"spec":{"name":123,"type":"reverse_proxy"}}`},
		{"主键空白", `{"spec":{"name":"   ","type":"reverse_proxy"}}`},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/api/v1/objects/service", strings.NewReader(c.spec))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s：期望 422，实际 %d（%s）", c.name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "VALIDATION_FAILED") {
			t.Errorf("%s：应带 VALIDATION_FAILED，实际 %s", c.name, rec.Body.String())
		}
	}
}

// TestReferenceValueMustBeTextID 引用字段收到对象（表单数组未序列化的 {id,value}）
// 必须被拒：早前 checkRef 对非字符串直接放过，脏数据会落库，直到渲染才炸。
func TestReferenceValueMustBeTextID(t *testing.T) {
	mux, _ := newObjectsMux(t)
	// 先建齐依赖，再塞脏引用。
	objJSON(t, mux, "POST", "/api/v1/objects/entrypoint", map[string]any{
		"spec": map[string]any{"protocol": "http", "ports": []int{80}},
	})
	objJSON(t, mux, "POST", "/api/v1/objects/domain", map[string]any{"spec": map[string]any{"name": "ok.example"}})

	// 引用字段整体是对象
	req := httptest.NewRequest("POST", "/api/v1/objects/route", strings.NewReader(
		`{"spec":{"name":"r1","entrypoint":"http","roots":{"id":"ok-example"},"backend":{"host":"127.0.0.1","port":80}}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("roots 为对象：期望 422，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	// roots 整体是对象：先被数组类型检查拦下（"类型应为数组"），也是 422；
	// 具体文案由数组分支给，重点是别让它落库。
	if !strings.Contains(rec.Body.String(), "应为数组") &&
		!strings.Contains(rec.Body.String(), "引用值必须是对象 id") {
		t.Errorf("错误文案应说明形状问题，实际 %s", rec.Body.String())
	}

	// 引用数组里混入对象
	req = httptest.NewRequest("POST", "/api/v1/objects/route", strings.NewReader(
		`{"spec":{"name":"r2","entrypoint":"http","roots":[{"id":"ok-example","value":"ok.example"}],"backend":{"host":"127.0.0.1","port":80}}}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("roots 数组含对象：期望 422，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "roots[0]") {
		t.Errorf("应定位到 roots[0]，实际 %s", rec.Body.String())
	}

	// 正常 id 数组必须照常通过（别把合法的也拦了）。
	req = httptest.NewRequest("POST", "/api/v1/objects/route", strings.NewReader(
		`{"spec":{"name":"r3","entrypoint":"http","roots":["ok-example"],"backend":{"host":"127.0.0.1","port":80}}}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("合法 id 数组应通过，实际 %d（%s）", rec.Code, rec.Body.String())
	}
}
