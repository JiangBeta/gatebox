package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// hijackServer 模拟 daemon 的 attach 端点:接管连接、回 101,然后按 fn 收发数据。
// 用它可以在不接触真实容器的前提下验证 hijack 的 HTTP 层逻辑。
//
// 关键:**必须在 Hijack() 之前读完 r.Body**。否则未消费的请求体会残留在连接
// 缓冲中,被后续当作会话数据读出(真实 daemon 会读完 body 再 upgrade)。
// 请求体以 []byte 形式传给 fn,因为 Hijack 后 r.Body 不再可读。
func hijackServer(t *testing.T, fn func(t *testing.T, req *http.Request, body []byte, rw *bufio.ReadWriter)) *Client {
	t.Helper()
	return newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取请求体: %v", err)
			return
		}

		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("测试服务器不支持 Hijack")
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer conn.Close()

		rw.WriteString("HTTP/1.1 101 UPGRADED\r\n")
		rw.WriteString("Content-Type: application/vnd.docker.raw-stream\r\n")
		rw.WriteString("Connection: Upgrade\r\n")
		rw.WriteString("Upgrade: tcp\r\n\r\n")
		if err := rw.Flush(); err != nil {
			return
		}
		fn(t, r, body, rw)
	})
}

func TestExecCreate(t *testing.T) {
	var gotBody ExecConfig
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"Id":"exec123"}`))
	})

	id, err := c.ExecCreate(context.Background(), "container1", ExecConfig{
		Cmd:          []string{"/bin/sh"},
		User:         "root",
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
	})
	if err != nil {
		t.Fatalf("ExecCreate: %v", err)
	}
	if id != "exec123" {
		t.Errorf("execID = %q", id)
	}
	if len(gotBody.Cmd) != 1 || gotBody.Cmd[0] != "/bin/sh" {
		t.Errorf("Cmd = %v", gotBody.Cmd)
	}
	if gotBody.User != "root" || !gotBody.Tty || !gotBody.AttachStdin {
		t.Errorf("配置未正确传递: %+v", gotBody)
	}
}

func TestExecCreate_MissingID(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})
	if _, err := c.ExecCreate(context.Background(), "c1", ExecConfig{}); err == nil {
		t.Error("daemon 未返回 ID 时应报错,否则后续 attach 会用空 ID 请求")
	}
}

// TestExecAttach_UpgradeHandshake 验证 hijack 发出的是合法的 Upgrade 请求。
func TestExecAttach_UpgradeHandshake(t *testing.T) {
	var (
		gotConnection, gotUpgrade, gotCT string
		gotBody                          map[string]bool
	)
	c := hijackServer(t, func(t *testing.T, r *http.Request, body []byte, rw *bufio.ReadWriter) {
		gotConnection = r.Header.Get("Connection")
		gotUpgrade = r.Header.Get("Upgrade")
		gotCT = r.Header.Get("Content-Type")
		json.Unmarshal(body, &gotBody)
		rw.Write(frame(StreamStdout, "ok"))
		rw.Flush()
	})

	stream, err := c.ExecAttach(context.Background(), "exec1", false)
	if err != nil {
		t.Fatalf("ExecAttach: %v", err)
	}
	defer stream.Close()

	// 读一帧,确保握手后连接可用
	st, payload, err := stream.Frames().Next()
	if err != nil {
		t.Fatalf("读取输出: %v", err)
	}
	if st != StreamStdout || string(payload) != "ok" {
		t.Errorf("输出 = (%v, %q)", st, payload)
	}

	if gotConnection != "Upgrade" {
		t.Errorf("Connection 头 = %q, want Upgrade", gotConnection)
	}
	if gotUpgrade != "tcp" {
		t.Errorf("Upgrade 头 = %q, want tcp", gotUpgrade)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotBody["Detach"] {
		t.Error("attach 时 Detach 应为 false")
	}
}

// TestExecAttach_Bidirectional 验证双向流:写进去的能被服务端读到,回写的能读出来。
func TestExecAttach_Bidirectional(t *testing.T) {
	echoed := make(chan string, 1)
	c := hijackServer(t, func(t *testing.T, r *http.Request, _ []byte, rw *bufio.ReadWriter) {
		line, err := rw.ReadString('\n')
		if err != nil {
			t.Errorf("读取客户端输入: %v", err)
			return
		}
		echoed <- line
		rw.Write(frame(StreamStdout, "echo: "+strings.TrimSpace(line)))
		rw.Flush()
	})

	stream, err := c.ExecAttach(context.Background(), "exec1", false)
	if err != nil {
		t.Fatalf("ExecAttach: %v", err)
	}
	defer stream.Close()

	if _, err := stream.Write([]byte("whoami\n")); err != nil {
		t.Fatalf("写入 stdin: %v", err)
	}

	select {
	case got := <-echoed:
		if strings.TrimSpace(got) != "whoami" {
			t.Errorf("服务端收到 %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("服务端未收到客户端输入 —— 写方向不通")
	}

	_, payload, err := stream.Frames().Next()
	if err != nil {
		t.Fatalf("读取输出: %v", err)
	}
	if string(payload) != "echo: whoami" {
		t.Errorf("输出 = %q", payload)
	}
}

// TestExecAttach_TTYRawStream TTY 会话没有帧头,输出应原样读出。
func TestExecAttach_TTYRawStream(t *testing.T) {
	c := hijackServer(t, func(t *testing.T, r *http.Request, _ []byte, rw *bufio.ReadWriter) {
		rw.WriteString("/ # \x1b[6n") // 裸终端输出,含 ANSI 转义
		rw.Flush()
	})

	stream, err := c.ExecAttach(context.Background(), "exec1", true)
	if err != nil {
		t.Fatalf("ExecAttach: %v", err)
	}
	defer stream.Close()

	if !stream.TTY() {
		t.Error("TTY() 应为 true")
	}
	_, payload, err := stream.Frames().Next()
	if err != nil {
		t.Fatalf("读取 TTY 输出: %v", err)
	}
	if !strings.HasPrefix(string(payload), "/ # ") {
		t.Errorf("TTY 输出被错误解析: %q", payload)
	}
}

func TestExecAttach_ErrorResponse(t *testing.T) {
	// daemon 拒绝时返回普通 HTTP 错误而非 101
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"No such exec instance: bogus"}`))
	})
	_, err := c.ExecAttach(context.Background(), "bogus", false)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestExecResize(t *testing.T) {
	var q url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	})
	if err := c.ExecResize(context.Background(), "exec1", 40, 120); err != nil {
		t.Fatalf("ExecResize: %v", err)
	}
	if q.Get("h") != "40" || q.Get("w") != "120" {
		t.Errorf("尺寸参数 = h:%q w:%q, want h:40 w:120", q.Get("h"), q.Get("w"))
	}
}

