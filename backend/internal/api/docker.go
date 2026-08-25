package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/docker/client"
	"github.com/JiangBeta/gatebox/internal/docker/compose"
	"github.com/JiangBeta/gatebox/internal/docker/stats"
	"github.com/JiangBeta/gatebox/internal/store"
)

// dockerAPI Docker 页的处理器。
type dockerAPI struct {
	cli     *client.Client
	coll    *stats.Collector
	s       *store.Store // 私有仓库凭证、编排项目等落库实体
	dj      string       // daemon.json 路径(docs §3.3.1)
	cmp     *compose.CLI // compose CLI 封装(docs §5.3)
	dataDir string       // 运行时数据根目录(定位 appData/)
}

// RegisterDocker 注册 Docker 相关路由。
func RegisterDocker(mux *http.ServeMux, cli *client.Client, coll *stats.Collector, s *store.Store, daemonJSON, dataDir string) {
	d := &dockerAPI{cli: cli, coll: coll, s: s, dj: daemonJSON, cmp: compose.New(dataDir), dataDir: dataDir}

	mux.HandleFunc("GET /api/v1/docker/info", d.info)
	mux.HandleFunc("GET /api/v1/docker/containers", d.listContainers)
	mux.HandleFunc("GET /api/v1/docker/containers/{id}", d.inspectContainer)
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/start", d.startContainer)
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/stop", d.stopContainer)
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/restart", d.restartContainer)
	mux.HandleFunc("DELETE /api/v1/docker/containers/{id}", d.removeContainer)
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/shells", d.detectShell)

	// 镜像(docs §3.3)
	mux.HandleFunc("GET /api/v1/docker/images", d.listImages)
	mux.HandleFunc("DELETE /api/v1/docker/images", d.removeImage) // ?ref=
	mux.HandleFunc("POST /api/v1/docker/images/load", d.loadImage)

	// 网络(docs §3.4)
	mux.HandleFunc("GET /api/v1/docker/networks", d.listNetworks)
	mux.HandleFunc("POST /api/v1/docker/networks", d.createNetwork)
	mux.HandleFunc("GET /api/v1/docker/networks/{id}", d.inspectNetwork)
	mux.HandleFunc("DELETE /api/v1/docker/networks/{id}", d.removeNetwork)

	// 存储卷(docs §3.5)
	mux.HandleFunc("GET /api/v1/docker/volumes", d.listVolumes)
	mux.HandleFunc("POST /api/v1/docker/volumes", d.createVolume)
	mux.HandleFunc("DELETE /api/v1/docker/volumes", d.removeVolume) // ?name=
	mux.HandleFunc("POST /api/v1/docker/volumes/prune", d.pruneVolumes)

	// 私有仓库 + daemon 配置(docs §3.3.1)
	mux.HandleFunc("GET /api/v1/docker/registries", d.listRegistries)
	mux.HandleFunc("POST /api/v1/docker/registries", d.createRegistry)
	mux.HandleFunc("PUT /api/v1/docker/registries/{id}", d.updateRegistry)
	mux.HandleFunc("DELETE /api/v1/docker/registries/{id}", d.deleteRegistry)
	mux.HandleFunc("GET /api/v1/docker/daemon", d.getDaemon)
	mux.HandleFunc("PUT /api/v1/docker/daemon", d.updateDaemon)

	// 编排(docs §3.2,见 docker_compose.go)
	mux.HandleFunc("GET /api/v1/docker/compose", d.listCompose)
	mux.HandleFunc("POST /api/v1/docker/compose", d.createCompose)
	mux.HandleFunc("POST /api/v1/docker/compose/validate", d.validateCompose)
	mux.HandleFunc("GET /api/v1/docker/compose/{project}", d.getCompose)
	mux.HandleFunc("PUT /api/v1/docker/compose/{project}", d.saveCompose)
	mux.HandleFunc("POST /api/v1/docker/compose/{project}/down", d.downCompose)
	mux.HandleFunc("POST /api/v1/docker/compose/{project}/restart", d.restartCompose)
	mux.HandleFunc("POST /api/v1/docker/compose/{project}/restore", d.restoreCompose)
	mux.HandleFunc("DELETE /api/v1/docker/compose/{project}", d.deleteCompose)

	// WebSocket(见 docker_ws.go)
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/logs", d.wsLogs)
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/exec", d.wsExec)
	mux.HandleFunc("GET /api/v1/docker/images/pull", d.wsPullImage)
	mux.HandleFunc("GET /api/v1/docker/compose/{project}/deploy", d.wsDeploy)
}

// --- 视图模型 ---

