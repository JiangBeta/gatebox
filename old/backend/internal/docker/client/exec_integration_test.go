package client

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// 本文件对接真实 daemon 验证 exec / hijack 路径。
//
// 与 integration_test.go 的纯只读测试不同,这里会**在容器内执行命令**,
// 但仅限无副作用的只读命令(echo / id / pwd / exit),不写文件、不改配置、
// 不影响容器进程。用户已确认这些容器处于测试环境。
//
// 设 GATEBOX_SKIP_DOCKER_IT 可整体跳过。

// findShellContainer 返回第一个探测到可用 shell 的容器及其 shell 路径。
func findShellContainer(t *testing.T, c *Client, ctx context.Context) (Container, string) {
	t.Helper()
	list, err := c.ListContainers(ctx, ListContainersOptions{})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(list) == 0 {
		t.Skip("跳过:本机无运行中的容器")
	}
	for _, ct := range list {
		shell, err := c.DetectShell(ctx, ct.ID, nil)
		if err == nil {
			return ct, shell
		}
	}
	t.Skip("跳过:所有容器均无可用 shell")
	return Container{}, ""
}

// TestIntegration_DetectShell 在真实容器上验证 shell 探测链。
//
// 预期能同时覆盖两条路径:基于 alpine 的镜像有 /bin/sh;
// 而 Go 静态二进制的 scratch 镜像连 sh 都没有,应返回 ErrNoShell。
func TestIntegration_DetectShell(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	list, err := c.ListContainers(ctx, ListContainersOptions{})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(list) == 0 {
		t.Skip("跳过:本机无运行中的容器")
	}

	var withShell, without int
	for _, ct := range list {
		shell, err := c.DetectShell(ctx, ct.ID, nil)
		switch {
		case err == nil:
			withShell++
			t.Logf("%-16s → %s", ct.Name(), shell)
		case errors.Is(err, ErrNoShell):
			without++
			t.Logf("%-16s → 无可用 shell(UI 应提示「该镜像不含 shell」)", ct.Name())
		default:
			t.Errorf("%s 探测出错(既非成功也非 ErrNoShell): %v", ct.Name(), err)
		}
	}
	t.Logf("共 %d 个容器:%d 个有 shell,%d 个没有", len(list), withShell, without)

	if withShell+without != len(list) {
		t.Error("存在既未探测成功也未归类为 ErrNoShell 的容器")
	}
}

