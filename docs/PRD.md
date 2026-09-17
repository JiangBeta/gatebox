# GateBox · PRD（产品需求文档）

> 状态：规划阶段 v2（2026-09「缝合怪」重构版）；架构已升级至 **v4**（统一描述 + 可观测 + 声明式调和），总纲见 [`docs/architecture.md`](architecture.md)。
> v2 变更背景：证书签发从 caddy 内置 ACME 改为 **acme.sh 全面接管**；新增 **flame（导航）/ mosdns（内网 DNS）/ Tailscale（组网）** 三个外部组件；目录架构重构；导航重构为 6 顶级入口。
> v3 变更背景：确立「组件生命周期边界（配方归上游、运行态归 GateBox）」「组件运行时接口」「数据驱动插件 + 静态索引」「核心/插件分类」「后端/前端分层标准」；**扩展平台（能力注册表 + 投影 API + 扩展契约）见 ADR-036**。原代码保留于 `./old/`。插件拔插化（主程序与插件分离 + GateBoxStore）见 ADR-037 ~ ADR-039。**应用网关统一框架（统一描述 / 可观测 / 声明式调和）见 ADR-040**；**架构 V4（以此为组织原则的总纲升级）见 ADR-041**。见 ADR-027 ~ ADR-041。
> 上一版存档于 `docs/PRD-v1.md`。
> 参考项目：Charon、caddy-docker-proxy、ctop、lazydocker、flame、mosdns、acme.sh

## 1. 定位

为 HomeLab 打造的**缝合怪应用网关**：以统一界面 + 针对 HomeLab 的自动化需求，整合现有成熟的外部组件，让用户点几下就能把应用挂到自己的域名上，零配置、零外部依赖（除了被缝合的工具本身）。

**整体思路**：

- 所有应用通过网关发布（Caddy）
- 发布时自动 SSL（acme.sh）
- Docker 应用通过 yaml（docker-compose）管理、自动发布（通过 label）
- 外网访问通过 DDNS 自动指向（ddns-go）
- 内网访问通过 DNS 自动指向（mosdns）
- 跨网访问时使用 VPN（Tailscale）
- 自动生成应用地图便于导航（flame）

## 2. 目标用户

家庭 / 小型服务器用户（HomeLab），希望把 Docker 应用、静态站点、宿主机服务通过域名访问，且不想手写反向代理配置。

## 3. 核心能力

| # | 能力 | 承担组件 | GateBox 职责 |
|---|---|---|---|
| 1 | 应用代理 | Caddy | 手工代理（IP:端口 / 静态文件）；docker label 自动代理；Caddyfile 生成 + Admin API `/load` |
| 2 | SSL 证书 | acme.sh | 发布时自动签发 / 续期；证书状态展示（caddy 用文件证书加载） |
| 3 | 应用编排 | docker + docker-compose | yaml 管理（表单 ⇄ YAML）；label 自动发布 |
| 4 | 外网访问 | ddns-go | 二级域名 DNS 上报（改写配置 + 重启生效） |
| 5 | 内网 DNS | mosdns | 内网域名记录自动指向 |
| 6 | 跨网组网 | Tailscale | VPN 状态与操作 |
| 7 | 应用地图导航 | flame | 页面内嵌 flame；应用地图自动生成 |

## 4. 系统架构

**控制面 + 独立外部组件**：GateBox 本体是控制面（Go + Vue3 + BoltDB），caddy / acme.sh / docker / docker-compose / ddns-go / mosdns / tailscale / flame 均为独立进程，独立升级，GateBox **不接管其生命周期**，只负责「改写它们的配置」+「调用它们的 API/CLI」。

```
GateBox（控制面：Go + Vue3 + BoltDB）    # conf/gatebox.db + secret.key 加密敏感字段
    │  改写配置 + 调 API/CLI
    ├── caddy            （反向代理 + 文件证书加载）
    ├── acme.sh          （证书签发与续期）
    ├── docker           （容器生命周期，Docker Engine API）
    ├── docker-compose   （编排，docker compose CLI）
    ├── ddns-go          （外网 DDNS 上报）
    ├── mosdns           （内网 DNS 记录）
    ├── tailscale        （跨网组网）
    └── flame            （应用地图导航，内嵌）
```

