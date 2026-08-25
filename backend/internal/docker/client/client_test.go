package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newTestClient 起一个 httptest 服务并返回指向它的 Client。
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("解析 httptest URL: %v", err)
	}
	return NewHTTP(ts.Client(), u.Scheme, u.Host)
}

func TestBuildURL(t *testing.T) {
	c := NewUnix("/var/run/docker.sock")

	got := c.buildURL("/containers/json", nil)
	want := "http://docker/" + APIVersion + "/containers/json"
	if got != want {
		t.Errorf("buildURL 无 query = %q, want %q", got, want)
	}

	q := url.Values{}
	q.Set("all", "1")
	q.Set("limit", "5")
	got = c.buildURL("/containers/json", q)
	if !strings.HasPrefix(got, want+"?") {
		t.Errorf("buildURL 带 query = %q, 应以 %q? 开头", got, want)
	}
	if !strings.Contains(got, "all=1") || !strings.Contains(got, "limit=5") {
		t.Errorf("buildURL 丢失查询参数: %q", got)
	}
}

// TestVersionPrefix 固化 ADR-014 的关键决策:请求路径必须携带 API 版本号。
func TestVersionPrefix(t *testing.T) {
	var gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[]`))
	})

	var out []any
	if err := c.getJSON(context.Background(), "/containers/json", nil, &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	want := "/" + APIVersion + "/containers/json"
	if gotPath != want {
		t.Errorf("请求路径 = %q, want %q", gotPath, want)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"404 映射 ErrNotFound", http.StatusNotFound, `{"message":"No such container: abc"}`, ErrNotFound},
		{"409 映射 ErrConflict", http.StatusConflict, `{"message":"container is running"}`, ErrConflict},
		{"304 映射 ErrNotModified", http.StatusNotModified, ``, ErrNotModified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if tc.body != "" {
					w.Write([]byte(tc.body))
				}
			})
			err := c.getJSON(context.Background(), "/containers/abc/json", nil, nil)
			if err == nil {
				t.Fatal("应返回错误")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.want)
			}
			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("应可断言为 *Error, got %T", err)
			}
			if apiErr.Status != tc.status {
				t.Errorf("Status = %d, want %d", apiErr.Status, tc.status)
			}
		})
	}
}

// TestErrorMessageExtracted 确认 daemon 的 message 被提取出来 ——
// 这是排障时用户唯一能看到的线索。
func TestErrorMessageExtracted(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"No such container: deadbeef"}`))
	})
	err := c.getJSON(context.Background(), "/containers/deadbeef/json", nil, nil)
	if !strings.Contains(err.Error(), "No such container: deadbeef") {
		t.Errorf("错误信息丢失 daemon message: %v", err)
	}
}

func TestParseError_NonJSONBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("plain text failure"))
	})
	err := c.getJSON(context.Background(), "/x", nil, nil)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "plain text failure") {
		t.Errorf("非 JSON 响应体应原样带出, got: %v", err)
	}
}

func TestParseError_EmptyBodyFallsBackToStatusText(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	err := c.getJSON(context.Background(), "/x", nil, nil)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), http.StatusText(http.StatusBadGateway)) {
		t.Errorf("空响应体应回退到状态文案, got: %v", err)
	}
}

func TestGetJSON_DecodesBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Id":"abc123","Name":"/jellyfin"}`))
	})
	var out struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := c.getJSON(context.Background(), "/containers/abc123/json", nil, &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if out.ID != "abc123" || out.Name != "/jellyfin" {
		t.Errorf("解码结果 = %+v", out)
	}
}

func TestPostJSON_SendsBodyAndContentType(t *testing.T) {
	var gotCT, gotBody string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		w.Write([]byte(`{"Id":"net1"}`))
	})

	in := map[string]string{"Name": "gatebox-net"}
	var out struct {
		ID string `json:"Id"`
	}
	if err := c.postJSON(context.Background(), "/networks/create", nil, in, &out); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if !strings.Contains(gotBody, "gatebox-net") {
		t.Errorf("请求体 = %q", gotBody)
	}
	if out.ID != "net1" {
		t.Errorf("响应解码 = %+v", out)
	}
}

func TestContextCancellation(t *testing.T) {
	// 流式端点的「终止」依赖 context 取消(如镜像拉取的终止按钮,ADR 无对应 API)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.getJSON(ctx, "/containers/json", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestPing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.55")
		w.WriteHeader(http.StatusOK)
	})
	v, err := c.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if v != "1.55" {
		t.Errorf("API 版本 = %q, want 1.55", v)
	}
}

func TestBoolArg(t *testing.T) {
	if boolArg(true) != "1" || boolArg(false) != "0" {
		t.Error("boolArg 应返回 Engine API 期望的 1/0")
	}
}

func TestNewUnix_DefaultSocket(t *testing.T) {
	c := NewUnix("")
	if c.dial == nil {
		t.Fatal("dial 未初始化")
	}
	// 仅验证构造不 panic 且 URL 可用;真实连通性由集成测试覆盖
	if !strings.HasPrefix(c.buildURL("/_ping", nil), "http://docker/") {
		t.Errorf("URL = %q", c.buildURL("/_ping", nil))
	}
}
