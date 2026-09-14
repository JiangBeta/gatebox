package stats

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/docker/client"
)

// --- 环形缓冲(纯逻辑) ---

func mkSample(i int) Sample {
	return Sample{At: time.Unix(int64(i), 0), CPUPercent: float64(i)}
}

func TestRingBuffer_UnderCapacity(t *testing.T) {
	e := &entry{}
	for i := 1; i <= 3; i++ {
		e.push(mkSample(i), 5)
	}
	got := e.history()
	if len(got) != 3 {
		t.Fatalf("历史长度 = %d, want 3", len(got))
	}
	for i, s := range got {
		if s.CPUPercent != float64(i+1) {
			t.Errorf("第 %d 项 = %v, want %v", i, s.CPUPercent, i+1)
		}
	}
	latest, ok := e.latest()
	if !ok || latest.CPUPercent != 3 {
		t.Errorf("latest = %v(ok=%v), want 3", latest.CPUPercent, ok)
	}
}

// TestRingBuffer_Wraps 缓冲写满后应覆盖最旧的采样,且历史仍按时间先后返回。
func TestRingBuffer_Wraps(t *testing.T) {
	const size = 3
	e := &entry{}
	for i := 1; i <= 5; i++ { // 1,2,3,4,5 → 保留 3,4,5
		e.push(mkSample(i), size)
	}

	got := e.history()
	if len(got) != size {
		t.Fatalf("历史长度 = %d, want %d", len(got), size)
	}
	want := []float64{3, 4, 5}
	for i, s := range got {
		if s.CPUPercent != want[i] {
			t.Errorf("第 %d 项 = %v, want %v(顺序错乱会让曲线时间轴倒置)", i, s.CPUPercent, want[i])
		}
	}
	latest, _ := e.latest()
	if latest.CPUPercent != 5 {
		t.Errorf("latest = %v, want 5", latest.CPUPercent)
	}
}

func TestRingBuffer_ExactlyFull(t *testing.T) {
	e := &entry{}
	for i := 1; i <= 3; i++ {
		e.push(mkSample(i), 3)
	}
	got := e.history()
	want := []float64{1, 2, 3}
	if len(got) != 3 {
		t.Fatalf("历史长度 = %d", len(got))
	}
	for i, s := range got {
		if s.CPUPercent != want[i] {
			t.Errorf("第 %d 项 = %v, want %v", i, s.CPUPercent, want[i])
		}
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	e := &entry{}
	if _, ok := e.latest(); ok {
		t.Error("空缓冲的 latest 应返回 false")
	}
	if h := e.history(); h != nil {
		t.Errorf("空缓冲的 history = %v, want nil", h)
	}
}

// --- 假 daemon ---

// fakeDaemon 模拟 daemon 的 containers/json 与 stats 流。
type fakeDaemon struct {
	mu         sync.Mutex
	containers []string // 当前"运行中"的容器名

	statsOpened  atomic.Int64 // 累计开启的 stats 流数
	statsActive  atomic.Int64 // 当前活跃的 stats 流数
	listRequests atomic.Int64
}

func (d *fakeDaemon) setContainers(names ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containers = names
}

func (d *fakeDaemon) snapshotContainers() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.containers...)
}

const statsPayload = `{"read":"2026-08-25T04:00:00Z",
"cpu_stats":{"cpu_usage":{"total_usage":2000000000},"system_cpu_usage":16000000000,"online_cpus":8},
"precpu_stats":{"cpu_usage":{"total_usage":1000000000},"system_cpu_usage":8000000000,"online_cpus":8},
"memory_stats":{"usage":1000,"limit":10000,"stats":{"inactive_file":200}},
"pids_stats":{"current":7}}`

func newFakeDaemon(t *testing.T) (*fakeDaemon, *client.Client) {
	t.Helper()
	d := &fakeDaemon{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			d.listRequests.Add(1)
			var sb strings.Builder
			sb.WriteString("[")
			for i, name := range d.snapshotContainers() {
				if i > 0 {
					sb.WriteString(",")
				}
				fmt.Fprintf(&sb, `{"Id":%q,"Names":["/%s"],"State":"running"}`, name, name)
			}
			sb.WriteString("]")
			w.Write([]byte(sb.String()))

		case strings.HasSuffix(r.URL.Path, "/stats"):
			d.statsOpened.Add(1)
			d.statsActive.Add(1)
			defer d.statsActive.Add(-1)

			flusher, _ := w.(http.Flusher)
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					if _, err := w.Write([]byte(statsPayload)); err != nil {
						return
					}
					if flusher != nil {
						flusher.Flush()
					}
				}
			}

		default:
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	return d, client.NewHTTP(ts.Client(), u.Scheme, u.Host)
}

