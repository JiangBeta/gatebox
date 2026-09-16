# GateBox

为 HomeLab 打造的极简、可视化 **AppGateway**（「缝合怪」实现）：以统一界面整合成熟外部组件，一个控制面统一「应用代理、SSL 证书、外/内网 DNS、Docker 容器运维、跨网组网、应用导航」。

- 显示名：GateBox；代码 / 目录 / 仓库 / 二进制统一小写 `gatebox`；域名 `gatebox.cn`；GitHub `JiangBeta/gatebox`。
- 参考项目：caddy-docker-proxy、ctop、lazydocker、flame、mosdns、acme.sh。

## 架构一句话

**控制面 + 独立外部组件**。GateBox 本体是控制面（Go + Vue3 + BoltDB）；caddy / acme.sh / docker / docker-compose / ddns-go / mosdns / tailscale / flame 均为独立组件。GateBox **管理其「进程」与「配置」、观测「版本」，但永不改写「配方」**（不自行编译/拼装模块），升级 = 搬运上游发布的制品。详见 [架构设计](docs/architecture.md) 与 [ADR-027](docs/adr/ADR-027.md)。

## 技术选型

| 层 | 选型 |
|---|---|
| 前端 | Vue3 + Vite + **Ant Design Vue 4.x**（四层：`design/lib/app/modules/shared`） |
| 后端 | Go（单二进制，`go:embed` 内嵌前端），分层骨架参 go-nunu（MIT） |
| 数据库 | BoltDB（`conf/gatebox.db`），敏感字段 AES-GCM（`conf/secret.key`） |
| 代理 | 生成 Caddyfile → Caddy Admin API `/load`（先校验、失败回退、成功落盘） |
| 证书 | acme.sh 全面接管（DNS-01，Caddy 文件证书加载） |
| 容器 | Docker Engine API（自研轻量 HTTP 封装，ADR-014）+ `docker compose` CLI |
| 扩展开关 | 组件运行时 + 数据驱动插件（`manifest + 静态索引`，无服务端）；拔插式插件与 GateBoxStore 见 ADR-037~039 |

## 核心 / 插件

- **core（默认安装）**：Caddy、acme.sh、docker（`attached`）、docker-compose。
- **optional（按需 / 插件）**：ddns-go、flame、mosdns、tailscale、coraza。

## 文档导航

- [架构设计（v3 总纲）](docs/architecture.md) ← **先读这个**
- [PRD（产品需求）](docs/PRD.md) · [术语表](docs/glossary.md) · [ADR](docs/adr/)（ADR-001 ~ ADR-039）
- 插件拔插化：[ADR-037](docs/adr/ADR-037.md)（分离与 GateBoxStore） · [ADR-038](docs/adr/ADR-038.md)（配方变体） · [ADR-039](docs/adr/ADR-039.md)（运行时契约）
- 插件作者文档（`plugin-authoring` / `catalog` / `extension-api` / `plugin-ui`）见 [**GateBoxStore**](https://github.com/JiangBeta/GateBoxStore)
- 单位设计：`docs/{infra,docker,gateway,domain,network,home,deploy}.md`

## 当前状态

**v3 重构：设计已定稿，待编码。** 按 [架构设计 §15](docs/architecture.md) 的 P0–P5 分期推进：地基 → 组件运行时 → 插件 → 前端重建 → 网络/首页/设置 → 部署交付。重构前代码完整保留于 `./old/`。

## 代码仓库结构

```
gatebox/
├── go.work                  # 仅纳入活跃模块 ./backend
├── backend/                 # 新后端（Go）
├── frontend/                # 新前端（Vue3）
├── plugins/schema/          # 扩展契约权威：manifest.v2 schema（插件源码见 GateBoxStore）
├── registry/schema/         # 扩展契约权威：catalog.v1 schema（含 variants[]）
├── tools/                   # 内置制品存档（离线安装）：tools/<id>/<os>-<arch>/
├── old/                     # 重构前原始代码（独立 go.mod）
├── docs/ scripts/ configs/
```

## 运行时目录 `$DATA_DIR`

运行时数据根目录（默认 `./data`，可用 `GATEBOX_DATA_DIR` 覆盖）：

```
$DATA_DIR/
├── conf/                          # gatebox.db · secret.key · gatebox.conf
├── tools/                         # 所有组件/插件运行目录（一组件一目录，二进制在此运行）
│   ├── caddy/{caddy, versions/<ver>/, user/, logs/, data/}
│   ├── acme/{acme.sh, certs/<fqdn>/{fullchain.pem,key.pem}}
│   ├── docker-compose/docker-compose
│   ├── ddnsgo/ · mosdns/ · tailscale/ · flame/
│   └── <plugin-id>/
├── appData/<projectName>/         # compose 项目（1 项目 = 1 目录）
├── Caddyfile                      # 最近一次成功 /load 的配置源备份
└── README.md
```

## 快速开始

> 开发工具链由 `mise` 管理（见 `mise.toml`）。

```bash
mise trust && mise install     # 首次：安装 go / node / pnpm
mise run test                  # 后端全部测试
mise run build                 # 构建二进制 → ./gatebox
mise run dev                   # 后端开发服务 0.0.0.0:8099
mise run web                   # 前端开发服务器 0.0.0.0
```

服务一律绑 `0.0.0.0`（需局域网访问）；开发端口 `8099`（`8080`/`8090` 被占用）。改了前端后**必须重新编译后端**（`go:embed` 编译期嵌入 `dist`）。

## 安全

- `docker.sock` 可写等价于宿主机 root；安装第三方插件制品 = 执行代码。二者均须在 UI/文档显式告知，并对安装做签名校验 + 用户确认。
- DNS 凭证 / 片段密钥 / 私有仓库密码加密存储（`conf/secret.key`，AES-GCM）。
