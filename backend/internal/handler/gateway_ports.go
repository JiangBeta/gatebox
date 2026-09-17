package handler

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
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/model"
)

var protocolRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

// nonHTTPBinding 非 HTTP 协议的监听配置(端口 + 网络)。
type nonHTTPBinding struct {
	ports []int
	nets  []string
}

// caddyPortOptions 从 port_bindings 派生 Caddyfile 生成选项。
//
// 协议类别由能力注册表决定(ADR-036 I1):核心不判断具体协议名是否属于某插件;
//   - class=http  → 内置监听端口(http_port/https_port);
//   - class=non-http → 收集中性规则,交给注册表中声明该类别的 renderer 产出全局片段;
//   - 无类别(无提供者) → 管理态,忽略。
func (a *gatewayAPI) caddyPortOptions(services []model.Service) (caddy.GenerateOptions, error) {
	bs, err := a.s.ListPortBindings()
	if err != nil {
		return caddy.GenerateOptions{}, err
	}
	if len(bs) == 0 {
		return caddy.GenerateOptions{HTTPPort: a.httpPort, HTTPSPort: a.httpsPort, ExtraHTTPSPorts: a.extraHTTPSPorts}, nil
	}
	opts := caddy.GenerateOptions{}
	nonHTTP := map[string]nonHTTPBinding{}
	for _, b := range bs {
		if !b.Enabled || len(b.Ports) == 0 {
			continue
		}
		switch a.classOf(b.Protocol) {
		case extension.ClassHTTP:
			switch b.Protocol {
			case model.DomainProtoHTTP:
				opts.HTTPPort = b.Ports[0]
				if len(b.Ports) > 1 {
					opts.ExtraHTTPPorts = append(opts.ExtraHTTPPorts, b.Ports[1:]...)
				}
			case model.DomainProtoHTTPS:
				opts.HTTPSPort = b.Ports[0]
				if len(b.Ports) > 1 {
					opts.ExtraHTTPSPorts = append(opts.ExtraHTTPSPorts, b.Ports[1:]...)
				}
			}
		case extension.ClassNonHTTP:
			nonHTTP[b.Protocol] = nonHTTPBinding{ports: b.Ports, nets: b.Nets()}
		}
	}
	rules := collectNonHTTPRules(services, nonHTTP)
	if len(rules) > 0 {
		if rd, ok := a.ext.RendererFor(extension.ClassNonHTTP); ok {
			snip, err := rd.Render(rules)
			if err != nil {
				return caddy.GenerateOptions{}, err
			}
			if strings.TrimSpace(snip) != "" {
				opts.GlobalSnippets = append(opts.GlobalSnippets, snip)
			}
		}
	}
	return opts, nil
}

// classOf 查询协议类别;ext 为空时回退内置 http/https 判断(不影响主闭环)。
func (a *gatewayAPI) classOf(protocol string) string {
	if a.ext == nil {
		if model.IsHTTPProto(protocol) {
			return extension.ClassHTTP
		}
		return ""
	}
	return a.ext.ClassOf(protocol)
}

// collectNonHTTPRules 汇集非 HTTP 中性代理规则(按协议+上游去重,稳定排序)。
func collectNonHTTPRules(services []model.Service, bindings map[string]nonHTTPBinding) []extension.ProxyRule {
	var rules []extension.ProxyRule
	seen := map[string]bool{}
	for _, svc := range services {
		if !svc.Enabled || svc.Type != model.RouteTypeReverseProxy || len(svc.Upstream) == 0 {
			continue
		}
		for _, d := range svc.Domains {
			b, ok := bindings[d.Protocol]
			if !ok || len(b.ports) == 0 || len(b.nets) == 0 {
				continue
			}
			key := d.Protocol + "|" + svc.Upstream[0]
			if seen[key] {
				continue
			}
			seen[key] = true
			rules = append(rules, extension.ProxyRule{
				Protocol: d.Protocol, Upstream: svc.Upstream[0], Ports: b.ports, Nets: b.nets,
			})
		}
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Protocol != rules[j].Protocol {
			return rules[i].Protocol < rules[j].Protocol
		}
		return rules[i].Upstream < rules[j].Upstream
	})
	return rules
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
	builtins := []model.PortBinding{
		{Protocol: "http", Description: "HTTP", Network: model.NetTCP, Ports: []int{httpDefault}, Enabled: true, Builtin: true, CreatedAt: now},
		{Protocol: "https", Description: "HTTPS", Network: model.NetTCP, Ports: httpsPorts, Enabled: true, Builtin: true, CreatedAt: now},
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
	Ports       []int  `json:"ports"`
	Network     string `json:"network"` // tcp | udp | both(缺省 tcp)
	Enabled     *bool  `json:"enabled"`
}

