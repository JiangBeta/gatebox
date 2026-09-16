package component

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/source"
)

// Info 组件对外视图 = 静态描述 + 版本 + 运行态。
type Info struct {
	Descriptor
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Status          Status `json:"status"`
	// Installed 制品/端点是否已安装（二进制或 socket 存在）。
	Installed bool   `json:"installed"`
	Repo      string `json:"repo,omitempty"`
}

// core 一个内置组件的描述与探测/运行方式。
type core struct {
	desc        Descriptor
	bin         string
	versionArgs []string
	repo        string
	assetHint   string
	// check 运行态探测方式：stat（端点存在）| always（CLI，安装即视为运行）| http（探活 URL）| pid（托管进程）。
	check    string
	checkURL string
	// 托管进程启动参数（仅 runnable 组件需要）。
	runArgs []string
	workDir string
	env     []string
	// reloadURL/reloadFile：以重载配置代替进程重启（如 caddy POST /load）。
	reloadURL  string
	reloadFile string
	// dockerSocket：经 Unix socket 查询 Engine 版本。
	dockerSocket string
	// processName：宿主进程名（系统托管组件，用于重载/重启定位，如 dockerd）。
	processName string
	// serviceUnit：系统服务单元名（如 caddy.service）。设置后「重启」优先经
	// systemd 重启该单元（可让替换后的制品生效），失败再回退配置重载。
	serviceUnit string
}

// CoreRegistry 内置组件注册表（核心 + 可选的系统探测）。
type CoreRegistry struct {
	dataDir string
	src     *source.Client
	cores   []core
}

// NewCoreRegistry 构造内置组件注册表。
func NewCoreRegistry(dataDir, caddyBin, caddyAdmin string, src *source.Client) *CoreRegistry {
	// 统一转绝对路径：spawn 会设置子进程工作目录（cmd.Dir），
	// 若二进制/配置文件为相对路径会相对该工作目录解析而找不到。
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	if caddyBin != "" {
		if abs, err := filepath.Abs(caddyBin); err == nil {
			caddyBin = abs
		}
	}
	tools := func(p ...string) string { return filepath.Join(append([]string{dataDir, "tools"}, p...)...) }
	caps := func(v ...string) []string { return v }
	cores := []core{
		{
			desc: Descriptor{ID: "caddy", Name: "Caddy", Summary: "反向代理与站点生成引擎",
				Kind: KindCore, Tier: "core", Tags: []string{"核心"},
				Provision: "managed", Runtime: "manage", Upgrade: "replace",
				Capabilities: caps("health", "config", "upgradable", "logs", "restartable"), DefaultEnabled: true,
				Source: Source{Channel: "official"}},
			bin: caddyBin, versionArgs: []string{"version"}, repo: "caddyserver/caddy", assetHint: "caddy",
			check: "http", checkURL: strings.TrimRight(caddyAdmin, "/") + "/config/",
			workDir:     filepath.Dir(caddyBin),
			reloadURL:   strings.TrimRight(caddyAdmin, "/") + "/load",
			reloadFile:  filepath.Join(dataDir, "Caddyfile"),
			serviceUnit: "caddy.service",
		},
		{
			desc: Descriptor{ID: "acme", Name: "acme.sh", Summary: "证书签发与自动续期",
				Kind: KindCore, Tier: "core", Tags: []string{"核心"},
				Provision: "managed", Runtime: "manage", Upgrade: "replace",
				Capabilities: caps("health", "config", "upgradable", "restartable"), DefaultEnabled: true,
				Source: Source{Channel: "official"}},
			bin: tools("acme", "acme.sh"), versionArgs: []string{"--version"}, repo: "acmesh-official/acme.sh", assetHint: "acme.sh",
			check: "always",
		},
		{
			desc: Descriptor{ID: "docker", Name: "Docker Engine", Summary: "容器运行时（由系统管理）",
				Kind: KindCore, Tier: "core", Tags: []string{"核心"},
				Provision: "attached", Runtime: "observe", Upgrade: "system",
				Capabilities: caps("health", "operable", "restartable"), DefaultEnabled: true,
				Source: Source{Channel: "system"}},
			bin:   "/var/run/docker.sock",
			check: "stat", dockerSocket: "/var/run/docker.sock", processName: "dockerd",
			repo: "moby/moby",
		},
		{
			desc: Descriptor{ID: "tailscale", Name: "Tailscale", Summary: "跨网组网，安全访问内网服务",
				Kind: KindProcess, Tier: "optional", Tags: []string{"独立进程"},
				Provision: "attached", Runtime: "observe", Upgrade: "system",
				Capabilities: caps("health", "operable"), Source: Source{Channel: "system"}},
			bin: "/usr/bin/tailscale", versionArgs: []string{"version"},
			check: "always",
		},
	}
	return &CoreRegistry{dataDir: dataDir, src: src, cores: cores}
}