// waitFor 轮询等待条件成立。
func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", desc)
}

// --- 采集生命周期 ---

func TestSubscribe_StartsCollection(t *testing.T) {
	d, cli := newFakeDaemon(t)
	d.setContainers("c1", "c2")

	c := New(cli, WithRefreshInterval(10*time.Millisecond), WithCooldown(time.Hour))
	defer c.Close()

	if c.Running() {
		t.Error("无订阅者时不应采集 —— 常驻服务不该为没人看的页面消耗 daemon")
	}

	unsub := c.Subscribe()
	defer unsub()

	if !c.Running() {
		t.Fatal("订阅后应开始采集")
	}
	waitFor(t, 2*time.Second, "两条 stats 流建立", func() bool {
		return d.statsActive.Load() == 2
	})
	waitFor(t, 2*time.Second, "采集到数据", func() bool {
		return len(c.SnapshotAll()) == 2
	})

	snap := c.SnapshotAll()
	for id, s := range snap {
		// Δcpu=1e9, Δsys=8e9, 8 核 → 100%
		if s.CPUPercent < 99 || s.CPUPercent > 101 {
			t.Errorf("%s CPU = %.2f, want ≈100", id, s.CPUPercent)
		}
		// usage 1000 - inactive_file 200 = 800
		if s.MemoryUsage != 800 {
			t.Errorf("%s 内存 = %d, want 800", id, s.MemoryUsage)
		}
		if s.PidsCurrent != 7 {
			t.Errorf("%s pids = %d, want 7", id, s.PidsCurrent)
		}
	}
}

