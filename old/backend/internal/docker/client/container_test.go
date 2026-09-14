package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"
)

func TestListContainers_QueryParams(t *testing.T) {
	var gotQuery url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`[]`))
	})

	_, err := c.ListContainers(context.Background(), ListContainersOptions{
		All:   true,
		Limit: 5,
		Filters: Filters{
			"label":  {"com.docker.compose.project=hosts"},
			"status": {"running"},
		},
	})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}

	if got := gotQuery.Get("all"); got != "1" {
		t.Errorf("all = %q, want 1", got)
	}
	if got := gotQuery.Get("limit"); got != "5" {
		t.Errorf("limit = %q, want 5", got)
	}

	// filters 必须是 JSON 编码的 map[string][]string
	var f map[string][]string
	if err := json.Unmarshal([]byte(gotQuery.Get("filters")), &f); err != nil {
		t.Fatalf("filters 不是合法 JSON: %v (原始值 %q)", err, gotQuery.Get("filters"))
	}
	if len(f["label"]) != 1 || f["label"][0] != "com.docker.compose.project=hosts" {
		t.Errorf("filters.label = %v", f["label"])
	}
	if len(f["status"]) != 1 || f["status"][0] != "running" {
		t.Errorf("filters.status = %v", f["status"])
	}
}

func TestListContainers_OmitsEmptyParams(t *testing.T) {
	var raw string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.Write([]byte(`[]`))
	})
	if _, err := c.ListContainers(context.Background(), ListContainersOptions{}); err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		t.Errorf("零值选项不应产生查询参数, got %q", raw)
	}
}

func TestListContainers_Decode(t *testing.T) {
	const body = `[{
		"Id": "6269e8d5e360",
		"Names": ["/traefik"],
		"Image": "traefik:v3.7.10",
		"Created": 1755000000,
		"State": "running",
		"Status": "Up 6 days (healthy)",
		"Ports": [{"IP":"0.0.0.0","PrivatePort":80,"PublicPort":8080,"Type":"tcp"}],
		"Labels": {"com.docker.compose.project":"hosts","com.docker.compose.service":"traefik"}
	}]`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})

	list, err := c.ListContainers(context.Background(), ListContainersOptions{All: true})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("容器数 = %d, want 1", len(list))
	}
	got := list[0]
	if got.Name() != "traefik" {
		t.Errorf("Name() = %q", got.Name())
	}
	if got.ComposeProject() != "hosts" || got.ComposeService() != "traefik" {
		t.Errorf("compose 标识 = (%q, %q)", got.ComposeProject(), got.ComposeService())
	}
	if len(got.Ports) != 1 || got.Ports[0].PublicPort != 8080 || got.Ports[0].Type != "tcp" {
		t.Errorf("Ports = %+v", got.Ports)
	}
	if got.CreatedAt().Unix() != 1755000000 {
		t.Errorf("CreatedAt() = %v", got.CreatedAt())
	}
}

func TestRemoveContainer_Options(t *testing.T) {
	cases := []struct {
		name string
		opts RemoveContainerOptions
		want map[string]string
	}{
		{"默认不带任何参数", RemoveContainerOptions{}, map[string]string{}},
		{"删除匿名卷", RemoveContainerOptions{RemoveVolumes: true}, map[string]string{"v": "1"}},
		{"强制删除", RemoveContainerOptions{Force: true}, map[string]string{"force": "1"}},
		{"全开", RemoveContainerOptions{RemoveVolumes: true, Force: true, RemoveLinks: true},
			map[string]string{"v": "1", "force": "1", "link": "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var q url.Values
			var method string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				q = r.URL.Query()
				method = r.Method
				w.WriteHeader(http.StatusNoContent)
			})
			if err := c.RemoveContainer(context.Background(), "abc", tc.opts); err != nil {
				t.Fatalf("RemoveContainer: %v", err)
			}
			if method != http.MethodDelete {
				t.Errorf("method = %s, want DELETE", method)
			}
			for k, want := range tc.want {
				if got := q.Get(k); got != want {
					t.Errorf("参数 %s = %q, want %q", k, got, want)
				}
			}
			if len(tc.want) == 0 && len(q) != 0 {
				t.Errorf("不应有查询参数, got %v", q)
			}
		})
	}
}

func TestStopContainer_Timeout(t *testing.T) {
	var q string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query().Get("t")
		w.WriteHeader(http.StatusNoContent)
	})

	timeout := 30
	if err := c.StopContainer(context.Background(), "abc", &timeout); err != nil {
		t.Fatal(err)
	}
	if q != "30" {
		t.Errorf("t = %q, want 30", q)
	}

	if err := c.StopContainer(context.Background(), "abc", nil); err != nil {
		t.Fatal(err)
	}
	if q != "" {
		t.Errorf("timeout 为 nil 时不应传 t 参数, got %q", q)
	}
}

