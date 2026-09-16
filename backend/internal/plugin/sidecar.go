package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/JiangBeta/gatebox/internal/model"
)

// ensureTokenPort 为插件分配 plugin token；process 类插件另分配本机回环端口。
func (m *Manager) ensureTokenPort(st *model.PluginState, needPort bool) {
	if st.Token == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		st.Token = hex.EncodeToString(b)
	}
	if needPort && st.Port == 0 {
		if p, err := freePort(); err == nil {
			st.Port = p
		}
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Token 返回插件 token（供前端 UI 初始化注入，ADR-039 §3）。
func (m *Manager) Token(id string) (string, bool) {
	st, err := m.repo.GetPluginState(id)
	if err != nil {
		return "", false
	}
	return st.Token, st.Token != ""
}

// 注意：sidecar 的 pid/日志与「插件提供的组件本体」区分命名（<id>-sidecar.*），
// 否则会与插件自身管理的组件进程（如 mosdns.pid）互相覆盖。
func (m *Manager) sidecarDir(id string) string { return filepath.Join(m.dataDir, "tools", id) }
func (m *Manager) sidecarBin(id string) string {
	return filepath.Join(m.sidecarDir(id), id+"-sidecar")
}
func (m *Manager) sidecarPid(id string) string {
	return filepath.Join(m.sidecarDir(id), id+"-sidecar.pid")
}
func (m *Manager) sidecarLog(id string) string {
	return filepath.Join(m.sidecarDir(id), id+"-sidecar.log")
}

// StartSidecar 启动 kind:process 的后端进程（后台运行 + pid 文件 + 日志重定向）。
func (m *Manager) StartSidecar(id string) error {
	st, err := m.state(id)
	if err != nil {
		return err
	}
	if !m.isProcess(id, st) {
		return nil
	}
	bin := m.sidecarBin(id)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("插件后端未安装: %s", bin)
	}

	// 清理可能残留的旧实例：sidecar 以 setsid 运行，GateBox 重启后会存活为孤儿，
	// 且旧端口仍被占用（否则新实例 bind 失败）。
	stopByPidFile(m.sidecarPid(id))
	_ = os.Remove(m.sidecarPid(id))

	// 每次启动重新分配回环端口，避免沿用持久化的旧端口（可能被孤儿占用）。
	if p, err := freePort(); err == nil {
		st.Port = p
	}
	m.ensureTokenPort(st, true)
	if err := m.repo.SavePluginState(st); err != nil {
		return err
	}

	logFile, _ := os.OpenFile(m.sidecarLog(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	cmd := exec.Command(bin)
	cmd.Dir = m.sidecarDir(id)
	cmd.Env = sidecarEnv(m.dataDir, id, st.Port, st.Token, m.coreURL)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	// 回收子进程，避免其退出后成为僵尸（defunct）。
	go func() { _ = cmd.Wait() }()
	_ = os.WriteFile(m.sidecarPid(id), []byte(strconv.Itoa(pid)), 0o644)
	time.Sleep(300 * time.Millisecond)
	if !processAlive(pid) {
		return fmt.Errorf("插件后端启动后立即退出，日志: %s", m.sidecarLog(id))
	}
	return nil
}

// isProcess 判断插件是否为 process 形态：优先用持久化的 Kind，
// 回退到目录查找（离线时目录可能尚未加载在线插件）。
func (m *Manager) isProcess(id string, st *model.PluginState) bool {
	if st != nil && st.Kind != "" {
		return st.Kind == "process"
	}
	if man, ok := m.find(id); ok {
		return man.Kind == "process"
	}
	return false
}

// ResumeSidecars 启动时为已启用的 process 插件恢复后端（顺带清理残留实例）。
func (m *Manager) ResumeSidecars() {
	states, err := m.repo.ListPluginStates()
	if err != nil {
		return
	}
	for _, st := range states {
		if st.State != "enabled" || !m.isProcess(st.ID, &st) {
			continue
		}
		if err := m.StartSidecar(st.ID); err != nil {
			m.SetMessage(st.ID, "启动插件后端失败: "+err.Error())
		}
	}
}

// StopAllSidecars 停止全部已启用插件的 sidecar（进程退出前调用，避免孤儿）。
func (m *Manager) StopAllSidecars() {
	states, err := m.repo.ListPluginStates()
	if err != nil {
		return
	}
	for _, st := range states {
		if !m.isProcess(st.ID, &st) {
			continue
		}
		_ = m.StopSidecar(st.ID)
	}
}

// SetMessage 更新插件消息（不改变状态）。
func (m *Manager) SetMessage(id, msg string) {
	st, err := m.state(id)
	if err != nil {
		return
	}
	st.Message = msg
	st.UpdatedAt = time.Now()
	_ = m.repo.SavePluginState(st)
}

// StopSidecar 停止后端进程（SIGTERM → 超时 SIGKILL）。
func (m *Manager) StopSidecar(id string) error {
	stopByPidFile(m.sidecarPid(id))
	_ = os.Remove(m.sidecarPid(id))
	return nil
}

// stopByPidFile 终止 pid 文件记录的进程（SIGTERM → 超时 SIGKILL）；不删除 pid 文件。
func stopByPidFile(path string) {
	pid := readPidFile(path)
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 30; i++ {
		if !processAlive(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// Proxy 把 /api/v1/plugins/<id>/* 反向代理到插件的 sidecar（仅本机回环 + token 校验）。
//
// 路由与鉴权由内核统一收口，插件后端不直接对外暴露（ADR-039 §1/§2）。
func (m *Manager) Proxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, err := m.state(id)
	if err != nil {
		http.Error(w, "插件不存在", http.StatusNotFound)
		return
	}
	if !m.isProcess(id, st) {
		http.Error(w, "该插件无后端进程", http.StatusNotFound)
		return
	}
	if st.Port == 0 {
		http.Error(w, "插件后端未就绪", http.StatusServiceUnavailable)
		return
	}
	tok := r.Header.Get("X-Plugin-Token")
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if st.Token != "" && tok != st.Token {
		http.Error(w, "plugin token 无效", http.StatusUnauthorized)
		return
	}
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(st.Port)}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "插件后端不可达: "+err.Error(), http.StatusBadGateway)
	}
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/"+id)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
	q := r.URL.Query()
	q.Del("token")
	r.URL.RawQuery = q.Encode()
	proxy.ServeHTTP(w, r)
}

// sidecarEnv 构造最小化环境变量。
//
// 只保留 PATH / LANG / TZ 与插件自身所需变量，**不继承控制面完整环境**，
// 避免把宿主机/控制面的无关密钥（如各类 TOKEN）泄露给插件进程（ADR-039 §9）。
func sidecarEnv(dataDir, id string, port int, token, coreURL string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Join(dataDir, "tools", id),
		"GATEBOX_PLUGIN_ID=" + id,
		"GATEBOX_PLUGIN_PORT=" + strconv.Itoa(port),
		"GATEBOX_PLUGIN_TOKEN=" + token,
		"GATEBOX_DATA_DIR=" + dataDir,
	}
	if coreURL != "" {
		env = append(env, "GATEBOX_CORE_URL="+coreURL)
	}
	for _, k := range []string{"LANG", "LC_ALL", "TZ"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func readPidFile(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return p
}

func processAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}
