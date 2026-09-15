package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearEnv 清空除 GATEBOX_DATA_DIR 外的相关 env,使三源加载落入 conf/默认位
// (data_dir 由各测试用 t.TempDir() 显式设置,避免读到宿主环境)。
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GATEBOX_ADDR", "GATEBOX_CADDY_ADMIN", "GATEBOX_CADDY_BIN",
		"GATEBOX_CADDY_HTTP_PORT", "GATEBOX_CADDY_HTTPS_PORT",
		"GATEBOX_DOCKER_SOCKET", "GATEBOX_DOCKER_DAEMON_JSON", "GATEBOX_ACME_BIN",
		"GATEBOX_STATIC_ROOT",
	} {
		t.Setenv(k, "")
	}
}

func writeConf(t *testing.T, dir, content string) {
	t.Helper()
	confDir := filepath.Join(dir, "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("mkdir conf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "gatebox.conf"), []byte(content), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
}

// TestLoadPriority 优先级矩阵:env 覆盖 conf,conf 覆盖默认。
func TestLoadPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEBOX_DATA_DIR", dir)
	clearEnv(t)
	writeConf(t, dir, "addr = 0.0.0.0:9999\ncaddy_http_port = 7777\ncaddy_https_port = 8443\ncaddy_bin = /custom/caddy\n")
	t.Setenv("GATEBOX_ADDR", "0.0.0.0:8888") // env 压过 conf

	c := Load()
	if c.Addr != "0.0.0.0:8888" {
		t.Errorf("addr = %q, want env 0.0.0.0:8888", c.Addr)
	}
	if c.CaddyHTTPPort != 7777 {
		t.Errorf("caddy_http_port = %d, want conf 7777", c.CaddyHTTPPort)
	}
	if c.CaddyHTTPSPort != 8443 {
		t.Errorf("caddy_https_port = %d, want conf 8443", c.CaddyHTTPSPort)
	}
	if c.CaddyBin != "/custom/caddy" {
		t.Errorf("caddy_bin = %q, want conf /custom/caddy", c.CaddyBin)
	}
	if c.DataDir != dir {
		t.Errorf("data_dir = %q, want %q(env)", c.DataDir, dir)
	}
	if c.ConfFile != filepath.Join(dir, "conf", "gatebox.conf") {
		t.Errorf("conf file = %q", c.ConfFile)
	}
}

// TestLoadConfOverDefault conf 兜底,无 env。
func TestLoadConfOverDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEBOX_DATA_DIR", dir)
	clearEnv(t)
	writeConf(t, dir, "addr = 0.0.0.0:5000\ncaddy_http_port = 8081\n")

	c := Load()
	if c.Addr != "0.0.0.0:5000" {
		t.Errorf("addr = %q, want conf 0.0.0.0:5000", c.Addr)
	}
	if c.CaddyHTTPPort != 8081 {
		t.Errorf("caddy_http_port = %d, want conf 8081", c.CaddyHTTPPort)
	}
	if c.DockerSocket != "/var/run/docker.sock" {
		t.Errorf("docker_socket = %q, want 默认", c.DockerSocket)
	}
}

// TestLoadDefaults 无 conf 无 env:默认值 + CaddyBin 由 data_dir 派生。
func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEBOX_DATA_DIR", dir)
	clearEnv(t)

	c := Load()
	if c.Addr != "0.0.0.0:8080" {
		t.Errorf("addr = %q, want 默认 0.0.0.0:8080", c.Addr)
	}
	if c.CaddyHTTPPort != 0 || c.CaddyHTTPSPort != 0 {
		t.Errorf("端口应默认 0, got %d/%d", c.CaddyHTTPPort, c.CaddyHTTPSPort)
	}
	if c.CaddyBin != filepath.Join(dir, "tools", "caddy", "caddy") {
		t.Errorf("caddy_bin = %q, want %q", c.CaddyBin, filepath.Join(dir, "tools", "caddy", "caddy"))
	}
	// 路径统一绝对化,避免依赖进程 cwd。
	if !filepath.IsAbs(c.DataDir) || !filepath.IsAbs(c.StaticRoot) {
		t.Errorf("data_dir/static_root 应为绝对路径: %q / %q", c.DataDir, c.StaticRoot)
	}
}

// TestLoadGeneratesDefaultConf 首次启动无 conf 时自动生成。
func TestLoadGeneratesDefaultConf(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEBOX_DATA_DIR", dir)
	clearEnv(t)

	Load()
	Load() // 幂等:重复加载不再改写

	b, err := os.ReadFile(filepath.Join(dir, "conf", "gatebox.conf"))
	if err != nil {
		t.Fatalf("读取生成 conf: %v", err)
	}
	s := string(b)
	for _, want := range []string{"data_dir = " + dir, "caddy_http_port = 0", "caddy_https_port = 0", "caddy_https_extra_ports ="} {
		if !strings.Contains(s, want) {
			t.Errorf("生成 conf 应含 %q, got:\n%s", want, s)
		}
	}
}

// TestLoadExtraHTTPSPorts 额外 https 端口列表解析(逗号分隔,非法项跳过)。
func TestLoadExtraHTTPSPorts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEBOX_DATA_DIR", dir)
	clearEnv(t)
	writeConf(t, dir, "addr = 0.0.0.0:5000\ncaddy_https_extra_ports = 9443, 9444\n")
	c := Load()
	if len(c.CaddyHTTPSExtraPorts) != 2 || c.CaddyHTTPSExtraPorts[0] != 9443 || c.CaddyHTTPSExtraPorts[1] != 9444 {
		t.Errorf("extra ports = %v, want [9443 9444]", c.CaddyHTTPSExtraPorts)
	}

	// env 优先 + 非法项跳过
	t.Setenv("GATEBOX_CADDY_HTTPS_EXTRA_PORTS", "99999,8443,abc")
	c2 := Load()
	if len(c2.CaddyHTTPSExtraPorts) != 1 || c2.CaddyHTTPSExtraPorts[0] != 8443 {
		t.Errorf("extra ports(env) = %v, want [8443]", c2.CaddyHTTPSExtraPorts)
	}
}

// TestLoadParserTolerance 注释/空行/缺 '='/未知键/非法整数均不中断。
func TestLoadParserTolerance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEBOX_DATA_DIR", dir)
	clearEnv(t)
	writeConf(t, dir, "# 注释行\n\nbad_line_without_equal\nunknown_key = whatever\ncaddy_http_port = notanumber\naddr = 0.0.0.0:5555\n")

	c := Load()
	if c.Addr != "0.0.0.0:5555" {
		t.Errorf("addr = %q, want conf 0.0.0.0:5555(合法行仍生效)", c.Addr)
	}
	if c.CaddyHTTPPort != 0 {
		t.Errorf("caddy_http_port = %d, want 默认 0(非法整数回退)", c.CaddyHTTPPort)
	}
}
