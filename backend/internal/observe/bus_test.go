package observe

import (
	"context"
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/component"
)

func TestBusPublishSubscribe(t *testing.T) {
	bus := NewBus(4)
	ch, cancel := bus.Subscribe(4)
	defer cancel()

	ev := bus.Publish(KindState, "caddy", "", map[string]any{"state": "running"})
	if ev.Seq != 1 || ev.Kind != KindState || ev.Component != "caddy" {
		t.Fatalf("bad event: %+v", ev)
	}
	select {
	case got := <-ch:
		if got.Seq != 1 {
			t.Fatalf("got seq %d", got.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("no event delivered")
	}
}

func TestBusRingReplay(t *testing.T) {
	bus := NewBus(2)
	bus.Publish(KindState, "a", "", nil)
	bus.Publish(KindState, "b", "", nil)
	bus.Publish(KindState, "c", "", nil)
	recent := bus.Recent()
	if len(recent) != 2 || recent[0].Component != "b" || recent[1].Component != "c" {
		t.Fatalf("ring wrong: %+v", recent)
	}
}

func TestCollectorPublishesOnChangeOnly(t *testing.T) {
	bus := NewBus(16)
	state := "running"
	act := "idle"
	p := Provider{
		ID:     "caddy",
		Status: func(_ context.Context) (component.Status, error) { return component.Status{State: state}, nil },
		Activity: func(_ context.Context) (component.Activity, error) {
			return component.Activity{Task: act}, nil
		},
	}
	c := NewCollector(bus, []Provider{p}, time.Second)
	c.Poll(context.Background())
	c.Poll(context.Background()) // 无变化 → 不重复发布
	if len(bus.Recent()) != 2 {  // state + activity 各一次
		t.Fatalf("want 2 events, got %d", len(bus.Recent()))
	}
	state = "stopped"
	c.Poll(context.Background())
	if len(bus.Recent()) != 3 {
		t.Fatalf("want 3 events after change, got %d", len(bus.Recent()))
	}
}
