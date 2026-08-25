# GateBox · PRD（产品需求文档）

> 状态：规划阶段（需求分析与架构决策已敲定，见 `docs/adr/`）
> 参考项目：Charon、caddy-docker-proxy、ctop、lazydocker

## 1. 定位

为 HomeLab 打造的**极简、可视化 AppGateway**：一个控制面统一「应用代理、域名证书、二级域名 DNS、Docker 容器运维」。用户点几下就能把应用挂到自己的域名上，零配置、零外部依赖。

## 2. 目标用户

家庭 / 小型服务器用户（HomeLab），希望把 Docker 应用、静态站点、宿主机服务通过域名访问，且不想手写反向代理配置。

## 3. 四个核心能力

1. **应用自动化代理** —— Docker 基于 label 的自动代理（复用 `caddy.*` 约定）；其他应用手动代理（IP:端口）。
2. **域名证书自动化** —— 基于 Caddy 的 ACME，DNS-01 为默认 challenge。
3. **二级域名 DNS 自动化** —— 基于 ddnsgo 自动上报（改写其配置 + 重启生效）。
4. **Docker 应用运维** —— 表单生成 `docker-compose.yaml`；查看容器运行情况。

## 4. 系统架构

**控制面 + 独立外部组件**（ADR-001）：

```
GateBox（控制面：Go + Vue3 + BoltDB）
    │  改写配置 + 调 API/CLI
    ├── caddy            （反向代理 + ACME 证书）
    ├── ddnsgo           （动态 DNS 上报）
    ├── docker           （容器生命周期，Docker Engine API）
    └── docker-compose   （编排，docker compose CLI）
```

- 外部组件独立进程、独立升级，本工具**不接管其生命周期**，只负责「改写它们的配置」+「调用它们的 API/CLI」。
- 配置变更零中断：Caddy 走 Admin API `/load` 原子加载。

## 5. 技术选型

| 层 | 选型 |
|---|---|
| 前端 | Vue3 + Vite + Naive UI；**i18n（默认中文，可扩展英文）** |
| 后端 | Go（单二进制，`go:embed` 内嵌前端静态资源） |
| 数据库 | BoltDB |
| 代理 | 生成 Caddyfile → Caddy Admin API `/load` |
| 证书 | Caddy ACME，DNS-01 默认（预装 cloudflare / dnspod.cn / aliyun 插件） |
| DDNS | 外部 ddnsgo（改写 YAML + 服务重启） |
| Docker | Docker Engine API（**自研轻量 HTTP 封装**，非 moby SDK——ADR-014）+ `docker compose` CLI；表单 ⇄ YAML 双向同步生成 `docker-compose.yaml` |

## 6. 领域模型

```
ComposeInstance (1) ──< Container (N) ──< App (M, M ≤ N)    # ADR-015
App (1) ──< ProxyRoute (N)
App.source ∈ { docker, manual, host }
ProxyRoute.type ∈ { reverse_proxy, file_server, tcp_stream }
Domain ── DDNS 上报（ddnsgo）+ 证书（caddy）
DNSCredential（统一管理，加密存储）
```

- **ComposeInstance**：一个 compose 项目 = 一个部署单位，起 N 个容器，其中只有需要被代理的容器升格为 App（**M ≤ N**）。主键为 `projectName`，落盘于 `appData/<projectName>/`（ADR-015）。
- **App**：可被代理的最小单元；`docker` 由 label 自动发现，`manual` 为手动 IP:端口（可多个 = 负载均衡），`host` 为宿主机服务。**指向容器的稳定标识为 `project + service`**，不用会变的容器 ID / IP（ADR-016）。
- **ProxyRoute**：`reverse_proxy`（后端 http/https，https 后端需处理自签证书信任，如 PVE 8006）、`file_server`（静态网页）、`tcp_stream`（L4 透传）。
- 证书、DNS 记录**不建独立实体**——由 caddy / ddnsgo 托管，本工具只读写其配置并展示状态。

## 7. 文件架构