// Get 返回组件视图。
func (r *CoreRegistry) Get(ctx context.Context, id string) (Info, bool) {
	for _, c := range r.cores {
		if c.desc.ID == id {
			return r.info(ctx, c), true
		}
	}
	return Info{}, false
}

// List 返回全部组件视图。
func (r *CoreRegistry) List(ctx context.Context) []Info {
	out := make([]Info, 0, len(r.cores))
	for _, c := range r.cores {
		out = append(out, r.info(ctx, c))
	}
	return out
}

func (r *CoreRegistry) info(ctx context.Context, c core) Info {
	info := Info{Descriptor: c.desc, Repo: c.repo}
	cur, installed, status := probe(ctx, c)
	info.Current = cur
	info.Installed = installed
	info.Status = status
	return info
}

// CheckUpdate 查询源通道最新版本（GitHub 官方源）。
func (r *CoreRegistry) CheckUpdate(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, errNotFound
	}
	info := r.info(ctx, c)
	if c.repo == "" {
		return info, nil
	}
	rel, err := r.src.GitHubLatest(ctx, c.repo)
	if err != nil {
		return info, err
	}
	applyLatest(&info, rel.Version)
	return info, nil
}

// CheckAll 并发检查所有组件的最新版本（无制品源的组件原样返回）。
func (r *CoreRegistry) CheckAll(ctx context.Context) []Info {
	out := make([]Info, len(r.cores))
	var wg sync.WaitGroup
	for i, c := range r.cores {
		wg.Add(1)
		go func(i int, c core) {
			defer wg.Done()
			info := r.info(ctx, c)
			if c.repo != "" {
				if rel, err := r.src.GitHubLatest(ctx, c.repo); err == nil {
					applyLatest(&info, rel.Version)
				}
			}
			out[i] = info
		}(i, c)
	}
	wg.Wait()
	return out
}

// Upgrade 用源通道最新制品原子替换（仅 replace 策略）。
func (r *CoreRegistry) Upgrade(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, errNotFound
	}
	info := r.info(ctx, c)
	if c.desc.Upgrade != "replace" {
		return info, errSystemManaged
	}
	if c.repo == "" {
		return info, errNoSource
	}
	ver, err := r.fetchAndInstall(ctx, c)
	if err != nil {
		return info, err
	}
	out := r.info(ctx, c)
	out.Latest = ver
	out.UpdateAvailable = false
	return out, nil
}

// Install 安装制品（下载最新 release 并原子落盘到组件路径）。
func (r *CoreRegistry) Install(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, errNotFound
	}
	info := r.info(ctx, c)
	if c.desc.Tier == "core" {
		return info, errCoreImmutable
	}
	if c.desc.Upgrade != "replace" {
		return info, errSystemManaged
	}
	if c.repo == "" {
		return info, errNoSource
	}
	if _, err := r.fetchAndInstall(ctx, c); err != nil {
		return info, err
	}
	return r.info(ctx, c), nil
}

