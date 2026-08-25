// Package compose 封装 docker compose CLI 的调用(docs §5.3)。
//
// compose 没有 Engine API 等价物,只能走 CLI。本包负责:
//   - 二进制探测(tools/docker-compose 优先,回退系统 docker compose)
//   - 项目发现(ls -a --format json)
//   - 校验(config -q,从 stdin 读,纯只读)
//   - 部署/停止/重启(up --progress json 流式 / down / restart)
//   - 应用层项目锁(同一 projectName 并发互斥,docs §4.1 Q16)
package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Project 是 `docker compose ls -a --format json` 里的一条。
type Project struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`      // "running(2)" / "exited(1)" / ""(未部署)
	ConfigFiles string `json:"ConfigFiles"` // 逗号分隔的绝对路径
}

// RunningCount 从 "running(2)" 这类状态里解析运行中的容器数。
func (p Project) RunningCount() int {
	return countInStatus(p.Status, "running")
}

// TotalCount 解析状态里的容器总数(running + exited + ...)。
func (p Project) TotalCount() int {
	total := 0
	for _, key := range []string{"running", "exited", "created", "restarting", "paused", "dead"} {
		total += countInStatus(p.Status, key)
	}
	return total
}

// countInStatus 从 "running(2), exited(1)" 里提取指定状态的计数。
func countInStatus(status, key string) int {
	for _, part := range strings.Split(status, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, key+"(") {
			continue
		}
		end := strings.Index(part, ")")
		if end < 0 {
			continue
		}
		n, err := strconv.Atoi(part[len(key)+1 : end])
		if err == nil {
			return n
		}
	}
	return 0
}

// CLI compose 命令行封装。
type CLI struct {
	bin     string // 独立二进制路径;空 = 走系统 `docker compose` 子命令
	dataDir string

	mu    sync.Mutex
	locks map[string]bool // projectName -> 是否部署中
}

// New 构造 CLI,探测可用的 compose 二进制。
//
// 优先用 GateBox 自带的 tools/docker-compose(独立升级、版本锁定);
// 不存在则回退系统的 docker compose 子命令。
func New(dataDir string) *CLI {
	bin := ""
	if p := filepath.Join(dataDir, "tools", "docker-compose"); fileExists(p) {
		bin = p
	}
	return &CLI{bin: bin, dataDir: dataDir, locks: map[string]bool{}}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// command 构造一条 compose 命令。bin 非空时用独立二进制,否则用 docker 子命令。
func (c *CLI) command(ctx context.Context, args ...string) *exec.Cmd {
	if c.bin != "" {
		return exec.CommandContext(ctx, c.bin, args...)
	}
	return exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
}

// ManagedDir 返回托管项目的数据目录 <dataDir>/appData/<project>。
func (c *CLI) ManagedDir(project string) string {
	return filepath.Join(c.dataDir, "appData", project)
}

// ManagedFile 返回托管项目的 compose 文件路径。
func (c *CLI) ManagedFile(project string) string {
	return filepath.Join(c.ManagedDir(project), "docker-compose.yaml")
}

// Discover 列出所有 compose 项目(docs §2.3 Q24:用 ls -a 而非扫容器 label)。
func (c *CLI) Discover(ctx context.Context) ([]Project, error) {
	out, err := c.command(ctx, "ls", "-a", "--format", "json").Output()
	if err != nil {
		// ls 在无项目或 daemon 异常时也会非 0,把 stderr 一起带上
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("compose ls 失败: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return []Project{}, nil
	}
	var projects []Project
	if err := json.Unmarshal([]byte(trimmed), &projects); err != nil {
		return nil, fmt.Errorf("解析 compose ls 输出: %w", err)
	}
	return projects, nil
}

// Validate 校验 YAML:docker compose -f - config -q,从 stdin 读入,纯只读。
//
// 返回 nil 表示语法与 schema 均合法。注意 config -q 的边界(docs §4.1):
// 查不出「镜像不存在/端口占用/挂载路径缺失/未定义变量」,这些需另行处理。
func (c *CLI) Validate(ctx context.Context, yamlStr, dir string) error {
	cmd := c.command(ctx, "-f", "-", "--project-directory", dir, "config", "-q")
	cmd.Stdin = strings.NewReader(yamlStr)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// Stream 包装一条已启动的流式命令,供调用方读取 stdout/stderr 并在结束时回收进程。
type Stream struct {
	io.ReadCloser
	cmd *exec.Cmd
}

// Wait 等待命令结束并返回进程错误(exit code 非 0 时为非 nil)。
func (s *Stream) Wait() error { return s.cmd.Wait() }

// Up 部署项目(docs §4.1 ⑤)。返回 --progress json 的事件流(JSON Lines)。
//
// 关键(docs §5.3):--progress 是全局选项须放最前;进度与错误全走 stderr,
// stdout 恒为空——只读 stdout 会得到一片空白。
func (c *CLI) Up(ctx context.Context, project, dir string) (*Stream, error) {
	cmd := c.command(ctx, "--progress", "json", "-p", project, "--project-directory", dir, "up", "-d")
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Stream{ReadCloser: stderr, cmd: cmd}, nil
}

// Down 停止并删除项目容器。file 为空时用 --project-directory 由 compose 自行定位。
func (c *CLI) Down(ctx context.Context, project, file, dir string) error {
	args := []string{"-p", project}
	if file != "" {
		args = append(args, "-f", file)
	}
	if dir != "" {
		args = append(args, "--project-directory", dir)
	}
	args = append(args, "down")
	return c.runCapture(ctx, args...)
}

// Restart 重启项目容器(不重建镜像)。
func (c *CLI) Restart(ctx context.Context, project, file, dir string) error {
	args := []string{"-p", project}
	if file != "" {
		args = append(args, "-f", file)
	}
	if dir != "" {
		args = append(args, "--project-directory", dir)
	}
	args = append(args, "restart")
	return c.runCapture(ctx, args...)
}

// runCapture 运行一条命令并捕获 stderr,失败时把 stderr 作为错误返回。
func (c *CLI) runCapture(ctx context.Context, args ...string) error {
	cmd := c.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// TryLock 尝试获取项目锁(docs §4.1 Q16)。已被占用时返回 false,不排队。
func (c *CLI) TryLock(project string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locks[project] {
		return false
	}
	c.locks[project] = true
	return true
}

// Unlock 释放项目锁。
func (c *CLI) Unlock(project string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.locks, project)
}
