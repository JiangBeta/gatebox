// Package stats 提供容器资源统计的单例采集器(docs/docker.md §5.2)。
//
// 存在的理由是收敛连接数:若让每个浏览器直连 daemon 的 stats 端点,
// 10 个容器 × 3 个标签页就是 30 条长连接。采集器让 N 个客户端始终
// 只对应 M 条 docker 流(M = 运行中的容器数)。
//
// 采集按需启停:GateBox 是常驻服务而非 ctop 那样开着才采的 CLI,
// 没人看 Docker 页时不该 24 小时消耗 daemon。
package stats

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/docker/client"
)

// 默认参数(docs §5.2 / Q18)。
const (
	// DefaultCooldown 最后一个订阅者离开后,采集继续运行的时长。
	// 用于吸收「切走再切回」的抖动,避免频繁启停 docker 流。
	DefaultCooldown = 30 * time.Second

	// DefaultHistorySize 每个容器保留的采样点数。仅存内存、不落盘 ——
	// BoltDB 是 B+ 树,扛不住时序数据的持续写入。
	DefaultHistorySize = 60

	// DefaultRefreshInterval 重新扫描容器列表的间隔,用于发现新起/已停的容器。
	DefaultRefreshInterval = 5 * time.Second
)

// Sample 一个采样点。
type Sample struct {
	At            time.Time `json:"at"`
	CPUPercent    float64   `json:"cpuPercent"`
	MemoryUsage   uint64    `json:"memoryUsage"` // 已扣除文件缓存
	MemoryLimit   uint64    `json:"memoryLimit"`
	MemoryPercent float64   `json:"memoryPercent"`
	NetworkRx     uint64    `json:"networkRx"`
	NetworkTx     uint64    `json:"networkTx"`
	BlockRead     uint64    `json:"blockRead"`
	BlockWrite    uint64    `json:"blockWrite"`
	PidsCurrent   uint64    `json:"pidsCurrent"`
}

// sampleFrom 把一次 docker 统计换算成采样点。
func sampleFrom(s *client.Stats, at time.Time) Sample {
	rx, tx := s.NetworkIO()
	read, write := s.BlockIO()
	return Sample{
		At:            at,
		CPUPercent:    s.CPUPercent(),
		MemoryUsage:   s.MemoryUsage(),
		MemoryLimit:   s.MemoryStats.Limit,
		MemoryPercent: s.MemoryPercent(),
		NetworkRx:     rx,
		NetworkTx:     tx,
		BlockRead:     read,
		BlockWrite:    write,
		PidsCurrent:   s.PidsStats.Current,
	}
}

// Snapshot 某容器的最新统计及其近期历史。
type Snapshot struct {
	ContainerID string   `json:"containerId"`
	Latest      Sample   `json:"latest"`
	History     []Sample `json:"history,omitempty"`
}

// Collector 单例统计采集器。
type Collector struct {
	cli      *client.Client
	cooldown time.Duration
	histSize int
	refresh  time.Duration

	// now 与 afterFunc 供测试注入时钟,生产环境走真实时间。
	now       func() time.Time
	afterFunc func(time.Duration, func()) *time.Timer

	mu        sync.Mutex
	subs      int                           // 当前订阅者数量
	running   bool                          // 采集循环是否在运行
	stop      context.CancelFunc            // 停止当前采集循环
	stopTimer *time.Timer                   // 冷却计时器
	entries   map[string]*entry             // 容器 ID → 采样缓冲
	streams   map[string]context.CancelFunc // 容器 ID → 该容器 stats 流的取消函数
	wg        sync.WaitGroup
}

// entry 单个容器的环形缓冲。
type entry struct {
	name    string
	samples []Sample
	next    int
	filled  bool
}

func (e *entry) push(s Sample, size int) {
	if len(e.samples) < size {
		e.samples = append(e.samples, s)
		e.next = len(e.samples) % size
		if len(e.samples) == size {
			e.filled = true
		}
		return
	}
	e.samples[e.next] = s
	e.next = (e.next + 1) % size
	e.filled = true
}

// latest 返回最近一个采样点。
func (e *entry) latest() (Sample, bool) {
	if len(e.samples) == 0 {
		return Sample{}, false
	}
	idx := e.next - 1
	if idx < 0 {
		idx = len(e.samples) - 1
	}
	return e.samples[idx], true
}

// history 按时间先后返回全部采样点。
func (e *entry) history() []Sample {
	if len(e.samples) == 0 {
		return nil
	}
	out := make([]Sample, 0, len(e.samples))
	if !e.filled {
		out = append(out, e.samples...)
		return out
	}
	out = append(out, e.samples[e.next:]...)
	out = append(out, e.samples[:e.next]...)
	return out
}

// Option 采集器可选配置。
type Option func(*Collector)

// WithCooldown 设置冷却时长。
func WithCooldown(d time.Duration) Option {
	return func(c *Collector) { c.cooldown = d }
}

// WithHistorySize 设置每容器保留的采样点数。
func WithHistorySize(n int) Option {
	return func(c *Collector) {
		if n > 0 {
			c.histSize = n
		}
	}
}

// WithRefreshInterval 设置容器列表的重扫间隔。
func WithRefreshInterval(d time.Duration) Option {
	return func(c *Collector) {
		if d > 0 {
			c.refresh = d
		}
	}
}

