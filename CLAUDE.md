# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

**GateBox** —— 为 HomeLab 打造的极简、可视化 AppGateway：一个控制面统一「应用代理、域名证书、二级域名 DNS、Docker 容器运维」。

- 显示名：GateBox；代码/目录/仓库/二进制统一小写 `gatebox`；域名 `gatebox.cn`；GitHub `JiangBeta/gatebox`。
- 参考项目：Charon、caddy-docker-proxy、ctop、lazydocker。

## 架构（核心）

**控制面 + 独立外部组件**：GateBox 本体是控制面（Go + Vue3 + BoltDB）；caddy、ddnsgo、docker、docker-compose 是 4 个独立外部组件——进程独立、独立升级，GateBox **不接管其生命周期**，只通过「**改写它们的配置** + **调用它们的 API/CLI**」读写。

- Caddy：生成 Caddyfile → Admin API `/load` 原子加载（**先校验、失败回退、成功写盘、零中断**）。
- DDNS：依赖外部 ddnsgo（改写其 YAML + 服务重启）。
- Docker：容器生命周期走 Docker Engine API（**自研轻量 HTTP 封装**，非 moby SDK，见 ADR-014）；compose 编排走 `docker compose` CLI。
- 证书：Caddy ACME，**DNS-01 默认**，预装 cloudflare / dnspod.cn / aliyun 插件（dnspod.cn 非 .com）。

## 技术选型

前端 Vue3 + Vite + Naive UI（i18n：默认中文，可扩展英文）；后端 Go（单二进制，`go:embed` 内嵌前端）；数据库 BoltDB。

## 领域模型

- **App**（source ∈ docker/manual/host）—< **ProxyRoute**（type ∈ reverse_proxy/file_server/tcp_stream）
- **Domain** 联动 DDNS 上报 + 证书；**ComposeInstance** 对应 docker-compose.yaml；**DNSCredential** 统一管理（加密存储）。
- 证书、DNS 记录不建独立实体（由 caddy/ddnsgo 托管）。

## 文件架构

- 运行时数据根目录 `$DATA_DIR`（用户指定，安装脚本创建并写入 DB）：`gatebox` 二进制、`db/gatebox.db`、`tools/{caddy,ddnsgo,docker}`、`appData/<projectName>/`（compose + www + conf）。
- 代码：`backend/`（internal/{config,models,store,caddy,ddns,docker,api,server}）+ `frontend/`（src/{api,views,components,stores,router,locales}）+ `scripts/` + `configs/`。

## 约定

- **沟通、思考、代码注释一律中文**。
- **MVP 优先**：单用户 admin；多用户/RBAC、UDP 代理、独立证书等后置。
- 生成的 Caddyfile 预留 `import` 扩展点（`tools/caddy/user/`），不覆盖用户手改。
- Docker label 复用 `caddy.*` 约定 + `gatebox.*` 扩展。

## 文档导航

- [PRD](docs/PRD.md) · [术语表](docs/glossary.md) · [ADR](docs/adr/)（ADR-001 ~ ADR-016）· [域名功能](docs/domain.md) · [Docker 功能](docs/docker.md) · [页面布局](docs/layout.md)

## 命令

开发工具链由 **mise** 管理（见 `mise.toml`）。首次进入项目：

```bash
mise trust && mise install     # 安装 go / node / pnpm
```

> `mise.toml` 里固化了两个必需设置，都是踩过的坑：
> - `node.compile = false` —— 否则 mise 会下载 node **源码**并 `./configure` 编译，而编译需要 python。
> - `disable_tools = ["python"]` —— 全局配置（home-manager 生成）声明了 python，但 pyenv 要从源码编译、需要 gcc；**装不上会阻塞所有 `mise exec`**，连 `go version` 都跑不到。

### 任务

```bash
mise run test     # 后端全部测试
mise run build    # 构建后端二进制
mise run dev      # 后端开发服务（0.0.0.0:8099）
mise run web      # 前端开发服务器（0.0.0.0）
```

### 后端（`backend/`）

```bash
go build ./...              # CGO_ENABLED=0 已由 mise.toml 的 [env] 提供
go test ./...
gofmt -l ./internal ./cmd   # 有输出即不合规
go vet ./...
```

**竞态检测需要 C 编译器**，mise 不提供，NixOS 上须另行定位（store 路径随系统更新变化）：

```bash
export PATH="$(dirname $(ls /nix/store/*gcc-wrapper*/bin/gcc | head -1)):$PATH"
CGO_ENABLED=1 go test ./... -race       # 并发代码改动后必跑
```

**Docker 集成测试**：对接真实 daemon，`/var/run/docker.sock` 不可用时自动跳过；`GATEBOX_SKIP_DOCKER_IT=1` 显式跳过。其中 `exec_integration_test.go` 会在容器内执行**只读**命令（echo / exit / shell 探测），不写文件、不改配置。

### 前端（`frontend/`）

```bash
pnpm build              # 输出到 backend/internal/web/dist（由 go:embed 内嵌）
pnpm dev --host 0.0.0.0
```

### 开发环境约定

- **服务一律绑 `0.0.0.0`，不要绑 `127.0.0.1`** —— 需要从局域网其他设备访问。
- 开发端口用 **8099**：本机 `8080` 被 traefik、`8090` 被 gateway-manager 占用。
- 改了前端后，后端**必须重新编译**才能生效（`go:embed` 在编译期嵌入 `dist`）。

## 开发工作方法

- 以「页面 & 功能」为开发单位；先设计某个功能时，其他页面功能不论，只专注当前功能。
- 单位内流程：① 讨论二级划分 → ② 讨论二级相关功能 & 页面设计 → ③ 撰写 PRD & 开发计划（计划含验证方案）→ ④ 开发 & 验证。
- 验证分 **AI 验证**（计划中的验证）与 **人工验证**（此时可打开实际页面测试）。

## 页面布局

- 一级导航（左侧）：仪表盘 / 网关 / Docker / 域名 / 设置 + 底部「退出」。
- 二级导航：右上 Tab；三级操作用 button + 弹出层。
- 网关页 Tab：代理应用列表 / 代理规则维护 / caddy 控制。
- 去 footer（官网/文档/版本信息下沉「设置 → 关于」）。
- 侧边栏底部（退出上方，小字）：Github 链接 / 文档链接 / 🌐 语言切换 / 🌓 主题（深/浅/auto）。
- 版本号：LOGO 右侧小字灰色，有更新弹红色 NEW Badge。

## 开发工作计划（逐需求：讨论 → 开发 → 验证 → 进入下一项）

1. 页面布局 & 域名（设计见 [docs/domain.md](docs/domain.md)）
2. Docker（设计见 [docs/docker.md](docs/docker.md)）
3. 网关
4. 控制台 & 设置（含用户认证）
