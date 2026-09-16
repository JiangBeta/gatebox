package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchCatalogAndVariantByKey(t *testing.T) {
	payload := map[string]any{
		"schema": "gatebox.catalog/v1",
		"variants": []map[string]any{
			{
				"key": "sha256:abc123", "component": "caddy", "version": "2.10.0",
				"features": []string{"github.com/mholt/caddy-l4"}, "os": "linux", "arch": "amd64",
				"url": "https://example.com/caddy.tar.gz", "sha256": "deadbeef",
			},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer ts.Close()

	cat, err := New("").FetchCatalog(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	v, ok := cat.VariantByKey("sha256:abc123")
	if !ok {
		t.Fatal("应命中变体")
	}
	if v.Component != "caddy" || v.Version != "2.10.0" || len(v.Features) != 1 {
		t.Fatalf("变体字段错误: %+v", v)
	}
	if _, ok := cat.VariantByKey("nope"); ok {
		t.Error("不存在的键不应命中")
	}
}

func TestInstallVersioned(t *testing.T) {
	dir := t.TempDir()
	dest, err := InstallVersioned(dir, "caddy", "sha256:key1", []byte("bin-v1"))
	if err != nil {
		t.Fatalf("InstallVersioned: %v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "bin-v1" {
		t.Fatalf("active 内容错误: %s", b)
	}
	// 版本目录留档
	if b, err := os.ReadFile(filepath.Join(dir, "tools", "caddy", "versions", "sha256-key1", "caddy")); err != nil || string(b) != "bin-v1" {
		t.Fatalf("版本留档错误: %v %s", err, b)
	}
	// 再装第二个变体：active 更新，旧版本仍在
	dest2, err := InstallVersioned(dir, "caddy", "sha256:key2", []byte("bin-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest2); string(b) != "bin-v2" {
		t.Fatalf("active 未更新: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "tools", "caddy", "versions", "sha256-key1", "caddy")); err != nil {
		t.Error("旧版本应保留以支持回滚")
	}
}
