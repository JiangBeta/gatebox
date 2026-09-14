package api

// 网关 → 端口:协议端口记录管理(ADR-026 多端口)。
//
// HTTP/HTTPS 为系统内置项(不可删除),其 Ports 驱动 caddy 监听:
//   HTTP: 首端口作为 caddy http_port;
//   HTTPS: 首端口作为 https_port,后续端口作为额外 https 端口(host:<port> 重复站点块)。
// 其它协议为管理态展示(L4 代理后置)。
//
// caddy 实际监听端口由 reloadCaddy 每次读取 port_bindings 动态决定——
// 保存端口后「重启」即生效,无需重启进程。

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/JiangBeta/gatebox/internal/caddy"
	"github.com/JiangBeta/gatebox/internal/models"
)

var protocolRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

// caddyPortOptions 从 port_bindings 派生 Caddyfile 生成端口选项。
// 无记录时回退启动配置(config 参数)。
func (a *gatewayAPI) caddyPortOptions() (caddy.GenerateOptions, error) {
	bs, err := a.s.ListPortBindings()
	if err != nil {
		return caddy.GenerateOptions{}, err
	}
	if len(bs) == 0 {
		return caddy.GenerateOptions{HTTPPort: a.httpPort, HTTPSPort: a.httpsPort, ExtraHTTPSPorts: a.extraHTTPSPorts}, nil
	}
	var opts caddy.GenerateOptions
	for _, b := range bs {
		if !b.Enabled || len(b.Ports) == 0 {
			continue
		}
		switch b.Protocol {
		case "http":
			opts.HTTPPort = b.Ports[0]
			if len(b.Ports) > 1 {
				opts.ExtraHTTPPorts = append(opts.ExtraHTTPPorts, b.Ports[1:]...)
			}
		case "https":
			opts.HTTPSPort = b.Ports[0]
			if len(b.Ports) > 1 {
				opts.ExtraHTTPSPorts = append(opts.ExtraHTTPSPorts, b.Ports[1:]...)
			}
		}
	}
	return opts, nil
}

// seedDefaultPorts 首次加载时用启动配置写入 HTTP/HTTPS 内置项。
func (a *gatewayAPI) seedDefaultPorts() error {
	bs, err := a.s.ListPortBindings()
	if err != nil {
		return err
	}
	if len(bs) > 0 {
		return nil
	}
	httpDefault, httpsDefault := a.httpPort, a.httpsPort
	if httpDefault <= 0 {
		httpDefault = 80
	}
	if httpsDefault <= 0 {
		httpsDefault = 443
	}
	now := time.Now()
	httpsPorts := []int{httpsDefault}
	httpsPorts = append(httpsPorts, a.extraHTTPSPorts...)
	builtins := []models.PortBinding{
		{Protocol: "http", Description: "HTTP", DefaultPort: 80, Ports: []int{httpDefault}, Enabled: true, Builtin: true, CreatedAt: now},
		{Protocol: "https", Description: "HTTPS", DefaultPort: 443, Ports: httpsPorts, Enabled: true, Builtin: true, CreatedAt: now},
	}
	for i := range builtins {
		if err := a.s.SavePortBinding(&builtins[i]); err != nil {
			return err
		}
	}
	return nil
}

// getGatewayPorts 返回全部协议端口记录(按协议名排序)。
func (a *gatewayAPI) getGatewayPorts(w http.ResponseWriter, r *http.Request) {
	if err := a.seedDefaultPorts(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	bs, err := a.s.ListPortBindings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].Protocol < bs[j].Protocol })
	writeJSON(w, http.StatusOK, bs)
}

type portBindingInput struct {
	Protocol    string `json:"protocol"`
	Description string `json:"description"`
	DefaultPort int    `json:"defaultPort"`
	Ports       []int  `json:"ports"`
	Enabled     *bool  `json:"enabled"`
}

func (in *portBindingInput) validate() (string, bool) {
	if !protocolRE.MatchString(in.Protocol) {
		return "协议名需为小写字母/数字", false
	}
	if in.DefaultPort <= 0 || in.DefaultPort > 65535 {
		return "默认端口非法", false
	}
	if len(in.Ports) == 0 {
		return "至少需要一个实际端口", false
	}
	for _, p := range in.Ports {
		if p <= 0 || p > 65535 {
			return "端口超出范围", false
		}
	}
	return "", true
}

// createGatewayPort 新增一条协议端口记录(HTTP/HTTPS 为内置,不可重复创建)。
func (a *gatewayAPI) createGatewayPort(w http.ResponseWriter, r *http.Request) {
	var in portBindingInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if in.Protocol == "http" || in.Protocol == "https" {
		writeErr(w, http.StatusBadRequest, "HTTP/HTTPS 为系统默认项,不可创建")
		return
	}
	if msg, ok := in.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if cur, _ := a.getPortBinding(in.Protocol); cur != nil {
		writeErr(w, http.StatusConflict, "该协议已存在")
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	p := models.PortBinding{
		Protocol: in.Protocol, Description: in.Description, DefaultPort: in.DefaultPort,
		Ports: in.Ports, Enabled: enabled, Builtin: false, CreatedAt: time.Now(),
	}
	if err := a.s.SavePortBinding(&p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// updateGatewayPort 更新一条端口记录(内置项也可改端口/说明/启停,但不可改协议名)。
func (a *gatewayAPI) updateGatewayPort(w http.ResponseWriter, r *http.Request) {
	protocol := r.PathValue("protocol")
	cur, err := a.getPortBinding(protocol)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cur == nil {
		writeErr(w, http.StatusNotFound, "协议不存在")
		return
	}
	var in portBindingInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if in.Protocol != "" && in.Protocol != cur.Protocol {
		writeErr(w, http.StatusBadRequest, "协议主键不可修改")
		return
	}
	in.Protocol = cur.Protocol
	if msg, ok := in.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	cur.Description = in.Description
	cur.DefaultPort = in.DefaultPort
	cur.Ports = in.Ports
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	if err := a.s.SavePortBinding(cur); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cur)
}

// deleteGatewayPort 删除协议端口记录(HTTP/HTTPS 内置项不可删除)。
func (a *gatewayAPI) deleteGatewayPort(w http.ResponseWriter, r *http.Request) {
	protocol := r.PathValue("protocol")
	cur, err := a.getPortBinding(protocol)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cur == nil {
		writeErr(w, http.StatusNotFound, "协议不存在")
		return
	}
	if cur.Builtin {
		writeErr(w, http.StatusConflict, "HTTP/HTTPS 为系统默认项,不可删除")
		return
	}
	if err := a.s.DeletePortBinding(protocol); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// restartGatewayPorts 按当前端口记录重新生成并加载 Caddyfile(即「重启」)。
func (a *gatewayAPI) restartGatewayPorts(w http.ResponseWriter, r *http.Request) {
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "重启失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// getPortBinding 按协议取一条记录;不存在返回 nil。
func (a *gatewayAPI) getPortBinding(protocol string) (*models.PortBinding, error) {
	bs, err := a.s.ListPortBindings()
	if err != nil {
		return nil, err
	}
	for i := range bs {
		if bs[i].Protocol == protocol {
			return &bs[i], nil
		}
	}
	return nil, nil
}

// logCert 追加一条证书操作日志(SSL 证书 → 日志)。
func (a *gatewayAPI) logCert(action, fqdn, status, msg, logFile string) {
	_ = a.s.SaveCertLog(&models.CertLog{
		ID: newID(), FQDN: fqdn, Action: action, Status: status, Message: msg, LogFile: logFile, CreatedAt: time.Now(),
	})
}
