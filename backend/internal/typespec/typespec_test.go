package typespec

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// TestModelsInSyncWithDocs 守住编译期副本与设计期真相的漂移。
//
// 模型文件在 docs/v4.1/model/ 是唯一真相，但 go:embed 不能引用包目录之外的路径，
// 所以编译期有一份副本在 models/ 下。这个测试保证两者逐字节一致。
// 改模型后若忘记同步，这里会失败并给出修复命令。
func TestModelsInSyncWithDocs(t *testing.T) {
	docsDir := filepath.Join("..", "..", "..", "docs", "v4.1", "model")
	if _, err := os.Stat(docsDir); err != nil {
		t.Skip("docs/v4.1/model 不在（发布构建），跳过漂移校验")
	}
	if _, err := Models.ReadDir("models"); err != nil {
		t.Fatalf("内嵌模型为空: %v", err)
	}
	fsys := os.DirFS(docsDir)
	var n int
	err := fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(rel, ".yaml") {
			return nil
		}
		want, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return err
		}
		got, err := Models.ReadFile("models/" + rel)
		if err != nil {
			return err
		}
		n++
		if string(got) != string(want) {
			t.Errorf("模型副本与 docs 不一致: %s\n修复: cp docs/v4.1/model/%s backend/internal/typespec/models/%s",
				rel, rel, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("docs/v4.1/model 下没有 yaml，漂移校验形同虚设")
	}
	t.Logf("已校验 %d 个模型文件", n)
}

func TestLoadEmbedded(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatalf("加载内嵌模型失败: %v", err)
	}
	for _, kind := range []string{KindRoute, KindService, KindMiddleware, KindEntrypoint, KindDomain, KindUser, KindHost} {
		sp, ok := r.Spec(kind)
		if !ok {
			t.Fatalf("缺少对象类型 %s", kind)
		}
		if sp.Title == "" || sp.Group == "" {
			t.Errorf("%s 缺 title/group: %+v", kind, sp)
		}
		if len(sp.Schema) == 0 {
			t.Errorf("%s 没有 schema 字段", kind)
		}
		if sp.Source == "" {
			t.Errorf("%s 缺 Source", kind)
		}
	}
}

func TestLoadLogSequences(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	sp, ok := r.Spec("log")
	if !ok {
		t.Fatal("缺少对象类型 log")
	}
	if len(sp.Sequences) == 0 {
		t.Fatal("log 类型应带步骤序列模板")
	}
	// 序列 id 是前端挑模板的依据，必须是 acme_issue / acme_renew 两个稳定名。
	ids := map[string]Sequence{}
	for _, s := range sp.Sequences {
		ids[s.ID] = s
		if s.Label == "" || len(s.Steps) == 0 {
			t.Errorf("序列 %s 不完整: %+v", s.ID, s)
		}
		for i, st := range s.Steps {
			if st.No != i+1 {
				t.Errorf("序列 %s 第 %d 步序号应为 %d，实际 %d", s.ID, i, i+1, st.No)
			}
			if st.Step == "" || st.Marker == "" {
				t.Errorf("序列 %s 第 %d 步缺 step/marker: %+v", s.ID, st.No, st)
			}
		}
	}
	for _, id := range []string{"acme_issue", "acme_renew"} {
		if _, ok := ids[id]; !ok {
			t.Errorf("缺序列模板 %s", id)
		}
	}
	// 签发比续期多几步（注册账户 / 等解析），对账时要能区分。
	if len(ids["acme_issue"].Steps) <= len(ids["acme_renew"].Steps) {
		t.Errorf("acme_issue 步骤数应多于 acme_renew，实际 %d vs %d",
			len(ids["acme_issue"].Steps), len(ids["acme_renew"].Steps))
	}
}

func TestCatalogMinimalSet(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := r.Catalog()

	// 服务类型：4 个 caddy 内置已装 + l4_proxy 未装（ADR-043 §1/§2）。
	installed := map[string]string{}
	for _, t := range c.ServiceTypes {
		if t.Installed {
			installed[t.ID] = t.From
		}
	}
	for _, id := range []string{"reverse_proxy", "file_server", "redirect", "respond"} {
		if installed[id] != "caddy" {
			t.Errorf("服务类型 %s 应为 caddy 内置已装，实际 %q", id, installed[id])
		}
	}
	if _, ok := installed["l4_proxy"]; ok {
		t.Error("l4_proxy 依赖 caddy-l4，本轮不应标为已装")
	}
	// ADR-043 §2：不得再有 tcp_proxy / udp_proxy 两个类型。
	for _, st := range c.ServiceTypes {
		if st.ID == "tcp_proxy" || st.ID == "udp_proxy" {
			t.Errorf("L4 类型已统一为 l4_proxy，不应存在 %s", st.ID)
		}
	}
	// 中间件类型：6 个全部 caddy 内置。
	if len(c.MiddlewareTypes) != 6 {
		t.Errorf("中间件类型应为 6 个，实际 %d", len(c.MiddlewareTypes))
	}
	for _, mt := range c.MiddlewareTypes {
		if !mt.Installed || mt.From != "caddy" {
			t.Errorf("中间件类型 %s 应为 caddy 内置已装: %+v", mt.ID, mt)
		}
	}
	if len(c.EntrypointProtocols) == 0 || len(c.Abilities) == 0 {
		t.Error("入口协议表或能力表为空")
	}
}

