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

	"github.com/JiangBeta/gatebox/internal/repository"
)

// serviceLogs 推送某服务的 access log(按任一域名行 host 过滤,ADR-020 §2)。
//
// 只对 manual service 有意义(docker 派生 service 的日志走 Docker 单位的容器日志)。
// 查询参数:follow(默认 true)、tail(默认 200)。
func (a *gatewayAPI) serviceLogs(w http.ResponseWriter, r *http.Request) {
	svc, err := a.s.GetService(r.PathValue("id"))
	if errors.Is(err, repository.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "服务不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	hosts := make(map[string]bool, len(svc.Domains))
	for _, d := range svc.Domains {
		hosts[d.Host()] = true
	}

	conn, err := acceptWS(w, r)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	logPath := filepath.Join(a.dataDir, "tools", "caddy", "logs", "access.log")
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

	for _, ln := range tailLines(f, tail, hosts) {
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
		if matchHostAny(line, hosts) {
			if err := writeJSONMsg(ctx, conn, logMessage{Data: line}); err != nil {
				return
			}
		}
	}
}

// tailLines 读文件尾部最后 n 行,过滤出匹配任一 host 的行(不改动文件 offset)。
func tailLines(f *os.File, n int, hosts map[string]bool) []string {
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
		if matchHostAny(ln, hosts) {
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
