package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/JiangBeta/gatebox/internal/docker/client"
)

// exec 会话的空闲上限(docs §5.4:30 分钟无输入自动断开)。
const execIdleTimeout = 30 * time.Minute

// acceptWS 完成 WebSocket 升级。
//
// InsecureSkipVerify 关闭了 Origin 校验:开发期前端在 5173 端口、后端在 8080,
// 属跨源;生产形态是 go:embed 同源。**引入用户认证后必须收紧这里**,
// 否则任意站点都能借用户浏览器连上 exec 通道(等价于宿主机 root)。
func acceptWS(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
}

// logMessage 推送给前端的一条日志。
type logMessage struct {
	Stream string `json:"stream"` // stdout / stderr
	Data   string `json:"data"`
	Error  string `json:"error,omitempty"`
}

// wsLogs 推送容器日志。
//
// 查询参数:follow(默认 true)、tail(默认 200)、timestamps。
func (d *dockerAPI) wsLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	conn, err := acceptWS(w, r)
	if err != nil {
		return // Accept 失败时已写过响应
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 日志流的解析方式取决于容器是否为 TTY —— 传错会输出乱码
	det, err := d.cli.InspectContainer(ctx, id)
	if err != nil {
		sendLogErr(ctx, conn, "读取容器信息失败: "+errMessage(err))
		return
	}

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "200"
	}
	follow := r.URL.Query().Get("follow") != "false"

	rc, err := d.cli.ContainerLogs(ctx, id, client.LogsOptions{
		Stdout:     true,
		Stderr:     true,
		Follow:     follow,
		Timestamps: r.URL.Query().Get("timestamps") == "true",
		Tail:       tail,
	})
	if err != nil {
		// 最典型的原因是日志驱动不支持回读,需要说人话(docs §5.5)
		msg := errMessage(err)
		if drv := det.LogDriver(); drv != "" && !client.LogDriverReadable(drv) {
			msg = "当前日志驱动 " + drv + " 不支持在线查看日志"
		}
		sendLogErr(ctx, conn, msg)
		return
	}
	defer rc.Close()

	// 客户端主动关闭时结束读取
	go func() {
		conn.Read(ctx) // 前端不发消息,读到错误即代表连接已断
		cancel()
	}()

	frames := client.NewLogFrames(rc, det.Config.Tty)
	for {
		if ctx.Err() != nil {
			return
		}
		st, payload, err := frames.Next()
		if len(payload) > 0 {
			msg := logMessage{Stream: st.String(), Data: string(payload)}
			if writeJSONMsg(ctx, conn, msg) != nil {
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				sendLogErr(ctx, conn, errMessage(err))
			}
			return
		}
	}
}

// execClientMessage 前端发来的控制台消息。
type execClientMessage struct {
	Type string `json:"type"`           // stdin / resize
	Data string `json:"data,omitempty"` // stdin 内容
	Cols uint   `json:"cols,omitempty"`
	Rows uint   `json:"rows,omitempty"`
}