### 运行时数据目录 `$DATA_DIR`（ADR-011）

位置由用户指定，安装脚本创建并写入 DB 供全局调用：

```
$DATA_DIR/
├── gatebox                # 本工具二进制
├── db/gatebox.db                   # BoltDB
├── tools/                          # 外部工具（项目目录统一管理）
│   ├── caddy/
│   │   ├── caddy                   # caddy 二进制
│   │   ├── Caddyfile               # 生成的 Caddyfile（/load 成功后落盘）
│   │   └── user/                   # 用户自定义 Caddyfile 片段（被 import）
│   ├── ddnsgo/
│   │   ├── ddnsgo                  # ddnsgo 二进制
│   │   └── .ddns_go_config.yaml    # 生成的 ddnsgo 配置
│   └── docker/docker-compose       # docker-compose 二进制
└── appData/
    └── <projectName>/              # 单位是 compose 项目，非 App（ADR-015）
        ├── docker-compose.yaml     # 该应用的 compose 文件
        ├── www/                    # file_server 静态站点文件
        └── conf/                   # 该应用的设置 & 数据
```

### 代码仓库结构

```
backend/    # Go：cmd/gatebox + internal/{config,models,store,caddy,ddns,docker,api,server}
frontend/   # Vue3：src/{api,views,components,stores,router,locales}
scripts/    # install.sh（多发行版/架构安装）、build.sh（build → go:embed → 单二进制）
configs/    # systemd/openrc/procd 服务单元模板
docs/       # PRD + glossary + adr/
```

## 8. 核心流程

- **添加 Docker 应用**：读容器 `caddy.*` label → 建 App + ProxyRoute → 生成 Caddyfile → `/load`
- **添加手动代理**：建 App（IP:端口，可多个）→ 同上
- **添加域名**：写 ddnsgo YAML → 重启 ddnsgo 上报 → Caddy DNS-01 签证书
- **生成 compose**：表单 ⇄ YAML → stdin 校验（`config -q`）→ 通过才落盘 `appData/<projectName>/docker-compose.yaml` → `docker compose up -d`
- **查看运行状态**：stats 由后端单例采集器聚合成快照供前端轮询；日志 / exec / 拉取 / 部署各走一条 WebSocket（docs/docker.md §5.2）

## 9. 功能范围

**MVP 内**：上述四个能力的最小闭环 + 5 页 UI（仪表盘 / 网关 / Docker / 域名 / 设置）。

**MVP 外（后置）**：多用户 / RBAC、UDP 代理、独立证书签发、发行版原生包、**多主机控制**（单一控制面管理多台主机，数据模型预留扩展点，后续实现）、**内网 DNS 记录自动下发**（adguardHome / MosDNS / RouterOS / OpenWRT 等内网解析服务器的记录自动写入）。

## 10. 部署交付

- 裸机单二进制，支持 debian / ubuntu / archlinux / armbian / nixos / openwrt，x86 & arm 多架构。
- `install.sh` + 多架构 tarball（本体 + caddy + ddnsgo）；docker 走系统包管理器，其余装入 `$DATA_DIR/tools/`。
- 组件装成系统服务（systemd / openrc / procd）。

## 11. 安全

- `docker.sock` 可写等价于宿主机 root —— 必须在 UI/文档显式告知。
- DNS 凭证加密存储（BoltDB）。
- 单用户 admin（JWT），认证/多用户后置。

## 12. 非功能性

- 多发行版 / 多架构；配置变更零中断；MVP 优先、可增量扩展；i18n 可扩展。

## 13. 开发工作计划

逐需求「讨论 → 开发 → 验证 → 进入下一项」：

1. 页面布局 & 域名（设计已定，见 [docs/domain.md](docs/domain.md)）
2. Docker（设计已定，见 [docs/docker.md](docs/docker.md)）
3. 网关
4. 控制台 & 设置（含用户认证）

## 14. ADR 索引

见 `docs/adr/`：ADR-001 ~ ADR-016。
