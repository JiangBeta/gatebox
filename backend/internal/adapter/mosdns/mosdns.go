// Package mosdns 读写外部 mosdns 组件的运行配置(ADR-001:只改写配置 + 调 API)。
//
// 覆盖三类操作:
//   - 默认配置生成 / 解析(基础设置表单 ⇄ config.yaml);
//   - 内网解析记录(hosts 插件引用的 hosts.txt)增删改;
//   - 状态探测、日志读取与缓存刷新。
//
// mosdns 由 GateBox 组件运行时启动(见 component/registry.go 的 pid 托管),
// 本包只负责配置文件,不接管进程生命周期。
package mosdns

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// 默认值与文件名。配置相对路径以 mosdns 工作目录(--dir)为基准。
const (
	DefaultListen   = ":5335"
	DefaultAPIPort  = 9091
	defaultLogLevel = "info"
	defaultCacheCap = 10240

	configFileName = "config.yaml"
	hostsFileName  = "hosts.txt"
	logFileName    = "mosdns.log"
)

// Settings 基础设置表单模型(与前端 mosdns/settings 接口一一对应)。
type Settings struct {
	Listen    string   `json:"listen"`    // DNS 监听地址,如 ":5335" 或 "0.0.0.0:53"
	LogLevel  string   `json:"logLevel"`  // debug | info | warn | error
	LocalDNS  []string `json:"localDns"`  // 本地/国内上游
	RemoteDNS []string `json:"remoteDns"` // 远程上游
	Cache     bool     `json:"cache"`     // 是否启用 DNS 缓存
	CacheSize int      `json:"cacheSize"` // 缓存条数
}

// Host 一条内网解析记录(hosts 插件):一个域名对应一个或多个 IP。
type Host struct {
	Domain string   `json:"domain"`
	IPs    []string `json:"ips"`
}

// Manager mosdns 配置管理:运行目录固定为 $DATA_DIR/tools/mosdns。
type Manager struct {
	dir string
}

// NewManager 构造 Manager,dir 为 mosdns 运行目录(不存在时惰性创建)。
func NewManager(dir string) *Manager {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return &Manager{dir: dir}
}

// Dir 返回 mosdns 运行目录。
func (m *Manager) Dir() string { return m.dir }

// ConfigPath 返回 config.yaml 路径。
func (m *Manager) ConfigPath() string { return filepath.Join(m.dir, configFileName) }

// DefaultSettings 默认基础设置。
func DefaultSettings() Settings {
	return Settings{
		Listen:    DefaultListen,
		LogLevel:  defaultLogLevel,
		LocalDNS:  []string{"223.5.5.5", "119.29.29.29"},
		RemoteDNS: []string{"tls://8.8.8.8", "tls://1.1.1.1"},
		Cache:     true,
		CacheSize: defaultCacheCap,
	}
}

// --- 配置生成 ---

// yaml 结构:字段声明顺序即 marshal 顺序。
type yamlLog struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
}

type yamlAPI struct {
	HTTP string `yaml:"http"`
}

type plugin struct {
	Tag  string `yaml:"tag"`
	Type string `yaml:"type"`
	Args any    `yaml:"args"`
}

type hostsArgs struct {
	Files []string `yaml:"files"`
}

type cacheArgs struct {
	Size         int    `yaml:"size"`
	LazyCacheTTL int    `yaml:"lazy_cache_ttl"`
	DumpFile     string `yaml:"dump_file,omitempty"`
	DumpInterval int    `yaml:"dump_interval,omitempty"`
}

type upstream struct {
	Addr string `yaml:"addr"`
}

type forwardArgs struct {
	Concurrent int        `yaml:"concurrent"`
	Upstreams  []upstream `yaml:"upstreams"`
}

type seqExec struct {
	Matches any    `yaml:"matches,omitempty"` // string 或 []string
	Exec    string `yaml:"exec"`
}

type fallbackArgs struct {
	Primary       string `yaml:"primary"`
	Secondary     string `yaml:"secondary"`
	Threshold     int    `yaml:"threshold"`
	AlwaysStandby bool   `yaml:"always_standby"`
}

type serverArgs struct {
	Entry  string `yaml:"entry"`
	Listen string `yaml:"listen"`
}

type fileConfig struct {
	Log     yamlLog  `yaml:"log"`
	API     yamlAPI  `yaml:"api"`
	Include []string `yaml:"include"`
	Plugins []plugin `yaml:"plugins"`
}