## 5. 技术选型

| 层 | 选型 |
|---|---|
| 前端 | Vue3 + Vite + **Ant Design Vue 4.x**（已从 naive-ui 迁移完成）；i18n（默认中文，可扩展英文） |
| 后端 | Go（单二进制，`go:embed` 内嵌前端静态资源） |
| 数据库 | BoltDB（`conf/gatebox.db`）；敏感字段 AES-GCM（`secret.key`） |
| 代理 | 生成 Caddyfile → Caddy Admin API `/load`（先校验、失败回退、成功写盘落 Caddyfile 备份） |
| 证书 | **acme.sh 全面接管**（独立签发 / 续期 / 状态展示；DNS-01 依赖 ddns-go 或 DNS 凭证） |
| Docker | Docker Engine API（**自研轻量 HTTP 封装**，非 moby SDK——ADR-014）+ `docker compose` CLI |
| DDNS | 外部 ddns-go（改写 YAML + 服务重启） |
| 内网 DNS | 外部 mosdns（改写配置 + 重启） |
| 组网 | 外部 Tailscale |
| 导航 | 外部 flame（页面内嵌联动） |

**技术需求**：

- 支持架构：amd64、arm
- 支持系统：debian、ubuntu、openwrt、archlinux、armbian
- 尽量简约、减少资源消耗

## 6. 领域模型

```
ComposeInstance (1) ──< Container (N) ──< App (M, M ≤ N)    # ADR-015
App (1) ──< ProxyRoute (N)
App.source ∈ { docker, manual, host }
ProxyRoute.type ∈ { reverse_proxy, file_server, tcp_stream }
Domain ── DDNS 上报（ddns-go）+ 证书（acme.sh）
DNSCredential（统一模型：证书签发与 DDNS 上报共用，加密存储）
```

- **ComposeInstance**：一个 compose 项目 = 一个部署单位，起 N 个容器，其中只有需要被代理的容器升格为 App（**M ≤ N**）。主键为 `projectName`，落盘于 `appData/<projectName>/`（ADR-015）。
- **App**：可被代理的最小单元；`docker` 由 label 自动发现，`manual` 为手动 IP:端口（可多个 = 负载均衡），`host` 为宿主机服务。**指向容器的稳定标识为 `project + service`**，不用会变的容器 ID / IP（ADR-016）。
- **ProxyRoute**：`reverse_proxy`（后端 http/https，https 后端需处理自签证书信任，如 PVE 8006）、`file_server`（静态网页）、`tcp_stream`（L4 透传）。
- **证书记录不建独立实体**：由 acme.sh 托管，本工具读写其配置并展示状态。
- **内网 DNS（mosdns）/ 组网（Tailscale）模型**：待讨论。

## 7. 文件架构

### 运行时目录 `$DATA_DIR`

位置由用户指定，安装脚本创建并写入基础配置：

```
<DATA_DIR>/
├── conf/                  # 配置文件目录
│   ├── gatebox.db           # BoltDB 主库（App / ProxyRoute / Caddy 片段 / 域 / DNS 凭证等）
│   ├── secret.key           # AES-GCM 密钥（加密 DNS 凭证、片段密钥、私有仓库密码等）
│   └── gatebox.conf         # 基础配置：运行目录、前后端端口号等信息
├── tools/                   # 第三方工具目录
│   ├── caddy/             # caddy：主程序、user/*.caddy 用户扩展片段、logs/
│   ├── acme/              # acme：acme.sh 主程序、ssl/ 证书目录、日志
│   ├── .../               # 其它工具目录（ddns-go / mosdns / docker-compose 等）
│   └── README.md          # tools 说明文件
├── appData/               # compose 项目数据目录（1 项目 = 1 目录 = 1 compose）
│   ├── <projectName>/       # 如 aria2 → docker-compose.yaml + 各服务配置/数据目录
│   │   └── docker-compose.yaml
│   └── README.md          # appData 说明文件
├── Caddyfile              # 最近一次成功生成并 `/load` 的 Caddyfile 备份
└── README.md              # 项目说明文件
```

