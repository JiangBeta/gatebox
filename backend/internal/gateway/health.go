// Package gateway 提供网关单位的运行时能力:docker route 派生与健康采集。
package gateway

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
)

// 健康状态值(前端据此渲染绿/红/灰)。
const (
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
	HealthUnknown   = "unknown"
)

// DefaultHealthInterval 健康轮询周期。
const DefaultHealthInterval = 5 * time.Second

// probeTimeout 单次主动探测超时。
const probeTimeout = 2 * time.Second

// upstreamFetcher 健康状态来源(caddy.Client 实现),便于测试注入。
type upstreamFetcher interface {
	Upstreams(ctx context.Context) ([]caddy.Upstream, error)
}

// HealthCollector 单例健康采集器(ADR-020 §1):定期轮询 caddy Admin API,
// 聚合 upstream 健康快照供前端轮询,避免列表逐行实时查 caddy 打爆 Admin API。
//
// 注意:caddy 的 GET /reverse_proxy/upstreams 自 2.5 起只返回
// address/num_requests/fails,不再暴露主动健康状态(动态 upstream 回归),
// 因此无 health_status 时由本采集器主动 TCP 探测地址可达性作为兜底。
type HealthCollector struct {
	fetch    upstreamFetcher
	interval time.Duration
	// probe 主动探测后端地址是否可达;默认 probeTCP。可为测试注入。
	probe func(ctx context.Context, address string) bool

	mu        sync.Mutex
	status    map[string]string // address → healthy|unhealthy
	reachable bool              // 最近一次轮询 caddy 是否可达
}

// NewHealthCollector 构造采集器并启动后台轮询。
func NewHealthCollector(fetch upstreamFetcher, interval time.Duration) *HealthCollector {
	c := &HealthCollector{fetch: fetch, interval: interval, probe: probeTCP, status: map[string]string{}}
	go c.loop()
	return c
}

func (c *HealthCollector) loop() {
	c.Poll()
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for range t.C {
		c.Poll()
	}
}

// Poll 执行一次轮询并更新快照。后台 loop 与测试均调用它。
func (c *HealthCollector) Poll() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ups, err := c.fetch.Upstreams(ctx)
	if err != nil {
		// caddy 不可达:清空快照、标记不可达,前端健康列统一「未知」(ADR-020 §1 降级)。
		c.mu.Lock()
		c.reachable = false
		c.status = map[string]string{}
		c.mu.Unlock()
		return
	}
	// 并发探测未带 health_status 的地址(不持锁,避免阻塞前端读取快照)。
	stati := make([]string, len(ups))
	var wg sync.WaitGroup
	for i, u := range ups {
		if u.HealthStatus != "" {
			stati[i] = u.HealthStatus
			continue
		}
		wg.Add(1)
		go func(idx int, address string) {
			defer wg.Done()
			stati[idx] = c.probeStatus(ctx, address)
		}(i, u.Address)
	}
	wg.Wait()

	next := make(map[string]string, len(ups))
	for i, u := range ups {
		next[u.Address] = stati[i]
	}
	c.mu.Lock()
	c.reachable = true
	c.status = next
	c.mu.Unlock()
}

// probeStatus 对无 health_status 的后端做主动探测;probe 未配置时返回未知。
func (c *HealthCollector) probeStatus(ctx context.Context, address string) string {
	if c.probe == nil {
		return HealthUnknown
	}
	if c.probe(ctx, address) {
		return HealthHealthy
	}
	return HealthUnhealthy
}

// probeTCP 以 TCP 连接探测地址是否可达(协议无关,兼容 http/https/tcp 后端)。
func probeTCP(ctx context.Context, address string) bool {
	d := net.Dialer{Timeout: probeTimeout}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Snapshot 返回 address → 健康状态的快照。
func (c *HealthCollector) Snapshot() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.status))
	for k, v := range c.status {
		out[k] = v
	}
	return out
}

// Reachable 报告 caddy Admin API 是否可达。
func (c *HealthCollector) Reachable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reachable
}

// Status 返回指定后端地址的健康状态;caddy 不可达或未收录时为 unknown。
func (c *HealthCollector) Status(address string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.reachable {
		return HealthUnknown
	}
	if st, ok := c.status[address]; ok {
		return st
	}
	return HealthUnknown
}
