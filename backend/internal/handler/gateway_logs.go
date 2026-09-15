package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/repository"
)

// findDerivedService 在 docker 派生集合中按稳定 ID 查找(无匹配返回 nil)。
func (a *gatewayAPI) findDerivedService(ctx context.Context, id string) *model.Service {
	for _, svc := range a.deriveDockerServices(ctx) {
		if svc.ID == id {
			s := svc
			return &s
		}
	}
	return nil
}

// serviceLogs 推送某服务的 access log。
//
// manual service 按 id 读其 GB_LOG_FILE;docker 派生 service 不落库,
// 按稳定 ID 在派生集合中取回(亦可用 ?service=<名称> 兜底)。查询参数:follow(默认 true)、tail(默认 200)。
func (a *gatewayAPI) serviceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, err := a.s.GetService(id)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		// docker 派生服务不落库:按稳定 ID 找回定义(含域名),否则独立日志缺失时
		// 回落全局 access.log 会因 host 过滤集为空而输出为空。
		svc = a.findDerivedService(r.Context(), id)
		if svc == nil {
			name := strings.TrimSpace(r.URL.Query().Get("service"))
			if name == "" {
				writeErr(w, http.StatusNotFound, "服务不存在")
				return
			}
			svc = &model.Service{Name: name}
		}
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	conn, err := acceptWS(w, r)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 优先读「按服务日志」片段的独立文件(ADR-033);缺失时回落全局 access log + host 过滤。
	// 派生服务 AppID 为 compose 项目名(非应用),日志应用段回落 default(与生成器一致)。
	appName := "default"
	if svc.AppID != "" {
		if app, err := a.s.GetApp(svc.AppID); err == nil {
			appName = app.Name
		}
	}
	hosts := make(map[string]bool, len(svc.Domains))
	for _, d := range svc.Domains {
		hosts[d.Host()] = true
	}
	logPath := caddy.ServiceLogPath(filepath.Join(a.dataDir, "logs", "caddy"), appName, svc.Name)
	filterHosts := false
	if _, statErr := os.Stat(logPath); statErr != nil {
		logPath = filepath.Join(a.dataDir, "logs", "caddy", "access.log")
		filterHosts = true
	}
	accept := func(line string) bool { return !filterHosts || matchHostAny(line, hosts) }

	f, err := os.Open(logPath)
	if err != nil {
		sendLogErr(ctx, conn, "打开日志文件失败: "+err.Error())
		return
	}
	defer f.Close()

	// 客户端主动关闭时结束读取
	go func() {
		conn.Read(ctx)
		cancel()
	}()

	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = n
		}
	}
	follow := r.URL.Query().Get("follow") != "false"

	for _, ln := range tailLines(f, tail, accept) {
		if err := writeJSONMsg(ctx, conn, logMessage{Data: ln}); err != nil {
			return
		}
	}
	if !follow {
		return
	}

	// 从末尾开始跟随新行
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return
	}
	reader := bufio.NewReader(f)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return
		}
		if accept(line) {
			if err := writeJSONMsg(ctx, conn, logMessage{Data: line}); err != nil {
				return
			}
		}
	}
}

// tailLines 读文件尾部最后 n 行,保留 accept 通过的行(不改动文件 offset)。
func tailLines(f *os.File, n int, accept func(string) bool) []string {
	const chunk = 256 * 1024
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	start := size - chunk
	if start < 0 {
		start = 0
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(string(buf), "\n") {
		if accept(ln) {
			out = append(out, ln)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// matchHostAny 判断一行 JSON access log 是否匹配任一 host。
func matchHostAny(line string, hosts map[string]bool) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	var l struct {
		Request struct {
			Host string `json:"host"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(line), &l); err != nil {
		return false
	}
	return hosts[l.Request.Host]
}