// Uninstall 卸载组件：先停进程，再删除组件目录（仅 Removable）。
func (r *CoreRegistry) Uninstall(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, errNotFound
	}
	info := r.info(ctx, c)
	if !c.desc.Removable {
		return info, errNotRemovable
	}
	if _, err := r.Stop(ctx, id); err != nil {
		return info, err
	}
	dir := filepath.Dir(c.bin)
	// 仅允许删除 $DATA_DIR/tools 下的组件目录，避免误删系统路径。
	rel, err := filepath.Rel(filepath.Join(r.dataDir, "tools"), dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return info, errNotRemovable
	}
	if err := os.RemoveAll(dir); err != nil {
		return info, err
	}
	return r.info(ctx, c), nil
}

// fetchAndInstall 下载最新匹配制品并原子安装，返回版本号。
func (r *CoreRegistry) fetchAndInstall(ctx context.Context, c core) (string, error) {
	rel, err := r.src.GitHubLatest(ctx, c.repo)
	if err != nil {
		return "", err
	}
	asset, ok := source.PickAsset(rel.Assets)
	if !ok {
		return "", errNoAsset
	}
	data, err := r.src.Get(ctx, asset.URL)
	if err != nil {
		return "", err
	}
	if err := source.VerifySHA256(data, asset.SHA256); err != nil {
		return "", err
	}
	bin, err := source.ExtractBinary(data, c.assetHint)
	if err != nil {
		return "", err
	}
	if c.bin == "" {
		return "", errNoBinPath
	}
	if err := source.InstallAtomic(c.bin, bin); err != nil {
		return "", err
	}
	return rel.Version, nil
}

func (r *CoreRegistry) find(id string) (core, bool) {
	for _, c := range r.cores {
		if c.desc.ID == id {
			return c, true
		}
	}
	return core{}, false
}

// probe 探测当前版本、是否已安装与运行态。
func probe(ctx context.Context, c core) (string, bool, Status) {
	st := Status{State: "unknown"}
	installed := false
	bin := resolveBin(c)
	switch c.check {
	case "http":
		// 端点可达即视为已安装且在运行；否则回退二进制存在性。
		if c.checkURL != "" && httpAlive(ctx, c.checkURL) {
			installed = true
			st = Status{State: "running", Healthy: true}
		} else if bin != "" {
			if _, err := os.Stat(bin); err == nil {
				installed = true
				st = Status{State: "stopped", Message: "服务未运行"}
			}
		}
	case "stat":
		if bin == "" {
			return "", false, st
		}
		if _, err := os.Stat(bin); err != nil {
			return "", false, Status{State: "stopped", Message: "未安装或路径不存在"}
		}
		installed = true
		st = Status{State: "running", Healthy: true}
	case "always":
		if bin == "" {
			return "", false, st
		}
		if _, err := os.Stat(bin); err != nil {
			return "", false, Status{State: "stopped", Message: "未安装或路径不存在"}
		}
		installed = true
		st = Status{State: "running", Healthy: true}
	default: // pid：托管进程
		if bin == "" {
			return "", false, st
		}
		if _, err := os.Stat(bin); err != nil {
			return "", false, Status{State: "stopped", Message: "未安装或路径不存在"}
		}
		installed = true
		switch pid := readPid(pidPath(c)); {
		case pid > 0 && processAlive(pid):
			st = Status{State: "running", Healthy: true}
		case pid > 0:
			st = Status{State: "error", Message: "进程已退出（残留 pid 文件）"}
		default:
			st = Status{State: "stopped", Message: "服务未运行"}
		}
	}
	if !installed {
		return "", installed, st
	}
	if c.dockerSocket != "" {
		return dockerVersion(ctx, c.dockerSocket), installed, st
	}
	if len(c.versionArgs) == 0 {
		return "", installed, st
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, c.versionArgs...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", installed, st
	}
	return source.ParseVersion(string(out)), installed, st
}

