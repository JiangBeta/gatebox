package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
	"github.com/JiangBeta/gatebox/internal/gateway"
	"github.com/JiangBeta/gatebox/internal/repository"
)

// newGatewayTest 组装网关 API 测试服务器(无 docker,零值健康采集器,mock caddy admin)。
func newGatewayTest(t *testing.T) *httptest.Server {
	t.Helper()
	return newGatewayTestWithDataDir(t, t.TempDir())
}

// newGatewayTestWithDataDir 同 newGatewayTest,但暴露 dataDir(供断言 Caddyfile 备份等)。
func newGatewayTestWithDataDir(t *testing.T, dataDir string) *httptest.Server {
	return newGatewayTestWithBin(t, dataDir, "")
}

// newGatewayTestWithBin 同 newGatewayTestWithDataDir,另可指定 caddyBin(供版本端点等测试)。
func newGatewayTestWithBin(t *testing.T, dataDir, caddyBin string) *httptest.Server {
	t.Helper()
	s, err := repository.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	caddyAdmin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/load" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"v2.11.4"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(caddyAdmin.Close)

	mux := http.NewServeMux()
	// 零值健康采集器:不启动后台轮询,健康状态恒 unknown。acme issuer 为 nil:测试不触发证书签发。
	// caddyBin 可指定(fake 二进制);空则 validate/version 走 dataDir fallback(缺失 → 降级)。
	RegisterGateway(mux, s, nil, caddy.NewClient(caddyAdmin.URL), &gateway.HealthCollector{}, dataDir, caddyBin, 0, 0, nil, nil)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func doJSON(t *testing.T, method, url string, body any, out any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp
}

const domainRowJSON = `{"protocol":"https","subdomain":"jellyfin","rootDomain":"neob.cn"}`

func createAppWithProxy(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	var created struct {
		ID string `json:"id"`
	}
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/apps", map[string]any{
		"name": "媒体",
		"services": []map[string]any{
			{
				"name": "jellyfin", "type": "reverse_proxy",
				"domains":  []map[string]any{{"protocol": "https", "subdomain": "jellyfin", "rootDomain": "neob.cn"}},
				"upstream": []string{"127.0.0.1:8096"},
			},
		},
	}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建应用状态 = %d, want 201", resp.StatusCode)
	}
	return created.ID
}

func TestGatewayAppCreateGroupedList(t *testing.T) {
	ts := newGatewayTest(t)
	appID := createAppWithProxy(t, ts)

	var list []groupView
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/v1/gateway/apps", nil, &list)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("列表状态 = %d, want 200", resp.StatusCode)
	}
	if len(list) != 1 {
		t.Fatalf("分组数 = %d, want 1", len(list))
	}
	g := list[0]
	if g.Source != "app" || g.Editable != true {
		t.Errorf("group source/editable = %q/%v", g.Source, g.Editable)
	}
	if len(g.Services) != 1 {
		t.Fatalf("服务数 = %d, want 1", len(g.Services))
	}
	if g.Services[0].Source != "manual" || g.Services[0].Name != "jellyfin" {
		t.Errorf("service = %+v", g.Services[0])
	}
	if g.Services[0].HealthURI != "/" {
		t.Errorf("healthUri = %q, want /", g.Services[0].HealthURI)
	}
	_ = appID
}

func TestGatewayServiceStopDelete(t *testing.T) {
	ts := newGatewayTest(t)
	createAppWithProxy(t, ts)
	var list []groupView
	doJSON(t, http.MethodGet, ts.URL+"/api/v1/gateway/apps", nil, &list)
	svcID := list[0].Services[0].ID

	// 删除启用中服务 → 409
	resp := doJSON(t, http.MethodDelete, ts.URL+"/api/v1/gateway/services/"+svcID, nil, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("删除启用中服务状态 = %d, want 409", resp.StatusCode)
	}

	// 停止 → 删除
	resp = doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/services/"+svcID+"/stop", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("停止状态 = %d, want 200", resp.StatusCode)
	}
	resp = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/gateway/services/"+svcID, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("删除状态 = %d, want 204", resp.StatusCode)
	}
}

func TestGatewayAppDeleteCascade(t *testing.T) {
	ts := newGatewayTest(t)
	appID := createAppWithProxy(t, ts)

	// 未停止服务时删 App → 409
	resp := doJSON(t, http.MethodDelete, ts.URL+"/api/v1/gateway/apps/"+appID, nil, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("删除启用中应用状态 = %d, want 409", resp.StatusCode)
	}

	// 停止应用下所有服务后删除 → 204 级联
	var list []groupView
	doJSON(t, http.MethodGet, ts.URL+"/api/v1/gateway/apps", nil, &list)
	for _, g := range list {
		if g.ID == appID {
			for _, svc := range g.Services {
				doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/services/"+svc.ID+"/stop", nil, nil)
			}
		}
	}
	resp = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/gateway/apps/"+appID, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("删除应用状态 = %d, want 204", resp.StatusCode)
	}
	doJSON(t, http.MethodGet, ts.URL+"/api/v1/gateway/apps", nil, &list)
	if len(list) != 0 {
		t.Fatalf("级联后应用数 = %d, want 0", len(list))
	}
}