func TestRouteBackendRequiredAndServiceOptional(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := r.Spec(KindRoute)

	var backend, service *Field
	for i := range sp.Schema {
		switch sp.Schema[i].Key {
		case "backend":
			backend = &sp.Schema[i]
		case "service":
			service = &sp.Schema[i]
		}
	}
	if backend == nil {
		t.Fatal("route.schema 缺 backend（ADR-043 §3）")
	}
	if !backend.Required || backend.Type != FieldObject {
		t.Errorf("backend 应为必填 object: %+v", backend)
	}
	if len(backend.Fields) != 2 {
		t.Fatalf("backend 应含 host/port 两个子字段，实际 %d", len(backend.Fields))
	}
	if backend.Fields[0].Key != "host" || backend.Fields[1].Key != "port" {
		t.Errorf("backend 子字段应为 host/port: %+v", backend.Fields)
	}
	if service == nil {
		t.Fatal("route.schema 缺 service")
	}
	if service.Required {
		t.Error("route.service 应为可选引用（L4 转发无上游服务，ADR-043 §3）")
	}
	if service.Type != FieldRef || service.Reference == nil || len(service.Reference.Types) != 1 || service.Reference.Types[0] != KindService {
		t.Errorf("service 应引用 service 对象: %+v", service)
	}
}

func TestKeyFields(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		KindRoute:      "name",
		KindService:    "name",
		KindMiddleware: "name",
		KindEntrypoint: "protocol",
		KindDomain:     "name",
		KindUser:       "name",
		KindHost:       "host",
	}
	for kind, want := range cases {
		f, ok := r.KeyField(kind)
		if !ok {
			t.Fatalf("%s 推不出主键", kind)
		}
		if f.Key != want {
			t.Errorf("%s 主键应为 %s，实际 %s", kind, want, f.Key)
		}
	}
}

func TestExpandType(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	// file_server 追加 root / browse。
	f, err := r.ExpandType(KindService, "file_server")
	if err != nil {
		t.Fatal(err)
	}
	if !hasKey(f, "root") || !hasKey(f, "browse") {
		t.Errorf("file_server 应追加 root/browse: %+v", f)
	}
	// reverse_proxy 不追加 file_server 专属参数（browse）。
	f, _ = r.ExpandType(KindService, "reverse_proxy")
	if hasKey(f, "browse") {
		t.Error("reverse_proxy 不应追加 browse")
	}
	// l4_proxy 追加 network。
	f, _ = r.ExpandType(KindService, "l4_proxy")
	if !hasKey(f, "network") {
		t.Error("l4_proxy 应追加 network")
	}
	// basic_auth 追加 users（catalog 与 middleware.typeParams 同源，去重后只有一份）。
	f, _ = r.ExpandType(KindMiddleware, "basic_auth")
	n := 0
	for _, x := range f {
		if x.Key == "users" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("users 字段应去重为 1 份，实际 %d", n)
	}
	// 未知类型报错。
	if _, err := r.ExpandType(KindService, "nope"); err == nil {
		t.Error("未知类型应报错")
	}
}

func TestFlatten(t *testing.T) {
	in := []Field{{
		Key: "backend", Type: FieldObject,
		Fields: []Field{{Key: "host", Type: FieldText}, {Key: "port", Type: FieldNumber}},
	}}
	out := Flatten(in)
	if len(out) != 3 {
		t.Fatalf("应展开为 3 个字段，实际 %d", len(out))
	}
	if out[1].Key != "backend.host" || out[2].Key != "backend.port" {
		t.Errorf("扁平键应为 backend.host / backend.port: %q %q", out[1].Key, out[2].Key)
	}
}

