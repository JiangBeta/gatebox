package mosdns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderConfigRoundTrip(t *testing.T) {
	m := NewManager(t.TempDir())
	s := Settings{
		Listen:    "0.0.0.0:5335",
		LogLevel:  "debug",
		LocalDNS:  []string{"223.5.5.5", "119.29.29.29"},
		RemoteDNS: []string{"tls://8.8.8.8"},
		Cache:     true,
		CacheSize: 2048,
	}
	b, err := m.RenderConfig(s)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if err := ValidateConfig(b); err != nil {
		t.Fatalf("生成的配置未通过校验: %v", err)
	}

	got, err := ParseSettings(b)
	if err != nil {
		t.Fatalf("ParseSettings: %v", err)
	}
	if got.Listen != s.Listen || got.LogLevel != s.LogLevel || got.CacheSize != s.CacheSize || !got.Cache {
		t.Errorf("往返设置不一致: got %+v want %+v", got, s)
	}
	if len(got.LocalDNS) != 2 || got.LocalDNS[0] != "223.5.5.5" {
		t.Errorf("LocalDNS 往返错误: %v", got.LocalDNS)
	}
	if len(got.RemoteDNS) != 1 || got.RemoteDNS[0] != "tls://8.8.8.8" {
		t.Errorf("RemoteDNS 往返错误: %v", got.RemoteDNS)
	}
}

func TestRenderConfigCacheDisabled(t *testing.T) {
	m := NewManager(t.TempDir())
	b, err := m.RenderConfig(Settings{Cache: false})
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if strings.Contains(string(b), "type: cache") {
		t.Error("关闭缓存时不应生成 cache 插件")
	}
	if got, _ := ParseSettings(b); got.Cache {
		t.Error("关闭缓存后 ParseSettings 仍返回 Cache=true")
	}
	// 关闭缓存时无 cache tag 可刷新。
	if err := m.writeRaw(b); err != nil {
		t.Fatal(err)
	}
	if tag := m.CacheTag(); tag != "" {
		t.Errorf("未启用缓存时 CacheTag 应为空, got %q", tag)
	}
}

// writeRaw 直接落盘原始配置(测试辅助)。
func (m *Manager) writeRaw(b []byte) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.ConfigPath(), b, 0o644)
}

func TestValidateConfigErrors(t *testing.T) {
	cases := map[string]string{
		"空内容":       "",
		"无 plugins": "log:\n  level: info\n",
		"缺 tag":     "plugins:\n  - type: cache\n    args: {}\n",
		"缺 type":    "plugins:\n  - tag: cache\n    args: {}\n",
		"tag 重复":    "plugins:\n  - tag: a\n    type: cache\n  - tag: a\n    type: hosts\n",
	}
	for name, content := range cases {
		if err := ValidateConfig([]byte(content)); err == nil {
			t.Errorf("%s: 期望报错, 实际通过", name)
		}
	}
}

func TestHostsRoundTrip(t *testing.T) {
	m := NewManager(t.TempDir())
	// 未生成配置时使用默认 hosts 路径。
	if got := m.HostsPath(); got != filepath.Join(m.Dir(), hostsFileName) {
		t.Fatalf("默认 hosts 路径错误: %s", got)
	}
	in := []Host{
		{Domain: "router.lan", IPs: []string{"192.168.1.1"}},
		{Domain: "nas.home", IPs: []string{"192.168.1.10", "2001:db8::1"}},
	}
	if err := m.WriteHosts(in); err != nil {
		t.Fatalf("WriteHosts: %v", err)
	}
	out, err := m.ReadHosts()
	if err != nil {
		t.Fatalf("ReadHosts: %v", err)
	}
	if len(out) != 2 || out[0].Domain != "router.lan" || out[0].IPs[0] != "192.168.1.1" {
		t.Fatalf("往返 hosts 不一致: %+v", out)
	}
	if len(out[1].IPs) != 2 || out[1].IPs[1] != "2001:db8::1" {
		t.Fatalf("IPv6 往返错误: %+v", out[1])
	}
}

func TestWriteHostsValidation(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.WriteHosts([]Host{{Domain: "", IPs: []string{"1.1.1.1"}}}); err == nil {
		t.Error("空域名应报错")
	}
	if err := m.WriteHosts([]Host{{Domain: "a.lan", IPs: nil}}); err == nil {
		t.Error("无 IP 应报错")
	}
	if err := m.WriteHosts([]Host{{Domain: "a.lan", IPs: []string{"not-an-ip"}}}); err == nil {
		t.Error("非法 IP 应报错")
	}
}

func TestParseHostsSkipsCommentsAndBlank(t *testing.T) {
	content := "# 注释\n\nrouter.lan 192.168.1.1 # 行内注释\nbad-line\n"
	out := parseHosts(content)
	if len(out) != 1 || out[0].Domain != "router.lan" || len(out[0].IPs) != 1 {
		t.Fatalf("解析结果不符: %+v", out)
	}
}

func TestHostsPathFromConfig(t *testing.T) {
	m := NewManager(t.TempDir())
	cfg := "plugins:\n  - tag: hosts\n    type: hosts\n    args:\n      files:\n        - rule/my-hosts.txt\n"
	if err := m.writeRaw([]byte(cfg)); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(m.Dir(), "rule", "my-hosts.txt")
	if got := m.HostsPath(); got != want {
		t.Errorf("HostsPath=%s want %s", got, want)
	}
}

