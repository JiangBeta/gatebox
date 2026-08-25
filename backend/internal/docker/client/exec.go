package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"time"
)

// ErrNoShell 容器内找不到可用的 shell(distroless / scratch 镜像常见)。
var ErrNoShell = errors.New("docker: 容器内无可用 shell")

// DefaultShells shell 探测的候选顺序(docs/docker.md §5.4)。
// bash 体验更好但很多精简镜像只有 sh;alpine 的 sh 实为 ash。
var DefaultShells = []string{"/bin/bash", "/bin/sh", "/bin/ash"}

// ExecConfig 创建 exec 会话的参数。
type ExecConfig struct {
	Cmd        []string `json:"Cmd"`
	User       string   `json:"User,omitempty"` // 空 = 容器默认用户;控制台高级选项可填 root
	WorkingDir string   `json:"WorkingDir,omitempty"`
	Env        []string `json:"Env,omitempty"`
	Privileged bool     `json:"Privileged,omitempty"`

	// Tty 决定**输出流的格式**,且与容器自身的 Tty 无关:
	// true  → 原始字节流(交互式控制台走这条,配合 xterm.js)
	// false → stdcopy 多路复用帧
	Tty          bool `json:"Tty"`
	AttachStdin  bool `json:"AttachStdin"`
	AttachStdout bool `json:"AttachStdout"`
	AttachStderr bool `json:"AttachStderr"`
}

// ExecInspect exec 会话的状态。
type ExecInspect struct {
	ID          string `json:"ID"`
	Running     bool   `json:"Running"`
	ExitCode    int    `json:"ExitCode"`
	ContainerID string `json:"ContainerID"`
	Pid         int    `json:"Pid"`
}

// ExecCreate 在容器内创建 exec 会话,返回 exec ID。
func (c *Client) ExecCreate(ctx context.Context, containerID string, cfg ExecConfig) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	if err := c.postJSON(ctx, "/containers/"+containerID+"/exec", nil, cfg, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("docker: exec 创建成功但未返回 ID")
	}
	return out.ID, nil
}

// ExecStream 一个已附着的 exec 会话:可双向读写的裸连接。
type ExecStream struct {
	conn net.Conn
	br   *bufio.Reader
	tty  bool
}

// Write 向会话的 stdin 写入(键盘输入)。
func (s *ExecStream) Write(p []byte) (int, error) { return s.conn.Write(p) }

// Read 从会话读取原始字节。非 TTY 会话读到的是多路复用帧,通常应改用 Frames。
func (s *ExecStream) Read(p []byte) (int, error) { return s.br.Read(p) }

// Frames 按会话的 TTY 设置返回合适的输出解析器。
func (s *ExecStream) Frames() LogFrames { return NewLogFrames(s.br, s.tty) }

// TTY 报告该会话是否为 TTY 模式。
func (s *ExecStream) TTY() bool { return s.tty }

