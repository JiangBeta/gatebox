package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// --- 类型 ---

// Image 镜像列表项(GET /images/json)。
//
// 注意:列表**不返回架构**,需要 InspectImage 才能拿到 Architecture。
// 镜像页要显示架构列时,上层需对每个镜像补一次 inspect(可并发)。
type Image struct {
	ID          string            `json:"Id"`
	ParentID    string            `json:"ParentId"`
	RepoTags    []string          `json:"RepoTags"`
	RepoDigests []string          `json:"RepoDigests"`
	Created     int64             `json:"Created"` // Unix 秒
	Size        int64             `json:"Size"`
	SharedSize  int64             `json:"SharedSize"`
	Containers  int64             `json:"Containers"` // -1 表示未统计
	Labels      map[string]string `json:"Labels"`
}

// CreatedAt 创建时间。
func (i *Image) CreatedAt() time.Time { return time.Unix(i.Created, 0) }

// Digest 返回第一个仓库摘要(去掉 repo 前缀),无则返回空串。
func (i *Image) Digest() string {
	for _, rd := range i.RepoDigests {
		if idx := strings.Index(rd, "@"); idx >= 0 {
			return rd[idx+1:]
		}
	}
	return ""
}

// ImageDetail 镜像详情(GET /images/{name}/json)。
type ImageDetail struct {
	ID           string           `json:"Id"`
	RepoTags     []string         `json:"RepoTags"`
	RepoDigests  []string         `json:"RepoDigests"`
	Created      time.Time        `json:"Created"`
	Size         int64            `json:"Size"`
	Architecture string           `json:"Architecture"` // amd64 / arm64 / arm ...
	Variant      string           `json:"Variant,omitempty"`
	Os           string           `json:"Os"`
	Config       *ContainerConfig `json:"Config,omitempty"`
}

// Platform 返回 "os/arch" 或 "os/arch/variant" 形式的平台标识。
func (d *ImageDetail) Platform() string {
	p := d.Os + "/" + d.Architecture
	if d.Variant != "" {
		p += "/" + d.Variant
	}
	return p
}

// RegistryAuth 拉取私有镜像所需的认证信息。
//
// 走 Engine API 原生的 X-Registry-Auth 头,**不写 ~/.docker/config.json** ——
// 后者会与用户的 CLI 登录态互相干扰,且凭证以 base64 明文落在用户主目录(docs §3.3.1)。
type RegistryAuth struct {
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	ServerAddress string `json:"serveraddress,omitempty"` // 不含协议的域名/IP
	IdentityToken string `json:"identitytoken,omitempty"`
}