> 注：v2 改为 `conf/` 集中配置（原 `db/` 平铺根目录）；`Caddyfile` 备份上提至 `$DATA_DIR` 根（配置固化）。

### 代码仓库结构

```
backend/    # Go：cmd/gatebox + internal/{config,models,store,caddy,acme,ddns,mosdns,network,docker,gateway,api,server,web}
frontend/   # Vue3：src/{api,views,components,stores,router,locales}
scripts/    # install.sh（多发行版/架构安装）、build.sh（build → go:embed → 单二进制）
configs/    # systemd/openrc/procd 服务单元模板
docs/       # PRD + glossary + adr/
```

## 8. 页面结构（1-2 级目录）

- **首页**：导航 / 仪表盘
- **网关**：代理 / Caddy 片段 / 变量
- **容器**（原 Docker）：概览 / 编排 / 镜像 / 网络 / 存储卷
- **域名**：概览 / 域名 / 证书 / DDNS
- **网络**：内网 DNS / Tailscale
- **设置**：—

> 术语统一：一级导航「Docker」改名「容器」，与领域名词对齐；域名页新增「DDNS」Tab（原证书侧栏并入各 Tab）。

## 9. 模块需求说明

### 9.1 网关（代理）

代理相关应用。

- **手工代理**：手工设置已有设备或应用（如 openwrt、PVE 主机等）；支持反向代理 & 静态文件。
- **自动代理**：基于运行中的 docker label 数据自动代理；该数据由「容器」模块获取并实时发送到 caddy API（`/reverse_proxy/upstreams`）。
- **配置固化**：新配置 `/load` 生效后，自动保存到 `$DATA_DIR/Caddyfile` 备份。
- **Caddy 片段**：固化的中间件 / 路由规则，可供调用。**默认自动关联**：所有代理启用压缩、阻止常见漏洞、按服务日志输出，反向代理另加支持 websocket；主动健康检查由服务字段（`healthUri`，默认 `/`）承载；后端服务为 HTTPS 时自动启用「忽略后端证书校验」（`tls_insecure_skip_verify`）。可手动启停某代理的片段，也可自定义片段（ADR-033）。
- **变量**：避免硬编码；除系统指定外可自定义。

> 完成情况：页面、手工代理、caddy 片段、变量功能已开发；**未验证**代理情况（Caddyfile validate）；未完成「片段与代理的关联（特别是自动关联）」「docker 自动代理打通（容器提供数据 → 网关给出 caddy api）」。
> 存在问题（**待讨论**）：docker 应用自动变化（新增 / 变更 / 停止）后代理列表的显示——人工操作则由容器模块联动网关只显示最新状态；自动变化（docker 停止 / 运行错误导致未启动）则列表保留并显示报错。

### 9.2 容器（原 Docker）

容器编排、操作、监控；**将容器的代理信息发送给网关的 caddy api**。

- 编排：compose yaml 表单 ⇄ YAML 双向同步（已开发）
- 操作：容器起停 / 日志 / exec；镜像拉取 / 删除 / 导入；网络、存储卷 CRUD（已开发）
- 监控：stats 采集（已开发）
- **当前 bug**：页面无法打开（一直转圈，无法获取 docker 信息）——**需优先修复**。

### 9.3 域名

- 证书签发 **由 acme.sh 接管，需重构**（原 caddy 内置 ACME 废弃）：
  - 发布时自动 SSL；独立签发 / 续期 / 状态展示（证书目录 `tools/acme/ssl/`）。
- **DNS 供应商凭证与 DDNS 共用**：统一凭证模型（`DNSCredential`），证书签发（acme.sh provider）与 DDNS 上报（ddns-go）使用同一份凭证。
- **新增 DDNS Tab**（使用 ddns-go）：查看 / 管理 DDNS 上报状态。

