// Package config 提供运行配置的加载与默认值。
//
// 三源加载(ADR-022 / infra.md §5):env > conf > 默认。
// conf 文件为 `$DATA_DIR/conf/gatebox.conf` 行式 `key = value`;首次启动缺失时自动生成。
// `data_dir` 定位 conf 自身(env 或默认 ./data),conf 内 data_dir 与定位不一致时告警。
package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultDataDir = "./data"
	confFile       = "gatebox.conf"
)

// Config 运行配置
type Config struct {
	DataDir string // 运行时数据根目录
	Addr    string // HTTP 监听地址

	CaddyAdmin     string // Caddy Admin API 地址
	CaddyBin       string // caddy 二进制路径(前置 validate;默认 $DATA_DIR/tools/caddy/caddy)
	CaddyHTTPPort  int    // 全局 http_port 覆盖(0=标准 80,与 traefik 共存用)
	CaddyHTTPSPort int    // 全局 https_port 覆盖(0=标准 443)
	// CaddyHTTPSExtraPorts 额外的 https 监听端口(逗号分隔,与主 https 端口并存,
	// 如 "443,9443"——每站点额外生成 host:<port> site block,ADR-026 网关端口多开)。
	CaddyHTTPSExtraPorts []int
	// StaticRoot 全局静态文件根目录(变量 <%GB_STATIC_ROOT%> 的取值,
	// 默认 $DATA_DIR/www;ADR-033)。
	StaticRoot string

	DockerSocket   string // Docker 守护进程 unix socket
	DaemonJSONPath string // dockerd 配置(镜像加速器白名单);NixOS/OpenWrt 等常不存在,只读展示
	AcmeBin        string // acme.sh 可执行文件路径;空使用 PATH 中的 "acme.sh"

	// CatalogURL 插件静态索引(catalog v1)地址：拉取插件目录与核心组件配方变体(ADR-037/038)。
	CatalogURL string
	// CatalogPubKey 发布者 Ed25519 公钥(base64)；非空时强制校验索引签名(ADR-037)。
	CatalogPubKey string
	// AdminPasswordHash 单管理员口令的 sha256(十六进制)；空则不启用控制面鉴权(ADR-030 §3)。
	AdminPasswordHash string
	// VariantRepo / VariantWorkflow 配方变体按需构建的目标仓库与 workflow(ADR-038 §4)。
	VariantRepo     string
	VariantWorkflow string
	// GitHubToken 调用 GitHub API(如 workflow dispatch)的令牌;空则只返回手动链接。
	GitHubToken string

	ConfFile string // 实际加载的 conf 文件路径(定位用)
}

// Load 从 env / conf / 默认值三源加载配置。
func Load() Config {
	// bootDataDir:env 或默认,用于定位 conf 文件自身。统一解析为绝对路径,
	// 使生成产物(日志/静态根/变量展示)不依赖进程 cwd。
	bootDataDir := absDir(getenv("GATEBOX_DATA_DIR", defaultDataDir))
	confPath := filepath.Join(bootDataDir, "conf", confFile)

	vals := readFile(confPath)
	if _, err := os.Stat(confPath); err != nil {
		if writeErr := writeDefaultConf(confPath, bootDataDir); writeErr != nil {
			log.Printf("生成默认 conf 失败(继续用默认配置): %v", writeErr)
		} else {
			log.Printf("已生成默认配置文件: %s", confPath)
		}
	}

	// data_dir:env > conf;与 bootDataDir 不一致时告警(install 应写一致)。
	dataDir := absDir(sourceStr("GATEBOX_DATA_DIR", "data_dir", defaultDataDir, vals))
	if dataDir != bootDataDir {
		log.Printf("conf data_dir=%s 与 conf 所在目录 %s 不一致,本次存读取按 conf 值;install 请保持一致", dataDir, bootDataDir)
	}

	return Config{
		DataDir:              dataDir,
		Addr:                 sourceStr("GATEBOX_ADDR", "addr", "0.0.0.0:8080", vals),
		CaddyAdmin:           sourceStr("GATEBOX_CADDY_ADMIN", "caddy_admin", "http://localhost:2019", vals),
		CaddyBin:             sourceStr("GATEBOX_CADDY_BIN", "caddy_bin", filepath.Join(dataDir, "tools", "caddy", "caddy"), vals),
		CaddyHTTPPort:        sourceInt("GATEBOX_CADDY_HTTP_PORT", "caddy_http_port", 0, vals),
		CaddyHTTPSPort:       sourceInt("GATEBOX_CADDY_HTTPS_PORT", "caddy_https_port", 0, vals),
		CaddyHTTPSExtraPorts: sourceIntList("GATEBOX_CADDY_HTTPS_EXTRA_PORTS", "caddy_https_extra_ports", vals),
		StaticRoot:           absDir(sourceStr("GATEBOX_STATIC_ROOT", "static_root", filepath.Join(dataDir, "www"), vals)),
		DockerSocket:         sourceStr("GATEBOX_DOCKER_SOCKET", "docker_socket", "/var/run/docker.sock", vals),
		DaemonJSONPath:       sourceStr("GATEBOX_DOCKER_DAEMON_JSON", "docker_daemon_json", "/etc/docker/daemon.json", vals),
		AcmeBin:              sourceStr("GATEBOX_ACME_BIN", "acme_bin", "", vals),
		CatalogURL:           sourceStr("GATEBOX_CATALOG_URL", "catalog_url", "https://jiangbeta.github.io/GateBoxStore/index.json", vals),
		CatalogPubKey:        sourceStr("GATEBOX_CATALOG_PUBKEY", "catalog_pubkey", "", vals),
		AdminPasswordHash:    sourceStr("GATEBOX_ADMIN_PASSWORD_SHA256", "admin_password_sha256", "", vals),
		VariantRepo:          sourceStr("GATEBOX_VARIANT_REPO", "variant_repo", "JiangBeta/GateBoxStore", vals),
		VariantWorkflow:      sourceStr("GATEBOX_VARIANT_WORKFLOW", "variant_workflow", "build-variant.yml", vals),
		GitHubToken:          sourceStr("GATEBOX_GITHUB_TOKEN", "github_token", "", vals),
		ConfFile:             confPath,
	}
}