// New 创建采集器。创建后不会立即采集,需有订阅者(见 Subscribe)。
func New(cli *client.Client, opts ...Option) *Collector {
	c := &Collector{
		cli:       cli,
		cooldown:  DefaultCooldown,
		histSize:  DefaultHistorySize,
		refresh:   DefaultRefreshInterval,
		now:       time.Now,
		afterFunc: time.AfterFunc,
		entries:   map[string]*entry{},
		streams:   map[string]context.CancelFunc{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Subscribe 登记一个订阅者并在必要时启动采集,返回退订函数。
//
// 退订函数可安全重复调用。最后一个订阅者退订后,采集会在冷却期结束时停止;
// 若冷却期内又有新订阅者进来,采集会继续、不会中断。
func (c *Collector) Subscribe() (unsubscribe func()) {
	c.mu.Lock()
	c.subs++
	// 冷却期内来了新订阅者:取消待执行的停止
	if c.stopTimer != nil {
		c.stopTimer.Stop()
		c.stopTimer = nil
	}
	if !c.running {
		c.startLocked()
	}
	c.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.subs--
			if c.subs > 0 {
				return
			}
			c.subs = 0
			// 进入冷却:到期仍无订阅者才真正停止
			c.stopTimer = c.afterFunc(c.cooldown, func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				if c.subs == 0 && c.running {
					c.stopLocked()
				}
			})
		})
	}
}

// Subscribers 返回当前订阅者数量。
func (c *Collector) Subscribers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subs
}

// Running 报告采集循环是否在运行。
func (c *Collector) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// startLocked 启动采集循环。调用者须持有 c.mu。
func (c *Collector) startLocked() {
	ctx, cancel := context.WithCancel(context.Background())
	c.stop = cancel
	c.running = true
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.loop(ctx)
	}()
}

// stopLocked 停止采集循环并清空缓冲。调用者须持有 c.mu。
func (c *Collector) stopLocked() {
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
	c.running = false
	for id, cancel := range c.streams {
		cancel()
		delete(c.streams, id)
	}
	// 缓冲不保留:停采期间的历史已断档,留着会让前端画出有空洞的曲线
	c.entries = map[string]*entry{}
}

// Close 停止采集并等待所有 goroutine 退出。
func (c *Collector) Close() {
	c.mu.Lock()
	if c.stopTimer != nil {
		c.stopTimer.Stop()
		c.stopTimer = nil
	}
	if c.running {
		c.stopLocked()
	}
	c.subs = 0
	c.mu.Unlock()
	c.wg.Wait()
}

// loop 周期性扫描运行中的容器,为新出现的容器开采集流、为消失的容器收流。
func (c *Collector) loop(ctx context.Context) {
	c.syncContainers(ctx)

	t := time.NewTicker(c.refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.syncContainers(ctx)
		}
	}
}

// syncContainers 对齐「运行中的容器」与「正在采集的流」。
func (c *Collector) syncContainers(ctx context.Context) {
	list, err := c.cli.ListContainers(ctx, client.ListContainersOptions{})
	if err != nil {
		return // 单次列举失败不影响已有流,下个周期重试
	}

	alive := make(map[string]struct{}, len(list))
	for _, ct := range list {
		alive[ct.ID] = struct{}{}
	}

	c.mu.Lock()
	// 停掉已不在运行的容器的流
	for id, cancel := range c.streams {
		if _, ok := alive[id]; !ok {
			cancel()
			delete(c.streams, id)
			delete(c.entries, id)
		}
	}
	// 为新容器开流
	var started []struct {
		id, name string
		ctx      context.Context
	}
	for _, ct := range list {
		if _, ok := c.streams[ct.ID]; ok {
			continue
		}
		sctx, scancel := context.WithCancel(ctx)
		c.streams[ct.ID] = scancel
		if _, ok := c.entries[ct.ID]; !ok {
			c.entries[ct.ID] = &entry{name: ct.Name()}
		}
		started = append(started, struct {
			id, name string
			ctx      context.Context
		}{ct.ID, ct.Name(), sctx})
	}
	c.mu.Unlock()

	for _, s := range started {
		c.wg.Add(1)
		go func(id string, sctx context.Context) {
			defer c.wg.Done()
			c.collectOne(sctx, id)
		}(s.id, s.ctx)
	}
}

// collectOne 为单个容器维持一条 stats 长流,断开后由下一轮 sync 重建。
func (c *Collector) collectOne(ctx context.Context, id string) {
	rc, err := c.cli.ContainerStatsStream(ctx, id)
	if err != nil {
		c.dropStream(id)
		return
	}
	defer rc.Close()

	err = client.DecodeStatsStream(ctx, rc, func(s *client.Stats) error {
		c.record(id, sampleFrom(s, c.now()))
		return nil
	})
	// 流正常结束或被取消都走到这里;非取消错误留待下轮 sync 重建
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		_ = err
	}
	c.dropStream(id)
}

// dropStream 清除某容器的流登记,使下一轮 sync 可以重建它。
func (c *Collector) dropStream(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel, ok := c.streams[id]; ok {
		cancel()
		delete(c.streams, id)
	}
}

// record 写入一个采样点。
func (c *Collector) record(id string, s Sample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		e = &entry{}
		c.entries[id] = e
	}
	e.push(s, c.histSize)
}

// Snapshot 返回某容器的最新统计。第二个返回值为 false 表示尚无数据
// (容器刚起、采集刚启动,或该容器未在采集中)。
func (c *Collector) Snapshot(id string) (Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		return Snapshot{}, false
	}
	latest, ok := e.latest()
	if !ok {
		return Snapshot{}, false
	}
	return Snapshot{ContainerID: id, Latest: latest}, true
}

// SnapshotAll 返回所有正在采集的容器的最新统计,供前端轮询。
func (c *Collector) SnapshotAll() map[string]Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]Sample, len(c.entries))
	for id, e := range c.entries {
		if s, ok := e.latest(); ok {
			out[id] = s
		}
	}
	return out
}

// History 返回某容器的采样历史(按时间先后)。
func (c *Collector) History(id string) []Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		return nil
	}
	return e.history()
}
