package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/repository"
)

// 本文件承载私有镜像仓库凭证 CRUD 与 daemon 配置管理(docs §3.3.1)。
//
// 两个性质不同的对象分两块:
//   - 私有仓库 = GateBox 实体,落 BoltDB(凭证 AES 加密),拉取时走 X-Registry-Auth;
//   - daemon 配置 = /etc/docker/daemon.json,只开放白名单字段,写盘 + SIGHUP 热重载。
//
// daemon 配置的整个风险控制都在「白名单」上:不在白名单内的字段(storage-driver、
// data-root …)必须重启 dockerd 才生效,而 live-restore 关闭时重启 dockerd 会停掉
// 所有运行中的容器,故这里绝不碰它们。

// --- 私有仓库 ---

// registryView 下发前端的仓库结构。Secret 明文永不下发,只回传 HasSecret。
type registryView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Scheme    string    `json:"scheme"`
	Username  string    `json:"username"`
	HasSecret bool      `json:"hasSecret"`
	CreatedAt time.Time `json:"createdAt"`
}

func toRegistryView(r *model.Registry) registryView {
	return registryView{
		ID:        r.ID,
		Name:      r.Name,
		URL:       r.URL,
		Scheme:    r.Scheme,
		Username:  r.Username,
		HasSecret: r.Secret != "",
		CreatedAt: r.CreatedAt,
	}
}

// registryInput 创建/更新请求体。
type registryInput struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Scheme   string `json:"scheme"`
	Username string `json:"username"`
	Secret   string `json:"secret"` // 更新时留空表示不修改
}

func (in *registryInput) validate() (ok bool, msg string) {
	if strings.TrimSpace(in.Name) == "" {
		return false, "仓库名称不能为空"
	}
	if strings.TrimSpace(in.URL) == "" {
		return false, "仓库地址不能为空"
	}
	switch in.Scheme {
	case "http", "https":
	default:
		return false, "协议只能是 http 或 https"
	}
	return true, ""
}