// 容器来源(docs §2.3 的三态)。
const (
	sourceManaged  = "managed"  // GateBox 在 appData 下创建(需编排数据层判定,当前均归为 external)
	sourceExternal = "external" // 外部 compose 项目
	sourceLoose    = "loose"    // docker run 起的游离容器
)

type portView struct {
	IP        string `json:"ip,omitempty"`
	Host      uint16 `json:"host,omitempty"`
	Container uint16 `json:"container"`
	Protocol  string `json:"protocol"`
}

type containerView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Source  string `json:"source"`
	Project string `json:"project,omitempty"`
	Service string `json:"service,omitempty"`

	Ports     []portView `json:"ports"`
	CreatedAt time.Time  `json:"createdAt"`
	StartedAt *time.Time `json:"startedAt,omitempty"`

	// TTY 与 LogsReadable 决定前端日志面板的行为(docs §5.5)
	TTY          bool   `json:"tty"`
	LogDriver    string `json:"logDriver,omitempty"`
	LogsReadable bool   `json:"logsReadable"`
	Health       string `json:"health,omitempty"`

	HasStats      bool    `json:"hasStats"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsage   uint64  `json:"memoryUsage"`
	MemoryLimit   uint64  `json:"memoryLimit"`
	MemoryPercent float64 `json:"memoryPercent"`
}

// --- 处理器 ---

func (d *dockerAPI) info(w http.ResponseWriter, r *http.Request) {
	info, err := d.cli.SystemInfo(r.Context())
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"serverVersion":     info.ServerVersion,
		"containers":        info.Containers,
		"containersRunning": info.ContainersRunning,
		"images":            info.Images,
		"loggingDriver":     info.LoggingDriver,
		"logsReadable":      info.LogsReadable(),
		"cgroupVersion":     info.CgroupVersion,
		"architecture":      info.Architecture,
		"imagePlatform":     info.ImagePlatform(),
		"ncpu":              info.NCPU,
		"memTotal":          info.MemTotal,
		"liveRestore":       info.LiveRestoreEnabled,
	})
}

// listContainers 返回容器列表,合并 inspect 详情与 stats 快照。
//
// 每次请求都 Touch 采集器:轮询期间采集保持运行,前端离开后
// 冷却期结束即自动停止(docs §5.2)。
func (d *dockerAPI) listContainers(w http.ResponseWriter, r *http.Request) {
	if d.coll != nil {
		d.coll.Touch()
	}

	list, err := d.cli.ListContainers(r.Context(), client.ListContainersOptions{All: true})
	if err != nil {
		writeDockerErr(w, err)
		return
	}

	details := d.inspectAll(r.Context(), list)
	snap := map[string]stats.Sample{}
	if d.coll != nil {
		snap = d.coll.SnapshotAll()
	}

	managed := d.managedProjects(r.Context())

	out := make([]containerView, 0, len(list))
	for _, ct := range list {
		out = append(out, buildView(ct, details[ct.ID], snap[ct.ID], managed))
	}
	// 运行中的排前面,其次按名称,保证列表顺序稳定
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := out[i].State == "running", out[j].State == "running"
		if ri != rj {
			return ri
		}
		return out[i].Name < out[j].Name
	})

	writeJSON(w, http.StatusOK, out)
}

// inspectAll 并发获取详情。列表接口不返回 StartedAt / Tty / 日志驱动,
// 而这三者分别是「运行时长」「日志解析方式」「日志可读性」所必需的。
func (d *dockerAPI) inspectAll(ctx context.Context, list []client.Container) map[string]*client.ContainerDetail {
	const concurrency = 8

	out := make(map[string]*client.ContainerDetail, len(list))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	sem := make(chan struct{}, concurrency)

	for _, ct := range list {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			det, err := d.cli.InspectContainer(ctx, id)
			if err != nil {
				return // 单个失败不影响整体,该容器降级为仅列表信息
			}
			mu.Lock()
			out[id] = det
			mu.Unlock()
		}(ct.ID)
	}
	wg.Wait()
	return out
}

// managedProjects 返回托管编排项目的 projectName 集合,供容器三态判定。
func (d *dockerAPI) managedProjects(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	if d.s == nil {
		return out
	}
	insts, err := d.s.ListComposeInstances()
	if err != nil {
		return out
	}
	for _, inst := range insts {
		if inst.Managed {
			out[inst.ProjectName] = true
		}
	}
	return out
}

func buildView(ct client.Container, det *client.ContainerDetail, sample stats.Sample, managed map[string]bool) containerView {
	v := containerView{
		ID:        ct.ID,
		Name:      ct.Name(),
		Image:     ct.Image,
		State:     ct.State,
		Status:    ct.Status,
		Project:   ct.ComposeProject(),
		Service:   ct.ComposeService(),
		CreatedAt: ct.CreatedAt(),
		Ports:     make([]portView, 0, len(ct.Ports)),
	}

	v.Source = sourceLoose
	if v.Project != "" {
		// 三态判定(docs §2.3):项目在编排数据层且 managed=true 才算「托管」,
		// 其余 compose 容器是「外部编排」;无 project label 的是「游离」。
		if managed[v.Project] {
			v.Source = sourceManaged
		} else {
			v.Source = sourceExternal
		}
	}

	for _, p := range ct.Ports {
		v.Ports = append(v.Ports, portView{
			IP: p.IP, Host: p.PublicPort, Container: p.PrivatePort, Protocol: p.Type,
		})
	}
	sort.SliceStable(v.Ports, func(i, j int) bool { return v.Ports[i].Container < v.Ports[j].Container })

	if det != nil {
		v.TTY = det.Config.Tty
		if det.State.Running && !det.State.StartedAt.IsZero() {
			t := det.State.StartedAt
			v.StartedAt = &t
		}
		if det.State.Health != nil {
			v.Health = det.State.Health.Status
		}
		// 容器可覆盖 daemon 的默认日志驱动,判定必须看容器自身的设置
		v.LogDriver = det.LogDriver()
	}
	// 空串表示随 daemon 默认,此时按可读处理;daemon 级信息见 /docker/info
	v.LogsReadable = client.LogDriverReadable(v.LogDriver)

	if sample.At.IsZero() {
		v.HasStats = false
	} else {
		v.HasStats = true
		v.CPUPercent = sample.CPUPercent
		v.MemoryUsage = sample.MemoryUsage
		v.MemoryLimit = sample.MemoryLimit
		v.MemoryPercent = sample.MemoryPercent
	}
	return v
}

func (d *dockerAPI) inspectContainer(w http.ResponseWriter, r *http.Request) {
	det, err := d.cli.InspectContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (d *dockerAPI) startContainer(w http.ResponseWriter, r *http.Request) {
	err := d.cli.StartContainer(r.Context(), r.PathValue("id"))
	// 已在运行:视为成功,用户的意图已经达成
	if err != nil && !errors.Is(err, client.ErrNotModified) {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *dockerAPI) stopContainer(w http.ResponseWriter, r *http.Request) {
	var timeout *int
	if v := r.URL.Query().Get("timeout"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			timeout = &n
		}
	}
	err := d.cli.StopContainer(r.Context(), r.PathValue("id"), timeout)
	if err != nil && !errors.Is(err, client.ErrNotModified) {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *dockerAPI) restartContainer(w http.ResponseWriter, r *http.Request) {
	if err := d.cli.RestartContainer(r.Context(), r.PathValue("id"), nil); err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// removeContainer 删除容器。
//
// 策略:只有已停止的容器才能删除(docs §4.5)。运行中的容器在此被拒绝,
// 而不是传 force 强删 —— 强删会跳过优雅停止,可能损坏应用数据。
func (d *dockerAPI) removeContainer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	det, err := d.cli.InspectContainer(r.Context(), id)
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	if det.State.Running {
		writeErr(w, http.StatusConflict, "容器正在运行,请先停止再删除")
		return
	}

	opts := client.RemoveContainerOptions{
		RemoveVolumes: r.URL.Query().Get("removeVolumes") == "true",
	}
	if err := d.cli.RemoveContainer(r.Context(), id, opts); err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// detectShell 探测容器内可用的 shell,供控制台预填命令。
func (d *dockerAPI) detectShell(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	shell, err := d.cli.DetectShell(ctx, r.PathValue("id"), nil)
	if errors.Is(err, client.ErrNoShell) {
		// 不是错误,而是一种确定的状态:该镜像不含 shell(docs §5.4)
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "shell": ""})
		return
	}
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "shell": shell})
}

// writeDockerErr 把 docker 客户端错误映射为合适的 HTTP 状态码。
func writeDockerErr(w http.ResponseWriter, err error) {
	var apiErr *client.Error
	switch {
	case errors.Is(err, client.ErrNotFound):
		writeErr(w, http.StatusNotFound, "对象不存在")
	case errors.Is(err, client.ErrConflict):
		msg := "操作冲突"
		if errors.As(err, &apiErr) && apiErr.Message != "" {
			msg = apiErr.Message
		}
		writeErr(w, http.StatusConflict, msg)
	default:
		msg := err.Error()
		if errors.As(err, &apiErr) && apiErr.Message != "" {
			msg = apiErr.Message
		}
		writeErr(w, http.StatusInternalServerError, msg)
	}
}
