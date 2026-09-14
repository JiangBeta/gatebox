// Package client 提供 Docker Engine API 的轻量 HTTP 封装(ADR-014)。
//
// 不依赖 moby SDK:直接对 Engine API 的 HTTP 契约编程。相比 Go SDK,
// HTTP 契约路径自带版本号、有明确的向后兼容承诺,稳定得多。
//
// 传输层可替换(本地 unix socket / 未来 tcp+TLS / ssh),以支持多主机(ADR-015)。
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIVersion Engine API 版本。显式写进请求路径,Docker 保证向后兼容。
// 宿主机 daemon 需支持 >= 1.40(Docker 19.03+)。
const APIVersion = "v1.44"

// DefaultSocket 本地 Docker 守护进程的默认 unix socket。
const DefaultSocket = "/var/run/docker.sock"

// 语义化哨兵错误。通过 Error.Unwrap 暴露,调用方用 errors.Is 判断。
var (
	ErrNotFound    = errors.New("docker: 资源不存在")
	ErrConflict    = errors.New("docker: 资源冲突")
	ErrNotModified = errors.New("docker: 状态未变更")
)

// Error Docker API 返回的错误响应。
type Error struct {
	Status  int    // HTTP 状态码
	Message string // daemon 返回的 message 字段
}

func (e *Error) Error() string {
	return fmt.Sprintf("docker api %d: %s", e.Status, e.Message)
}

// Unwrap 将常见状态码映射为哨兵错误,支持 errors.Is 判断。
func (e *Error) Unwrap() error {
	switch e.Status {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	case http.StatusNotModified:
		return ErrNotModified
	}
	return nil
}

// Client 到某个 Docker 守护进程的连接。
//
// 不设置 http.Client.Timeout——流式端点(logs follow / stats / events / pull)
// 是长连接,超时一律由调用方通过 context 控制。
type Client struct {
	hc     *http.Client
	scheme string
	host   string // URL 的 host 部分;unix socket 场景下是占位符
	dial   func(ctx context.Context) (net.Conn, error)
}

// NewUnix 连接本地 unix socket。socket 为空时使用 DefaultSocket。
func NewUnix(socket string) *Client {
	if socket == "" {
		socket = DefaultSocket
	}
	dial := func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}
	return &Client{
		hc: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dial(ctx)
				},
				// unix socket 无需连接池保活的复杂策略,但保留空闲连接以复用
				MaxIdleConns:          16,
				IdleConnTimeout:       30 * time.Second,
				ResponseHeaderTimeout: 0, // 流式端点不限制响应头等待
			},
		},
		scheme: "http",
		host:   "docker", // unix socket 下 host 无意义,仅用于构造合法 URL
		dial:   dial,
	}
}

// NewHTTP 连接 TCP 上的 daemon(多主机预留;生产环境须配合 TLS)。
func NewHTTP(hc *http.Client, scheme, host string) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{
		hc:     hc,
		scheme: scheme,
		host:   host,
		dial: func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", host)
		},
	}
}

// Ping 探测 daemon 可达性,返回其报告的 API 版本。
func (c *Client) Ping(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/_ping", nil, nil)
	if err != nil {
		return "", err
	}
	defer drain(resp.Body)
	return resp.Header.Get("Api-Version"), nil
}

// --- 请求辅助 ---

// buildURL 拼装带版本前缀的请求地址。
func (c *Client) buildURL(path string, q url.Values) string {
	u := url.URL{
		Scheme: c.scheme,
		Host:   c.host,
		Path:   "/" + APIVersion + path,
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// do 发起请求并把 >=400 的响应转成 *Error。调用方负责关闭 resp.Body。
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any) (*http.Response, error) {
	return c.doWith(ctx, method, path, q, body, nil)
}

// doWith 同 do,但可附加自定义请求头(如拉取镜像的 X-Registry-Auth)。
//
// body 为 io.Reader 时按原始流发送(用于上传 tar),否则按 JSON 序列化。
func (c *Client) doWith(ctx context.Context, method, path string, q url.Values, body any, headers map[string]string) (*http.Response, error) {
	var (
		rdr         io.Reader
		contentType string
	)
	switch b := body.(type) {
	case nil:
	case io.Reader:
		rdr, contentType = b, "application/x-tar"
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体: %w", err)
		}
		rdr, contentType = bytes.NewReader(raw), "application/json"
	}

	req, err := http.NewRequestWithContext(ctx, method, c.buildURL(path, q), rdr)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	// 304 无响应体,但语义上是「已是目标状态」,同样按错误返回供调用方用 errors.Is 忽略
	if resp.StatusCode >= 400 || resp.StatusCode == http.StatusNotModified {
		defer drain(resp.Body)
		return nil, parseError(resp)
	}
	return resp, nil
}

// getJSON GET 并把响应体解码到 out。out 为 nil 时丢弃响应体。
func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return err
	}
	defer drain(resp.Body)
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// postJSON POST 并可选解码响应体。
func (c *Client) postJSON(ctx context.Context, path string, q url.Values, body, out any) error {
	resp, err := c.do(ctx, http.MethodPost, path, q, body)
	if err != nil {
		return err
	}
	defer drain(resp.Body)
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// deleteReq DELETE 并可选解码响应体。
func (c *Client) deleteReq(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.do(ctx, http.MethodDelete, path, q, nil)
	if err != nil {
		return err
	}
	defer drain(resp.Body)
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// stream 发起流式请求,返回未读取的响应体。调用方负责 Close。
// 用于 logs follow / stats / events / pull 等长连接端点。
func (c *Client) stream(ctx context.Context, method, path string, q url.Values, body any) (io.ReadCloser, error) {
	return c.streamWith(ctx, method, path, q, body, nil)
}

// streamWith 同 stream,但可附加自定义请求头。
func (c *Client) streamWith(ctx context.Context, method, path string, q url.Values, body any, headers map[string]string) (io.ReadCloser, error) {
	resp, err := c.doWith(ctx, method, path, q, body, headers)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// hijack 建立可双向读写的裸连接,用于 exec attach。
//
// Engine API 的 attach 端点通过 HTTP Upgrade 把连接交还给调用方,
// 之后是裸 TCP 语义,不能再走 http.Client。故此处自行拨号并手写请求。
func (c *Client) hijack(ctx context.Context, path string, q url.Values, body any) (net.Conn, *bufio.Reader, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("序列化请求体: %w", err)
		}
		payload = b
	}

	conn, err := c.dial(ctx)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL(path, q), bytes.NewReader(payload))
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	req.Host = c.host

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("发送 hijack 请求: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("读取 hijack 响应: %w", err)
	}
	// 101 Switching Protocols 是预期结果;200 亦可接受(daemon 不升级直接流式返回)
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		defer conn.Close()
		return nil, nil, parseError(resp)
	}
	return conn, br, nil
}

// --- 内部工具 ---

// parseError 从错误响应中提取 daemon 的 message。
func parseError(resp *http.Response) error {
	e := &Error{Status: resp.StatusCode}
	var payload struct {
		Message string `json:"message"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		e.Message = payload.Message
	} else {
		e.Message = strings.TrimSpace(string(body))
		if e.Message == "" {
			e.Message = http.StatusText(resp.StatusCode)
		}
	}
	return e
}

// drain 读空并关闭响应体,以便底层连接可被复用。
func drain(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, 512<<10))
	_ = rc.Close()
}

// boolArg 把 bool 转成 Engine API 期望的查询参数值。
func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
