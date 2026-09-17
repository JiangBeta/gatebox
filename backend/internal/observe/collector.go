package observe

import (
	"context"
	"time"

	"github.com/JiangBeta/gatebox/internal/component"
)

// Provider 一个可观测组件的状态/活动来源（由装配层注入）。
type Provider struct {
	ID       string
	Status   func(context.Context) (component.Status, error)
	Activity func(context.Context) (component.Activity, error)
}

// Collector 定时轮询组件 state/activity，仅在变化时发布事件（避免 SSE 噪声）。
type Collector struct {
	bus      *Bus
	provider []Provider
	interval time.Duration

	lastState    map[string]string
	lastActivity map[string]string
}

// NewCollector 构造采集器。
func NewCollector(bus *Bus, providers []Provider, interval time.Duration) *Collector {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Collector{
		bus:          bus,
		provider:     providers,
		interval:     interval,
		lastState:    map[string]string{},
		lastActivity: map[string]string{},
	}
}

// Start 启动后台轮询直到 ctx 结束。
func (c *Collector) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(c.interval)
		defer t.Stop()
		c.Poll(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.Poll(ctx)
			}
		}
	}()
}

// Poll 执行一次采集（导出以便测试）。
func (c *Collector) Poll(ctx context.Context) {
	for _, p := range c.provider {
		if p.Status != nil {
			if st, err := p.Status(ctx); err == nil {
				if c.lastState[p.ID] != st.State {
					c.lastState[p.ID] = st.State
					c.bus.Publish(KindState, p.ID, "", st)
				}
			}
		}
		if p.Activity != nil {
			if act, err := p.Activity(ctx); err == nil {
				key := act.Task + "|" + act.Step
				if c.lastActivity[p.ID] != key {
					c.lastActivity[p.ID] = key
					c.bus.Publish(KindActivity, p.ID, "", act)
				}
			}
		}
	}
}

// PublishRun 供调和器发布 run/step 事件（薄封装，统一 JSON 编码）。
func (c *Collector) PublishRun(runID, componentID string, payload any) {
	if c == nil || c.bus == nil {
		return
	}
	c.bus.Publish(KindRun, componentID, runID, payload)
}