### 9.4 网络（内网 DNS + Tailscale）

- **内网 DNS（mosdns）**：内网域名记录自动指向。
- **组网（Tailscale）**：跨网访问。
- 接入深度**待讨论**（仅部署 + 引导 + 状态展示，还是完全接管改写配置 + 重启）。

### 9.5 首页 & 设置

- 首页：导航（**内嵌 flame**）+ 仪表盘（聚合状态）。
- 设置：关于 / 主题（深 / 浅 / auto）/ 语言 / 版本信息（更新 NEW Badge）/ 退出；用户认证（单用户 admin，JWT——后置）。

## 10. 核心流程

- **添加 Docker 应用**：容器读 `gatebox.*` label → 建 App + ProxyRoute → docker 自动代理信息发给网关 → 生成 Caddyfile → `/load`
- **添加手工代理**：建 App（IP:端口，可多个）→ 同上
- **证书签发**：建域 / 用统一 DNS 凭证 → 触发 acme.sh 签发 / 续期 → caddy 文件证书加载
- **外网访问**：写 ddns-go YAML → 重启 ddns-go 上报
- **内网访问**：写 mosdns 配置 → 重启生效（深度待讨论）
- **生成 compose**：表单 ⇄ YAML → stdin 校验（`config -q`）→ 通过才落盘 `appData/<projectName>/docker-compose.yaml` → `docker compose up -d`

## 11. 功能范围

**MVP 内**：6 页 UI（首页 / 网关 / 容器 / 域名 / 网络 / 设置）+ 网关手工 & 自动代理闭环 + acme.sh 证书 + ddns-go DDNS + mosdns 内网 DNS + Tailscale 状态 + flame 内嵌导航 + 容器编排运维。

**MVP 外（后置）**：多用户 / RBAC、UDP 代理、独立证书管理 UI 高级功能（重新申请 / 私钥下载 / 申请历史）、发行版原生包、多主机控制。

## 12. 部署交付

- 裸机单二进制，支持 debian / ubuntu / archlinux / armbian / openwrt，amd64 & arm 多架构。
- `install.sh` + 多架构 tarball（本体 + caddy + acme.sh + ddns-go + mosdns + flame）；docker 走系统包管理器。
- 组件装成系统服务（systemd / openrc / procd）。

## 13. 安全

- `docker.sock` 可写等价于宿主机 root —— 必须在 UI/文档显式告知。
- DNS 凭证 / 片段密钥 / 私有仓库密码加密存储（`conf/secret.key` AES-GCM）。

## 14. 开发工作计划（v2）

> **v3 重构分期（P0 地基 → P1 组件运行时 → P2 插件 → P3 前端重建 → P4 网络/首页/设置 → P5 部署交付）见 [`docs/architecture.md`](architecture.md) §15，以该分期为准。**

逐单位「讨论 → 撰写单位设计文档 → 开发 → 验证 → 进入下一项」。**当前阶段：先文档后代码**；网络模块深度待讨论。

| # | 单位 | 设计文档 | 范围 | 依赖 |
|---|---|---|---|---|
| 1 | **架构与目录重构** | [infra.md](docs/infra.md) | `conf/`（gatebox.db / secret.key / gatebox.conf）+ `tools/acme` + Caddyfile 备份上提；config/store 路径重构；数据迁移；前端导航 6 入口 & 「Docker→容器」更名 | 旧版全部功能回归 |
| 2 | **容器修复** | [docker.md](docs/docker.md) §12 | 修「一直转圈无法获取 docker 信息」bug；容器→网关 caddy api 代理数据联动；变化显示策略（人工 vs 自动） | 1 |
| 3 | **网关收尾** | [gateway.md](docs/gateway.md) §9 | 片段-代理自动关联（默认片段 + HTTPS 后端忽略证书校验）；docker 自动代理打通；Caddyfile validate + 端到端验证 | 1、2 |
| 4 | **域名重构** | [domain.md](docs/domain.md) §9 | acme.sh 全面接管（签发 / 续期 / 状态）；统一凭证模型（证书 + DDNS 共用）；域名页 4 Tab（概览 / 域名 / 证书 / DDNS） | 1、3 |
| 5 | **网络模块** | [network.md](docs/network.md) | mosdns 内网 DNS；Tailscale。深度**待讨论**（D1） | 1 |
| 6 | **首页 & 设置** | [home.md](docs/home.md) | flame 部署联动 + 页面内嵌导航；仪表盘聚合；设置页（关于 / 主题 / 语言 / 版本 / 退出） | 3、4 |
| 7 | **部署交付** | [deploy.md](docs/deploy.md) | `install.sh` + 多架构 tarball（含 caddy / acme.sh / ddns-go / mosdns / flame）；systemd / openrc / procd 服务单元 | 1–6 |