// wsExec 提供交互式控制台。
//
// 前端发 JSON 控制消息,服务端回推终端原始输出(文本帧)。
func (d *dockerAPI) wsExec(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	conn, err := acceptWS(w, r)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	shell := r.URL.Query().Get("shell")
	if shell == "" {
		detected, derr := d.cli.DetectShell(ctx, id, nil)
		if errors.Is(derr, client.ErrNoShell) {
			writeText(ctx, conn, "该镜像不含可用的 shell,无法打开控制台。\r\n")
			return
		}
		if derr != nil {
			writeText(ctx, conn, "探测 shell 失败: "+errMessage(derr)+"\r\n")
			return
		}
		shell = detected
	}

	cfg := client.ExecConfig{
		Cmd:          []string{shell},
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		User:         r.URL.Query().Get("user"),
	}
	execID, err := d.cli.ExecCreate(ctx, id, cfg)
	if err != nil {
		writeText(ctx, conn, "创建会话失败: "+errMessage(err)+"\r\n")
		return
	}

	stream, err := d.cli.ExecAttach(ctx, execID, true)
	if err != nil {
		writeText(ctx, conn, "附着会话失败: "+errMessage(err)+"\r\n")
		return
	}
	defer stream.Close()

	if c, rows := parseSize(r.URL.Query().Get("cols")), parseSize(r.URL.Query().Get("rows")); c > 0 && rows > 0 {
		_ = d.cli.ExecResize(ctx, execID, rows, c)
	}

	// 容器 → 浏览器
	go func() {
		defer cancel()
		frames := stream.Frames()
		for {
			_, payload, err := frames.Next()
			if len(payload) > 0 {
				if writeText(ctx, conn, string(payload)) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// 浏览器 → 容器
	for {
		if err := stream.SetDeadline(time.Now().Add(execIdleTimeout)); err != nil {
			return
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg execClientMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "stdin":
			if _, err := stream.Write([]byte(msg.Data)); err != nil {
				return
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = d.cli.ExecResize(ctx, execID, msg.Rows, msg.Cols)
			}
		}
	}
}

// pullProgress 推送给前端的一条拉取进度。
type pullProgress struct {
	Percent      float64                `json:"percent"`      // 0-100
	Status       string                 `json:"status"`       // 最近一条全局状态文本
	Done         bool                   `json:"done"`         // 拉取完成
	ByteWeighted bool                   `json:"byteWeighted"` // false 表示已降级为按层数计
	Layers       []client.LayerProgress `json:"layers"`
	Error        string                 `json:"error,omitempty"`
}

// wsPullImage 拉取镜像并推送聚合进度。
//
// 查询参数:ref(name[:tag])、platform(如 linux/amd64,空则按宿主机)。
// Engine API 没有取消拉取的端点,「终止」即断开连接、取消 ctx(docs §4.4)。
func (d *dockerAPI) wsPullImage(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("ref")
	platform := r.URL.Query().Get("platform")
	if ref == "" {
		writeErr(w, http.StatusBadRequest, "缺少镜像名")
		return
	}

	conn, err := acceptWS(w, r)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	rc, err := d.cli.PullImage(ctx, ref, platform, nil)
	if err != nil {
		_ = writeJSONMsg(ctx, conn, pullProgress{Error: errMessage(err)})
		return
	}
	defer rc.Close()

	// 前端关闭连接即代表取消
	go func() {
		conn.Read(ctx)
		cancel()
	}()

	err = client.DecodePullStream(ctx, rc, func(p client.PullProgress) error {
		return writeJSONMsg(ctx, conn, pullProgress{
			Percent:      p.Percent,
			Status:       p.Status,
			Done:         p.Done,
			ByteWeighted: p.ByteWeighted,
			Layers:       p.Layers,
		})
	})
	// 正常结束(err 为 nil)由最后的 done 消息通知;用户取消(context.Canceled)静默返回;
	// 其余错误(如镜像不存在)补一条错误消息。
	if err != nil && !errors.Is(err, context.Canceled) {
		_ = writeJSONMsg(ctx, conn, pullProgress{Error: errMessage(err)})
	}
}

// deployProgress 推送给前端的一条部署进度。
type deployProgress struct {
	ID     string `json:"id"`              // "Container x" / "Image y" / "Network z"
	Status string `json:"status"`          // Working / Done / Error
	Text   string `json:"text"`            // "Starting" / "Pulling" / ...
	Error  string `json:"error,omitempty"` // 整体失败信息
	Done   bool   `json:"done"`            // 整个部署结束(成功)
}

// composeEvent compose --progress json 输出的一行(字段可能随版本增减,只取用到的)。
type composeEvent struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Text   string `json:"text"`
}

// wsDeploy 部署编排(docs §4.1 ⑤):up --progress json 流式推送。
//
// 流程:项目锁 → 读盘 YAML → up → 逐行推送 → 成功后写 lastDeployedYAML。
// 同 projectName 并发部署直接 409 拒绝,不排队(docs §4.1 Q16)。
func (d *dockerAPI) wsDeploy(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")

	inst, err := d.s.GetComposeInstance(project)
	if err != nil {
		writeErr(w, http.StatusNotFound, "项目不存在")
		return
	}

	if !d.cmp.TryLock(project) {
		writeErr(w, http.StatusConflict, "该应用正在部署中")
		return
	}
	defer d.cmp.Unlock(project)

	conn, err := acceptWS(w, r)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	file, dir := d.composePaths(inst)
	yamlBytes, _ := os.ReadFile(file)

	st, err := d.cmp.Up(ctx, project, dir)
	if err != nil {
		_ = writeJSONMsg(ctx, conn, deployProgress{Error: errMessage(err)})
		return
	}
	defer st.Close()

	// 前端关闭连接即代表取消
	go func() {
		conn.Read(ctx)
		cancel()
	}()

	dec := json.NewDecoder(st)
	for {
		if ctx.Err() != nil {
			return
		}
		var ev composeEvent
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			break // 解析失败视为流结束
		}
		if ev.ID == "" {
			continue
		}
		msg := deployProgress{ID: ev.ID, Status: ev.Status, Text: ev.Text}
		if ev.Status == "Error" {
			msg.Error = ev.Text
		}
		if writeJSONMsg(ctx, conn, msg) != nil {
			return
		}
	}

	// 进程结束,用 exit code 判定成败(docs §4.1:启动期失败部分残留时 exit 非 0)
	if werr := st.Wait(); werr != nil && ctx.Err() == nil {
		_ = writeJSONMsg(ctx, conn, deployProgress{Error: errMessage(werr)})
		return
	}

	// 成功:写入 lastDeployedYAML(docs §4.1 Q16',纯数据,供「恢复」按钮)
	if ctx.Err() == nil {
		inst.LastDeployedYAML = string(yamlBytes)
		inst.LastDeployedAt = time.Now()
		_ = d.s.SaveComposeInstance(inst)
		d.syncCaddyAsync(r) // 部署成功后容器已运行,自动同步到网关(ADR-026 §7)
		_ = writeJSONMsg(ctx, conn, deployProgress{Done: true})
	}
}

// --- 辅助 ---

func writeJSONMsg(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func writeText(ctx context.Context, conn *websocket.Conn, s string) error {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, []byte(s))
}

func sendLogErr(ctx context.Context, conn *websocket.Conn, msg string) {
	_ = writeJSONMsg(ctx, conn, logMessage{Error: msg})
}

// errMessage 提取 daemon 报错中对用户有意义的部分。
func errMessage(err error) string {
	var apiErr *client.Error
	if errors.As(err, &apiErr) && apiErr.Message != "" {
		return apiErr.Message
	}
	return err.Error()
}

func parseSize(s string) uint {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return uint(n)
}