// RenderConfig 依据基础设置生成完整的 mosdns config.yaml。
//
// 生成的主流程:hosts → 缓存 → (本地主用/远程备用)fallback。
// 不依赖 geosite/geoip 等外部数据文件,首次安装即可运行;
// 需要分流/广告拦截等高级策略时,请在「配置文件」页手工编辑。
func (m *Manager) RenderConfig(s Settings) ([]byte, error) {
	s = normalizeSettings(s)
	cfg := fileConfig{
		Log:     yamlLog{Level: s.LogLevel, File: m.defaultLogPath()},
		API:     yamlAPI{HTTP: fmt.Sprintf("127.0.0.1:%d", DefaultAPIPort)},
		Include: []string{},
	}

	plugins := []plugin{
		{Tag: "hosts", Type: "hosts", Args: hostsArgs{Files: []string{hostsFileName}}},
	}
	if s.Cache {
		plugins = append(plugins, plugin{Tag: "cache", Type: "cache", Args: cacheArgs{
			Size: s.CacheSize, LazyCacheTTL: 86400, DumpFile: "cache.dump", DumpInterval: 600,
		}})
	}
	plugins = append(plugins,
		plugin{Tag: "forward_local", Type: "forward", Args: forwardArgs{Concurrent: 2, Upstreams: toUpstreams(s.LocalDNS)}},
		plugin{Tag: "forward_remote", Type: "forward", Args: forwardArgs{Concurrent: 2, Upstreams: toUpstreams(s.RemoteDNS)}},
		plugin{Tag: "local_sequence", Type: "sequence", Args: []seqExec{{Exec: "$forward_local"}}},
		plugin{Tag: "remote_sequence", Type: "sequence", Args: []seqExec{{Exec: "prefer_ipv4"}, {Exec: "$forward_remote"}}},
		plugin{Tag: "fallback", Type: "fallback", Args: fallbackArgs{
			Primary: "local_sequence", Secondary: "remote_sequence", Threshold: 500, AlwaysStandby: true,
		}},
		plugin{Tag: "main_sequence", Type: "sequence", Args: mainSequence(s.Cache)},
		plugin{Tag: "udp_server", Type: "udp_server", Args: serverArgs{Entry: "main_sequence", Listen: s.Listen}},
		plugin{Tag: "tcp_server", Type: "tcp_server", Args: serverArgs{Entry: "main_sequence", Listen: s.Listen}},
	)
	cfg.Plugins = plugins

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

// mainSequence 生成主运行序列:hosts 命中即返回,再走缓存,最后 fallback(本地主用)。
func mainSequence(cache bool) []seqExec {
	out := []seqExec{
		{Exec: "$hosts"},
		{Matches: "has_resp", Exec: "accept"},
	}
	if cache {
		out = append(out,
			seqExec{Exec: "$cache"},
			seqExec{Matches: "has_resp", Exec: "accept"},
		)
	}
	out = append(out, seqExec{Exec: "$fallback"})
	return out
}

func toUpstreams(addrs []string) []upstream {
	out := make([]upstream, 0, len(addrs))
	for _, a := range addrs {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, upstream{Addr: a})
		}
	}
	return out
}

// normalizeSettings 补全空字段的默认值。
func normalizeSettings(s Settings) Settings {
	def := DefaultSettings()
	if strings.TrimSpace(s.Listen) == "" {
		s.Listen = def.Listen
	}
	switch s.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		s.LogLevel = def.LogLevel
	}
	if len(s.LocalDNS) == 0 {
		s.LocalDNS = def.LocalDNS
	}
	if len(s.RemoteDNS) == 0 {
		s.RemoteDNS = def.RemoteDNS
	}
	if s.CacheSize <= 0 {
		s.CacheSize = def.CacheSize
	}
	return s
}

// --- 配置读写 ---

// ReadConfig 读取 config.yaml 原文;文件不存在返回 ErrNotConfigured。
func (m *Manager) ReadConfig() ([]byte, error) {
	b, err := os.ReadFile(m.ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotConfigured
	}
	return b, err
}

