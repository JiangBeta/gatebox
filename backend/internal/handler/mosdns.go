package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/mosdns"
	"github.com/JiangBeta/gatebox/internal/component"
)

// mosdnsAPI 内网 DNS(mosdns)管理接口:设置 / 配置 / 规则 / 内网解析 / 数据库 / 日志 / 缓存。
type mosdnsAPI struct {
	m   *mosdns.Manager
	reg *component.CoreRegistry
}

// RegisterMosdns 注册 mosdns 管理路由。
func RegisterMosdns(mux *http.ServeMux, m *mosdns.Manager, reg *component.CoreRegistry) {
	h := &mosdnsAPI{m: m, reg: reg}

	mux.HandleFunc("GET /api/v1/mosdns/status", h.status)
	mux.HandleFunc("GET /api/v1/mosdns/settings", h.getSettings)
	mux.HandleFunc("PUT /api/v1/mosdns/settings", h.putSettings)
	mux.HandleFunc("GET /api/v1/mosdns/config", h.getConfig)
	mux.HandleFunc("PUT /api/v1/mosdns/config", h.putConfig)
	mux.HandleFunc("GET /api/v1/mosdns/hosts", h.getHosts)
	mux.HandleFunc("PUT /api/v1/mosdns/hosts", h.putHosts)
	mux.HandleFunc("GET /api/v1/mosdns/rules", h.listRules)
	mux.HandleFunc("GET /api/v1/mosdns/rules/{name}", h.getRule)
	mux.HandleFunc("PUT /api/v1/mosdns/rules/{name}", h.putRule)
	mux.HandleFunc("GET /api/v1/mosdns/geodata", h.listGeodata)
	mux.HandleFunc("POST /api/v1/mosdns/geodata/update", h.updateGeodata)
	mux.HandleFunc("POST /api/v1/mosdns/adblock/update", h.updateAdblock)
	mux.HandleFunc("GET /api/v1/mosdns/logs", h.getLogs)
	mux.HandleFunc("GET /api/v1/mosdns/logs/stream", h.logsStream)
	mux.HandleFunc("DELETE /api/v1/mosdns/logs", h.clearLogs)
	mux.HandleFunc("POST /api/v1/mosdns/flush", h.flush)
}

// mosdnsStatus 页面状态卡:进程态 + 配置解析出的路径/端口。
type mosdnsStatus struct {
	Installed       bool   `json:"installed"`
	State           string `json:"state"`
	Healthy         bool   `json:"healthy"`
	Version         string `json:"version"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Listen          string `json:"listen"`
	APIAddr         string `json:"apiAddr"`
	ConfigPath      string `json:"configPath"`
	ConfigExists    bool   `json:"configExists"`
	HostsPath       string `json:"hostsPath"`
	LogFile         string `json:"logFile"`
	CacheTag        string `json:"cacheTag"`
}

func (h *mosdnsAPI) status(w http.ResponseWriter, r *http.Request) {
	st := mosdnsStatus{
		State:      "unknown",
		Listen:     mosdns.DefaultListen,
		APIAddr:    h.m.APIAddr(),
		ConfigPath: h.m.ConfigPath(),
		HostsPath:  h.m.HostsPath(),
		LogFile:    h.m.LogFile(),
		CacheTag:   h.m.CacheTag(),
	}
	if _, err := h.m.ReadConfig(); err == nil {
		st.ConfigExists = true
	}
	if s, err := h.m.ReadSettings(); err == nil {
		st.Listen = s.Listen
	}
	if h.reg != nil {
		if info, ok := h.reg.Get(r.Context(), "mosdns"); ok {
			st.Installed = info.Installed
			st.State = info.Status.State
			st.Healthy = info.Status.Healthy
			st.Version = info.Current
			st.Latest = info.Latest
			st.UpdateAvailable = info.UpdateAvailable
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *mosdnsAPI) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.m.ReadSettings()
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// putSettings 保存设置并重新生成 config.yaml(覆盖手工改动,前端已提示)。
func (h *mosdnsAPI) putSettings(w http.ResponseWriter, r *http.Request) {
	var in mosdns.Settings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	if msg := validateSettings(in); msg != "" {
		writeErrCode(w, http.StatusBadRequest, "INVALID_SETTINGS", msg)
		return
	}
	content, err := h.m.RenderConfig(in)
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "RENDER_FAILED", err.Error())
		return
	}
	if err := h.m.WriteConfig(content); err != nil {
		writeErrCode(w, http.StatusInternalServerError, "WRITE_FAILED", err.Error())
		return
	}
	if err := h.m.WriteSettings(in); err != nil {
		writeErrCode(w, http.StatusInternalServerError, "WRITE_FAILED", err.Error())
		return
	}
	s, _ := h.m.ReadSettings()
	writeJSON(w, http.StatusOK, s)
}

// validateSettings 校验监听地址、上游与数值范围。
func validateSettings(s mosdns.Settings) string {
	if msg := validateListen(s.Listen); msg != "" {
		return msg
	}
	if len(s.LocalDNS) == 0 || len(s.RemoteDNS) == 0 {
		return "本地与远程上游 DNS 至少各配置一个"
	}
	if s.Cache && s.CacheSize <= 0 {
		return "缓存容量必须大于 0"
	}
	if s.Concurrent != 0 && (s.Concurrent < 1 || s.Concurrent > 3) {
		return "并发查询数需为 0(默认)或 1-3"
	}
	if s.EnableECSRomote && net.ParseIP(strings.TrimSpace(s.RemoteECSIP)) == nil {
		return "ECS 客户端子网 IP 不合法"
	}
	return ""
}

// validateListen 校验监听地址格式(host:port 或 :port)。
func validateListen(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "监听地址不能为空"
	}
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return "监听地址格式应为 host:port 或 :port"
	}
	return ""
}

func (h *mosdnsAPI) getConfig(w http.ResponseWriter, r *http.Request) {
	content, err := h.m.ReadConfig()
	if errors.Is(err, mosdns.ErrNotConfigured) {
		writeJSON(w, http.StatusOK, map[string]any{"content": "", "path": h.m.ConfigPath(), "configured": false})
		return
	}
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": string(content), "path": h.m.ConfigPath(), "configured": true})
}

// putConfig 写入手工编辑的 config.yaml(校验 YAML 与插件 tag/type)。
func (h *mosdnsAPI) putConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	if err := h.m.WriteConfig([]byte(in.Content)); err != nil {
		writeErrCode(w, http.StatusBadRequest, "INVALID_CONFIG", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": h.m.ConfigPath()})
}

func (h *mosdnsAPI) getHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := h.m.ReadHosts()
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hosts)
}

func (h *mosdnsAPI) putHosts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hosts []mosdns.Host `json:"hosts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	if err := h.m.WriteHosts(in.Hosts); err != nil {
		writeErrCode(w, http.StatusBadRequest, "INVALID_HOSTS", err.Error())
		return
	}
	hosts, _ := h.m.ReadHosts()
	writeJSON(w, http.StatusOK, hosts)
}