// CloseWrite 关闭写方向(发送 EOF),保留读方向以收完剩余输出。
func (s *ExecStream) CloseWrite() error {
	if cw, ok := s.conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// Close 关闭整个会话。
func (s *ExecStream) Close() error { return s.conn.Close() }

// SetDeadline 设置读写超时,用于实现「30 分钟无输入自动断开」(docs §5.4)。
func (s *ExecStream) SetDeadline(t time.Time) error { return s.conn.SetDeadline(t) }

// ExecAttach 启动 exec 会话并附着,返回可双向通信的流。
//
// tty 必须与 ExecCreate 时传入的 ExecConfig.Tty 一致,否则输出解析方式会错配。
func (c *Client) ExecAttach(ctx context.Context, execID string, tty bool) (*ExecStream, error) {
	body := struct {
		Detach bool `json:"Detach"`
		Tty    bool `json:"Tty"`
	}{Detach: false, Tty: tty}

	conn, br, err := c.hijack(ctx, "/exec/"+execID+"/start", nil, body)
	if err != nil {
		return nil, err
	}
	return &ExecStream{conn: conn, br: br, tty: tty}, nil
}

// ExecStartDetached 启动 exec 会话但不附着,用于只关心退出码的探测类调用。
func (c *Client) ExecStartDetached(ctx context.Context, execID string) error {
	body := struct {
		Detach bool `json:"Detach"`
		Tty    bool `json:"Tty"`
	}{Detach: true, Tty: false}
	return c.postJSON(ctx, "/exec/"+execID+"/start", nil, body, nil)
}

// ExecInspect 查询 exec 会话状态(含退出码)。
func (c *Client) ExecInspect(ctx context.Context, execID string) (*ExecInspect, error) {
	var out ExecInspect
	if err := c.getJSON(ctx, "/exec/"+execID+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ExecResize 调整 exec 会话的终端尺寸(前端 xterm.js 窗口变化时调用)。
func (c *Client) ExecResize(ctx context.Context, execID string, height, width uint) error {
	q := url.Values{}
	q.Set("h", strconv.FormatUint(uint64(height), 10))
	q.Set("w", strconv.FormatUint(uint64(width), 10))
	return c.postJSON(ctx, "/exec/"+execID+"/resize", q, nil, nil)
}

// ExecWait 轮询等待 exec 结束,返回其退出码。
//
// Engine API 没有「等待 exec 结束」的阻塞端点,只能轮询 inspect。
func (c *Client) ExecWait(ctx context.Context, execID string, poll time.Duration) (int, error) {
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		insp, err := c.ExecInspect(ctx, execID)
		if err != nil {
			return -1, err
		}
		if !insp.Running {
			return insp.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-t.C:
		}
	}
}

// DetectShell 依次尝试候选 shell,返回容器内第一个可用的那个。
//
// candidates 为空时使用 DefaultShells。全部不可用时返回 ErrNoShell ——
// UI 应据此明确提示「该镜像不含 shell」,而不是打开一个空白终端(docs §5.4)。
func (c *Client) DetectShell(ctx context.Context, containerID string, candidates []string) (string, error) {
	if len(candidates) == 0 {
		candidates = DefaultShells
	}
	var lastErr error
	for _, sh := range candidates {
		ok, err := c.shellUsable(ctx, containerID, sh)
		if err != nil {
			lastErr = err
			continue
		}
		if ok {
			return sh, nil
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("%w(最后一次探测失败: %v)", ErrNoShell, lastErr)
	}
	return "", ErrNoShell
}

// shellUsable 通过运行 `<shell> -c exit 0` 判断该 shell 是否存在且可执行。
func (c *Client) shellUsable(ctx context.Context, containerID, shell string) (bool, error) {
	execID, err := c.ExecCreate(ctx, containerID, ExecConfig{
		Cmd: []string{shell, "-c", "exit 0"},
	})
	if err != nil {
		return false, err
	}
	if err := c.ExecStartDetached(ctx, execID); err != nil {
		// shell 不存在时,daemon 在 start 阶段报 "executable file not found"
		return false, nil
	}
	code, err := c.ExecWait(ctx, execID, 30*time.Millisecond)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// ExecOnce 在容器内执行一条命令并收集其输出,用于探测类的短命令。
//
// 不适合长时间运行或交互式命令——那类场景应走 ExecAttach。
func (c *Client) ExecOnce(ctx context.Context, containerID string, cmd []string) (stdout, stderr []byte, exitCode int, err error) {
	execID, err := c.ExecCreate(ctx, containerID, ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false, // 需要区分 stdout/stderr,故用多路复用
	})
	if err != nil {
		return nil, nil, -1, err
	}

	stream, err := c.ExecAttach(ctx, execID, false)
	if err != nil {
		return nil, nil, -1, err
	}
	defer stream.Close()

	var outBuf, errBuf writeBuffer
	if _, err := StdCopy(&outBuf, &errBuf, stream.br); err != nil && err != io.EOF {
		return outBuf.b, errBuf.b, -1, err
	}

	code, err := c.ExecWait(ctx, execID, 30*time.Millisecond)
	if err != nil {
		return outBuf.b, errBuf.b, -1, err
	}
	return outBuf.b, errBuf.b, code, nil
}

// writeBuffer 一个极简的 io.Writer,避免为两个缓冲各引入 bytes.Buffer 的开销。
type writeBuffer struct{ b []byte }

func (w *writeBuffer) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}
