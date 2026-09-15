// Package plugin 提供插件引擎：manifest v2 描述 + 内置目录 + 生命周期状态机 + 贡献注册。
//
// 设计见 docs/adr/ADR-036（扩展平台）与 docs/architecture.md §4。核心不认插件身份，
// 启用中的插件把 capabilities/renderer 贡献注册进扩展注册表，供核心查表消费。
package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/source"
)

// Publisher 发布者信息（appstore 签名预留）。
type Publisher struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	KeyID string `json:"keyID,omitempty"`
}

// Requires 插件对宿主与 GateBox 版本的依赖。
type Requires struct {
	Gatebox      string   `json:"gatebox,omitempty"`
	ExtensionAPI string   `json:"extensionApi,omitempty"`
	Components   []string `json:"components,omitempty"`
	OS           []string `json:"os,omitempty"`
	Arch         []string `json:"arch,omitempty"`
}

// 制品 role。
const (
	RoleBinary  = "binary"  // 主可执行文件
	RoleSidecar = "sidecar" // 后端逻辑侧车（预留）
	RoleUI      = "ui"      // 前端页面代码（预留）
	RoleAssets  = "assets"  // 静态资源
)

// Artifact 某 role + 平台的可安装制品。
type Artifact struct {
	Role    string  `json:"role"`
	OS      string  `json:"os,omitempty"`
	Arch    string  `json:"arch,omitempty"`
	URL     string  `json:"url"`
	SHA256  string  `json:"sha256,omitempty"`
	Size    int64   `json:"size,omitempty"`
	Format  string  `json:"format,omitempty"` // ui 制品格式（esm | iframe | tarball）
	Entry   string  `json:"entry,omitempty"`
	Install Install `json:"install,omitempty"`
}

// Install 制品落盘位置与权限。
type Install struct {
	To   string `json:"to,omitempty"`
	Mode string `json:"mode,omitempty"`
}

// Contribution 通用贡献项（capabilities 用）。
type Contribution struct {
	Point string         `json:"point"`
	Data  map[string]any `json:"data,omitempty"`
}

// BackendContribution 后端贡献项（renderer / config-sync / reconcile）。
type BackendContribution struct {
	Point      string            `json:"point"`
	For        string            `json:"for,omitempty"`        // renderer：协议类别
	Scope      string            `json:"scope,omitempty"`      // global | site
	Projection string            `json:"projection,omitempty"` // config-sync / reconcile：投影名
	Target     string            `json:"target,omitempty"`     // config-sync：落盘目标
	Entry      string            `json:"entry,omitempty"`      // reconcile：侧车入口
	Interval   string            `json:"interval,omitempty"`   // reconcile：兜底轮询
	Impl       map[string]string `json:"impl,omitempty"`       // {type: template, template: "..."}
}

// UIContributions 前端贡献（导航 / 路由 / 槽位 / 页面）。
type UIContributions struct {
	Nav    []NavItem         `json:"nav,omitempty"`
	Routes []NavItem         `json:"routes,omitempty"`
	Slots  []Slot            `json:"slots,omitempty"`
	Page   map[string]string `json:"page,omitempty"`
}

// NavItem 导航/路由入口。
type NavItem struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
}

// Slot 前端具名合并点。
type Slot struct {
	Slot string `json:"slot"`
	From string `json:"from,omitempty"`
}

// DataContribution 数据订阅声明（消费核心投影）。
type DataContribution struct {
	Subscribe []string `json:"subscribe,omitempty"`
}

// Contributions 插件全部贡献。
type Contributions struct {
	Capabilities []Contribution        `json:"capabilities,omitempty"`
	Backend      []BackendContribution `json:"backend,omitempty"`
	UI           UIContributions       `json:"ui,omitempty"`
	Data         DataContribution      `json:"data,omitempty"`
}

// Permission 权限声明（appstore 式）。
type Permission struct {
	Filesystem map[string][]string `json:"filesystem,omitempty"`
	Network    []string            `json:"network,omitempty"`
	API        []string            `json:"api,omitempty"`
}