## 15. 待新增 / 修订 ADR

| # | 主题 | 状态 |
|---|---|---|
| ADR-013 | 证书签发主体变更：caddy 内置 ACME → **acme.sh 全面接管**（caddy 文件证书加载） | **已修订**（2026-09-03，见文件内修订说明） |
| ADR-022 | 目录架构 v2：`conf/` 集中配置 + `Caddyfile` 备份上提 | 新增 |
| ADR-023 | 新增外部组件：flame（内嵌导航）、mosdns（内网 DNS）、tailscale（组网）接入边界 | 新增 |
| ADR-024 | 统一 DNS 凭证模型（acme.sh 证书 + ddns-go 上报共用） | 新增 |
| ADR-025 | 一级导航 v2：「Docker」→「容器」、6 入口、域名页 DDNS Tab | 新增 |
| ADR-027 | 组件生命周期边界与升级本体（配方归上游 / 运行态归 GateBox / 制品源三通道） | 新增（v3） |
| ADR-028 | 组件运行时接口（可选接口 + Capabilities 驱动 UI） | 新增（v3） |
| ADR-029 | 插件模型与分发（数据驱动 + 三形态 + 静态索引，无服务端） | 新增（v3） |
| ADR-030 | 核心/插件分类与运行时目录（core/optional + `$DATA_DIR/tools`） | 新增（v3） |
| ADR-031 | 后端分层与仓库目录 v3（go-nunu 骨架 + `old/` + `go.work`） | 新增（v3） |
| ADR-032 | 前端分层与设计 token 强制（AntD 四层 + lint 门禁） | 新增（v3） |
| ADR-033 | Caddy 内置片段定案（7 片段 + 1 字段，修复非法 code 导致的加载失败） | 新增（v3） |
| ADR-034 | 服务运行身份与 Docker 权限授予（低权用户 + polkit 最小授权 + 软/硬重启） | 新增（v3） |
| ADR-035 | 网关与容器统一变量模型（`<%KEY%>` / `${KEY}` 共用存储） | 新增（v3） |
| ADR-036 | 扩展平台（扩展点 + 能力注册表 + 投影契约 + appstore 数据模型） | 新增（v3） |
| ADR-037 | 主程序与插件分离（拔插式插件 + 统一安装面 + 生命周期三档 + GateBoxStore） | 新增（v3） |
| ADR-038 | 核心组件配方变体与制品矩阵（feature 并集 + xcaddy CI 构建 + 互斥兜底） | 新增（v3） |
| ADR-039 | 插件运行时契约（sidecar + API 反代 + plugin token + L0/L1 UI 宿主 + dns-provider） | 新增（v3） |
| ADR-040 | 应用网关统一框架（统一描述 / 可观测 / 声明式调和） | 新增（v3） |
| ADR-041 | 架构 V4（以统一框架为组织原则的总纲升级） | 新增（v4） |

## 16. ADR 索引

见 `docs/adr/`：ADR-001 ~ ADR-041。v4 架构总纲见 [`docs/architecture.md`](architecture.md)；插件拔插化（ADR-037~039）配套独立仓库 [`JiangBeta/GateBoxStore`](https://github.com/JiangBeta/GateBoxStore)。