func (d *dockerAPI) listRegistries(w http.ResponseWriter, r *http.Request) {
	regs, err := d.s.ListRegistries()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]registryView, 0, len(regs))
	for i := range regs {
		out = append(out, toRegistryView(&regs[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *dockerAPI) createRegistry(w http.ResponseWriter, r *http.Request) {
	var in registryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if ok, msg := in.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}

	reg := &model.Registry{
		ID:        newID(),
		Name:      strings.TrimSpace(in.Name),
		URL:       strings.TrimSpace(in.URL),
		Scheme:    in.Scheme,
		Username:  strings.TrimSpace(in.Username),
		Secret:    in.Secret,
		CreatedAt: time.Now(),
	}
	if err := d.s.SaveRegistry(reg); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toRegistryView(reg))
}

func (d *dockerAPI) updateRegistry(w http.ResponseWriter, r *http.Request) {
	reg, err := d.s.GetRegistry(r.PathValue("id"))
	if errors.Is(err, repository.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "仓库不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var in registryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if ok, msg := in.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}

	reg.Name = strings.TrimSpace(in.Name)
	reg.URL = strings.TrimSpace(in.URL)
	reg.Scheme = in.Scheme
	reg.Username = strings.TrimSpace(in.Username)
	if in.Secret != "" {
		reg.Secret = in.Secret // 留空 = 保持原密码
	}
	if err := d.s.SaveRegistry(reg); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toRegistryView(reg))
}

func (d *dockerAPI) deleteRegistry(w http.ResponseWriter, r *http.Request) {
	if err := d.s.DeleteRegistry(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// --- daemon 配置(docs §3.3.1) ---

// daemonWhiteList daemon.json 中可热重载(SIGHUP)且我们开放的字段。
// 这三项在官方 SIGHUP 可热重载的 13 项白名单内,不重启进程、不影响运行中容器。
type daemonWhiteList struct {
	RegistryMirrors        []string `json:"registry-mirrors,omitempty"`
	InsecureRegistries     []string `json:"insecure-registries,omitempty"`
	MaxConcurrentDownloads *int     `json:"max-concurrent-downloads,omitempty"`
}

// daemonView 下发前端的 daemon 配置视图。
type daemonView struct {
	Writable               bool     `json:"writable"`
	ReadOnlyReason         string   `json:"readOnlyReason,omitempty"`
	Mirrors                []string `json:"mirrors"`
	InsecureRegistries     []string `json:"insecureRegistries"`
	MaxConcurrentDownloads int      `json:"maxConcurrentDownloads"`
	LiveRestore            bool     `json:"liveRestore"`
}

// probeDaemonJSON 探测 daemon.json 是否存在且可写。
func probeDaemonJSON(path string) (writable bool, reason string) {
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return false, "daemon.json 不存在:本机由 Nix/UCI 等声明式系统托管,请在系统配置中修改"
	case err != nil:
		return false, "无法访问 daemon.json: " + err.Error()
	case info.IsDir():
		return false, "daemon.json 是一个目录"
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false, "daemon.json 不可写(需要 root 权限)"
	}
	_ = f.Close()
	return true, ""
}

func (d *dockerAPI) getDaemon(w http.ResponseWriter, r *http.Request) {
	info, err := d.cli.SystemInfo(r.Context())
	if err != nil {
		writeDockerErr(w, err)
		return
	}

	view := daemonView{
		// 只读时从 docker info 读运行时真值展示(docs §3.3.1)
		Mirrors:            info.RegistryConfig.Mirrors,
		InsecureRegistries: info.RegistryConfig.InsecureRegistryCIDRs,
		LiveRestore:        info.LiveRestoreEnabled,
	}

	if writable, reason := probeDaemonJSON(d.dj); writable {
		view.Writable = true
		if wl, err := readDaemonWhiteList(d.dj); err == nil {
			view.Mirrors = wl.RegistryMirrors
			view.InsecureRegistries = wl.InsecureRegistries
			if wl.MaxConcurrentDownloads != nil {
				view.MaxConcurrentDownloads = *wl.MaxConcurrentDownloads
			}
		}
	} else {
		view.ReadOnlyReason = reason
	}

	writeJSON(w, http.StatusOK, view)
}

func (d *dockerAPI) updateDaemon(w http.ResponseWriter, r *http.Request) {
	if writable, reason := probeDaemonJSON(d.dj); !writable {
		writeErr(w, http.StatusConflict, reason)
		return
	}

	var in struct {
		Mirrors                []string `json:"mirrors"`
		InsecureRegistries     []string `json:"insecureRegistries"`
		MaxConcurrentDownloads *int     `json:"maxConcurrentDownloads"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}

	if err := writeDaemonWhiteList(d.dj, in.Mirrors, in.InsecureRegistries, in.MaxConcurrentDownloads); err != nil {
		writeErr(w, http.StatusInternalServerError, "写 daemon.json 失败: "+err.Error())
		return
	}
	if err := reloadDaemon(); err != nil {
		writeErr(w, http.StatusInternalServerError, "写盘成功但热重载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readDaemonWhiteList 读取白名单字段(其他字段不解析,原样保留在文件里)。
func readDaemonWhiteList(path string) (*daemonWhiteList, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var full map[string]json.RawMessage
	if err := json.Unmarshal(raw, &full); err != nil {
		return nil, err
	}
	out := &daemonWhiteList{}
	if v, ok := full["registry-mirrors"]; ok {
		_ = json.Unmarshal(v, &out.RegistryMirrors)
	}
	if v, ok := full["insecure-registries"]; ok {
		_ = json.Unmarshal(v, &out.InsecureRegistries)
	}
	if v, ok := full["max-concurrent-downloads"]; ok {
		_ = json.Unmarshal(v, &out.MaxConcurrentDownloads)
	}
	return out, nil
}

// writeDaemonWhiteList 覆盖白名单字段,其余字段原样保留。
func writeDaemonWhiteList(path string, mirrors, insecure []string, maxDownloads *int) error {
	var full map[string]any
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &full); err != nil {
			return err
		}
	}
	if full == nil {
		full = map[string]any{}
	}

	full["registry-mirrors"] = nonNilSlice(mirrors)
	full["insecure-registries"] = nonNilSlice(insecure)
	if maxDownloads != nil {
		full["max-concurrent-downloads"] = *maxDownloads
	} else {
		delete(full, "max-concurrent-downloads")
	}

	data, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// reloadDaemon 向 dockerd 发 SIGHUP 触发热重载。仅白名单字段可走这条路径。
func reloadDaemon() error {
	pid, err := dockerdPID()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGHUP)
}

// dockerdPID 定位 dockerd 进程:优先读 pid 文件,回退 pgrep。
func dockerdPID() (int, error) {
	for _, p := range []string{"/var/run/docker.pid", "/run/docker.pid"} {
		if b, err := os.ReadFile(p); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid, nil
			}
		}
	}
	out, err := exec.Command("pgrep", "-x", "dockerd").Output()
	if err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pid > 0 {
			return pid, nil
		}
	}
	return 0, errors.New("找不到 dockerd 进程")
}