// TestIntegration_ExecOnce 在真实容器内执行只读命令并校验输出。
func TestIntegration_ExecOnce(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	target, shell := findShellContainer(t, c, ctx)

	const marker = "GATEBOX_EXEC_OK"
	stdout, stderr, code, err := c.ExecOnce(ctx, target.ID, []string{shell, "-c", "echo " + marker})
	if err != nil {
		t.Fatalf("ExecOnce: %v", err)
	}
	if code != 0 {
		t.Errorf("退出码 = %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(string(stdout), marker) {
		t.Errorf("stdout = %q, 应包含 %q", stdout, marker)
	}
	t.Logf("%s: stdout=%q 退出码=%d", target.Name(), strings.TrimSpace(string(stdout)), code)
}

// TestIntegration_ExecOnce_SeparatesStreams 验证 stdcopy 在真实数据上
// 正确分离 stdout 与 stderr —— 这是多路复用解析的端到端确认。
func TestIntegration_ExecOnce_SeparatesStreams(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	target, shell := findShellContainer(t, c, ctx)

	stdout, stderr, code, err := c.ExecOnce(ctx, target.ID,
		[]string{shell, "-c", "echo TO_STDOUT; echo TO_STDERR 1>&2"})
	if err != nil {
		t.Fatalf("ExecOnce: %v", err)
	}
	if code != 0 {
		t.Errorf("退出码 = %d", code)
	}
	if !strings.Contains(string(stdout), "TO_STDOUT") {
		t.Errorf("stdout = %q, 应含 TO_STDOUT", stdout)
	}
	if strings.Contains(string(stdout), "TO_STDERR") {
		t.Error("stderr 内容混入了 stdout —— 多路复用分离失败")
	}
	if !strings.Contains(string(stderr), "TO_STDERR") {
		t.Errorf("stderr = %q, 应含 TO_STDERR", stderr)
	}
	t.Logf("%s: 流分离正确 stdout=%q stderr=%q",
		target.Name(), strings.TrimSpace(string(stdout)), strings.TrimSpace(string(stderr)))
}

// TestIntegration_ExecOnce_NonZeroExit 验证非零退出码被如实带回。
func TestIntegration_ExecOnce_NonZeroExit(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	target, shell := findShellContainer(t, c, ctx)

	_, _, code, err := c.ExecOnce(ctx, target.ID, []string{shell, "-c", "exit 42"})
	if err != nil {
		t.Fatalf("ExecOnce: %v", err)
	}
	if code != 42 {
		t.Errorf("退出码 = %d, want 42", code)
	}
}

// TestIntegration_ExecAttachTTY 走完整的交互式控制台路径:
// hijack 升级 → 写 stdin → 读 TTY 原始输出。这正是前端 xterm.js 的通道。
func TestIntegration_ExecAttachTTY(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	target, shell := findShellContainer(t, c, ctx)

	execID, err := c.ExecCreate(ctx, target.ID, ExecConfig{
		Cmd:          []string{shell},
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		t.Fatalf("ExecCreate: %v", err)
	}

	stream, err := c.ExecAttach(ctx, execID, true)
	if err != nil {
		t.Fatalf("ExecAttach(hijack): %v", err)
	}
	defer stream.Close()

	// 防止读取永久阻塞
	if err := stream.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	const marker = "GATEBOX_TTY_MARKER"
	if _, err := stream.Write([]byte("echo " + marker + "\nexit\n")); err != nil {
		t.Fatalf("写入 stdin: %v", err)
	}

	var sb strings.Builder
	frames := stream.Frames()
	for sb.Len() < 8192 {
		_, payload, err := frames.Next()
		if len(payload) > 0 {
			sb.Write(payload)
			if strings.Contains(sb.String(), marker) && strings.Count(sb.String(), marker) >= 1 {
				// TTY 会回显输入,故 marker 可能出现两次;拿到即可
			}
		}
		if err != nil {
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			// 读超时也视为结束(shell 可能未随 exit 立即关闭连接)
			if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
				break
			}
			t.Fatalf("读取 TTY 输出: %v", err)
		}
	}

	out := sb.String()
	if !strings.Contains(out, marker) {
		t.Errorf("TTY 输出中未找到标记 %q,实际读到 %d 字节: %q", marker, len(out), truncate(out, 300))
	} else {
		t.Logf("%s(%s): TTY 交互成功,读到 %d 字节", target.Name(), shell, len(out))
	}

	// 会话结束后退出码应可查
	insp, err := c.ExecInspect(ctx, execID)
	if err != nil {
		t.Fatalf("ExecInspect: %v", err)
	}
	t.Logf("exec 状态: running=%v exitCode=%d", insp.Running, insp.ExitCode)
}

// TestIntegration_ExecResize 验证终端尺寸调整(前端窗口变化时调用)。
func TestIntegration_ExecResize(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	target, shell := findShellContainer(t, c, ctx)

	execID, err := c.ExecCreate(ctx, target.ID, ExecConfig{
		Cmd:          []string{shell},
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
	})
	if err != nil {
		t.Fatalf("ExecCreate: %v", err)
	}
	stream, err := c.ExecAttach(ctx, execID, true)
	if err != nil {
		t.Fatalf("ExecAttach: %v", err)
	}
	defer stream.Close()

	// resize 必须在会话启动后调用,否则 daemon 报 "process not found"
	if err := c.ExecResize(ctx, execID, 40, 120); err != nil {
		t.Errorf("ExecResize: %v", err)
	} else {
		t.Logf("%s: 终端尺寸调整为 40x120 成功", target.Name())
	}
	stream.Write([]byte("exit\n"))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
