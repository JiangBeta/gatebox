package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/source"
)

// TestRefreshOnlineMerge 在线目录合并：内置优先、requires 不兼容被过滤。
func TestRefreshOnlineMerge(t *testing.T) {
	repo, err := repository.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer repo.Close()

	manifest := func(id string, requires map[string]string) map[string]any {
		return map[string]any{
			"apiVersion": "gatebox/v2", "kind": "config-only", "id": id,
			"name": id, "version": "1.0.0", "requires": requires,
		}
	}
	payload := map[string]any{
		"schema": "gatebox.catalog/v1",
		"plugins": []map[string]any{
			{"id": "online-x", "versions": []map[string]any{
				{"version": "1.0.0", "channel": "official", "manifest": manifest("online-x", map[string]string{"extensionApi": ">=1"})},
			}},
			{"id": "too-new", "versions": []map[string]any{
				{"version": "9.0.0", "channel": "official", "manifest": manifest("too-new", map[string]string{"extensionApi": ">=99"})},
			}},
			// 与内置同 id → 内置优先，忽略在线版本
			{"id": "caddy-l4", "versions": []map[string]any{
				{"version": "9.9.9", "channel": "official", "manifest": manifest("caddy-l4", nil)},
			}},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer ts.Close()

	m := NewManager(repo, source.New(""), t.TempDir(), extension.NewRegistry())
	m.SetCatalogURL(ts.URL)
	m.SetGateboxVersion("0.4.0")
	if err := m.RefreshOnline(context.Background()); err != nil {
		t.Fatalf("RefreshOnline: %v", err)
	}

	ids := map[string]string{}
	list, _ := m.List()
	for _, v := range list {
		ids[v.ID] = v.Version
	}
	if ids["online-x"] != "1.0.0" {
		t.Errorf("在线插件应合并: %v", ids["online-x"])
	}
	if _, ok := ids["too-new"]; ok {
		t.Error("requires.extensionApi 不兼容的插件应被过滤")
	}
	if ids["caddy-l4"] == "9.9.9" {
		t.Error("与内置同 id 时应以内置为准")
	}
}
