package component

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/source"
)

// TestApplyVariant 变体解析 + 下载 + 版本化安装（不触发重启）。
func TestApplyVariant(t *testing.T) {
	features := []string{"github.com/mholt/caddy-l4"}
	key := extension.VariantKey("caddy", "2.10.0", "linux", archOf(), features)

	var binURL string
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()
	binURL = ts.URL + "/caddy.bin"
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema": "gatebox.catalog/v1",
			"variants": []map[string]any{{
				"key": key, "component": "caddy", "version": "2.10.0",
				"features": features, "os": "linux", "arch": archOf(),
				"url": binURL, "sha256": "",
			}},
		})
	})
	mux.HandleFunc("/caddy.bin", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("caddy-variant-binary"))
	})

	dir := t.TempDir()
	reg := &CoreRegistry{dataDir: dir, src: source.New("")}
	dest, err := reg.ApplyVariant(context.Background(), "caddy", "2.10.0", features, ts.URL+"/index.json")
	if err != nil {
		t.Fatalf("ApplyVariant: %v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "caddy-variant-binary" {
		t.Fatalf("active 内容错误: %q", b)
	}
	// 版本留档（dest 是 tools/caddy/caddy，版本目录在 tools/caddy/versions/<key>）
	entries, _ := os.ReadDir(filepath.Join(dir, "tools", "caddy", "versions"))
	if len(entries) != 1 {
		t.Fatalf("应有一个版本留档目录, got %d", len(entries))
	}
}

// TestApplyVariantNotFound 索引无匹配变体时报错（提示按需构建）。
func TestApplyVariantNotFound(t *testing.T) {
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": "gatebox.catalog/v1", "variants": []any{}})
	})
	reg := &CoreRegistry{dataDir: t.TempDir(), src: source.New("")}
	if _, err := reg.ApplyVariant(context.Background(), "caddy", "2.10.0", nil, ts.URL+"/index.json"); err == nil {
		t.Fatal("无匹配变体应报错")
	}
}