// WriteConfig 校验并原子写入 config.yaml(保留一份 .bak 备份)。
func (m *Manager) WriteConfig(b []byte) error {
	if err := ValidateConfig(b); err != nil {
		return err
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	// 备份旧配置,仅当存在且内容不同。
	if old, err := os.ReadFile(m.ConfigPath()); err == nil && string(old) != string(b) {
		_ = os.WriteFile(m.ConfigPath()+".bak", old, 0o644)
	}
	tmp, err := os.CreateTemp(m.dir, ".config-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_ = tmp.Chmod(0o644)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, m.ConfigPath())
}

// ValidateConfig 校验 config.yaml 至少是合法 YAML 且插件 tag/type 齐备。
func ValidateConfig(b []byte) error {
	if len(strings.TrimSpace(string(b))) == 0 {
		return errors.New("配置内容为空")
	}
	var raw rawConfig
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("YAML 解析失败: %w", err)
	}
	if len(raw.Plugins) == 0 {
		return errors.New("配置缺少 plugins 列表")
	}
	seen := map[string]bool{}
	for i, p := range raw.Plugins {
		if strings.TrimSpace(p.Tag) == "" {
			return fmt.Errorf("第 %d 个插件缺少 tag", i+1)
		}
		if strings.TrimSpace(p.Type) == "" {
			return fmt.Errorf("插件 %s 缺少 type", p.Tag)
		}
		if seen[p.Tag] {
			return fmt.Errorf("插件 tag 重复: %s", p.Tag)
		}
		seen[p.Tag] = true
	}
	return nil
}

// --- 设置解析 ---

type rawConfig struct {
	Log struct {
		Level string `yaml:"level"`
		File  string `yaml:"file"`
	} `yaml:"log"`
	API struct {
		HTTP string `yaml:"http"`
	} `yaml:"api"`
	Plugins []struct {
		Tag  string `yaml:"tag"`
		Type string `yaml:"type"`
		// Args 形态随插件而异(sequence 为列表,forward/cache 为映射),故用 any 承载。
		Args any `yaml:"args"`
	} `yaml:"plugins"`
}

// argsMap 把插件 args 归一为映射(非映射返回 nil)。
func argsMap(args any) map[string]any {
	m, _ := args.(map[string]any)
	return m
}

// ParseSettings 从已有 config.yaml 抽取基础设置,供表单回填;缺失字段用默认值。
func ParseSettings(b []byte) (Settings, error) {
	s := DefaultSettings()
	var raw rawConfig
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return s, fmt.Errorf("YAML 解析失败: %w", err)
	}
	if lvl := strings.TrimSpace(raw.Log.Level); lvl != "" {
		s.LogLevel = lvl
	}
	// 缓存按实际配置判定,避免默认值造成假阳性。
	s.Cache = false
	for _, p := range raw.Plugins {
		args := argsMap(p.Args)
		switch {
		case p.Type == "cache":
			s.Cache = true
			if n := argInt(args, "size"); n > 0 {
				s.CacheSize = n
			}
		case p.Tag == "forward_local":
			if u := argUpstreams(args); len(u) > 0 {
				s.LocalDNS = u
			}
		case p.Tag == "forward_remote":
			if u := argUpstreams(args); len(u) > 0 {
				s.RemoteDNS = u
			}
		case p.Type == "udp_server" || p.Type == "tcp_server":
			if l := argString(args, "listen"); l != "" {
				s.Listen = l
			}
		}
	}
	return s, nil
}

// Settings 读取并解析当前配置;未生成配置时返回默认设置。
func (m *Manager) Settings() (Settings, error) {
	b, err := m.ReadConfig()
	if errors.Is(err, ErrNotConfigured) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return DefaultSettings(), err
	}
	return ParseSettings(b)
}

// argString 从插件 args 取字符串(容错:yaml 可能解析为其他标量)。
func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch v := args[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

// argInt 从插件 args 取整数。
func argInt(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case uint64:
		return int(v)
	default:
		return 0
	}
}

// argUpstreams 从 forward 插件 args 取 upstreams[].addr。
func argUpstreams(args map[string]any) []string {
	if args == nil {
		return nil
	}
	list, ok := args["upstreams"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if addr, ok := m["addr"].(string); ok && strings.TrimSpace(addr) != "" {
			out = append(out, strings.TrimSpace(addr))
		}
	}
	return out
}

// --- 路径解析 ---

// defaultLogPath 默认日志文件(绝对路径,避免相对目录歧义)。
func (m *Manager) defaultLogPath() string { return filepath.Join(m.dir, logFileName) }

// LogFile 解析配置中的日志文件路径,相对路径按 mosdns 工作目录展开。
func (m *Manager) LogFile() string {
	if b, err := m.ReadConfig(); err == nil {
		var raw rawConfig
		if yaml.Unmarshal(b, &raw) == nil {
			if p := strings.TrimSpace(raw.Log.File); p != "" {
				if !filepath.IsAbs(p) {
					p = filepath.Join(m.dir, p)
				}
				return p
			}
		}
	}
	return m.defaultLogPath()
}

// APIAddr 解析配置中的 api.http 地址,缺省 127.0.0.1:9091。
func (m *Manager) APIAddr() string {
	if b, err := m.ReadConfig(); err == nil {
		var raw rawConfig
		if yaml.Unmarshal(b, &raw) == nil {
			if a := strings.TrimSpace(raw.API.HTTP); a != "" {
				return a
			}
		}
	}
	return fmt.Sprintf("127.0.0.1:%d", DefaultAPIPort)
}

