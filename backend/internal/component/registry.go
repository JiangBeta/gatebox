package component

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/JiangBeta/gatebox/internal/source"
)

// Info 组件对外视图 = 静态描述 + 版本 + 运行态。
type Info struct {
	Descriptor
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Status          Status `json:"status"`
	Repo            string `json:"repo,omitempty"`
}

// core 一个内置组件的描述与探测方式。
type core struct {
	desc        Descriptor
	bin         string
	versionArgs []string
	repo        string
	assetHint   string
}

// CoreRegistry 内置组件注册表（核心 + 可选的系统探测）。
type CoreRegistry struct {
	dataDir string
	src     *source.Client
	cores   []core
}

// NewCoreRegistry 构造内置组件注册表。
func NewCoreRegistry(dataDir, caddyBin string, src *source.Client) *CoreRegistry {
	tools := func(p ...string) string { return filepath.Join(append([]string{dataDir, "tools"}, p...)...) }
	caps := func(v ...string) []string { return v }
	cores := []core{
		{
			desc: Descriptor{ID: "caddy", Name: "Caddy", Kind: KindCore, Tier: "core",
				Provision: "managed", Runtime: "manage", Upgrade: "replace",
				Capabilities: caps("health", "config", "upgradable", "logs"), DefaultEnabled: true,
				Source: Source{Channel: "official"}},
			bin: caddyBin, versionArgs: []string{"version"}, repo: "caddyserver/caddy", assetHint: "caddy",
		},
		{
			desc: Descriptor{ID: "acme", Name: "acme.sh", Kind: KindCore, Tier: "core",
				Provision: "managed", Runtime: "manage", Upgrade: "replace",
				Capabilities: caps("health", "config", "upgradable"), DefaultEnabled: true,
				Source: Source{Channel: "official"}},
			bin: tools("acme", "acme.sh"), versionArgs: []string{"--version"}, repo: "acmesh-official/acme.sh", assetHint: "acme.sh",
		},
		{
			desc: Descriptor{ID: "docker-compose", Name: "docker-compose", Kind: KindCore, Tier: "core",
				Provision: "managed", Runtime: "observe", Upgrade: "replace",
				Capabilities: caps("health", "upgradable"), DefaultEnabled: true,
				Source: Source{Channel: "official"}},
			bin: tools("docker-compose", "docker-compose"), versionArgs: []string{"version"}, repo: "docker/compose", assetHint: "docker-compose",
		},
		{
			desc: Descriptor{ID: "docker", Name: "Docker Engine", Kind: KindCore, Tier: "core",
				Provision: "attached", Runtime: "observe", Upgrade: "system",
				Capabilities: caps("health", "operable"), DefaultEnabled: true,
				Source: Source{Channel: "system"}},
			bin: "/var/run/docker.sock",
		},
		{
			desc: Descriptor{ID: "ddns-go", Name: "ddns-go", Kind: KindProcess, Tier: "optional",
				Provision: "managed", Runtime: "manage", Upgrade: "replace", Bundled: true,
				Capabilities: caps("health", "config", "upgradable", "logs"), Source: Source{Channel: "official"}},
			bin: tools("ddnsgo", "ddns-go"), versionArgs: []string{"-v"}, repo: "jeessy2/ddns-go", assetHint: "ddns-go",
		},
		{
			desc: Descriptor{ID: "mosdns", Name: "mosdns", Kind: KindProcess, Tier: "optional",
				Provision: "managed", Runtime: "manage", Upgrade: "replace",
				Capabilities: caps("health", "config", "upgradable", "logs"), Source: Source{Channel: "official"}},
			bin: tools("mosdns", "mosdns"), versionArgs: []string{"version"}, repo: "IrineSistiana/mosdns", assetHint: "mosdns",
		},
		{
			desc: Descriptor{ID: "tailscale", Name: "Tailscale", Kind: KindProcess, Tier: "optional",
				Provision: "attached", Runtime: "observe", Upgrade: "system",
				Capabilities: caps("health", "operable"), Source: Source{Channel: "system"}},
			bin: "/usr/bin/tailscale", versionArgs: []string{"version"},
		},
		{
			desc: Descriptor{ID: "flame", Name: "Flame", Kind: KindProcess, Tier: "optional",
				Provision: "managed", Runtime: "manage", Upgrade: "replace", Bundled: true,
				Capabilities: caps("health", "upgradable", "logs"), Source: Source{Channel: "official"}},
			bin: tools("flame", "flame"), repo: "xiangsx/flame", assetHint: "flame",
		},
	}
	return &CoreRegistry{dataDir: dataDir, src: src, cores: cores}
}