func TestExecInspect(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ID":"exec1","Running":false,"ExitCode":127,"ContainerID":"c1"}`))
	})
	insp, err := c.ExecInspect(context.Background(), "exec1")
	if err != nil {
		t.Fatalf("ExecInspect: %v", err)
	}
	if insp.Running || insp.ExitCode != 127 {
		t.Errorf("inspect = %+v", insp)
	}
}

func TestExecWait(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Write([]byte(`{"Running":true}`))
			return
		}
		w.Write([]byte(`{"Running":false,"ExitCode":42}`))
	})

	code, err := c.ExecWait(context.Background(), "exec1", time.Millisecond)
	if err != nil {
		t.Fatalf("ExecWait: %v", err)
	}
	if code != 42 {
		t.Errorf("退出码 = %d, want 42", code)
	}
	if calls < 3 {
		t.Errorf("轮询次数 = %d, 应持续轮询直到 Running=false", calls)
	}
}

func TestExecWait_ContextCancel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Running":true}`)) // 永远不结束
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := c.ExecWait(ctx, "exec1", time.Millisecond); err == nil {
		t.Error("ctx 超时后应返回错误,否则会无限轮询")
	}
}

// TestDetectShell 验证探测链:bash 不存在时回退到 sh。
func TestDetectShell(t *testing.T) {
	// 记录每次 exec create 请求的命令
	var attempted []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/exec"):
			var cfg ExecConfig
			json.NewDecoder(r.Body).Decode(&cfg)
			if len(cfg.Cmd) > 0 {
				attempted = append(attempted, cfg.Cmd[0])
			}
			// bash 不存在 → 创建成功但 start 失败;sh 存在
			if strings.Contains(cfg.Cmd[0], "bash") {
				w.Write([]byte(`{"Id":"exec-bash"}`))
			} else {
				w.Write([]byte(`{"Id":"exec-sh"}`))
			}
		case strings.HasSuffix(r.URL.Path, "/start"):
			if strings.Contains(r.URL.Path, "exec-bash") {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message":"OCI runtime exec failed: exec: \"/bin/bash\": stat /bin/bash: no such file or directory"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/json"):
			w.Write([]byte(`{"Running":false,"ExitCode":0}`))
		}
	})

	shell, err := c.DetectShell(context.Background(), "c1", nil)
	if err != nil {
		t.Fatalf("DetectShell: %v", err)
	}
	if shell != "/bin/sh" {
		t.Errorf("探测结果 = %q, want /bin/sh", shell)
	}
	if len(attempted) < 2 || attempted[0] != "/bin/bash" {
		t.Errorf("探测顺序 = %v, 应先试 bash", attempted)
	}
}

