package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/adapter/mosdns"
)

// newMosdnsTest 构造仅含 mosdns 路由的测试服务器(reg=nil,不探测进程)。
func newMosdnsTest(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	RegisterMosdns(mux, mosdns.NewManager(t.TempDir()), nil)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// mosdnsReq 发起请求并返回状态码与响应原文(raw 为 JSON 字符串时直接作为 body)。
func mosdnsReq(t *testing.T, method, url, raw string) (int, string) {
	t.Helper()
	var body any
	if raw != "" {
		body = json.RawMessage(raw)
	}
	var out json.RawMessage
	resp := doJSON(t, method, url, body, &out)
	return resp.StatusCode, string(out)
}

func TestMosdnsStatusAndSettings(t *testing.T) {
	ts := newMosdnsTest(t)

	code, body := mosdnsReq(t, http.MethodGet, ts.URL+"/api/v1/mosdns/status", "")
	if code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", code, body)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("status 非 JSON: %v", err)
	}
	if st["configExists"] != false {
		t.Errorf("初始 configExists 应为 false: %v", st["configExists"])
	}
	if st["listen"] != mosdns.DefaultListen {
		t.Errorf("默认 listen 错误: %v", st["listen"])
	}

	// 保存基础设置 → 生成配置。
	code, body = mosdnsReq(t, http.MethodPut, ts.URL+"/api/v1/mosdns/settings",
		`{"listen":":5353","logLevel":"warn","localDns":["1.1.1.1"],"remoteDns":["tls://8.8.8.8"],"cache":true,"cacheSize":4096}`)
	if code != http.StatusOK {
		t.Fatalf("putSettings code=%d body=%s", code, body)
	}

	code, body = mosdnsReq(t, http.MethodGet, ts.URL+"/api/v1/mosdns/config", "")
	if code != http.StatusOK || !strings.Contains(body, `"configured":true`) {
		t.Fatalf("getConfig code=%d body=%s", code, body)
	}
	if !strings.Contains(body, ":5353") {
		t.Errorf("生成配置应含监听端口: %s", body)
	}
}

func TestMosdnsSettingsValidation(t *testing.T) {
	ts := newMosdnsTest(t)
	cases := []string{
		`{"listen":"noport","localDns":["1.1.1.1"],"remoteDns":["8.8.8.8"]}`,
		`{"listen":":53","localDns":[],"remoteDns":["8.8.8.8"]}`,
		`{"listen":":53","localDns":["1.1.1.1"],"remoteDns":["8.8.8.8"],"cache":true,"cacheSize":0}`,
	}
	for _, c := range cases {
		if code, _ := mosdnsReq(t, http.MethodPut, ts.URL+"/api/v1/mosdns/settings", c); code != http.StatusBadRequest {
			t.Errorf("非法设置应 400, body=%s code=%d", c, code)
		}
	}
}

func TestMosdnsHostsEndpoints(t *testing.T) {
	ts := newMosdnsTest(t)

	// 首次保存设置以生成配置(hosts 路径随之确定)。
	mosdnsReq(t, http.MethodPut, ts.URL+"/api/v1/mosdns/settings",
		`{"listen":":5335","logLevel":"info","localDns":["1.1.1.1"],"remoteDns":["8.8.8.8"],"cache":false,"cacheSize":0}`)

	code, body := mosdnsReq(t, http.MethodPut, ts.URL+"/api/v1/mosdns/hosts",
		`{"hosts":[{"domain":"router.lan","ips":["192.168.1.1"]},{"domain":"nas.home","ips":["192.168.1.2","2001:db8::1"]}]}`)
	if code != http.StatusOK {
		t.Fatalf("putHosts code=%d body=%s", code, body)
	}

	code, body = mosdnsReq(t, http.MethodGet, ts.URL+"/api/v1/mosdns/hosts", "")
	if code != http.StatusOK || !strings.Contains(body, "router.lan") {
		t.Fatalf("getHosts code=%d body=%s", code, body)
	}

	if code, _ := mosdnsReq(t, http.MethodPut, ts.URL+"/api/v1/mosdns/hosts",
		`{"hosts":[{"domain":"bad.lan","ips":["not-ip"]}]}`); code != http.StatusBadRequest {
		t.Errorf("非法 IP 应 400, code=%d", code)
	}
}

func TestMosdnsConfigValidation(t *testing.T) {
	ts := newMosdnsTest(t)
	if code, _ := mosdnsReq(t, http.MethodPut, ts.URL+"/api/v1/mosdns/config", `{"content":"plugins: []"}`); code != http.StatusBadRequest {
		t.Error("空 plugins 应 400")
	}
	valid := `{"content":"log:\n  level: info\nplugins:\n  - tag: hosts\n    type: hosts\n    args: {}\n"}`
	if code, body := mosdnsReq(t, http.MethodPut, ts.URL+"/api/v1/mosdns/config", valid); code != http.StatusOK {
		t.Errorf("合法配置应 200, code=%d body=%s", code, body)
	}
}

func TestMosdnsFlushWithoutCache(t *testing.T) {
	ts := newMosdnsTest(t)
	mosdnsReq(t, http.MethodPut, ts.URL+"/api/v1/mosdns/settings",
		`{"listen":":5335","logLevel":"info","localDns":["1.1.1.1"],"remoteDns":["8.8.8.8"],"cache":false,"cacheSize":0}`)
	if code, _ := mosdnsReq(t, http.MethodPost, ts.URL+"/api/v1/mosdns/flush", ""); code != http.StatusBadGateway {
		t.Errorf("无缓存插件时刷新应 502, code=%d", code)
	}
}
