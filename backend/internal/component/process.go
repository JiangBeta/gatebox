package component

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Start 启动托管进程（后台运行 + pid 文件）。
func (r *CoreRegistry) Start(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, errNotFound
	}
	info := r.info(ctx, c)
	if !runnable(c) {
		return info, errNotRunnable
	}
	if !info.Installed {
		return info, errNotInstalled
	}
	if pid := readPid(pidPath(c)); pid > 0 && processAlive(pid) {
		return info, errAlreadyRun
	}
	_ = os.Remove(pidPath(c))
	if err := spawn(c); err != nil {
		return info, err
	}
	// 短暂等待，识别「启动即退出」（如端口被占用）。
	time.Sleep(300 * time.Millisecond)
	pid := readPid(pidPath(c))
	if pid <= 0 || !processAlive(pid) {
		return r.info(ctx, c), fmt.Errorf("进程启动后立即退出，请查看日志 %s", filepath.Join(c.workDir, c.desc.ID+".log"))
	}
	return r.info(ctx, c), nil
}

// Stop 停止托管进程（SIGTERM → 超时 SIGKILL）；同时兜底清理同名残留进程。
func (r *CoreRegistry) Stop(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, errNotFound
	}
	info := r.info(ctx, c)
	if !runnable(c) {
		return info, errNotRunnable
	}
	pids := map[int]bool{}
	if pid := readPid(pidPath(c)); pid > 0 {
		pids[pid] = true
	}
	for _, pid := range findPids(filepath.Base(resolveBin(c))) {
		pids[pid] = true
	}
	for pid := range pids {
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}
	for i := 0; i < 50; i++ {
		if !anyAlive(pids) {
			break
		}
		select {
		case <-ctx.Done():
			return r.info(ctx, c), ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	for pid := range pids {
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	_ = os.Remove(pidPath(c))
	return r.info(ctx, c), nil
}

// Restart 重启组件：配置类（reloadURL）走配置重载，进程类走 stop + start。
func (r *CoreRegistry) Restart(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, errNotFound
	}
	info := r.info(ctx, c)
	// 系统服务单元:优先经 systemd 重启(让替换后的制品生效);失败且有 reloadURL 时回退配置重载。
	if c.serviceUnit != "" {
		if err := restartServiceUnit(ctx, c.serviceUnit); err == nil {
			return r.info(ctx, c), nil
		} else if c.reloadURL == "" {
			return info, err
		}
	}
	if c.reloadURL != "" {
		if !info.Installed {
			return info, errNotInstalled
		}
		data, err := os.ReadFile(c.reloadFile)
		if err != nil {
			return info, fmt.Errorf("读取配置失败: %w", err)
		}
		if err := postConfig(ctx, c.reloadURL, data); err != nil {
			return info, err
		}
		return r.info(ctx, c), nil
	}
	if !runnable(c) {
		if !restartable(c) {
			return info, errNotRunnable
		}
		// 系统托管组件（docker）：有宿主进程则 SIGHUP 重载配置，无权限/无进程则退化为重探测。
		if c.processName != "" {
			for _, pid := range findPids(c.processName) {
				_ = syscall.Kill(pid, syscall.SIGHUP)
			}
		}
		return r.info(ctx, c), nil
	}
	if _, err := r.Stop(ctx, id); err != nil {
		return info, err
	}
	return r.Start(ctx, id)
}

func runnable(c core) bool {
	return hasCap(c, "runnable")
}

func restartable(c core) bool {
	return hasCap(c, "restartable")
}

func hasCap(c core, name string) bool {
	for _, cap := range c.desc.Capabilities {
		if cap == name {
			return true
		}
	}
	return false
}

// spawn 以会话首进程方式拉起二进制，输出重定向到组件日志文件。
func spawn(c core) error {
	bin := resolveBin(c)
	if bin == "" {
		return errNoBinPath
	}
	// 子进程会 chdir 到 workDir，相对路径会被相对 workDir 解析而找不到；
	// 这里统一转绝对路径（构造期已转，双保险）。
	if abs, err := filepath.Abs(bin); err == nil {
		bin = abs
	}
	if c.workDir == "" {
		c.workDir = filepath.Dir(c.bin)
	}
	if err := os.MkdirAll(c.workDir, 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(c.workDir, c.desc.ID+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()

	cmd := exec.Command(bin, c.runArgs...)
	cmd.Dir = c.workDir
	cmd.Env = append(os.Environ(), c.env...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return &Err{Code: "START_FAILED", Msg: "启动失败: " + err.Error()}
	}
	pid := cmd.Process.Pid
	// 异步回收子进程，避免 Stop 后残留僵尸进程。
	go func() { _ = cmd.Wait() }()
	return os.WriteFile(pidPath(c), []byte(strconv.Itoa(pid)), 0o644)
}

// postConfig 以 Caddyfile 格式 POST 配置到重载端点（caddy POST /load）。
func postConfig(ctx context.Context, url string, data []byte) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("重载失败(%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func pidPath(c core) string {
	return filepath.Join(filepath.Dir(c.bin), c.desc.ID+".pid")
}

func readPid(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// findPids 在 /proc 中查找命令行首段与 base 同名的进程。
func findPids(base string) []int {
	if base == "" || base == "." || base == "/" {
		return nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
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
		if pid, err := strconv.Atoi(name); err == nil && pid > 0 {
			out = append(out, pid)
		}
	}
	return out
}

func anyAlive(pids map[int]bool) bool {
	for pid := range pids {
		if processAlive(pid) {
			return true
		}
	}
	return false
}

// processAlive 利用 signal 0 探测进程是否存在。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// restartServiceUnit 经 systemd 重启单元(system 级)。
// 失败返回错误,由调用方决定是否回退到配置重载(如权限不足)。
func restartServiceUnit(ctx context.Context, unit string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl 不可用: %w", err)
	}
	out, err := exec.CommandContext(ctx, "systemctl", "restart", unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("重启 %s 失败: %v: %s", unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}
