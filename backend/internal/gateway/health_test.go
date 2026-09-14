package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
)

type mockFetcher struct {
	ups []caddy.Upstream
	err error
}

func (m *mockFetcher) Upstreams(_ context.Context) ([]caddy.Upstream, error) {
	return m.ups, m.err
}

func TestHealthCollector(t *testing.T) {
	m := &mockFetcher{ups: []caddy.Upstream{
		{Address: "127.0.0.1:8096", HealthStatus: "healthy"},
		{Address: "127.0.0.1:9000", HealthStatus: "unhealthy"},
	}}
	c := &HealthCollector{fetch: m, status: map[string]string{}}
	c.Poll()

	if !c.Reachable() {
		t.Fatal("caddy 应可达")
	}
	if got := c.Status("127.0.0.1:8096"); got != HealthHealthy {
		t.Errorf("8096 状态 = %q, want healthy", got)
	}
	if got := c.Status("127.0.0.1:9000"); got != HealthUnhealthy {
		t.Errorf("9000 状态 = %q, want unhealthy", got)
	}
	if got := c.Status("127.0.0.1:1234"); got != HealthUnknown {
		t.Errorf("未收录地址 = %q, want unknown", got)
	}
	if len(c.Snapshot()) != 2 {
		t.Errorf("快照条目 = %d, want 2", len(c.Snapshot()))
	}
}

func TestHealthCollectorUnreachable(t *testing.T) {
	m := &mockFetcher{err: errors.New("connection refused")}
	c := &HealthCollector{fetch: m, status: map[string]string{"127.0.0.1:8096": HealthHealthy}}
	c.Poll()

	if c.Reachable() {
		t.Fatal("caddy 应不可达")
	}
	if got := c.Status("127.0.0.1:8096"); got != HealthUnknown {
		t.Errorf("不可达时状态 = %q, want unknown", got)
	}
	if len(c.Snapshot()) != 0 {
		t.Errorf("不可达时快照应为空: %v", c.Snapshot())
	}
}