func TestGatewayFragmentRefDelete(t *testing.T) {
	ts := newGatewayTest(t)

	var f struct {
		ID string `json:"id"`
	}
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/fragments", map[string]any{
		"name": "自定义头", "tag": "handler", "code": "header {\n\tX-Foo bar\n}",
	}, &f)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建片段状态 = %d, want 201", resp.StatusCode)
	}

	// 创建引用该片段的 service
	var app struct {
		ID string `json:"id"`
	}
	doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/apps", map[string]any{
		"name": "app",
		"services": []map[string]any{
			{
				"name": "svc", "type": "reverse_proxy",
				"domains":     []map[string]any{{"protocol": "https", "subdomain": "a", "rootDomain": "neob.cn"}},
				"upstream":    []string{"127.0.0.1:80"},
				"fragmentIds": []string{f.ID},
			},
		},
	}, &app)

	// 被引用时删除片段 → 409
	resp = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/gateway/fragments/"+f.ID, nil, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("删除被引用片段状态 = %d, want 409", resp.StatusCode)
	}
}

func TestGatewayVariableCRUD(t *testing.T) {
	ts := newGatewayTest(t)

	// 保留前缀 GB_ 禁建
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/variables", map[string]any{
		"key": "GB_FOO", "value": "x",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GB_ 前缀状态 = %d, want 400", resp.StatusCode)
	}

	var v struct {
		Key string `json:"key"`
	}
	resp = doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/variables", map[string]any{
		"key": "BACKEND_IP", "value": "192.168.1.10",
	}, &v)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建变量状态 = %d, want 201", resp.StatusCode)
	}

	var list []struct {
		Key string `json:"key"`
	}
	doJSON(t, http.MethodGet, ts.URL+"/api/v1/gateway/variables", nil, &list)
	if len(list) != 1 {
		t.Fatalf("变量数 = %d, want 1", len(list))
	}

	resp = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/gateway/variables/BACKEND_IP", nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("删除变量状态 = %d, want 204", resp.StatusCode)
	}
}

func TestGatewayCreateValidation(t *testing.T) {
	ts := newGatewayTest(t)
	// 反代缺后端
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/apps", map[string]any{
		"name": "x",
		"services": []map[string]any{
			{"name": "s", "type": "reverse_proxy",
				"domains": []map[string]any{{"protocol": "https", "rootDomain": "neob.cn"}}},
		},
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺后端状态 = %d, want 400", resp.StatusCode)
	}
	// 无域名行
	resp = doJSON(t, http.MethodPost, ts.URL+"/api/v1/gateway/apps", map[string]any{
		"name": "y",
		"services": []map[string]any{
			{"name": "s", "type": "reverse_proxy", "upstream": []string{"127.0.0.1:1"}},
		},
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("无域名状态 = %d, want 400", resp.StatusCode)
	}
}

// TestGatewayVersion caddy 版本端点:配置 caddyBin 时透传 CLI 输出;缺失时降级空版本。
func TestGatewayVersion(t *testing.T) {
	dataDir := t.TempDir()
	// 假 caddy 二进制:输出 v2.11.4
	bin := filepath.Join(dataDir, "fake-caddy")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'v2.11.4 h1:abc'\n"), 0o700); err != nil {
		t.Fatalf("写 fake caddy: %v", err)
	}
	ts := newGatewayTestWithBin(t, dataDir, bin)

	var out struct {
		Version   string `json:"version"`
		Reachable bool   `json:"reachable"`
	}
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/v1/gateway/version", nil, &out)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("version 状态 = %d, want 200", resp.StatusCode)
	}
	if out.Version != "v2.11.4" || !out.Reachable {
		t.Errorf("version = %+v, want v2.11.4/reachable", out)
	}
}

// TestGatewayCaddyfileBackup 成功 /load 后把 Caddyfile 备份落到 $DATA_DIR/Caddyfile。
func TestGatewayCaddyfileBackup(t *testing.T) {
	dataDir := t.TempDir()
	ts := newGatewayTestWithDataDir(t, dataDir)
	createAppWithProxy(t, ts)

	path, err := os.ReadFile(filepath.Join(dataDir, "Caddyfile"))
	if err != nil {
		t.Fatalf("读取备份 Caddyfile: %v", err)
	}
	content := string(path)
	if !strings.Contains(content, "jellyfin.neob.cn") {
		t.Errorf("备份应含代理域名, got:\n%s", content)
	}
	if !strings.Contains(content, "reverse_proxy 127.0.0.1:8096") {
		t.Errorf("备份应含反代后端, got:\n%s", content)
	}
}