// Manifest 插件声明（v2）。
type Manifest struct {
	APIVersion    string        `json:"apiVersion"`
	Kind          string        `json:"kind"` // caddy-module | process | config-only
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Version       string        `json:"version"`
	Summary       string        `json:"summary"`
	Tags          []string      `json:"tags,omitempty"`
	Publisher     Publisher     `json:"publisher,omitempty"`
	Requires      Requires      `json:"requires,omitempty"`
	Channel       string        `json:"channel"`
	Artifacts     []Artifact    `json:"artifacts,omitempty"`
	Contributions Contributions `json:"contributions,omitempty"`
	Permissions   []Permission  `json:"permissions,omitempty"`
}

// View 插件对外视图 = manifest + 持久化状态。
type View struct {
	Manifest
	State   string            `json:"state"`
	Message string            `json:"message,omitempty"`
	Config  map[string]string `json:"config,omitempty"`
}

// Manager 插件管理器。
type Manager struct {
	repo    *repository.Store
	src     *source.Client
	dataDir string
	ext     *extension.Registry
	catalog []Manifest
}

// NewManager 构造插件管理器，并把已启用插件的贡献注册进扩展注册表。
func NewManager(repo *repository.Store, src *source.Client, dataDir string, ext *extension.Registry) *Manager {
	m := &Manager{repo: repo, src: src, dataDir: dataDir, ext: ext, catalog: builtinCatalog()}
	m.Refresh()
	return m
}

// Refresh 重建扩展注册表内容：先注销全部内置插件，再注册启用中的插件。
// 任何状态变更（install/enable/disable/remove）后调用。
func (m *Manager) Refresh() {
	if m.ext == nil {
		return
	}
	for _, man := range m.catalog {
		m.ext.Unregister(man.ID)
	}
	states, err := m.repo.ListPluginStates()
	if err != nil {
		return
	}
	byID := make(map[string]model.PluginState, len(states))
	for _, s := range states {
		byID[s.ID] = s
	}
	for _, man := range m.catalog {
		st, ok := byID[man.ID]
		if !ok || st.State != "enabled" {
			continue
		}
		if p, err := providerFor(man); err == nil {
			m.ext.Register(p)
		}
	}
}

// List 返回全部插件（目录 + 状态合并）。
func (m *Manager) List() ([]View, error) {
	states, err := m.repo.ListPluginStates()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]model.PluginState, len(states))
	for _, s := range states {
		byID[s.ID] = s
	}
	out := make([]View, 0, len(m.catalog))
	for _, man := range m.catalog {
		v := View{Manifest: man, State: "available"}
		if st, ok := byID[man.ID]; ok {
			v.State = st.State
			v.Message = st.Message
			v.Config = st.Config
		}
		out = append(out, v)
	}
	return out, nil
}

// Get 返回单个插件。
func (m *Manager) Get(id string) (View, bool, error) {
	list, err := m.List()
	if err != nil {
		return View{}, false, err
	}
	for _, v := range list {
		if v.ID == id {
			return v, true, nil
		}
	}
	return View{}, false, nil
}

// Install 安装插件：有制品源则下载安装，否则登记为已安装（缺制品源时提示）。
func (m *Manager) Install(ctx context.Context, id string) (View, error) {
	man, ok := m.find(id)
	if !ok {
		return View{}, ErrNotFound
	}
	st, err := m.state(id)
	if err != nil {
		return View{}, err
	}
	msg := ""
	if art, ok := pickBinary(man); ok {
		data, err := m.src.Get(ctx, art.URL)
		if err != nil {
			return m.fail(id, "下载制品失败: "+err.Error())
		}
		if err := source.VerifySHA256(data, art.SHA256); err != nil {
			return m.fail(id, err.Error())
		}
		bin, err := source.ExtractBinary(data, man.ID)
		if err != nil {
			return m.fail(id, err.Error())
		}
		dest := filepath.Join(m.dataDir, "tools", man.ID, man.ID)
		if err := source.InstallAtomic(dest, bin); err != nil {
			return m.fail(id, err.Error())
		}
		msg = "制品已安装"
		// caddy-module:替换 active caddy 制品(备份原文件)——重启 Caddy 后生效。
		// 这是通用 kind 语义，核心不认具体插件。
		if man.Kind == "caddy-module" {
			mainBin := filepath.Join(m.dataDir, "tools", "caddy", "caddy")
			if _, statErr := os.Stat(mainBin); statErr == nil {
				_ = os.Rename(mainBin, mainBin+".bak")
			}
			if err := source.InstallAtomic(mainBin, bin); err != nil {
				return m.fail(id, "替换 caddy 二进制失败: "+err.Error())
			}
			msg = "制品已安装并替换主 caddy 二进制（请在「扩展 → 组件」重启 Caddy 后生效）"
		}
	} else {
		msg = "已登记（未配置制品源，未下载制品）"
	}
	st.State = "installed"
	st.Version = man.Version
	st.Channel = man.Channel
	st.Message = msg
	st.UpdatedAt = time.Now()
	if st.InstalledAt.IsZero() {
		st.InstalledAt = time.Now()
	}
	if err := m.repo.SavePluginState(st); err != nil {
		return View{}, err
	}
	m.Refresh()
	return m.view(man, *st), nil
}

