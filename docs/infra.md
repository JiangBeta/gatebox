# GateBox · 单位① 架构与目录重构设计 & 开发计划

> 状态：设计已定（PRD v2 单位①，先文档后代码）
> 关联：`docs/PRD.md`（§7 文件架构 / §14 计划）、`docs/adr/ADR-002 / 011 / 013`、`docs/layout.md`
> 前置：无（基于现状代码直接重构）

## 1. 定位与范围

本单位的核心是**把运行时目录从 v1 的「根目录平铺」收敛为 PRD v2 的「`conf/` 集中」**，并把前端导航升级为 v2 的 6 入口结构。

范围：

1. `$DATA_DIR` 目录布局收敛：`db/` → `conf/`，`Caddyfile` 备份上提根目录，新增 `conf/gatebox.conf`。
2. 路径与配置常量化：所有硬编码路径改为经 `conf/gatebox.conf` / `Config` 下发。
3. 已存数据的一键迁移（DB + 密钥 + 证书目录 + ddnsgo 配置，不丢数据）。
4. 前端导航 v2：一级导航 6 入口（首页/网关/容器/域名/网络/设置）、「Docker」更名「容器」。

边界：涉及**纯机械重构**；业务逻辑（网关/容器/域名功能本体）不动，只保证回归。新增导航页（网络/首页）本单位的页面内容留到单位⑤⑥。

## 2. 现状与差距

| 项 | 现状（v1） | 目标（v2） | 差距 |
|---|---|---|---|
| BoltDB | `$DATA_DIR/db/gatebox.db`（`store.Open` 硬编码） | `$DATA_DIR/conf/gatebox.db` | 移动 + 路径常量化 |
| 加密密钥 | `$DATA_DIR/db/secret.key` | `$DATA_DIR/conf/secret.key` | 移动 + 路径常量化 |
| 基础配置 | 仅环境变量（`config.Load()`） | `conf/gatebox.conf`（env 优先、conf 兜底） | 新增文件与优先级 |
| Caddyfile 备份 | `$DATA_DIR/tools/caddy/Caddyfile`（reloadCaddy 内写盘） | `$DATA_DIR/Caddyfile` | 上提根目录 + 常规化 |
| acme 证书 | `$DATA_DIR/tools/acme/certs/<fqdn>/`（ADR-013 已定） | 同（保留） | 无 |
| ddnsgo 配置 | `$DATA_DIR/tools/ddnsgo/.ddns_go_config.yaml` | 同（保留） | 无 |
| 前端导航 | 仪表盘/网关/Docker/域名/设置（5 入口，路由 `/docker`） | 首页/网关/容器/域名/网络/设置（6 入口，路由 `/containers`） | 导航重构 + 更名 |

## 3. 目标目录（PRD v2 §7，落地版）

```
<DATA_DIR>/
├── conf/                       # 集中配置（原 db/）
│   ├── gatebox.db                # BoltDB（store.Open）
│   ├── secret.key                # AES-GCM 主密钥
│   └── gatebox.conf              # 基础配置（见 §5）
├── tools/                      # 外部工具（不变）
│   ├── caddy/                    # caddy 二进制 + user/*.caddy + logs/
│   ├── acme/                     # acme.sh + certs/<fqdn>/{fullchain.pem,key.pem}
│   ├── ddnsgo/                   # ddns-go 二进制 + .ddns_go_config.yaml
│   └── ...
├── appData/<projectName>/      # compose 项目（不变）
├── Caddyfile                   # 最近一次成功 /load 的配置源备份（上提）
└── README.md
```

- 删除根目录 `db/`（迁移后）；`tools/caddy/Caddyfile` 不再作为备份，只保留 `user/` 与 `logs/`。

## 4. 迁移方案（不丢数据）

首次启动（`store.Open` 前）做一次性检测 + 迁移，幂等：

1. `conf/` 不存在而 `db/` 存在 → 建 `conf/`，移动 `gatebox.db` / `secret.key` 到 `conf/`；旧 `db/` 保留空目录并写入 `.migrated` 标记。
2. `conf/gatebox.conf` 不存在 → 生成（含迁移后的路径）。
3. Caddyfile：若 `tools/caddy/Caddyfile` 存在且根目录无 `Caddyfile` → 复制到根目录。
4. acme / ddnsgo 目录位置不变，不动。
5. 迁移失败（移动中 IO 错误）→ 报错退出，不进入正常启动；旧 `db/` 数据保留可手工恢复。

> 回退：迁移只移动文件不删除原文件；确认启动成功后，安装脚本 / 文档指引用户删除 `.migrated` 目录。

## 5. 基础配置文件 `conf/gatebox.conf`

职责：**记录运行目录位置、端口号等运行参数**，作为环境变量的持久化兜底（machine-readable，行式 `key = value`）。

字段草案（与 `config.Config` 一一对应，env 优先于 conf 优先于默认值）：