func TestReadLogTail(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := os.MkdirAll(m.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("line-")
		sb.WriteString(strings.Repeat("x", 40))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(m.LogFile(), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := m.ReadLog(300)
	if err != nil {
		t.Fatalf("ReadLog: %v", err)
	}
	if len(got) == 0 || len(got) > 300 {
		t.Fatalf("尾部读取长度异常: %d", len(got))
	}
	if strings.HasPrefix(got, "xxx") {
		t.Error("尾部读取应以完整行开头")
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("尾部读取末尾应保留换行")
	}
}

func TestReadLogMissing(t *testing.T) {
	m := NewManager(t.TempDir())
	got, err := m.ReadLog(100)
	if err != nil || got != "" {
		t.Fatalf("日志缺失应返回空: got=%q err=%v", got, err)
	}
	if err := m.ClearLog(); err != nil {
		t.Fatalf("清空不存在的日志应忽略: %v", err)
	}
}

func TestFlushCacheNoCachePlugin(t *testing.T) {
	m := NewManager(t.TempDir())
	b, _ := m.RenderConfig(Settings{Cache: false})
	if err := m.writeRaw(b); err != nil {
		t.Fatal(err)
	}
	if err := m.FlushCache(context.Background()); !errors.Is(err, ErrNoCache) {
		t.Fatalf("期望 ErrNoCache, got %v", err)
	}
}

func TestSettingsMissingConfig(t *testing.T) {
	m := NewManager(t.TempDir())
	s, err := m.ReadSettings()
	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	if s.Listen != DefaultListen || !s.Cache {
		t.Fatalf("缺配置时应返回默认设置: %+v", s)
	}
}

func TestAPIDefaults(t *testing.T) {
	m := NewManager(t.TempDir())
	if got := m.APIAddr(); got != "127.0.0.1:9091" {
		t.Errorf("默认 API 地址错误: %s", got)
	}
	if got := m.LogFile(); got != filepath.Join(m.Dir(), logFileName) {
		t.Errorf("默认日志路径错误: %s", got)
	}
}

func TestRuleReadWrite(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.WriteRule("whitelist", "example.com\nfull:foo.bar"); err != nil {
		t.Fatalf("WriteRule: %v", err)
	}
	got, err := m.ReadRule("whitelist")
	if err != nil {
		t.Fatalf("ReadRule: %v", err)
	}
	if !strings.Contains(got, "example.com") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("规则内容不符: %q", got)
	}
	if err := m.WriteRule("nope", "x"); !errors.Is(err, ErrBadRule) {
		t.Fatalf("未知规则应报 ErrBadRule, got %v", err)
	}
	// 缺失文件读取为空。
	if s, err := m.ReadRule("blocklist"); err != nil || s != "" {
		t.Fatalf("缺失规则应返回空: %q %v", s, err)
	}
	if got := len(GetRuleMeta()); got < 8 {
		t.Fatalf("规则清单应补全(>=8), got %d", got)
	}
}

func TestWriteConfigCreatesDataFiles(t *testing.T) {
	m := NewManager(t.TempDir())
	b, err := m.RenderConfig(DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WriteConfig(b); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	for _, f := range append([]string{hostsFileName}, geoFileNames...) {
		if _, err := os.Stat(filepath.Join(m.Dir(), f)); err != nil {
			t.Errorf("数据文件未创建: %s (%v)", f, err)
		}
	}
	for _, r := range GetRuleMeta() {
		if _, err := os.Stat(m.RulePath(r.Name)); err != nil {
			t.Errorf("规则文件未创建: %s (%v)", r.Name, err)
		}
	}
}

func TestRenderConfigRuleWiring(t *testing.T) {
	m := NewManager(t.TempDir())
	s := DefaultSettings()
	s.Adblock = true
	s.Cloudflare = true
	s.CloudflareIP = []string{"1.2.3.4"}
	s.AppleOptimization = true
	s.CustomStreamMediaDNS = true
	s.StreamDNS = []string{"tls://8.8.8.8"}
	s.MinimalTTL = 60
	s.MaximumTTL = 3600
	b, err := m.RenderConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	for _, want := range []string{
		"tag: whitelist", "tag: blocklist", "tag: greylist", "tag: ddnslist",
		"tag: redirect", "tag: local_ptr", "tag: stream_media", "tag: adlist",
		"tag: cloudflare_cidr", "tag: geosite_no_cn", "tag: apple_domain_fallback",
		"black_hole 1.2.3.4", "ttl 60-3600", "tag: query_is_stream_media_domain",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("配置缺少 %q", want)
		}
	}
}

func TestUpdateGeodataFetches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("example.com\n"))
	}))
	defer srv.Close()
	m := NewManager(t.TempDir())
	s := DefaultSettings()
	s.AdSources = []string{srv.URL + "/ads.txt"}
	if err := m.WriteSettings(s); err != nil {
		t.Fatal(err)
	}
	res := m.UpdateAdSources(context.Background())
	if len(res) != 1 || res[0].Error != "" {
		t.Fatalf("广告来源下载失败: %+v", res)
	}
	got, err := os.ReadFile(filepath.Join(m.Dir(), adlistDirName, "ads.txt"))
	if err != nil || !strings.Contains(string(got), "example.com") {
		t.Fatalf("广告规则未落盘: %v %q", err, got)
	}
	if files := m.adlistFiles(); len(files) != 2 {
		t.Fatalf("adlistFiles 应含 adblock.txt + ads.txt: %v", files)
	}
}
