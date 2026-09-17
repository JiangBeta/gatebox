package observe

import (
	"encoding/json"
	"sync"
	"time"
)

// Bus 进程内事件总线：发布/订阅 + 环形缓冲（供 SSE 首连回放）。
type Bus struct {
	mu      sync.Mutex
	seq     uint64
	ring    []Event
	ringCap int
	subs    map[int]chan Event
	nextSub int
}

// NewBus 构造总线；ringCap 为环形缓冲容量（<=0 取默认 256）。
func NewBus(ringCap int) *Bus {
	if ringCap <= 0 {
		ringCap = 256
	}
	return &Bus{ringCap: ringCap, subs: map[int]chan Event{}}
}

// Publish 发布事件：写入环形缓冲并分发给订阅者（订阅者缓冲满则丢弃，不阻塞）。
func (b *Bus) Publish(kind Kind, component, runID string, data any) Event {
	var raw json.RawMessage
	if data != nil {
		if bs, err := json.Marshal(data); err == nil {
			raw = bs
		}
	}
	b.mu.Lock()
	b.seq++
	ev := Event{Seq: b.seq, Kind: kind, Component: component, RunID: runID, Data: raw, At: time.Now().Unix()}
	b.ring = append(b.ring, ev)
	if len(b.ring) > b.ringCap {
		b.ring = b.ring[len(b.ring)-b.ringCap:]
	}
	subs := make([]chan Event, 0, len(b.subs))
	for _, ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
	return ev
}

// Subscribe 订阅事件；cancel 退订并关闭通道。
func (b *Bus) Subscribe(buf int) (<-chan Event, func()) {
	if buf <= 0 {
		buf = 32
	}
	ch := make(chan Event, buf)
	b.mu.Lock()
	id := b.nextSub
	b.nextSub++
	b.subs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Recent 返回环形缓冲中的事件（升序），用于 SSE 首连回放。
func (b *Bus) Recent() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, len(b.ring))
	copy(out, b.ring)
	return out
}