// --- 规则列表 ---

func (h *mosdnsAPI) listRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, mosdns.GetRuleMeta())
}

func (h *mosdnsAPI) getRule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	content, err := h.m.ReadRule(name)
	if errors.Is(err, mosdns.ErrBadRule) {
		writeErrCode(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "content": content, "path": h.m.RulePath(name)})
}

func (h *mosdnsAPI) putRule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var in struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	if err := h.m.WriteRule(name, in.Content); err != nil {
		if errors.Is(err, mosdns.ErrBadRule) {
			writeErrCode(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		writeErrCode(w, http.StatusInternalServerError, "WRITE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "path": h.m.RulePath(name)})
}

// --- 数据库 ---

func (h *mosdnsAPI) listGeodata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.m.GeoList())
}

// updateGeodata 同步下载全部数据库文件(前端使用较长超时)。
func (h *mosdnsAPI) updateGeodata(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	results := h.m.UpdateGeodata(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "items": h.m.GeoList()})
}

// updateAdblock 下载广告规则来源。
func (h *mosdnsAPI) updateAdblock(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	results := h.m.UpdateAdSources(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// --- 日志 ---

func (h *mosdnsAPI) getLogs(w http.ResponseWriter, r *http.Request) {
	content, err := h.m.ReadLog(256 << 10)
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": content, "path": h.m.LogFile()})
}

func (h *mosdnsAPI) clearLogs(w http.ResponseWriter, r *http.Request) {
	if err := h.m.ClearLog(); err != nil {
		writeErrCode(w, http.StatusInternalServerError, "CLEAR_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// logsStream 以 WebSocket 推送 mosdns 日志尾部并持续跟随(参照网关代理日志)。
// 查询参数:tail(默认 500)、follow(默认 true)。
func (h *mosdnsAPI) logsStream(w http.ResponseWriter, r *http.Request) {
	conn, err := acceptWS(w, r)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		conn.Read(ctx)
		cancel()
	}()

	f, err := os.Open(h.m.LogFile())
	if err != nil {
		sendLogErr(ctx, conn, "打开日志文件失败: "+err.Error())
		return
	}
	defer f.Close()

	tail := 500
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = n
		}
	}
	for _, ln := range tailLines(f, tail, func(string) bool { return true }) {
		if err := writeJSONMsg(ctx, conn, logMessage{Data: ln}); err != nil {
			return
		}
	}
	if r.URL.Query().Get("follow") == "false" {
		return
	}
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
		if err := writeJSONMsg(ctx, conn, logMessage{Data: line}); err != nil {
			return
		}
	}
}

func (h *mosdnsAPI) flush(w http.ResponseWriter, r *http.Request) {
	if err := h.m.FlushCache(r.Context()); err != nil {
		writeErrCode(w, http.StatusBadGateway, "FLUSH_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