// TestDetectShell_NoShell distroless / scratch 镜像连 sh 都没有,
// 必须返回 ErrNoShell 以便 UI 明确提示,而不是打开空白终端。
func TestDetectShell_NoShell(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/exec"):
			w.Write([]byte(`{"Id":"e1"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"executable file not found in $PATH"}`))
		}
	})

	_, err := c.DetectShell(context.Background(), "c1", nil)
	if !errors.Is(err, ErrNoShell) {
		t.Errorf("err = %v, want ErrNoShell", err)
	}
}

func TestExecOnce(t *testing.T) {
	c := hijackExecServer(t, func(rw *bufio.ReadWriter) {
		rw.Write(frame(StreamStdout, "hello from stdout"))
		rw.Write(frame(StreamStderr, "warning on stderr"))
		rw.Flush()
	})

	stdout, stderr, code, err := c.ExecOnce(context.Background(), "c1", []string{"echo", "hi"})
	if err != nil {
		t.Fatalf("ExecOnce: %v", err)
	}
	if string(stdout) != "hello from stdout" {
		t.Errorf("stdout = %q", stdout)
	}
	if string(stderr) != "warning on stderr" {
		t.Errorf("stderr = %q", stderr)
	}
	if code != 0 {
		t.Errorf("退出码 = %d", code)
	}
}

// hijackExecServer 模拟完整的 exec 流程:create → attach(hijack) → inspect。
func hijackExecServer(t *testing.T, write func(*bufio.ReadWriter)) *Client {
	t.Helper()
	return newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/exec"):
			w.Write([]byte(`{"Id":"e1"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("不支持 Hijack")
				return
			}
			conn, rw, err := hj.Hijack()
			if err != nil {
				t.Errorf("Hijack: %v", err)
				return
			}
			defer conn.Close()
			rw.WriteString("HTTP/1.1 101 UPGRADED\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			rw.Flush()
			write(rw)
		case strings.HasSuffix(r.URL.Path, "/json"):
			w.Write([]byte(`{"Running":false,"ExitCode":0}`))
		}
	})
}

func TestExecStream_CloseWrite(t *testing.T) {
	done := make(chan struct{})
	c := hijackServer(t, func(t *testing.T, r *http.Request, _ []byte, rw *bufio.ReadWriter) {
		// 客户端 CloseWrite 后这里应读到 EOF
		_, err := io.ReadAll(rw)
		if err != nil && err != io.EOF {
			t.Errorf("读取至 EOF 出错: %v", err)
		}
		close(done)
	})

	stream, err := c.ExecAttach(context.Background(), "e1", true)
	if err != nil {
		t.Fatalf("ExecAttach: %v", err)
	}
	defer stream.Close()

	stream.Write([]byte("input"))
	if err := stream.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("CloseWrite 未让服务端读到 EOF")
	}
}
