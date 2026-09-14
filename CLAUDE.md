# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

**GateBox** —— 为 HomeLab 打造的极简、可视化 AppGateway（**「缝合怪」实现，PRD v2**）：以统一界面 + HomeLab 自动化需求整合成熟外部组件，一个控制面统一「应用代理、SSL 证书、外/内网 DNS、Docker 容器运维、跨网组网、应用导航」。

- 显示名：GateBox；代码/目录/仓库/二进制统一小写 `gatebox`；域名 `gatebox.cn`；GitHub `JiangBeta/gatebox`。
- 参考项目：Charon、caddy-docker-proxy、ctop、lazydocker、flame、mosdns、acme.sh。
- 归档：旧版 PRD 见 `docs/PRD-v1.md`。

## 架构（核心）

**控制面 + 独立外部组件**：GateBox 本体是控制面（Go + Vue3 + BoltDB）；外部组件（caddy / acme.sh / docker / docker-compose / ddns-go / mosdns / tailscale / flame）进程独立、独立升级，GateBox **不接管其生命周期**，只通过「**改写它们的配置** + **调用它们的 API/CLI**」读写。

- Caddy：生成 Caddyfile → Admin API `/load` 原子加载（**先校验、失败回退、成功写盘 $DATA_DIR/Caddyfile、零中断**）。
- 证书：**acme.sh 全面接管**（签发 / 续期 / 状态展示），Caddy 用文件证书加载（PRD v2 变更，替代原 caddy 内置 DNS-01 ACME）。
- DDNS：依赖外部 ddns-go（改写其 YAML + 服务重启）。
- Docker：容器生命周期走 Docker Engine API（**自研轻量 HTTP 封装**，非 moby SDK，见 ADR-014）；compose 编排走 `docker compose` CLI；容器代理信息实时发给网关 caddy api。
- 内网 DNS：外部 mosdns（改写配置 + 重启）；组网：外部 tailscale（**接入深度待讨论**）。
- 导航：外部 flame，页面**内嵌联动**。

## 技术选型

前端 Vue3 + Vite + **Ant Design Vue 4.x**（已从 naive-ui 迁移完成；i18n 默认中文、可扩展英文）；后端 Go（单二进制，`go:embed` 内嵌前端）；数据库 BoltDB（`conf/gatebox.db`）+ AES-GCM（`conf/secret.key`）；支持 amd64 / arm，debian / ubuntu / openwrt / archlinux / armbian，尽量低资源。

## 领域模型

- **App**（source ∈ docker/manual/host）—< **ProxyRoute**（type ∈ reverse_proxy/file_server/tcp_stream）
- **Domain** 联动 DDNS 上报（ddns-go）+ 证书（acme.sh）；**ComposeInstance** 对应 docker-compose.yaml；**DNSCredential** 统一模型（**证书签发与 DDNS 上报共用**，加密存储）。
- 证书记录不建独立实体（由 acme.sh 托管）；mosdns / tailscale 模型待讨论。

## 文件架构

- 运行时数据根目录 `$DATA_DIR`（用户指定，安装脚本创建并写入基础配置）：`conf/`（gatebox.db + secret.key + gatebox.conf）、`tools/`（caddy + acme + 其它工具）、`appData/<projectName>/`（compose + 数据）、`$DATA_DIR/Caddyfile`（最近成功 `/load` 的备份）、README。
- 代码：`backend/`（internal/{config,models,store,caddy,acme,ddns,docker,gateway,api,server,web}）+ `frontend/`（src/{api,views,components,stores,router,locales}）+ `scripts/` + `configs/`。

## 约定

- **沟通、思考、代码注释一律中文**。
- **MVP 优先**：单用户 admin；多用户/RBAC、UDP 代理、独立证书管理高级功能等后置。
- 生成的 Caddyfile 预留 `import` 扩展点（`tools/caddy/user/`），不覆盖用户手改。
- Docker label 复用 `caddy.*` 约定 + `gatebox.*` 扩展。

## 文档导航

- [PRD(v2)](docs/PRD.md) · [PRD(v1 存档)](docs/PRD-v1.md) · [术语表](docs/glossary.md) · [ADR](docs/adr/)（ADR-001 ~ ADR-021）· [页面布局](docs/layout.md)

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

> **CodeMirror 坑**：`codemirror` 包只导出 `EditorView`/`basicSetup`，`ViewPlugin`/`Decoration`/`WidgetType` 需从 `@codemirror/view` 显式 import；`@codemirror/state`/`view` 等需显式 `pnpm add`（pnpm 不提升传递依赖）。

### 开发环境约定

- **服务一律绑 `0.0.0.0`，不要绑 `127.0.0.1`** —— 需要从局域网其他设备访问。
- 开发端口用 **8099**：本机 `8080` 被 traefik、`8090` 被 gateway-manager 占用。
- 改了前端后，后端**必须重新编译**才能生效（`go:embed` 在编译期嵌入 `dist`）。

## 开发工作方法

- 以「页面 & 功能」为开发单位；先设计某个功能时，其他页面功能不论，只专注当前功能。
- 单位内流程：① 讨论二级划分 → ② 讨论二级相关功能 & 页面设计 → ③ 撰写 PRD & 开发计划（计划含验证方案）→ ④ 开发 & 验证。
- 验证分 **AI 验证**（计划中的验证）与 **人工验证**（此时可打开实际页面测试）。

## 页面布局

- 一级导航（左侧，6 入口）：**首页 / 网关 / 容器 / 域名 / 网络 / 设置** + 底部「退出」（PRD v2）。
- 首页：导航（**内嵌 flame**）+ 仪表盘；容器 = 原「Docker」。
- 二级导航：右上 Tab；三级操作用 button + 弹出层（表单/详情用 Drawer 右滑，确认用 Modal，见 `docs/layout.md` §4）。
- 网关页 Tab：代理 / Caddy 片段 / 变量；容器页 Tab：概览 / 编排 / 镜像 / 网络 / 存储卷；域名页 Tab：概览 / 域名 / 证书 / **DDNS**（新增）。
- 去 footer（官网/文档/版本信息下沉「设置 → 关于」）。
- 侧边栏底部（退出上方，小字）：Github 链接 / 文档链接 / 🌐 语言切换 / 🌓 主题（深/浅/auto）。
- 版本号：LOGO 右侧小字灰色，有更新弹红色 NEW Badge。

## 开发工作计划（v2，逐单位：讨论 → 撰写单位设计 → 开发 → 验证 → 进入下一项）

当前阶段：**先文档后代码**；网络模块深度待讨论。单位见 `docs/PRD.md` §14：

1. 架构与目录重构（`conf/` + `tools/acme` + Caddyfile 备份上提 + 数据迁移 + 导航/更名）
2. 容器修复（修「无法获取 docker 信息一直转圈」bug + 代理数据联动网关）
3. 网关收尾（片段-代理自动关联 + docker 自动代理 + Caddyfile validate/端到端）
4. 域名重构（acme.sh 全面接管 + 统一凭证 + 域名页 4 Tab 含 DDNS）
5. 网络模块（mosdns / Tailscale，深度待讨论）
6. 首页 & 设置（flame 内嵌 + 仪表盘 + 设置页）
7. 部署交付（install.sh + 多架构 tarball + systemd/openrc/procd）
