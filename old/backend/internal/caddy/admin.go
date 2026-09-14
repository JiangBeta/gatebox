package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client Caddy Admin API 客户端(最小实现:POST /load + GET /reverse_proxy/upstreams,ADR-020 §4)。
type Client struct {
	admin string
	http  *http.Client
}

// NewClient 构造 Admin API 客户端。admin 形如 http://localhost:2019。
func NewClient(admin string) *Client {
	return &Client{
		admin: strings.TrimRight(admin, "/"),
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Load 用完整 Caddyfile 文本原子加载配置(ADR-002)。
//
// 采用「先校验、失败回退、成功落盘」:caddy 先 adapt + 校验,通过才原子替换
// 运行时配置;失败返回错误、旧配置继续服务,由调用方决定是否回退落盘。
// 注意:caddy 对「适配通过、启动失败」的配置返回 HTTP 200 并把错误写进 response
// body(尾随 {"error":...}),因此除状态码外还需检测 body(v2.11.4 实测)。
func (c *Client) Load(ctx context.Context, caddyfile string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.admin+"/load", bytes.NewReader([]byte(caddyfile)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("caddy Admin API 不可达: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	body := strings.TrimSpace(string(b))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("caddy /load 失败(%d): %s", resp.StatusCode, body)
	}
	if err := detectLoadBodyError(body); err != nil {
		return err
	}
	return nil
}

// detectLoadBodyError 从 /load 成功响应体中识别 caddy 写回的错误对象。
func detectLoadBodyError(body string) error {
	idx := strings.Index(body, `{"error":`)
	if idx < 0 {
		return nil
	}
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body[idx:]), &e); err != nil {
		return fmt.Errorf("caddy /load 返回错误: %s", body[idx:])
	}
	return fmt.Errorf("caddy /load 失败: %s", e.Error)
}

// Upstream 反向代理 upstream 的健康状态(对应 GET /reverse_proxy/upstreams)。
type Upstream struct {
	Address      string `json:"address"`
	HealthStatus string `json:"health_status"` // healthy | unhealthy
}

// Upstreams 返回所有 reverse_proxy upstream 及其健康状态(ADR-020 §1 健康状态真相源)。
func (c *Client) Upstreams(ctx context.Context) ([]Upstream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.admin+"/reverse_proxy/upstreams", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caddy Admin API 不可达: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("caddy /reverse_proxy/upstreams 失败(%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out []Upstream
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析 upstreams 响应: %w", err)
	}
	if out == nil {
		out = []Upstream{}
	}
	return out, nil
}
