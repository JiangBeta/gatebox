package repository

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/JiangBeta/gatebox/internal/model"
	bolt "go.etcd.io/bbolt"
)

func TestServiceCRUD(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	svc := &model.Service{
		ID:        "s1",
		AppID:     "app1",
		Name:      "jellyfin",
		Type:      model.RouteTypeReverseProxy,
		Domains:   []model.ProxyDomain{{Protocol: model.DomainProtoHTTPS, Subdomain: "jellyfin", RootDomain: "neob.cn"}},
		Upstream:  []string{"127.0.0.1:8096"},
		HealthURI: "/health",
		Enabled:   true,
	}
	if err := s.SaveService(svc); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.GetService("s1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Domains[0].Host() != "jellyfin.neob.cn" || len(got.Upstream) != 1 || got.Upstream[0] != "127.0.0.1:8096" {
		t.Fatalf("字段不一致: %+v", got)
	}
	if !got.Enabled {
		t.Fatal("enabled 应为 true")
	}
	list, _ := s.ListServices()
	if len(list) != 1 {
		t.Fatalf("服务数 = %d, want 1", len(list))
	}
	byApp, _ := s.ListServicesByApp("app1")
	if len(byApp) != 1 {
		t.Fatalf("按 app 服务数 = %d, want 1", len(byApp))
	}
	if err := s.DeleteService("s1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetService("s1"); err != ErrNotFound {
		t.Fatalf("删除后应 NotFound, got %v", err)
	}
}

func TestAppCascadeQuery(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	app := &model.App{ID: "app1", Name: "媒体"}
	if err := s.SaveApp(app); err != nil {
		t.Fatalf("save app: %v", err)
	}
	if err := s.SaveService(&model.Service{ID: "s1", AppID: "app1", Name: "jellyfin", Domains: []model.ProxyDomain{{RootDomain: "neob.cn"}}}); err != nil {
		t.Fatalf("save svc: %v", err)
	}
	gotApp, err := s.GetApp("app1")
	if err != nil || gotApp.Name != "媒体" {
		t.Fatalf("get app: %v %v", gotApp, err)
	}
	if err := s.DeleteApp("app1"); err != nil {
		t.Fatalf("delete app: %v", err)
	}
	if _, err := s.GetApp("app1"); err != ErrNotFound {
		t.Fatalf("删除后应 NotFound, got %v", err)
	}
	// 服务仍残留(级联由 API 层负责,勿层层复样板)
	svc, err := s.GetService("s1")
	if err != nil || svc == nil {
		t.Fatalf("get svc %v %v", svc, err)
	}
}

func TestFragmentEncryptedAtRest(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	f := &model.Fragment{
		ID: "f1", Name: "auth",
		Code: "basic_auth {\n\tadmin secretToken\n}",
	}
	if err := s.SaveFragment(f); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.GetFragment("f1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Code != f.Code {
		t.Fatalf("解密不一致: %q", got.Code)
	}
	var raw []byte
	_ = s.db.View(func(tx *bolt.Tx) error {
		raw = tx.Bucket(bucketFragments).Get([]byte("f1"))
		return nil
	})
	if bytes.Contains(raw, []byte("secretToken")) {
		t.Fatal("bucket 原始字节不应包含明文 token")
	}
}

