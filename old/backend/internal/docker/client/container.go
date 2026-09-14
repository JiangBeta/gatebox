package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Filters Engine API 的过滤条件,序列化为 JSON 后作为 filters 查询参数。
// 例:Filters{"label": {"com.docker.compose.project=jellyfin"}}
type Filters map[string][]string

func (f Filters) apply(q url.Values) error {
	if len(f) == 0 {
		return nil
	}
	b, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("序列化 filters: %w", err)
	}
	q.Set("filters", string(b))
	return nil
}

// ListContainersOptions 容器列表选项。
type ListContainersOptions struct {
	All     bool // 含已停止容器。GateBox 列表默认为 true
	Limit   int
	Filters Filters
}

// ListContainers 列出容器。
func (c *Client) ListContainers(ctx context.Context, opts ListContainersOptions) ([]Container, error) {
	q := url.Values{}
	if opts.All {
		q.Set("all", boolArg(true))
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if err := opts.Filters.apply(q); err != nil {
		return nil, err
	}

	var out []Container
	if err := c.getJSON(ctx, "/containers/json", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InspectContainer 获取容器详情。Config.Tty 决定日志流的解析方式。
func (c *Client) InspectContainer(ctx context.Context, id string) (*ContainerDetail, error) {
	var out ContainerDetail
	if err := c.getJSON(ctx, "/containers/"+id+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartContainer 启动容器。容器已在运行时 daemon 返回 304,
// 此处转为 ErrNotModified,调用方可用 errors.Is 判断后忽略。
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/containers/"+id+"/start", nil, nil, nil)
}

// StopContainer 停止容器。timeout 为优雅停止的等待秒数,nil 表示用 daemon 默认值。
// 容器已停止时返回 ErrNotModified。
func (c *Client) StopContainer(ctx context.Context, id string, timeout *int) error {
	q := url.Values{}
	if timeout != nil {
		q.Set("t", strconv.Itoa(*timeout))
	}
	return c.postJSON(ctx, "/containers/"+id+"/stop", q, nil, nil)
}

// RestartContainer 重启容器。
func (c *Client) RestartContainer(ctx context.Context, id string, timeout *int) error {
	q := url.Values{}
	if timeout != nil {
		q.Set("t", strconv.Itoa(*timeout))
	}
	return c.postJSON(ctx, "/containers/"+id+"/restart", q, nil, nil)
}

// KillContainer 向容器发送信号,signal 为空时默认 SIGKILL。
func (c *Client) KillContainer(ctx context.Context, id, signal string) error {
	q := url.Values{}
	if signal != "" {
		q.Set("signal", signal)
	}
	return c.postJSON(ctx, "/containers/"+id+"/kill", q, nil, nil)
}

// RemoveContainerOptions 删除容器的选项。
type RemoveContainerOptions struct {
	// RemoveVolumes 同时删除该容器的匿名卷。
	// UI 上对应一个默认不勾选的复选框(docs/docker.md §4.5)。
	RemoveVolumes bool
	Force         bool // 强制删除运行中的容器
	RemoveLinks   bool
}

// RemoveContainer 删除容器。
//
// 注意:GateBox 的策略是「只有已停止的容器才能删除」,故正常路径下 Force 恒为 false;
// 运行中容器的删除请求应在 API 层被拒绝,而非在此处强删。
func (c *Client) RemoveContainer(ctx context.Context, id string, opts RemoveContainerOptions) error {
	q := url.Values{}
	if opts.RemoveVolumes {
		q.Set("v", boolArg(true))
	}
	if opts.Force {
		q.Set("force", boolArg(true))
	}
	if opts.RemoveLinks {
		q.Set("link", boolArg(true))
	}
	return c.deleteReq(ctx, "/containers/"+id, q, nil)
}

// LogsOptions 日志读取选项。
type LogsOptions struct {
	Stdout     bool
	Stderr     bool
	Follow     bool
	Timestamps bool
	Tail       string // 行数,或 "all";空串等同 "all"
	Since      time.Time
	Until      time.Time
}

// ContainerLogs 返回容器日志流,调用方负责 Close。
//
// 流的格式取决于容器是否为 TTY:非 TTY 是多路复用帧,TTY 是原始字节。
// 用 NewLogFrames 包装可屏蔽这一差异。
func (c *Client) ContainerLogs(ctx context.Context, id string, opts LogsOptions) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("stdout", boolArg(opts.Stdout))
	q.Set("stderr", boolArg(opts.Stderr))
	if opts.Follow {
		q.Set("follow", boolArg(true))
	}
	if opts.Timestamps {
		q.Set("timestamps", boolArg(true))
	}
	if opts.Tail != "" {
		q.Set("tail", opts.Tail)
	}
	if !opts.Since.IsZero() {
		q.Set("since", strconv.FormatInt(opts.Since.Unix(), 10))
	}
	if !opts.Until.IsZero() {
		q.Set("until", strconv.FormatInt(opts.Until.Unix(), 10))
	}
	return c.stream(ctx, http.MethodGet, "/containers/"+id+"/logs", q, nil)
}

// ContainerStatsStream 返回持续推送的统计流(每秒一条 JSON),调用方负责 Close。
//
// 采集器用此端点而非 stream=false 的轮询:后者每次请求 daemon 内部都要
// 采样等待约一秒,N 个容器串行会非常慢(docs/docker.md §5.2)。
func (c *Client) ContainerStatsStream(ctx context.Context, id string) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("stream", boolArg(true))
	return c.stream(ctx, http.MethodGet, "/containers/"+id+"/stats", q, nil)
}

// ContainerStatsOnce 取一次统计快照。
//
// daemon 在 stream=false 时仍会返回 precpu_stats,故单次结果即可算出 CPU 占用率。
func (c *Client) ContainerStatsOnce(ctx context.Context, id string) (*Stats, error) {
	q := url.Values{}
	q.Set("stream", boolArg(false))
	var out Stats
	if err := c.getJSON(ctx, "/containers/"+id+"/stats", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ContainerTop 列出容器内的进程。
func (c *Client) ContainerTop(ctx context.Context, id, psArgs string) (titles []string, processes [][]string, err error) {
	q := url.Values{}
	if psArgs != "" {
		q.Set("ps_args", psArgs)
	}
	var out struct {
		Titles    []string   `json:"Titles"`
		Processes [][]string `json:"Processes"`
	}
	if err := c.getJSON(ctx, "/containers/"+id+"/top", q, &out); err != nil {
		return nil, nil, err
	}
	return out.Titles, out.Processes, nil
}

// --- 日志帧的统一抽象 ---

// LogFrames 屏蔽 TTY 与非 TTY 的流格式差异,统一按帧读取。
type LogFrames interface {
	// Next 读取下一段输出。返回的切片在下次调用前有效。
	Next() (StreamType, []byte, error)
}

// NewLogFrames 按容器是否为 TTY 选择合适的解析方式。
//
// tty 参数应取自 InspectContainer 的 Config.Tty。传错会导致:
// 把 TTY 流当多路复用解析 → 帧长度异常报错;
// 把多路复用流当 TTY 处理 → 8 字节帧头混入正文显示为乱码。
func NewLogFrames(r io.Reader, tty bool) LogFrames {
	if tty {
		return &rawFrames{r: r}
	}
	return NewFrameReader(r)
}

// rawFrames 把无帧头的 TTY 流按读取块包装成 stdout 帧。
type rawFrames struct {
	r   io.Reader
	buf []byte
}

func (f *rawFrames) Next() (StreamType, []byte, error) {
	if f.buf == nil {
		f.buf = make([]byte, 32<<10)
	}
	n, err := f.r.Read(f.buf)
	if n > 0 {
		// Read 允许同时返回 n>0 与非 nil 错误,数据优先
		return StreamStdout, f.buf[:n], nil
	}
	if err != nil {
		return StreamStdout, nil, err
	}
	return StreamStdout, nil, nil
}

// DecodeStatsStream 从统计流中逐条解码。ctx 取消时返回。
//
// 每条统计之间由 daemon 以换行分隔,直接用 json.Decoder 连续解码即可。
func DecodeStatsStream(ctx context.Context, r io.Reader, fn func(*Stats) error) error {
	dec := json.NewDecoder(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var s Stats
		if err := dec.Decode(&s); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := fn(&s); err != nil {
			return err
		}
	}
}
