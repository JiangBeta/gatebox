// Package plugin 提供插件引擎：manifest v2 描述 + 内置目录 + 生命周期状态机 + 贡献注册。
//
// 设计见 docs/adr/ADR-036（扩展平台）与 docs/architecture.md §4。核心不认插件身份，
// 启用中的插件把 capabilities/renderer 贡献注册进扩展注册表，供核心查表消费。
package plugin

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/source"
	"gopkg.in/yaml.v3"
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
	// builtin 随包内置的插件目录（在线目录合并时的基底）。
	builtin []Manifest
	// catalog 当前生效目录：builtin + 在线索引合并。
	catalog []Manifest
	// coreURL 控制面自身地址（注入 sidecar，供其调用投影 API）。
	coreURL string
	// catalogURL 在线静态索引地址（拉取在线插件目录，ADR-037 §7）。
	catalogURL string
	// gateboxVersion 宿主版本，用于 requires.gatebox 兼容过滤。
	gateboxVersion string
}

// SetCoreURL 注入控制面基址（如 http://127.0.0.1:8099）。
func (m *Manager) SetCoreURL(u string) { m.coreURL = u }

// SetCatalogURL 注入在线静态索引地址。
func (m *Manager) SetCatalogURL(u string) { m.catalogURL = u }

// SetGateboxVersion 注入宿主版本（requires.gatebox 校验用）。
func (m *Manager) SetGateboxVersion(v string) { m.gateboxVersion = v }