// resolveBin 解析组件可执行文件路径：配置路径 > PATH > 运行中的同名进程。
// 不再回退 ~/.acme.sh：acme.sh 的工作目录固定为 <dataDir>/tools/acme(--home)。
func resolveBin(c core) string {
	if c.bin == "" {
		return ""
	}
	if _, err := os.Stat(c.bin); err == nil {
		return c.bin
	}
	base := filepath.Base(c.bin)
	if p, err := exec.LookPath(base); err == nil {
		return p
	}
	if p := findRunningBin(base); p != "" {
		return p
	}
	return c.bin
}

// findRunningBin 在 /proc 中查找命令行首段与 base 同名的进程可执行路径。
func findRunningBin(base string) string {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", name, "cmdline"))
		if err != nil || len(b) == 0 {
			continue
		}
		arg0 := string(b)
		if i := strings.IndexByte(arg0, 0); i >= 0 {
			arg0 = arg0[:i]
		}
		if filepath.Base(arg0) != base {
			continue
		}
		if _, err := os.Stat(arg0); err == nil {
			return arg0
		}
	}
	return ""
}

// dockerVersion 经 Unix socket 查询 Docker Engine 版本。
func dockerVersion(ctx context.Context, sock string) string {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}
	cli := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "http://docker/version", nil)
	if err != nil {
		return ""
	}
	resp, err := cli.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var v struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return ""
	}
	return source.ParseVersion(v.Version)
}

// httpAlive 探测 HTTP 端点是否可达（任何 HTTP 响应都算存活）。
func httpAlive(ctx context.Context, url string) bool {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

func curNeedUpdate(cur, latest string) bool {
	if cur == "" || latest == "" {
		return false
	}
	return source.CompareVersions(cur, latest) < 0
}

// applyLatest 写入最新版本。当系统版本领先于源（如 acme.sh master 版本高于其最新
// release）时视为已是最新，避免出现「最新版本低于当前版本」的错误展示。
func applyLatest(info *Info, latest string) {
	if info.Current == "" {
		info.Latest = latest
		info.UpdateAvailable = false
		return
	}
	if source.CompareVersions(latest, info.Current) > 0 {
		info.Latest = latest
		info.UpdateAvailable = true
		return
	}
	info.Latest = info.Current
	info.UpdateAvailable = false
}

// 语义化错误，供 handler 判断。
var (
	errSystemManaged = &Err{Code: "SYSTEM_MANAGED", Msg: "该组件由系统/包管理器管理，请使用 apt/opkg 等升级"}
	errNoSource      = &Err{Code: "NO_SOURCE", Msg: "该组件未配置制品源，无法自动升级"}
	errNoAsset       = &Err{Code: "NO_ASSET", Msg: "源中未找到匹配当前架构的制品"}
	errNoBinPath     = &Err{Code: "NO_BIN", Msg: "组件未配置可执行文件路径"}
	errNotFound      = &Err{Code: "NOT_FOUND", Msg: "组件不存在"}
	errCoreImmutable = &Err{Code: "CORE_IMMUTABLE", Msg: "核心组件不可安装/卸载"}
	errNotRemovable  = &Err{Code: "NOT_REMOVABLE", Msg: "该组件由系统管理，不可卸载"}
	errNotInstalled  = &Err{Code: "NOT_INSTALLED", Msg: "组件尚未安装"}
	errAlreadyRun    = &Err{Code: "ALREADY_RUNNING", Msg: "组件已在运行"}
	errNotRunning    = &Err{Code: "NOT_RUNNING", Msg: "组件未运行"}
	errNotRunnable   = &Err{Code: "NOT_RUNNABLE", Msg: "该组件不支持启停操作"}
)

// Err 带错误码的语义化错误。
type Err struct {
	Code string
	Msg  string
}

func (e *Err) Error() string { return e.Msg }

// AsErr 提取语义化错误。
func AsErr(err error) (*Err, bool) {
	var e *Err
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