// header 编码为 X-Registry-Auth 头的值:base64url(JSON)。
func (a *RegistryAuth) header() (string, error) {
	if a == nil {
		return "", nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// --- 端点 ---

// ListImagesOptions 镜像列表选项。
type ListImagesOptions struct {
	All     bool // 含中间层镜像
	Filters Filters
}

// ListImages 列出镜像。
func (c *Client) ListImages(ctx context.Context, opts ListImagesOptions) ([]Image, error) {
	q := url.Values{}
	if opts.All {
		q.Set("all", boolArg(true))
	}
	if err := opts.Filters.apply(q); err != nil {
		return nil, err
	}
	var out []Image
	if err := c.getJSON(ctx, "/images/json", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InspectImage 获取镜像详情(含架构)。ref 可以是 ID、name:tag 或 digest。
func (c *Client) InspectImage(ctx context.Context, ref string) (*ImageDetail, error) {
	var out ImageDetail
	if err := c.getJSON(ctx, "/images/"+ref+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveImageOptions 删除镜像的选项。
type RemoveImageOptions struct {
	Force   bool
	NoPrune bool
}

// RemovedImage 删除操作的结果条目。
type RemovedImage struct {
	Untagged string `json:"Untagged,omitempty"`
	Deleted  string `json:"Deleted,omitempty"`
}

// RemoveImage 删除镜像。
//
// 镜像被容器占用时 daemon 返回 409(ErrConflict);GateBox 的策略是先查出
// 占用它的容器并列给用户看,而不是直接强删(docs §4.5)。
func (c *Client) RemoveImage(ctx context.Context, ref string, opts RemoveImageOptions) ([]RemovedImage, error) {
	q := url.Values{}
	if opts.Force {
		q.Set("force", boolArg(true))
	}
	if opts.NoPrune {
		q.Set("noprune", boolArg(true))
	}
	var out []RemovedImage
	if err := c.deleteReq(ctx, "/images/"+ref, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PullImage 拉取镜像,返回进度事件流(JSON Lines)。调用方负责 Close。
//
// platform 形如 "linux/amd64",空则由 daemon 按宿主机架构选择。
// auth 为 nil 时匿名拉取。
//
// Engine API **没有取消拉取的端点** —— 「终止」按钮的实现方式是取消 ctx
// 从而断开这条连接,已下载的层会保留在本地(docs §4.4)。
func (c *Client) PullImage(ctx context.Context, ref, platform string, auth *RegistryAuth) (io.ReadCloser, error) {
	name, tag := splitImageRef(ref)
	q := url.Values{}
	q.Set("fromImage", name)
	if tag != "" {
		q.Set("tag", tag)
	}
	if platform != "" {
		q.Set("platform", platform)
	}

	headers := map[string]string{}
	if h, err := auth.header(); err != nil {
		return nil, err
	} else if h != "" {
		headers["X-Registry-Auth"] = h
	}
	return c.streamWith(ctx, http.MethodPost, "/images/create", q, nil, headers)
}

// LoadImage 从 tar 归档导入镜像,返回进度事件流。调用方负责 Close。
func (c *Client) LoadImage(ctx context.Context, r io.Reader, quiet bool) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("quiet", boolArg(quiet))
	return c.streamWith(ctx, http.MethodPost, "/images/load", q, r, nil)
}

// PruneImages 清理未使用的镜像,返回释放的空间(字节)。
func (c *Client) PruneImages(ctx context.Context, filters Filters) (deleted []RemovedImage, reclaimed uint64, err error) {
	q := url.Values{}
	if err := filters.apply(q); err != nil {
		return nil, 0, err
	}
	var out struct {
		ImagesDeleted  []RemovedImage `json:"ImagesDeleted"`
		SpaceReclaimed uint64         `json:"SpaceReclaimed"`
	}
	if err := c.postJSON(ctx, "/images/prune", q, nil, &out); err != nil {
		return nil, 0, err
	}
	return out.ImagesDeleted, out.SpaceReclaimed, nil
}

// splitImageRef 拆分镜像引用为 name 与 tag/digest。
// 需要区分 tag 冒号与 registry 端口号里的冒号:"localhost:5000/nginx" 无 tag。
func splitImageRef(ref string) (name, tag string) {
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		return ref[:i], ref[i+1:] // digest 形式
	}
	i := strings.LastIndex(ref, ":")
	if i < 0 {
		return ref, ""
	}
	// 冒号后若还有斜杠,说明那是端口号而非 tag
	if strings.Contains(ref[i+1:], "/") {
		return ref, ""
	}
	return ref[:i], ref[i+1:]
}

// --- 拉取进度聚合(docs/docker.md §4.4) ---

// ProgressDetail 单条事件里的字节进度。
type ProgressDetail struct {
	Current uint64 `json:"current"`
	Total   uint64 `json:"total"`
}

// PullMessage 拉取事件流中的一条消息。
type PullMessage struct {
	Status         string          `json:"status"`
	ID             string          `json:"id"`
	Progress       string          `json:"progress"`
	ProgressDetail *ProgressDetail `json:"progressDetail"`
	Error          string          `json:"error"`
	ErrorDetail    *struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}

// 层的生命周期阶段。
type layerPhase int

const (
	phasePending layerPhase = iota
	phaseDownloading
	phaseDownloaded
	phaseExtracting
	phaseComplete
)

func (p layerPhase) String() string {
	switch p {
	case phaseDownloading:
		return "downloading"
	case phaseDownloaded:
		return "downloaded"
	case phaseExtracting:
		return "extracting"
	case phaseComplete:
		return "complete"
	default:
		return "pending"
	}
}

type layerState struct {
	id                 string
	phase              layerPhase
	dlCurrent, dlTotal uint64
	exCurrent, exTotal uint64
	alreadyExists      bool
}

// LayerProgress 单层的进度快照(供 UI 展开明细)。
type LayerProgress struct {
	ID      string `json:"id"`
	Phase   string `json:"phase"`
	Current uint64 `json:"current"`
	Total   uint64 `json:"total"`
}

// PullProgress 聚合后的整体进度。
type PullProgress struct {
	Percent float64         `json:"percent"` // 0-100
	Layers  []LayerProgress `json:"layers"`
	Status  string          `json:"status"` // 最近一条全局状态文本
	Done    bool            `json:"done"`
	Err     error           `json:"-"`

	// ByteWeighted 为 false 表示有层的下载总量未知,已降级为「完成层数/总层数」。
	// UI 可据此说明进度的精确程度。
	ByteWeighted   bool   `json:"byteWeighted"`
	Current, Total uint64 `json:"-"` // 已下载 / 总字节(仅字节加权模式有意义)
}

// 下载与解压在总进度中的权重(docs §4.4)。
const (
	downloadWeight = 0.8
	extractWeight  = 0.2
)

// ProgressAggregator 把 docker pull 的层级事件流聚合成单一进度。
//
// 非并发安全:应在读取事件流的同一个 goroutine 内使用。
type ProgressAggregator struct {
	layers map[string]*layerState
	order  []string
	status string
	err    error
	done   bool
}

// NewProgressAggregator 创建聚合器。
func NewProgressAggregator() *ProgressAggregator {
	return &ProgressAggregator{layers: map[string]*layerState{}}
}

// isLayerStatus 判断该状态是否描述某一层(而非整体)。
func isLayerStatus(s string) bool {
	switch s {
	case "Pulling fs layer", "Waiting", "Downloading", "Verifying Checksum",
		"Download complete", "Extracting", "Pull complete", "Already exists":
		return true
	}
	return false
}

// Apply 应用一条事件。
func (a *ProgressAggregator) Apply(m PullMessage) {
	if m.Error != "" || (m.ErrorDetail != nil && m.ErrorDetail.Message != "") {
		msg := m.Error
		if msg == "" {
			msg = m.ErrorDetail.Message
		}
		a.err = &Error{Status: 0, Message: msg}
		return
	}

	if !isLayerStatus(m.Status) {
		if m.Status != "" {
			a.status = m.Status
		}
		// "Status: Downloaded newer image for x" / "Status: Image is up to date for x"
		if strings.HasPrefix(m.Status, "Status:") {
			a.done = true
		}
		return
	}

	if m.ID == "" {
		return
	}
	l := a.layer(m.ID)

	switch m.Status {
	case "Pulling fs layer", "Waiting":
		if l.phase < phaseDownloading {
			l.phase = phasePending
		}
	case "Downloading":
		l.phase = phaseDownloading
		if m.ProgressDetail != nil {
			l.dlCurrent = m.ProgressDetail.Current
			if m.ProgressDetail.Total > 0 {
				l.dlTotal = m.ProgressDetail.Total
			}
		}
	case "Verifying Checksum", "Download complete":
		l.phase = phaseDownloaded
		if l.dlTotal > 0 {
			l.dlCurrent = l.dlTotal // 补齐,避免停在 99%
		}
	case "Extracting":
		l.phase = phaseExtracting
		if m.ProgressDetail != nil {
			l.exCurrent = m.ProgressDetail.Current
			if m.ProgressDetail.Total > 0 {
				l.exTotal = m.ProgressDetail.Total
			}
		}
	case "Pull complete":
		l.phase = phaseComplete
		if l.dlTotal > 0 {
			l.dlCurrent = l.dlTotal
		}
		if l.exTotal > 0 {
			l.exCurrent = l.exTotal
		}
	case "Already exists":
		// 本地已有该层:直接完成,且不参与字节加权(它没有下载量)
		l.phase = phaseComplete
		l.alreadyExists = true
	}
}

func (a *ProgressAggregator) layer(id string) *layerState {
	if l, ok := a.layers[id]; ok {
		return l
	}
	l := &layerState{id: id}
	a.layers[id] = l
	a.order = append(a.order, id)
	return l
}

// Progress 返回当前聚合进度。
//
// 字节加权:总进度 = Σ(层权重 × 层进度) / Σ层权重,层权重取其下载总量,
// 层进度 = 下载比例×0.8 + 解压比例×0.2。
//
// 降级:若存在「正在下载但总量未知」的层,则整体退回「完成层数/总层数」——
// 此时字节分母不可信,继续按字节算会得到跳变的假百分比(docs §4.4)。
// 注意 "Already exists" 的层不触发降级:它们确定已完成,只是没有下载量。
func (a *ProgressAggregator) Progress() PullProgress {
	p := PullProgress{
		Status: a.status,
		Done:   a.done,
		Err:    a.err,
		Layers: make([]LayerProgress, 0, len(a.order)),
	}

	var (
		weightSum   float64
		weightedVal float64
		completed   int
		unknownSize bool
		curBytes    uint64
		totalBytes  uint64
	)

	for _, id := range a.order {
		l := a.layers[id]

		cur, total := l.dlCurrent, l.dlTotal
		if l.phase == phaseExtracting || (l.phase == phaseComplete && l.exTotal > 0) {
			cur, total = l.exCurrent, l.exTotal
		}
		p.Layers = append(p.Layers, LayerProgress{
			ID: l.id, Phase: l.phase.String(), Current: cur, Total: total,
		})

		if l.phase == phaseComplete {
			completed++
		}
		// 尚未完成却不知道总量 —— 字节进度不可信
		if l.dlTotal == 0 && !l.alreadyExists && l.phase != phaseComplete {
			unknownSize = true
		}
		if l.dlTotal == 0 {
			continue // 不参与字节加权
		}

		w := float64(l.dlTotal)
		weightSum += w
		weightedVal += w * layerFraction(l)
		curBytes += l.dlCurrent
		totalBytes += l.dlTotal
	}

	switch {
	case len(a.order) == 0:
		p.Percent = 0
		if a.done {
			p.Percent = 100
		}
	case unknownSize || weightSum == 0:
		p.ByteWeighted = false
		p.Percent = float64(completed) / float64(len(a.order)) * 100
	default:
		p.ByteWeighted = true
		p.Percent = weightedVal / weightSum * 100
		p.Current, p.Total = curBytes, totalBytes
	}

	if a.done && p.Err == nil {
		p.Percent = 100
	}
	if p.Percent > 100 {
		p.Percent = 100
	}
	return p
}

// layerFraction 单层的完成比例(0-1),下载占 80%、解压占 20%。
func layerFraction(l *layerState) float64 {
	if l.phase == phaseComplete {
		return 1
	}
	var f float64
	if l.dlTotal > 0 {
		dl := float64(l.dlCurrent) / float64(l.dlTotal)
		if dl > 1 {
			dl = 1
		}
		f += dl * downloadWeight
	}
	if l.exTotal > 0 {
		ex := float64(l.exCurrent) / float64(l.exTotal)
		if ex > 1 {
			ex = 1
		}
		f += ex * extractWeight
	}
	return f
}

// DecodePullStream 解析拉取事件流并在每次进度更新时回调。
//
// 回调返回错误会中止解析(用于订阅者断开)。流正常结束返回 nil;
// 若事件流中携带了 daemon 报告的错误(如镜像不存在),返回该错误。
func DecodePullStream(ctx context.Context, r io.Reader, fn func(PullProgress) error) error {
	agg := NewProgressAggregator()
	dec := json.NewDecoder(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var m PullMessage
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		agg.Apply(m)
		if err := fn(agg.Progress()); err != nil {
			return err
		}
	}
	return agg.Progress().Err
}