func TestValidate(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	exists := func(kind, id string) bool {
		switch kind {
		case KindService:
			return id == "svc-aria"
		case KindDomain:
			return id == "neob.cn"
		case KindEntrypoint:
			return id == "https" || id == "http" || id == "ssh"
		}
		return false
	}

	// 合法的 L4 路由：无 service，自持 backend。
	errs := r.Validate(KindRoute, map[string]any{
		"name":       "ssh-fwd",
		"backend":    map[string]any{"host": "192.168.1.10", "port": 22.0},
		"entrypoint": "ssh",
		"tls":        "off",
		"enabled":    true,
	}, exists)
	if len(errs) != 0 {
		t.Errorf("合法路由不应报错: %+v", errs)
	}

	// 缺 backend → 必填错误。
	errs = r.Validate(KindRoute, map[string]any{"name": "x", "entrypoint": "https"}, exists)
	if !hasErr(errs, "backend") {
		t.Errorf("缺 backend 应报错: %+v", errs)
	}

	// 悬空 service 引用。
	errs = r.Validate(KindRoute, map[string]any{
		"name": "x", "service": "svc-nope",
		"backend": map[string]any{"host": "h", "port": 1.0}, "entrypoint": "https",
	}, exists)
	if !hasErr(errs, "service") {
		t.Errorf("悬空 service 引用应报错: %+v", errs)
	}

	// 未安装类型：l4_proxy（service 侧）。
	errs = r.Validate(KindService, map[string]any{
		"name": "l4", "type": "l4_proxy",
		"backend": map[string]any{"host": "h", "port": 1.0},
	}, exists)
	if !hasErr(errs, "type") {
		t.Errorf("未安装类型应报错: %+v", errs)
	}

	// 目录外的类型。
	errs = r.Validate(KindService, map[string]any{
		"name": "x", "type": "not_in_catalog",
		"backend": map[string]any{"host": "h", "port": 1.0},
	}, exists)
	if !hasErr(errs, "type") {
		t.Errorf("目录外类型应报错: %+v", errs)
	}

	// 根域名 pattern。
	errs = r.Validate(KindDomain, map[string]any{"name": "不合法域名!"}, exists)
	if !hasErr(errs, "name") {
		t.Errorf("非法根域名应报错: %+v", errs)
	}
	// 悬空根域名引用（数组）。
	errs = r.Validate(KindRoute, map[string]any{
		"name": "x", "backend": map[string]any{"host": "h", "port": 1.0},
		"entrypoint": "https", "roots": []any{"no-such.cn"},
	}, exists)
	if !hasErrPrefix(errs, "roots[0]") {
		t.Errorf("数组元素悬空引用应报错: %+v", errs)
	}
	// 类型不匹配（嵌套 object 的子字段）。
	errs = r.Validate(KindService, map[string]any{
		"name": "x", "type": "reverse_proxy",
		"backend": map[string]any{"host": "h", "port": "not-a-number"},
	}, exists)
	if !hasErr(errs, "backend.port") {
		t.Errorf("port 应校验数字: %+v", errs)
	}
	// 缺 object 子字段。
	errs = r.Validate(KindService, map[string]any{
		"name": "x", "type": "reverse_proxy", "backend": map[string]any{"host": "h"},
	}, exists)
	if !hasErr(errs, "backend.port") {
		t.Errorf("缺 port 应报错: %+v", errs)
	}
}

func TestReferenceTargets(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	refs := r.ReferenceTargets(KindRoute)
	want := map[string][]string{
		"service":     {KindService},
		"roots":       {KindDomain},
		"entrypoint":  {KindEntrypoint},
		"middlewares": {KindMiddleware},
	}
	for _, rt := range refs {
		exp, ok := want[rt.Field]
		if !ok {
			t.Errorf("route 出现意外引用字段 %s", rt.Field)
			continue
		}
		if len(rt.Types) != 1 || rt.Types[0] != exp[0] {
			t.Errorf("%s 应引用 %v，实际 %v", rt.Field, exp, rt.Types)
		}
	}
	who := r.ReferencedBy(KindMiddleware)
	found := false
	for _, k := range who {
		if k == KindRoute {
			found = true
		}
	}
	if !found {
		t.Errorf("middleware 应被 route 引用，实际 %v", who)
	}
}

func TestLoadRejectsBrokenModel(t *testing.T) {
	// 无 kind 的文件被跳过。
	fsys := fstest.MapFS{
		"_naming.yaml": {Data: []byte("a: 1\n")},
		"catalog.yaml": {Data: []byte("kind: catalog\nservice_types: [{id: a, label: A, from: caddy, installed: true}]\n")},
		"x.yaml":       {Data: []byte("kind: x\ntitle: X\ngroup: G\nschema: [{key: n, label: N, type: text}]\n")},
	}
	if _, err := LoadFS(fsys); err != nil {
		t.Fatalf("应能加载: %v", err)
	}
	// 缺 catalog 必须失败（否则所有下拉都空）。
	if _, err := LoadFS(fstest.MapFS{"x.yaml": {Data: []byte("kind: x\n")}}); err == nil {
		t.Fatal("缺类型目录应报错")
	}
}

func hasKey(list []Field, key string) bool {
	for _, f := range list {
		if f.Key == key {
			return true
		}
	}
	return false
}

func hasErr(errs []FieldError, field string) bool { return hasErrPrefix(errs, field) }

func hasErrPrefix(errs []FieldError, prefix string) bool {
	for _, e := range errs {
		if e.Field == prefix {
			return true
		}
	}
	return false
}