// Get 返回组件视图。
func (r *CoreRegistry) Get(ctx context.Context, id string) (Info, bool) {
	for _, c := range r.cores {
		if c.desc.ID == id {
			return r.info(ctx, c), true
		}
	}
	return Info{}, false
}

// List 返回全部组件视图。
func (r *CoreRegistry) List(ctx context.Context) []Info {
	out := make([]Info, 0, len(r.cores))
	for _, c := range r.cores {
		out = append(out, r.info(ctx, c))
	}
	return out
}

func (r *CoreRegistry) info(ctx context.Context, c core) Info {
	info := Info{Descriptor: c.desc, Repo: c.repo}
	cur, status := probe(ctx, c)
	info.Current = cur
	info.Status = status
	return info
}

// CheckUpdate 查询源通道最新版本（GitHub 官方源）。
func (r *CoreRegistry) CheckUpdate(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, os.ErrNotExist
	}
	info := r.info(ctx, c)
	if c.repo == "" {
		return info, nil
	}
	rel, err := r.src.GitHubLatest(ctx, c.repo)
	if err != nil {
		return info, err
	}
	info.Latest = rel.Version
	info.UpdateAvailable = curNeedUpdate(info.Current, rel.Version)
	return info, nil
}

// Upgrade 用源通道最新制品原子替换（仅 replace 策略）。
func (r *CoreRegistry) Upgrade(ctx context.Context, id string) (Info, error) {
	c, ok := r.find(id)
	if !ok {
		return Info{}, os.ErrNotExist
	}
	info := r.info(ctx, c)
	if c.desc.Upgrade != "replace" {
		return info, errSystemManaged
	}
	if c.repo == "" {
		return info, errNoSource
	}
	rel, err := r.src.GitHubLatest(ctx, c.repo)
	if err != nil {
		return info, err
	}
	asset, ok := source.PickAsset(rel.Assets)
	if !ok {
		return info, errNoAsset
	}
	data, err := r.src.Get(ctx, asset.URL)
	if err != nil {
		return info, err
	}
	if err := source.VerifySHA256(data, asset.SHA256); err != nil {
		return info, err
	}
	bin, err := source.ExtractBinary(data, c.assetHint)
	if err != nil {
		return info, err
	}
	if c.bin == "" {
		return info, errNoBinPath
	}
	if err := source.InstallAtomic(c.bin, bin); err != nil {
		return info, err
	}
	info.Latest = rel.Version
	out := r.info(ctx, c)
	out.Latest = rel.Version
	out.UpdateAvailable = false
	return out, nil
}

func (r *CoreRegistry) find(id string) (core, bool) {
	for _, c := range r.cores {
		if c.desc.ID == id {
			return c, true
		}
	}
	return core{}, false
}

// probe 探测当前版本与运行态。
func probe(ctx context.Context, c core) (string, Status) {
	st := Status{State: "unknown"}
	if c.bin == "" {
		return "", st
	}
	if _, err := os.Stat(c.bin); err != nil {
		st.State = "stopped"
		st.Message = "未安装或路径不存在"
		return "", st
	}
	st.State = "running"
	st.Healthy = true
	if len(c.versionArgs) == 0 {
		return "", st
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, c.bin, c.versionArgs...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", st
	}
	return source.ParseVersion(string(out)), st
}

func curNeedUpdate(cur, latest string) bool {
	if cur == "" || latest == "" {
		return false
	}
	return source.CompareVersions(cur, latest) < 0
}

// 语义化错误，供 handler 判断。
var (
	errSystemManaged = &Err{Code: "SYSTEM_MANAGED", Msg: "该组件由系统/包管理器管理，请使用 apt/opkg 等升级"}
	errNoSource      = &Err{Code: "NO_SOURCE", Msg: "该组件未配置制品源，无法自动升级"}
	errNoAsset       = &Err{Code: "NO_ASSET", Msg: "源中未找到匹配当前架构的制品"}
	errNoBinPath     = &Err{Code: "NO_BIN", Msg: "组件未配置可执行文件路径"}
)

// Err 带错误码的语义化错误。
type Err struct {
	Code string
	Msg  string
}

func (e *Err) Error() string { return e.Msg }

// AsErr 提取语义化错误。
func AsErr(err error) (*Err, bool) {
	var e *Err
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
