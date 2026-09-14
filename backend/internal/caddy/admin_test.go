package caddy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/load" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "text/caddyfile" {
			t.Errorf("Content-Type = %q, want text/caddyfile", ct)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	const caddyfile = "app.neob.cn {\n\treverse_proxy 127.0.0.1:8096\n}\n"
	if err := c.Load(context.Background(), caddyfile); err != nil {
		t.Fatalf("load: %v", err)
	}
	if gotBody != caddyfile {
		t.Errorf("body = %q, want %q", gotBody, caddyfile)
	}
}

func TestLoadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Error: adapting config using caddyfile: ..."))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.Load(context.Background(), "invalid")
	if err == nil {
		t.Fatal("应报错")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("错误应含状态码 400: %v", err)
	}
}

func TestLoadBodyError(t *testing.T) {
	// caddy 对「适配通过但启动失败」的配置返回 200,错误写在 body 尾随的 JSON 对象里。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(
			`[{"file":"Caddyfile","line":1,"message":"not formatted"}]` +
				`{"error":"loading config: http app: listen :443: permission denied"}`,
		))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.Load(context.Background(), ":443 {\n respond ok\n}\n")
	if err == nil {
		t.Fatal("body 含 error 应报错")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("错误应带 caddy body 信息: %v", err)
	}
}

func TestUpstreams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reverse_proxy/upstreams" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"address":"127.0.0.1:8096","health_status":"healthy"}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ups, err := c.Upstreams(context.Background())
	if err != nil {
		t.Fatalf("upstreams: %v", err)
	}
	if len(ups) != 1 || ups[0].Address != "127.0.0.1:8096" || ups[0].HealthStatus != "healthy" {
		t.Errorf("解析结果错误: %+v", ups)
	}
}

func TestUpstreamsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ups, err := c.Upstreams(context.Background())
	if err != nil {
		t.Fatalf("upstreams: %v", err)
	}
	if ups == nil || len(ups) != 0 {
		t.Errorf("空列表应为非 nil 空切片: %#v", ups)
	}
}