// RefreshOnline 拉取在线索引并把其中的插件目录合并进 catalog（内置优先）。
//
// 仅接受满足 requires（gatebox/extensionApi）的插件；失败时保持现有目录不变。
func (m *Manager) RefreshOnline(ctx context.Context) error {
	if m.catalogURL == "" || m.src == nil {
		return nil
	}
	cat, err := m.src.FetchCatalog(ctx, m.catalogURL)
	if err != nil {
		return err
	}
	merged := make([]Manifest, 0, len(m.builtin)+len(cat.Plugins))
	merged = append(merged, m.builtin...)
	seen := map[string]bool{}
	for _, b := range m.builtin {
		seen[b.ID] = true
	}
	for _, p := range cat.Plugins {
		if p.ID == "" || seen[p.ID] {
			continue
		}
		ver, ok := p.Latest()
		if !ok || len(ver.Manifest) == 0 {
			continue
		}
		var man Manifest
		if err := json.Unmarshal(ver.Manifest, &man); err != nil {
			continue
		}
		if man.ID == "" {
			man.ID = p.ID
		}
		if man.Version == "" {
			man.Version = ver.Version
		}
		if !m.compatible(man) {
			continue
		}
		seen[man.ID] = true
		merged = append(merged, man)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	m.catalog = merged
	m.Refresh()
	return nil
}

// compatible 校验 requires.gatebox / requires.extensionApi。
func (m *Manager) compatible(man Manifest) bool {
	if !apiSatisfies(man.Requires.ExtensionAPI, ExtensionAPIVersion) {
		return false
	}
	if !minSatisfies(man.Requires.Gatebox, m.gateboxVersion) {
		return false
	}
	return true
}

// ExtensionAPIVersion 当前扩展 API 版本（ADR-037 §5）。
const ExtensionAPIVersion = 1

// apiSatisfies 解析 `>=N` 形式的 extensionApi 约束。
func apiSatisfies(req string, cur int) bool {
	req = strings.TrimSpace(req)
	if req == "" {
		return true
	}
	req = strings.TrimSpace(strings.TrimPrefix(req, ">="))
	n, err := strconv.Atoi(req)
	if err != nil {
		return true // 无法解析的约束不阻塞
	}
	return cur >= n
}

// minSatisfies 解析 `>=x` 形式的 gatebox 版本约束（ver 为空时放行）。
func minSatisfies(req, ver string) bool {
	req = strings.TrimSpace(req)
	if req == "" || ver == "" {
		return true
	}
	req = strings.TrimSpace(strings.TrimPrefix(req, ">="))
	if req == "" {
		return true
	}
	return source.CompareVersions(ver, req) >= 0
}

// NewManager 构造插件管理器，并把已启用插件的贡献注册进扩展注册表。
func NewManager(repo *repository.Store, src *source.Client, dataDir string, ext *extension.Registry) *Manager {
	builtin := builtinCatalog()
	m := &Manager{repo: repo, src: src, dataDir: dataDir, ext: ext, builtin: builtin, catalog: builtin}
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

// UIDir 返回插件 UI 制品目录；未安装 UI 时返回空串。
//
// UI 制品由内核静态托管于 /plugins/<id>/（ADR-039 §3），插件不自行对外暴露端口。
func (m *Manager) UIDir(id string) string {
	dir := filepath.Join(m.dataDir, "tools", id, "ui")
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir
	}
	return ""
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
	// 分配 plugin token（process 类另分配回环端口）。
	m.ensureTokenPort(st, man.Kind == "process")

	msg := "已登记（未配置制品源，未下载制品）"
	if len(man.Artifacts) > 0 {
		if err := m.installArtifacts(ctx, man); err != nil {
			return m.fail(id, err.Error())
		}
		msg = "制品已安装"
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

// Enable 启用插件；process 类同时拉起 sidecar（ADR-039 §1）。
func (m *Manager) Enable(id string) (View, error) {
	v, err := m.transition(id, "enabled", "已启用")
	if err != nil {
		return v, err
	}
	if err := m.StartSidecar(id); err != nil {
		return m.fail(id, "启动插件后端失败: "+err.Error())
	}
	return v, nil
}

// Disable 停用插件（保留制品与配置，停止 sidecar）。
func (m *Manager) Disable(id string) (View, error) {
	_ = m.StopSidecar(id)
	return m.transition(id, "disabled", "已停用（保留制品）")
}

// Remove 卸载插件：删制品 + 注销贡献 + 停止 sidecar；配置与密钥保留（ADR-037 §4）。
func (m *Manager) Remove(id string) (View, error) {
	man, ok := m.find(id)
	if !ok {
		return View{}, ErrNotFound
	}
	_ = m.StopSidecar(id)
	// 删除制品目录（tools/<id>/）。
	_ = os.RemoveAll(filepath.Join(m.dataDir, "tools", id))
	// 保留用户配置与密钥：状态退回 available 而非删除。
	if prev, err := m.repo.GetPluginState(id); err == nil {
		prev.State = "available"
		prev.Message = "已卸载（配置保留）"
		prev.Port = 0
		prev.UpdatedAt = time.Now()
		_ = m.repo.SavePluginState(prev)
	} else {
		_ = m.repo.DeletePluginState(id)
	}
	m.Refresh()
	return m.view(man, model.PluginState{State: "available"}), nil
}

// PermissionsOfToken 按 plugin token 解析插件 id 与其 manifest 权限（ADR-039 §2）。
//
// 供投影 API 鉴权与 scope 校验；token 不复用管理员 session。
func (m *Manager) PermissionsOfToken(token string) (string, []Permission, bool) {
	if token == "" {
		return "", nil, false
	}
	states, err := m.repo.ListPluginStates()
	if err != nil {
		return "", nil, false
	}
	for _, st := range states {
		if st.Token == "" || st.Token != token {
			continue
		}
		if man, ok := m.find(st.ID); ok {
			return st.ID, man.Permissions, true
		}
		return st.ID, nil, true
	}
	return "", nil, false
}

// Purge 彻底删除插件：删制品 + 删配置与密钥，不可恢复（ADR-037 §4）。
func (m *Manager) Purge(id string) (View, error) {
	man, ok := m.find(id)
	if !ok {
		return View{}, ErrNotFound
	}
	_ = m.StopSidecar(id)
	_ = os.RemoveAll(filepath.Join(m.dataDir, "tools", id))
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

//go:embed builtin/*.yaml
var builtinFS embed.FS

// builtinCatalog 加载随包内置的插件 manifest（磁盘 YAML，非 Go 硬编码）。
//
// ADR-037 §7：内置插件以 manifest 文件随包分发，运行时解析加载；在线插件从静态索引拉取，
// 二者走同一解析与状态机。核心不再包含任何插件业务代码。
func builtinCatalog() []Manifest {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil
	}
	out := make([]Manifest, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			continue
		}
		m, err := ParseManifestYAML(b)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ParseManifestYAML 解析 YAML manifest。
//
// 经 JSON 桥接复用 `json` 标签，保证 YAML 与 JSON 两种表示的语义完全一致；
// GateBoxStore 的 manifest.yaml 与此同源。
func ParseManifestYAML(b []byte) (Manifest, error) {
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return Manifest{}, err
	}
	jb, err := json.Marshal(raw)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(jb, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
