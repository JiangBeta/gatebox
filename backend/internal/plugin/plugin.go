// Package plugin 提供插件引擎：manifest 描述 + 内置目录 + 生命周期状态机。
//
// 设计见 docs/architecture.md §4 与 ADR-029（数据驱动；v1 不引入解释器/子进程协议）。
package plugin

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"time"

	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/source"
)

// Requires 插件对宿主与 GateBox 版本的依赖。
type Requires struct {
	Gatebox    string   `json:"gatebox,omitempty"`
	Components []string `json:"components,omitempty"`
}

// Artifact 某架构的制品。
type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
}

// Contribution 插件对 UI 的贡献点（v1：导航 + 设置表单）。
type Contribution struct {
	Nav  []NavItem         `json:"nav,omitempty"`
	Page map[string]string `json:"page,omitempty"`
}

// NavItem 导航入口。
type NavItem struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
}

// Manifest 插件声明（v1 字段集）。
type Manifest struct {
	APIVersion    string              `json:"apiVersion"`
	Kind          string              `json:"kind"` // caddy-module | process | config-only
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Version       string              `json:"version"`
	Summary       string              `json:"summary"`
	Requires      Requires            `json:"requires,omitempty"`
	Channel       string              `json:"channel"`
	Artifacts     map[string]Artifact `json:"artifacts,omitempty"`
	Contributions Contribution        `json:"contributions,omitempty"`
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
	catalog []Manifest
}

// NewManager 构造插件管理器。
func NewManager(repo *repository.Store, src *source.Client, dataDir string) *Manager {
	return &Manager{repo: repo, src: src, dataDir: dataDir, catalog: builtinCatalog()}
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
	if art, ok := man.Artifacts["linux-"+arch()]; ok && art.URL != "" {
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

// Remove 卸载插件（删制品 + 移除状态）。
func (m *Manager) Remove(id string) (View, error) {
	man, ok := m.find(id)
	if !ok {
		return View{}, ErrNotFound
	}
	_ = m.repo.DeletePluginState(id)
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

func builtinCatalog() []Manifest {
	return []Manifest{
		{
			APIVersion: "gatebox/v1", Kind: "caddy-module", ID: "coraza", Name: "Coraza WAF",
			Version: "0.1.0", Channel: "official",
			Summary:  "OWASP CRS 规则的 Web 应用防火墙（需含 coraza 模块的 Caddy 制品）",
			Requires: Requires{Gatebox: ">=0.3.0", Components: []string{"caddy"}},
			Contributions: Contribution{
				Nav:  []NavItem{{Path: "/plugins/coraza", Label: "WAF", Icon: "shield"}},
				Page: map[string]string{"type": "settings"},
			},
		},
		{
			APIVersion: "gatebox/v1", Kind: "caddy-module", ID: "geoip", Name: "GeoIP",
			Version: "0.1.0", Channel: "official",
			Summary:  "按国家/地区做访问控制与分流（需含 geoip 模块的 Caddy 制品）",
			Requires: Requires{Components: []string{"caddy"}},
			Contributions: Contribution{
				Page: map[string]string{"type": "settings"},
			},
		},
		{
			APIVersion: "gatebox/v1", Kind: "config-only", ID: "realip", Name: "Real IP",
			Version: "0.1.0", Channel: "official",
			Summary:  "在 Caddyfile 注入 real_ip 指令，还原客户端真实地址",
			Requires: Requires{Components: []string{"caddy"}},
			Contributions: Contribution{
				Page: map[string]string{"type": "settings"},
			},
		},
	}
}
