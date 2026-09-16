// GateBox 控制面入口。
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/acme"
	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
	"github.com/JiangBeta/gatebox/internal/adapter/cert"
	dockerclient "github.com/JiangBeta/gatebox/internal/adapter/docker/client"
	"github.com/JiangBeta/gatebox/internal/adapter/docker/stats"
	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/config"
	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/gateway"
	"github.com/JiangBeta/gatebox/internal/plugin"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/server"
	"github.com/JiangBeta/gatebox/internal/source"
)

// 版本号：构建时经 -ldflags "-X main.version=..." 注入。
var version = "dev"

func main() {
	cfg := config.Load()

	st, err := repository.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("打开存储失败: %v", err)
	}
	defer st.Close()

	// 证书由独立 acme.sh 签发(DNS-01),落盘于 $DATA_DIR/tools/acme/certs/。
	// acme.sh 工作目录固定在 $DATA_DIR/tools/acme(--home),不经过 ~/.acme.sh。
	acmeCertsDir := filepath.Join(cfg.DataDir, "tools", "acme", "certs")
	cm := cert.NewAcme(acmeCertsDir)
	ac := acme.New(acmeCertsDir, cfg.AcmeBin)
	if email, err := st.GetACMEEmail(); err != nil {
		log.Printf("读取 ACME 注册邮箱失败(继续): %v", err)
	} else if email != "" {
		ac.SetEmail(email)
	}

	// Docker 是可选外部组件:daemon 不可用时其余功能照常工作,
	// Docker 页在前端降级提示,而非让整个控制面启动失败。
	dc := dockerclient.NewUnix(cfg.DockerSocket)
	var coll *stats.Collector
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
	apiVer, pingErr := dc.Ping(pingCtx)
	pingCancel()
	if pingErr != nil {
		log.Printf("Docker 不可用(%v):Docker 页将不可用,其余功能不受影响", pingErr)
		dc = nil
	} else {
		log.Printf("Docker 已连接: %s (API %s)", cfg.DockerSocket, apiVer)
		coll = stats.New(dc)
		defer coll.Close()
	}

	caddyCli := caddy.NewClient(cfg.CaddyAdmin)
	health := gateway.NewHealthCollector(caddyCli, gateway.DefaultHealthInterval)

	// 组件运行时（制品源 + 内置组件注册表）与插件管理器。
	src := source.New(os.Getenv("GATEBOX_GITHUB_TOKEN"))
	reg := component.NewCoreRegistry(cfg.DataDir, cfg.CaddyBin, cfg.CaddyAdmin, src)

	// 扩展平台：核心能力与内置提供者注册为与插件同构的 provider（ADR-036）。
	ext := extension.NewRegistry()
	ext.Register(extension.CoreProvider())
	// ddns-go 已迁为 GateBoxStore 的 kind:process 插件：经投影 API + reconcile 自收敛
	// （ADR-036 I2 / ADR-039），核心不再内置其配置生成逻辑。

	// DNS 凭证供应商改为查扩展注册表（核心不硬编码，ADR-039 §5）。
	ac.SetProviders(ext.DNSProvider)
	mgr := plugin.NewManager(st, src, cfg.DataDir, ext)
	// 注入控制面基址：sidecar 插件据此调用投影 API（回环地址）。
	if _, port, err := net.SplitHostPort(cfg.Addr); err == nil {
		mgr.SetCoreURL("http://127.0.0.1:" + port)
	}

	// 内网 DNS（mosdns）已迁为 kind:process 插件（GateBoxStore/plugins/mosdns）：
	// 其管理逻辑由插件 sidecar 承载，mosdns 本体由 sidecar 代管（ADR-037 决策 (a)）。
	handler := server.New(st, cm, dc, coll, caddyCli, health, cfg.DaemonJSONPath, cfg.DataDir, cfg.CaddyBin, cfg.StaticRoot, cfg.CatalogURL, cfg.CaddyHTTPPort, cfg.CaddyHTTPSPort, cfg.CaddyHTTPSExtraPorts, ac, reg, mgr, ext)

	log.Printf("GateBox %s 启动: 监听 %s, 数据目录 %s", version, cfg.Addr, cfg.DataDir)
	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