// normalizeNetwork 归一化网络取值;http/https 恒 tcp。
func normalizeNetwork(protocol, network string) (string, bool) {
	if protocol == model.DomainProtoHTTP || protocol == model.DomainProtoHTTPS {
		return model.NetTCP, true
	}
	switch network {
	case "", model.NetTCP:
		return model.NetTCP, true
	case model.NetUDP:
		return model.NetUDP, true
	case model.NetBoth:
		return model.NetBoth, true
	default:
		return "", false
	}
}

func (in *portBindingInput) validate() (string, bool) {
	if !protocolRE.MatchString(in.Protocol) {
		return "协议名需为小写字母/数字", false
	}
	if _, ok := normalizeNetwork(in.Protocol, in.Network); !ok {
		return "网络需为 tcp / udp / both", false
	}
	if len(in.Ports) == 0 {
		return "至少需要一个实际端口", false
	}
	seen := make(map[int]bool, len(in.Ports))
	for _, p := range in.Ports {
		if p < 1 || p > 65535 {
			return fmt.Sprintf("端口 %d 超出范围(1~65535)", p), false
		}
		if seen[p] {
			return fmt.Sprintf("实际端口 %d 重复", p), false
		}
		seen[p] = true
	}
	return "", true
}

// checkPortConflict 校验 ports 是否与其它协议已登记的端口冲突(跳过 protocol 自身)。
func (a *gatewayAPI) checkPortConflict(protocol string, ports []int) (string, bool) {
	bs, err := a.s.ListPortBindings()
	if err != nil {
		return "读取端口失败: " + err.Error(), false
	}
	for _, b := range bs {
		if b.Protocol == protocol {
			continue
		}
		for _, p := range ports {
			for _, existing := range b.Ports {
				if p == existing {
					return fmt.Sprintf("端口 %d 已被协议 %s 占用", p, b.Protocol), false
				}
			}
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
	if msg, ok := a.checkPortConflict(in.Protocol, in.Ports); !ok {
		writeErr(w, http.StatusConflict, msg)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	netVal, _ := normalizeNetwork(in.Protocol, in.Network)
	p := model.PortBinding{
		Protocol: in.Protocol, Description: in.Description,
		Ports: in.Ports, Network: netVal, Enabled: enabled, Builtin: false, CreatedAt: time.Now(),
	}
	if err := a.s.SavePortBinding(&p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	triggerReconcile("port", p.Protocol)
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
	if msg, ok := a.checkPortConflict(in.Protocol, in.Ports); !ok {
		writeErr(w, http.StatusConflict, msg)
		return
	}
	cur.Description = in.Description
	cur.Ports = in.Ports
	cur.Network, _ = normalizeNetwork(in.Protocol, in.Network)
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	if err := a.s.SavePortBinding(cur); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	triggerReconcile("port", cur.Protocol)
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
	triggerReconcile("port", protocol)
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
func (a *gatewayAPI) getPortBinding(protocol string) (*model.PortBinding, error) {
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
	_ = a.s.SaveCertLog(&model.CertLog{
		ID: newID(), FQDN: fqdn, Action: action, Status: status, Message: msg, LogFile: logFile, CreatedAt: time.Now(),
	})
}
