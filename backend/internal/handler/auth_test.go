package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthFlow(t *testing.T) {
	sum := sha256.Sum256([]byte("s3cret"))
	a := NewAuth(hex.EncodeToString(sum[:]))
	if !a.Enabled() {
		t.Fatal("应启用鉴权")
	}

	mux := http.NewServeMux()
	a.Register(mux)
	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	h := a.Middleware(mux)
	ts := httptest.NewServer(h)
	defer ts.Close()

	// 未登录 → 401
	if code, _ := authReq(t, http.MethodGet, ts.URL+"/api/v1/ping", ""); code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401, got %d", code)
	}
	// 健康检查放行
	if code, _ := authReq(t, http.MethodGet, ts.URL+"/api/v1/health", ""); code != http.StatusOK {
		t.Fatalf("health 应放行, got %d", code)
	}
	// 错误口令 → 401
	if code, _ := authReq(t, http.MethodPost, ts.URL+"/api/v1/auth/login", `{"password":"wrong"}`); code != http.StatusUnauthorized {
		t.Fatalf("错误口令应 401, got %d", code)
	}

	// 正确口令 → 拿到会话 Cookie
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"password":"s3cret"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录应 200, got %d", resp.StatusCode)
	}
	var sess *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			sess = c
		}
	}
	if sess == nil {
		t.Fatal("未下发会话 Cookie")
	}
	// 带会话 → 放行
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/ping", nil)
	req2.AddCookie(sess)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("带会话应放行, got %d", resp2.StatusCode)
	}
}

func TestAuthDisabled(t *testing.T) {
	if NewAuth("").Enabled() {
		t.Error("空口令应不启用")
	}
	mux := http.NewServeMux()
	NewAuth("").Register(mux)
	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	ts := httptest.NewServer(NewAuth("").Middleware(mux))
	defer ts.Close()
	if code, _ := authReq(t, http.MethodGet, ts.URL+"/api/v1/ping", ""); code != http.StatusOK {
		t.Errorf("未启用时应放行, got %d", code)
	}
}

func authReq(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	r, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}
