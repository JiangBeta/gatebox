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
- Docker：容器生命周期走 Docker Engine API（moby SDK）；compose 编排走 `docker compose` CLI。
- 证书：Caddy ACME，**DNS-01 默认**，预装 cloudflare / dnspod.cn / aliyun 插件（dnspod.cn 非 .com）。

## 技术选型

前端 Vue3 + Vite + Naive UI（i18n：默认中文，可扩展英文）；后端 Go（单二进制，`go:embed` 内嵌前端）；数据库 BoltDB。

## 领域模型

- **App**（source ∈ docker/manual/host）—< **ProxyRoute**（type ∈ reverse_proxy/file_server/tcp_stream）
- **Domain** 联动 DDNS 上报 + 证书；**ComposeInstance** 对应 docker-compose.yaml；**DNSCredential** 统一管理（加密存储）。
- 证书、DNS 记录不建独立实体（由 caddy/ddnsgo 托管）。

## 文件架构

- 运行时数据根目录 `$DATA_DIR`（用户指定，安装脚本创建并写入 DB）：`gatebox` 二进制、`db/appgateway.db`、`tools/{caddy,ddnsgo,docker}`、`appData/<app>/`（compose + www + conf）。
- 代码：`backend/`（internal/{config,models,store,caddy,ddns,docker,api,server}）+ `frontend/`（src/{api,views,components,stores,router,locales}）+ `scripts/` + `configs/`。

## 约定

- **沟通、思考、代码注释一律中文**。
- **MVP 优先**：单用户 admin；多用户/RBAC、UDP 代理、独立证书等后置。
- 生成的 Caddyfile 预留 `import` 扩展点（`tools/caddy/user/`），不覆盖用户手改。
- Docker label 复用 `caddy.*` 约定 + `appgateway.*` 扩展。

## 文档导航

- [PRD](docs/PRD.md) · [术语表](docs/glossary.md) · [ADR](docs/adr/)（ADR-001 ~ ADR-011）

## 命令

> 项目尚未实现，无 build/test 命令。落地后补全。

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

1. 页面布局 & 域名
2. Docker
3. 网关
4. 控制台 & 设置（含用户认证）