// CacheTag 返回配置中 cache 插件(用于刷新缓存)的 tag;无缓存插件返回空。
func (m *Manager) CacheTag() string {
	b, err := m.ReadConfig()
	if err != nil {
		return ""
	}
	var raw rawConfig
	if yaml.Unmarshal(b, &raw) != nil {
		return ""
	}
	for _, p := range raw.Plugins {
		if p.Type == "cache" {
			return p.Tag
		}
	}
	return ""
}

// HostsPath 解析 hosts 插件引用的文件,相对路径按工作目录展开;无插件时用默认值。
func (m *Manager) HostsPath() string {
	if b, err := m.ReadConfig(); err == nil {
		var raw rawConfig
		if yaml.Unmarshal(b, &raw) == nil {
			for _, p := range raw.Plugins {
				if p.Type != "hosts" {
					continue
				}
				if files, ok := argsMap(p.Args)["files"].([]any); ok && len(files) > 0 {
					if f, ok := files[0].(string); ok && strings.TrimSpace(f) != "" {
						if filepath.IsAbs(f) {
							return f
						}
						return filepath.Join(m.dir, f)
					}
				}
			}
		}
	}
	return filepath.Join(m.dir, hostsFileName)
}

// --- 内网解析记录(hosts) ---

// ReadHosts 读取 hosts 文件;文件不存在返回空列表。
func (m *Manager) ReadHosts() ([]Host, error) {
	b, err := os.ReadFile(m.HostsPath())
	if errors.Is(err, os.ErrNotExist) {
		return []Host{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseHosts(string(b)), nil
}

// parseHosts 解析 hosts 文本(mosdns hosts 格式:域名在前,IP 在后,支持一行多个)。
func parseHosts(content string) []Host {
	out := []Host{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, Host{Domain: fields[0], IPs: append([]string{}, fields[1:]...)})
	}
	return out
}

// WriteHosts 校验并原子写入 hosts 文件(按 mosdns hosts 格式)。
func (m *Manager) WriteHosts(hosts []Host) error {
	var sb strings.Builder
	sb.WriteString("# GateBox 内网解析记录(域名 IP...,支持一行多个 IP / IPv6)\n")
	for _, h := range hosts {
		domain := strings.TrimSpace(h.Domain)
		if domain == "" {
			return errors.New("域名不能为空")
		}
		if strings.ContainsAny(domain, " \t") {
			return fmt.Errorf("域名 %q 含空白字符", domain)
		}
		if len(h.IPs) == 0 {
			return fmt.Errorf("域名 %s 至少需要一个 IP", domain)
		}
		ips := make([]string, 0, len(h.IPs))
		for _, raw := range h.IPs {
			ip := strings.TrimSpace(raw)
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("域名 %s 的 IP 不合法: %s", domain, raw)
			}
			ips = append(ips, ip)
		}
		sb.WriteString(domain)
		sb.WriteByte(' ')
		sb.WriteString(strings.Join(ips, " "))
		sb.WriteByte('\n')
	}

	path := m.HostsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hosts-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_ = tmp.Chmod(0o644)
	if _, err := tmp.WriteString(sb.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// --- 日志 ---

// ReadLog 读取日志尾部(最多 maxBytes,至少保留整行)。
func (m *Manager) ReadLog(maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 256 << 10
	}
	f, err := os.Open(m.LogFile())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := st.Size()
	offset := int64(0)
	if size > maxBytes {
		offset = size - maxBytes
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text := string(buf)
	// 从中间截断时丢弃首个不完整行。
	if offset > 0 {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	return text, nil
}

// ClearLog 清空日志文件(不存在则忽略)。
func (m *Manager) ClearLog() error {
	err := os.Truncate(m.LogFile(), 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// --- 缓存刷新 ---

// FlushCache 经 mosdns 插件 API 清空缓存(需配置中启用 cache 插件且 api.http 可达)。
func (m *Manager) FlushCache(ctx context.Context) error {
	tag := m.CacheTag()
	if tag == "" {
		return ErrNoCache
	}
	url := fmt.Sprintf("http://%s/plugins/%s/flush", m.APIAddr(), tag)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("mosdns API 不可达(%s): %w", m.APIAddr(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("刷新缓存失败(%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// ErrNotConfigured 配置尚未生成。
var ErrNotConfigured = errors.New("mosdns 配置尚未生成")

// ErrNoCache 配置中未启用缓存插件。
var ErrNoCache = errors.New("未启用缓存插件，无法刷新缓存")