// TestStartContainer_AlreadyRunning 覆盖 304:启动已运行的容器不是失败,
// 调用方可用 errors.Is(err, ErrNotModified) 判定为「已是目标状态」。
func TestStartContainer_AlreadyRunning(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})
	err := c.StartContainer(context.Background(), "abc")
	if err == nil {
		t.Fatal("304 应返回错误以便调用方区分")
	}
	if !errors.Is(err, ErrNotModified) {
		t.Errorf("err = %v, want ErrNotModified", err)
	}
}

func TestContainerLogs_QueryParams(t *testing.T) {
	var q url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.Write(frame(StreamStdout, "log line"))
	})

	rc, err := c.ContainerLogs(context.Background(), "abc", LogsOptions{
		Stdout: true, Stderr: true, Follow: true, Timestamps: true, Tail: "100",
	})
	if err != nil {
		t.Fatalf("ContainerLogs: %v", err)
	}
	defer rc.Close()

	for k, want := range map[string]string{
		"stdout": "1", "stderr": "1", "follow": "1", "timestamps": "1", "tail": "100",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("参数 %s = %q, want %q", k, got, want)
		}
	}

	st, payload, err := NewFrameReader(rc).Next()
	if err != nil {
		t.Fatalf("解析日志帧: %v", err)
	}
	if st != StreamStdout || string(payload) != "log line" {
		t.Errorf("帧 = (%v, %q)", st, payload)
	}
}

// TestNewLogFrames 覆盖 TTY 与非 TTY 两条路径 ——
// 传错 tty 会导致乱码或帧长度异常,是日志功能最容易出错的地方。
func TestNewLogFrames(t *testing.T) {
	t.Run("非 TTY 走多路复用解析", func(t *testing.T) {
		var buf bytes.Buffer
		buf.Write(frame(StreamStdout, "out"))
		buf.Write(frame(StreamStderr, "err"))

		lf := NewLogFrames(&buf, false)
		st, p, err := lf.Next()
		if err != nil || st != StreamStdout || string(p) != "out" {
			t.Fatalf("第一帧 = (%v, %q, %v)", st, p, err)
		}
		st, p, err = lf.Next()
		if err != nil || st != StreamStderr || string(p) != "err" {
			t.Fatalf("第二帧 = (%v, %q, %v)", st, p, err)
		}
	})

	t.Run("TTY 按原始流处理", func(t *testing.T) {
		// TTY 流没有帧头。若误用 FrameReader,"raw termi..." 的前 8 字节
		// 会被当成帧头,解析出的长度必然错乱。
		raw := "raw terminal output without frame headers"
		lf := NewLogFrames(strings.NewReader(raw), true)

		var sb strings.Builder
		for {
			st, p, err := lf.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("Next: %v", err)
			}
			if st != StreamStdout {
				t.Errorf("TTY 流应统一归为 stdout, got %v", st)
			}
			sb.Write(p)
		}
		if sb.String() != raw {
			t.Errorf("TTY 输出 = %q, want %q", sb.String(), raw)
		}
	})

	t.Run("TTY 流被切碎时不丢数据", func(t *testing.T) {
		raw := "chunked tty output"
		lf := NewLogFrames(iotest.OneByteReader(strings.NewReader(raw)), true)
		var sb strings.Builder
		for {
			_, p, err := lf.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(p)
		}
		if sb.String() != raw {
			t.Errorf("= %q, want %q", sb.String(), raw)
		}
	})
}

func TestDecodeStatsStream(t *testing.T) {
	// daemon 以连续 JSON 对象的形式推送,对象之间无分隔符要求
	stream := strings.NewReader(realStatsJSON + realStatsJSON + realStatsJSON)

	var n int
	err := DecodeStatsStream(context.Background(), stream, func(s *Stats) error {
		n++
		if s.MemoryStats.Usage != 32092160 {
			t.Errorf("第 %d 条 usage = %d", n, s.MemoryStats.Usage)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("DecodeStatsStream: %v", err)
	}
	if n != 3 {
		t.Errorf("解码条数 = %d, want 3", n)
	}
}

func TestDecodeStatsStream_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := DecodeStatsStream(ctx, strings.NewReader(realStatsJSON), func(*Stats) error { return nil })
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDecodeStatsStream_CallbackError(t *testing.T) {
	// 回调返回错误应中止解码(用于订阅者断开时停止采集)
	stop := io.ErrClosedPipe
	err := DecodeStatsStream(context.Background(), strings.NewReader(realStatsJSON+realStatsJSON),
		func(*Stats) error { return stop })
	if err != stop {
		t.Errorf("err = %v, want %v", err, stop)
	}
}

func TestContainerTop(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Titles":["PID","CMD"],"Processes":[["1","/traefik"],["12","sh"]]}`))
	})
	titles, procs, err := c.ContainerTop(context.Background(), "abc", "")
	if err != nil {
		t.Fatalf("ContainerTop: %v", err)
	}
	if len(titles) != 2 || titles[0] != "PID" {
		t.Errorf("titles = %v", titles)
	}
	if len(procs) != 2 || procs[0][1] != "/traefik" {
		t.Errorf("processes = %v", procs)
	}
}