// knownKeys 合法 conf 键集合(用于对未知键告警)。
var knownKeys = map[string]bool{
	"data_dir": true, "addr": true, "caddy_admin": true, "caddy_bin": true,
	"caddy_http_port": true, "caddy_https_port": true, "caddy_https_extra_ports": true,
	"static_root":   true,
	"docker_socket": true, "docker_daemon_json": true, "acme_bin": true, "catalog_url": true, "catalog_pubkey": true,
	"github_token": true, "variant_repo": true, "variant_workflow": true, "admin_password_sha256": true,
}

// readFile 解析 line `key = value` 的 conf 文件;不存在返回空,解析问题仅告警。
func readFile(path string) map[string]string {
	vals := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("conf 读取失败(回退默认): %v", err)
		}
		return vals
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			log.Printf("conf %s:%d 缺少 '=',忽略: %q", path, line, raw)
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if !knownKeys[key] {
			log.Printf("conf %s:%d 未知键 %q,忽略", path, line, key)
			continue
		}
		vals[key] = value
	}
	if err := sc.Err(); err != nil {
		log.Printf("conf 读取中断: %v", err)
	}
	return vals
}

// writeDefaultConf 首次启动生成默认配置文件。
func writeDefaultConf(path, dataDir string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "# GateBox 基础配置(env 优先、conf 兜底;行式 key = value)\n" +
		"# 首次启动自动生成;修改后需重启生效。\n\n" +
		"data_dir = " + dataDir + "\n" +
		"addr = 0.0.0.0:8080\n" +
		"caddy_admin = http://localhost:2019\n" +
		"caddy_bin = " + filepath.Join(dataDir, "tools", "caddy", "caddy") + "\n" +
		"caddy_http_port = 0\n" +
		"caddy_https_port = 0\n" +
		"caddy_https_extra_ports =\n" +
		"static_root = " + filepath.Join(dataDir, "www") + "\n" +
		"docker_socket = /var/run/docker.sock\n" +
		"docker_daemon_json = /etc/docker/daemon.json\n" +
		"acme_bin =\n" +
		"catalog_url = https://jiangbeta.github.io/GateBoxStore/index.json\n" +
		"catalog_pubkey =\n" +
		"github_token =\n" +
		"variant_repo = JiangBeta/GateBoxStore\n" +
		"variant_workflow = build-variant.yml\n" +
		"# 控制面鉴权口令的 sha256(十六进制)；留空则不启用。生成：printf %%s 你的口令 | sha256sum\n" +
		"admin_password_sha256 =\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// sourceStr 三源取字符串:env > conf > 默认。
func sourceStr(envKey, key, def string, vals map[string]string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if v, ok := vals[key]; ok {
		return v
	}
	return def
}

// sourceInt 三源取整数:env > conf > 默认;非法值回退默认并告警。
func sourceInt(envKey, key string, def int, vals map[string]string) int {
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("env %s 非法整数 %q,用默认 %d", envKey, v, def)
	}
	if v, ok := vals[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("conf %s 非法整数 %q,用默认 %d", key, v, def)
	}
	return def
}

// sourceIntList 三源取端口列表(逗号分隔整数):env > conf;非法项跳过。
func sourceIntList(envKey, key string, vals map[string]string) []int {
	src := os.Getenv(envKey)
	if src == "" {
		src = vals[key]
	}
	out := make([]int, 0, 2)
	for _, part := range strings.Split(src, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n <= 65535 {
			out = append(out, n)
		} else {
			log.Printf("conf/env %s 非法端口 %q,忽略", key, p)
		}
	}
	return out
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// absDir 把路径解析为绝对路径;失败时原样返回。
func absDir(p string) string {
	if p == "" {
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