| key | 对应 env | 默认 | 说明 |
|---|---|---|---|
| `data_dir` | `GATEBOX_DATA_DIR` | `./data` | 运行时数据根目录 |
| `addr` | `GATEBOX_ADDR` | `0.0.0.0:8080` | 后端监听地址（开发默认 8099，见 CLAUDE.md） |
| `caddy_admin` | `GATEBOX_CADDY_ADMIN` | `http://localhost:2019` | Caddy Admin API |
| `caddy_bin` | `GATEBOX_CADDY_BIN` | `<data_dir>/tools/caddy/caddy` | caddy 二进制(前置 validate,ADR-002) |
| `caddy_http_port` | `GATEBOX_CADDY_HTTP_PORT` | `0`(标准 80) | 全局 `http_port` 覆盖(与 traefik 共存时改高端口) |
| `caddy_https_port` | `GATEBOX_CADDY_HTTPS_PORT` | `0`(标准 443) | 全局 `https_port` 覆盖(同左) |
| `docker_socket` | `GATEBOX_DOCKER_SOCKET` | `/var/run/docker.sock` | Docker unix socket |
| `docker_daemon_json` | `GATEBOX_DOCKER_DAEMON_JSON` | `/etc/docker/daemon.json` | daemon 白名单配置 |
| `acme_bin` | `GATEBOX_ACME_BIN` | `acme.sh`（PATH） | acme.sh 可执行文件 |
| `web_port` | — | 前端开发服务器端口（写入由 `run web` 生成的 dev 配置） | 供前端 dev 联动 |

- 后端加载顺序：env 已设置 → 用 env；否则读 `conf/gatebox.conf`；否则默认值。
- `data_dir` 首次由安装脚本写入；conf 中 `data_dir` 变更需重启生效，UI 不动态切换。
- conf 定位：`<env GATEBOX_DATA_DIR 或 ./data>/conf/gatebox.conf`；conf 内 `data_dir` 与定位目录不一致时仅告警（install 应写一致）。

> **2026-09-07 落地**：`config.Load()` 已实现三源加载（env > conf > 默认）+ 首次启动自动生成默认 conf（`${DATA_DIR}/conf/gatebox.conf`，0644），键表与 Config 字段一一对应。新增 `caddy_bin / caddy_http_port / caddy_https_port`（含 caddy 端口覆盖，见 ADR-022 补记）。

## 6. 后端改造点

| 位置 | 改动 |
|---|---|
| `internal/config/config.go` | `Config` 增加 `ConfFile string`；`Load()` 增加「env → gatebox.conf → 默认」优先级；解析 `data_dir` 等字段 |
| `internal/store/store.go` | `Open` 的 db/secret.key 路径由 `dataDir/conf/`；`loadOrCreateKey` 同步 |
| `internal/store/store.go` | 新增 `Migrate(dataDir) error`：v1 目录检测 + 移动 + `.migrated` 标记（幂等） |
| `backend/cmd/gatebox/main.go` | 启动时先 `store.Migrate` 再 `store.Open` |
| `internal/caddy/tls.go` | `renderTLSDNS` 已有路径；如需抽象 `CertsDir` 来自 config（不动默认值） |
| `internal/api/gateway_api.go`（reloadCaddy） | Caddyfile 写盘路径改 `$DATA_DIR/Caddyfile`；写入前确保目录存在 |
| `internal/web/` | 不变（前端静态内嵌） |

基础设施约束：

- 不改 BoltDB bucket 结构（`domains / dns_credentials / registries / compose_instances / apps / services / fragments / variables / fragment_toggles`）。
- 不引入新的数据模型。

## 7. 前端改造点

| 位置 | 改动 |
|---|---|
| `frontend/src/router/index.ts` | 一级导航 6 入口：`/`（首页：导航+仪表盘）、`/gateway`、`/containers`（原 `/docker`，标题「容器」）、`/domain`、`/network`（占位）、`/settings` |
| `frontend/src/App.vue` 侧边栏 | 菜单项改 6 入口 + 图标；「Docker」→「容器」 |
| `frontend/src/api/http.ts` | baseURL 不变（`/api/v1`） |
| 域名页 | 保留 4 Tab（单位④再加「DDNS」）；导航结构不动 |
| 网络页 / 首页 | 本阶段仅占位空态（`Empty`），内容见单位⑤⑥ |

## 8. 验证方案

| 类型 | 内容 |
|---|---|
| 单测 | `config.Load` 优先级（env > conf > 默认）；`store.Migrate` 幂等（空目录 / 仅 db / 已迁移 三种形态）；迁移后 `Open` 正常、密钥解密旧数据成功 |
| 集成 | 构造 v1 目录 fixture → 迁移 → 数据不丢（CRUD 往返） |
| 回归 | 全部既有后端测试通过；前端构建通过 |
| 人工 | 用既有 `gatebox.db` 启动新版 → 域名/容器/网关数据均可见；Caddyfile 备份出现在根目录；导航 6 入口路由可达 |

## 9. 风险与回退

- 移动旧 `db/` 是唯一破坏性动作 → 迁移采用「复制到 `conf/` + 标记不删」两段式；确认后再清 `db/`。
- conf 解析错误容错：文件不存在 → 回退默认 + 首次启动写入；字段非法 → 报错并提示删除对应行，不静默覆盖。
- 前端路由 `/docker` → `/containers` 为 hash 路由，历史链接失效可接受（HomeLab 单机）。

## 10. 开发计划（含验证方案）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | config 三源加载 + conf 读写 | `Config.ConfFile`、conf 解析器、首次生成 | 单测优先级矩阵 | 改 env 看优先 |
| 2 | store 迁移 + db/密钥路径收敛 | `Migrate` + `Open` 改 `conf/` | 单测三形态幂等；既有数据解密切换 | 旧库升级数据可见 |
| 3 | reloadCaddy 写盘上提 | `$DATA_DIR/Caddyfile` | 集成：写盘路径断言 | 看根目录 Caddyfile |
| 4 | 前端导航 v2 + 更名 | 6 入口路由 + 侧边栏 + 占位页 | 构建通过、路由可达 | 切 6 页正常 |
| 5 | 回归与收尾 | 全量测试 + 人工核对 | `mise run test` 全绿 | 页面/链路无回退 |