// TestOneStreamPerContainer 是这个包存在的理由:
// 多个订阅者不应放大 docker 连接数。
func TestOneStreamPerContainer(t *testing.T) {
	d, cli := newFakeDaemon(t)
	d.setContainers("c1")

	c := New(cli, WithRefreshInterval(10*time.Millisecond), WithCooldown(time.Hour))
	defer c.Close()

	var unsubs []func()
	for i := 0; i < 10; i++ { // 模拟 10 个浏览器标签页
		unsubs = append(unsubs, c.Subscribe())
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	waitFor(t, 2*time.Second, "stats 流建立", func() bool {
		return d.statsActive.Load() >= 1
	})
	time.Sleep(100 * time.Millisecond) // 给可能的重复开流留出时间

	if got := d.statsActive.Load(); got != 1 {
		t.Errorf("10 个订阅者产生了 %d 条 docker 流, want 1 —— 连接数被放大了", got)
	}
	if c.Subscribers() != 10 {
		t.Errorf("订阅者数 = %d, want 10", c.Subscribers())
	}
}

// TestCooldown_StopsAfterLastUnsubscribe 最后一个订阅者离开后,
// 冷却期结束才真正停止采集。
func TestCooldown_StopsAfterLastUnsubscribe(t *testing.T) {
	d, cli := newFakeDaemon(t)
	d.setContainers("c1")

	c := New(cli, WithRefreshInterval(10*time.Millisecond), WithCooldown(80*time.Millisecond))
	defer c.Close()

	unsub := c.Subscribe()
	waitFor(t, 2*time.Second, "开始采集", func() bool { return d.statsActive.Load() == 1 })

	unsub()
	// 冷却期内仍应在采集
	time.Sleep(30 * time.Millisecond)
	if !c.Running() {
		t.Error("冷却期内不应停止采集")
	}

	waitFor(t, 2*time.Second, "冷却后停止", func() bool { return !c.Running() })
	waitFor(t, 2*time.Second, "docker 流关闭", func() bool { return d.statsActive.Load() == 0 })
}

// TestCooldown_ResubscribeCancelsStop 冷却期内重新订阅应取消停止,
// 避免用户切走再切回时采集被打断。
func TestCooldown_ResubscribeCancelsStop(t *testing.T) {
	d, cli := newFakeDaemon(t)
	d.setContainers("c1")

	c := New(cli, WithRefreshInterval(10*time.Millisecond), WithCooldown(100*time.Millisecond))
	defer c.Close()

	unsub := c.Subscribe()
	waitFor(t, 2*time.Second, "开始采集", func() bool { return d.statsActive.Load() == 1 })
	openedBefore := d.statsOpened.Load()

	unsub()
	time.Sleep(30 * time.Millisecond) // 仍在冷却期内
	unsub2 := c.Subscribe()
	defer unsub2()

	// 等过原冷却期,采集应仍在运行
	time.Sleep(150 * time.Millisecond)
	if !c.Running() {
		t.Fatal("冷却期内重新订阅后,采集不应停止")
	}
	if got := d.statsOpened.Load(); got != openedBefore {
		t.Errorf("stats 流被重开了 %d 次,应复用原有流", got-openedBefore)
	}
}

// TestUnsubscribe_Idempotent 退订函数重复调用不应把计数减成负数。
func TestUnsubscribe_Idempotent(t *testing.T) {
	_, cli := newFakeDaemon(t)
	c := New(cli, WithCooldown(time.Hour))
	defer c.Close()

	unsub := c.Subscribe()
	unsub()
	unsub()
	unsub()

	if got := c.Subscribers(); got != 0 {
		t.Errorf("订阅者数 = %d, want 0", got)
	}
}

// TestContainerRemoved_StreamDropped 容器停止后应收掉它的流与缓冲,
// 否则已消失的容器会一直留在列表里。
func TestContainerRemoved_StreamDropped(t *testing.T) {
	d, cli := newFakeDaemon(t)
	d.setContainers("c1", "c2")

	c := New(cli, WithRefreshInterval(10*time.Millisecond), WithCooldown(time.Hour))
	defer c.Close()

	unsub := c.Subscribe()
	defer unsub()

	waitFor(t, 2*time.Second, "两个容器都在采集", func() bool {
		return len(c.SnapshotAll()) == 2
	})

	d.setContainers("c1") // c2 停止
	waitFor(t, 2*time.Second, "c2 被移除", func() bool {
		_, ok := c.Snapshot("c2")
		return !ok
	})
	waitFor(t, 2*time.Second, "只剩一条流", func() bool {
		return d.statsActive.Load() == 1
	})

	if _, ok := c.Snapshot("c1"); !ok {
		t.Error("c1 仍在运行,不应被移除")
	}
}

// TestNewContainer_PickedUp 新起的容器应在下个扫描周期被纳入采集。
func TestNewContainer_PickedUp(t *testing.T) {
	d, cli := newFakeDaemon(t)
	d.setContainers("c1")

	c := New(cli, WithRefreshInterval(10*time.Millisecond), WithCooldown(time.Hour))
	defer c.Close()

	unsub := c.Subscribe()
	defer unsub()

	waitFor(t, 2*time.Second, "c1 在采集", func() bool {
		_, ok := c.Snapshot("c1")
		return ok
	})

	d.setContainers("c1", "c2")
	waitFor(t, 2*time.Second, "c2 被发现", func() bool {
		_, ok := c.Snapshot("c2")
		return ok
	})
}

func TestHistory_Accumulates(t *testing.T) {
	d, cli := newFakeDaemon(t)
	d.setContainers("c1")

	c := New(cli, WithRefreshInterval(10*time.Millisecond),
		WithCooldown(time.Hour), WithHistorySize(4))
	defer c.Close()

	unsub := c.Subscribe()
	defer unsub()

	waitFor(t, 3*time.Second, "累积到 4 个采样点", func() bool {
		return len(c.History("c1")) == 4
	})
	// 环形缓冲封顶,不应无限增长
	time.Sleep(100 * time.Millisecond)
	if got := len(c.History("c1")); got != 4 {
		t.Errorf("历史长度 = %d, want 4(应被环形缓冲封顶)", got)
	}
}

func TestSnapshot_UnknownContainer(t *testing.T) {
	_, cli := newFakeDaemon(t)
	c := New(cli, WithCooldown(time.Hour))
	defer c.Close()

	if _, ok := c.Snapshot("nope"); ok {
		t.Error("未采集的容器应返回 false")
	}
	if h := c.History("nope"); h != nil {
		t.Errorf("未采集的容器 History = %v, want nil", h)
	}
}

func TestClose_StopsEverything(t *testing.T) {
	d, cli := newFakeDaemon(t)
	d.setContainers("c1", "c2")

	c := New(cli, WithRefreshInterval(10*time.Millisecond), WithCooldown(time.Hour))
	unsub := c.Subscribe()
	defer unsub()

	waitFor(t, 2*time.Second, "开始采集", func() bool { return d.statsActive.Load() == 2 })

	c.Close()

	if c.Running() {
		t.Error("Close 后不应仍在运行")
	}
	waitFor(t, 2*time.Second, "所有流关闭", func() bool { return d.statsActive.Load() == 0 })
}

// TestListFailure_DoesNotCrash daemon 列举失败时应静默重试,不 panic、不停采。
func TestListFailure_DoesNotCrash(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	cli := client.NewHTTP(ts.Client(), u.Scheme, u.Host)

	c := New(cli, WithRefreshInterval(5*time.Millisecond), WithCooldown(time.Hour))
	defer c.Close()

	unsub := c.Subscribe()
	defer unsub()

	time.Sleep(50 * time.Millisecond)
	if !c.Running() {
		t.Error("列举失败不应终止采集循环 —— 下个周期应重试")
	}
	if len(c.SnapshotAll()) != 0 {
		t.Error("列举失败时不应有数据")
	}
}