// Enable 启用插件。
func (m *Manager) Enable(id string) (View, error) {
	return m.transition(id, "enabled", "已启用")
}

// Disable 停用插件（保留制品）。
func (m *Manager) Disable(id string) (View, error) {
	return m.transition(id, "disabled", "已停用（保留制品）")
}

// Remove 卸载插件（删制品 + 移除状态 + 注销贡献）。
func (m *Manager) Remove(id string) (View, error) {
	man, ok := m.find(id)
	if !ok {
		return View{}, ErrNotFound
	}
	_ = m.repo.DeletePluginState(id)
	m.Refresh()
	return m.view(man, model.PluginState{State: "available"}), nil
}

func (m *Manager) transition(id, state, msg string) (View, error) {
	man, ok := m.find(id)
	if !ok {
		return View{}, ErrNotFound
	}
	st, err := m.state(id)
	if err != nil {
		return View{}, err
	}
	if st.State == "available" && state == "enabled" {
		return View{}, ErrNotInstalled
	}
	st.State = state
	st.Message = msg
	st.UpdatedAt = time.Now()
	if err := m.repo.SavePluginState(st); err != nil {
		return View{}, err
	}
	m.Refresh()
	return m.view(man, *st), nil
}

func (m *Manager) state(id string) (*model.PluginState, error) {
	st, err := m.repo.GetPluginState(id)
	if errors.Is(err, repository.ErrNotFound) {
		return &model.PluginState{ID: id, State: "available"}, nil
	}
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (m *Manager) fail(id, msg string) (View, error) {
	man, _ := m.find(id)
	st, _ := m.state(id)
	st.State = "error"
	st.Message = msg
	st.UpdatedAt = time.Now()
	_ = m.repo.SavePluginState(st)
	m.Refresh()
	return m.view(man, *st), errors.New(msg)
}

func (m *Manager) view(man Manifest, st model.PluginState) View {
	return View{Manifest: man, State: st.State, Message: st.Message, Config: st.Config}
}

func (m *Manager) find(id string) (Manifest, bool) {
	for _, man := range m.catalog {
		if man.ID == id {
			return man, true
		}
	}
	return Manifest{}, false
}

// providerFor 把一个 manifest 的贡献编译为扩展提供者。
func providerFor(man Manifest) (extension.Provider, error) {
	p := extension.Provider{ID: man.ID, Version: man.Version, Renderers: map[string]extension.Renderer{}}
	for _, c := range man.Contributions.Capabilities {
		p.Capabilities = append(p.Capabilities, extension.Capability{
			ID: man.ID + "." + c.Point, Point: c.Point, Provider: man.ID, Version: man.Version, Meta: c.Data,
		})
	}
	for _, b := range man.Contributions.Backend {
		if b.Point == extension.PointRenderer && b.For != "" {
			if b.Impl["type"] == "template" {
				rd, err := extension.NewTemplateRenderer(man.ID+"."+b.For, b.Impl["template"])
				if err != nil {
					return extension.Provider{}, err
				}
				p.Renderers[b.For] = rd
			}
		}
	}
	return p, nil
}

// pickBinary 选择当前平台的主二进制制品。
func pickBinary(man Manifest) (Artifact, bool) {
	for _, a := range man.Artifacts {
		if a.Role != "" && a.Role != RoleBinary {
			continue
		}
		if a.OS != "" && a.OS != "linux" {
			continue
		}
		if a.Arch != "" && a.Arch != arch() {
			continue
		}
		if a.URL != "" {
			return a, true
		}
	}
	return Artifact{}, false
}

// 语义化错误。
var (
	ErrNotFound     = errors.New("插件不存在")
	ErrNotInstalled = errors.New("插件尚未安装")
)

func arch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

// caddyL4Template 是 Caddy-L4 插件随包携带的 renderer 模板（插件数据，非核心代码）。
//
// 核心只把中性 ProxyRule 交给注册表里的 renderer，对此模板内容一无所知。
const caddyL4Template = `layer4 {
{{- range .Rules }}
{{- $r := . }}
{{- range $r.Nets }}
{{- $net := . }}
{{- range $r.Ports }}
	{{ $net }}/:{{ . }} {
		route {
			proxy {{ $net }}/{{ $r.Upstream }}
		}
	}
{{- end }}
{{- end }}
{{- end }}
}`

func builtinCatalog() []Manifest {
	return []Manifest{
		{
			APIVersion: "gatebox/v2", Kind: "caddy-module", ID: "coraza", Name: "Coraza WAF",
			Version: "0.1.0", Channel: "official", Tags: []string{"Caddy插件"},
			Summary:  "OWASP CRS 规则的 Web 应用防火墙（需含 coraza 模块的 Caddy 制品）",
			Requires: Requires{Gatebox: ">=0.3.0", ExtensionAPI: ">=1", Components: []string{"caddy"}},
			Contributions: Contributions{
				UI: UIContributions{
					Nav:  []NavItem{{Path: "/plugins/coraza", Label: "WAF", Icon: "shield"}},
					Page: map[string]string{"type": "settings"},
				},
			},
		},
		{
			APIVersion: "gatebox/v2", Kind: "caddy-module", ID: "geoip", Name: "GeoIP",
			Version: "0.1.0", Channel: "official", Tags: []string{"Caddy插件"},
			Summary:  "按国家/地区做访问控制与分流（需含 geoip 模块的 Caddy 制品）",
			Requires: Requires{ExtensionAPI: ">=1", Components: []string{"caddy"}},
			Contributions: Contributions{
				UI: UIContributions{Page: map[string]string{"type": "settings"}},
			},
		},
		{
			APIVersion: "gatebox/v2", Kind: "caddy-module", ID: "caddy-l4", Name: "Caddy L4",
			Version: "0.1.0", Channel: "official", Tags: []string{"Caddy插件", "TCP/UDP"},
			Summary:  "为 Caddy 增加 TCP/UDP（L4）代理能力（Caddy 官方构建 API 动态构建，含 github.com/mholt/caddy-l4）",
			Requires: Requires{Gatebox: ">=0.3.0", ExtensionAPI: ">=1", Components: []string{"caddy"}},
			Artifacts: []Artifact{
				{Role: RoleBinary, OS: "linux", Arch: "amd64", URL: "https://caddyserver.com/api/download?os=linux&arch=amd64&p=github.com/mholt/caddy-l4"},
				{Role: RoleBinary, OS: "linux", Arch: "arm64", URL: "https://caddyserver.com/api/download?os=linux&arch=arm64&p=github.com/mholt/caddy-l4"},
			},
			Contributions: Contributions{
				Capabilities: []Contribution{
					{Point: extension.PointProxyProtocols, Data: map[string]any{
						"class": extension.ClassNonHTTP, "label": "TCP/UDP",
						"networks": []string{"tcp", "udp"}, "requiresPrimaryDomain": false,
					}},
				},
				Backend: []BackendContribution{
					{Point: extension.PointRenderer, For: extension.ClassNonHTTP, Scope: "global",
						Impl: map[string]string{"type": "template", "template": caddyL4Template}},
				},
				UI: UIContributions{
					Slots: []Slot{{Slot: "port-form.protocol-options", From: extension.PointProxyProtocols}},
					Page:  map[string]string{"type": "settings"},
				},
			},
			Permissions: []Permission{
				{Filesystem: map[string][]string{"write": {"tools/caddy-l4"}}},
			},
		},
		{
			APIVersion: "gatebox/v2", Kind: "config-only", ID: "realip", Name: "Real IP",
			Version: "0.1.0", Channel: "official", Tags: []string{"Caddy插件"},
			Summary:  "在 Caddyfile 注入 real_ip 指令，还原客户端真实地址",
			Requires: Requires{ExtensionAPI: ">=1", Components: []string{"caddy"}},
			Contributions: Contributions{
				UI: UIContributions{Page: map[string]string{"type": "settings"}},
			},
		},
	}
}
