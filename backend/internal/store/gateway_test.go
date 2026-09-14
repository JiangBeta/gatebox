package store

import (
	"bytes"
	"testing"

	"github.com/JiangBeta/gatebox/internal/models"
	bolt "go.etcd.io/bbolt"
)

func TestServiceCRUD(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	svc := &models.Service{
		ID:        "s1",
		AppID:     "app1",
		Name:      "jellyfin",
		Type:      models.RouteTypeReverseProxy,
		Domains:   []models.ProxyDomain{{Protocol: models.DomainProtoHTTPS, Subdomain: "jellyfin", RootDomain: "neob.cn"}},
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

	app := &models.App{ID: "app1", Name: "媒体"}
	if err := s.SaveApp(app); err != nil {
		t.Fatalf("save app: %v", err)
	}
	if err := s.SaveService(&models.Service{ID: "s1", AppID: "app1", Name: "jellyfin", Domains: []models.ProxyDomain{{RootDomain: "neob.cn"}}}); err != nil {
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

	f := &models.Fragment{
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

	v := &models.Variable{Key: "BACKEND_IP", Value: "192.168.1.10"}
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

// TestContainerVariables_Independent 容器变量与网关变量独立存储。
func TestContainerVariables_Independent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if err := s.SaveContainerVariable(&models.Variable{Key: "MY_VAR", Value: "x"}); err != nil {
		t.Fatalf("save container var: %v", err)
	}
	// 网关 bucket 不受影响:同一 key 不存在于网关。
	if _, err := s.GetVariable("MY_VAR"); err != ErrNotFound {
		t.Errorf("网关变量不应包含容器变量, got err=%v", err)
	}
	got, err := s.GetContainerVariable("MY_VAR")
	if err != nil || got.Value != "x" {
		t.Errorf("容器变量读取失败: %+v err=%v", got, err)
	}
	if err := s.DeleteContainerVariable("MY_VAR"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetContainerVariable("MY_VAR"); err != ErrNotFound {
		t.Errorf("删除后应不存在, err=%v", err)
	}
}