func TestVariableRoundTrip(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	v := &model.Variable{Key: "BACKEND_IP", Value: "192.168.1.10"}
	if err := s.SaveVariable(v); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.GetVariable("BACKEND_IP")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Value != "192.168.1.10" {
		t.Fatalf("值不一致: %q", got.Value)
	}
	list, _ := s.ListVariables()
	if len(list) != 1 {
		t.Fatalf("变量数 = %d, want 1", len(list))
	}
	if err := s.DeleteVariable("BACKEND_IP"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetVariable("BACKEND_IP"); err != ErrNotFound {
		t.Fatalf("删除后应 NotFound, got %v", err)
	}
}

// TestMigrateVariables 变量统一迁移(ADR-035 §8):导入无冲突项、跳过同名异值与非法键并报告。
func TestMigrateVariables(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// 构造历史遗留:往旧桶写记录,网关侧预置同名异值,并清除首次迁移标记以重跑。
	err = s.db.Update(func(tx *bolt.Tx) error {
		src, err := tx.CreateBucketIfNotExists(bucketContainerVariables)
		if err != nil {
			return err
		}
		put := func(v model.Variable) error {
			plain, _ := json.Marshal(v)
			return src.Put([]byte(v.Key), plain)
		}
		for _, v := range []model.Variable{
			{Key: "NEW_VAR", Value: "from-container"},
			{Key: "SHARED", Value: "container"},
			{Key: "BAD-KEY", Value: "x"},
		} {
			if err := put(v); err != nil {
				return err
			}
		}
		dst := tx.Bucket(bucketVariables)
		plain, _ := json.Marshal(model.Variable{Key: "SHARED", Value: "gateway"})
		if err := dst.Put([]byte("SHARED"), plain); err != nil {
			return err
		}
		meta := tx.Bucket(bucketMeta)
		if err := meta.Delete([]byte(metaKeyVariablesMigrated)); err != nil {
			return err
		}
		return meta.Delete([]byte(metaKeyVariablesReport))
	})
	if err != nil {
		t.Fatalf("prepare legacy: %v", err)
	}

	if err := s.MigrateVariables(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// NEW_VAR 已导入;SHARED 保留网关原值;BAD-KEY 未导入。
	got, err := s.GetVariable("NEW_VAR")
	if err != nil || got.Value != "from-container" {
		t.Fatalf("NEW_VAR 应导入, got=%+v err=%v", got, err)
	}
	shared, _ := s.GetVariable("SHARED")
	if shared == nil || shared.Value != "gateway" {
		t.Fatalf("SHARED 应保留网关值, got=%+v", shared)
	}
	if _, err := s.GetVariable("BAD-KEY"); err != ErrNotFound {
		t.Errorf("非法键不应导入, err=%v", err)
	}

	rep, err := s.VariableMigrationReport()
	if err != nil || rep == nil {
		t.Fatalf("迁移报告缺失: %+v err=%v", rep, err)
	}
	if rep.Migrated != 1 || len(rep.Conflicts) != 2 {
		t.Fatalf("报告 = migrated:%d conflicts:%d, want 1/2", rep.Migrated, len(rep.Conflicts))
	}
	if err := s.ClearVariableMigrationReport(); err != nil {
		t.Fatalf("clear report: %v", err)
	}
	if rep, _ := s.VariableMigrationReport(); rep != nil {
		t.Errorf("清除后报告应为 nil, got=%+v", rep)
	}
}

// TestVariableKeyCharset 键名规则收敛为 compose 可识别的字符集(ADR-035 §3)。
func TestVariableKeyCharset(t *testing.T) {
	valid := []string{"BACKEND_IP", "_x", "A1", "a_b_c"}
	invalid := []string{"", " GB_FOO", "GB_FOO", "1ABC", "A-B", "A B", "中文"}
	for _, k := range valid {
		if !model.ValidVariableKey(k) {
			t.Errorf("%q 应合法", k)
		}
	}
	for _, k := range invalid {
		if model.ValidVariableKey(k) {
			t.Errorf("%q 应非法", k)
		}
	}
}

func TestDerivedDisabled(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	got, err := s.ListDerivedDisabled()
	if err != nil || len(got) != 0 {
		t.Fatalf("初始应为空: %v err=%v", got, err)
	}
	if err := s.SetDerivedDisabled("docker:p~s~h~0", true); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ = s.ListDerivedDisabled()
	if !got["docker:p~s~h~0"] {
		t.Errorf("应含已停止派生: %v", got)
	}
	if err := s.SetDerivedDisabled("docker:p~s~h~0", false); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = s.ListDerivedDisabled()
	if len(got) != 0 {
		t.Errorf("清除后应为空: %v", got)
	}
}
