// Package gateway 提供网关单位的运行时能力:docker route 派生与健康采集。
package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/caddy"
)

// 健康状态值(前端据此渲染绿/红/灰)。
const (
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
	HealthUnknown   = "unknown"
)

// DefaultHealthInterval 健康轮询周期。
const DefaultHealthInterval = 5 * time.Second

// upstreamFetcher 健康状态来源(caddy.Client 实现),便于测试注入。
type upstreamFetcher interface {
	Upstreams(ctx context.Context) ([]caddy.Upstream, error)
}

// HealthCollector 单例健康采集器(ADR-020 §1):定期轮询 caddy Admin API,
// 聚合 upstream 健康快照供前端轮询,避免列表逐行实时查 caddy 打爆 Admin API。
type HealthCollector struct {
	fetch    upstreamFetcher
	interval time.Duration

	mu        sync.Mutex
	status    map[string]string // address → healthy|unhealthy
	reachable bool              // 最近一次轮询 caddy 是否可达
}

// NewHealthCollector 构造采集器并启动后台轮询。
func NewHealthCollector(fetch upstreamFetcher, interval time.Duration) *HealthCollector {
	c := &HealthCollector{fetch: fetch, interval: interval, status: map[string]string{}}
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

	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		// caddy 不可达:清空快照、标记不可达,前端健康列统一「未知」(ADR-020 §1 降级)。
		c.reachable = false
		c.status = map[string]string{}
		return
	}
	c.reachable = true
	next := make(map[string]string, len(ups))
	for _, u := range ups {
		st := u.HealthStatus
		if st == "" {
			st = HealthUnknown
		}
		next[u.Address] = st
	}
	c.status = next
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
