# GateBox 架构设计（v3 重构总纲）

> 状态：**已定稿**（逐轮讨论 + 领域建模产出，编码前的唯一权威设计）
> 日期：2026-09-14
> 关联：`docs/PRD.md`、`docs/glossary.md`、`docs/adr/`（ADR-027 ~ ADR-032）
> 旧版实现完整保留于 `./old/`（见 §13）。

本文件是 v3 重构的**总纲**：定义系统本体、组件/插件模型、分层与命名标准、目录蓝图与推进分期。**编码中凡遇「放哪一层 / 用什么字号 / 怎么命名」，以本文件为准，不再逐次讨论。**

---

## 1. 定位与目标

GateBox 是为 HomeLab 打造的**极简、可视化 AppGateway**（「缝合怪」实现）：以统一界面整合成熟外部组件，让用户把 Docker 应用、静态站点、宿主服务通过域名访问，并自动完成代理 / 证书 / DNS。

**重构价值函数（冲突时的优先级）：**

1. **骨架清晰** —— 先把组件运行时抽象立起来（这是插件化、轻量、后续一切功能的地基）
2. **插件化扩展**
3. **低资源占用**（OpenWrt 等设备）—— 贯穿全程的硬约束，不是事后优化
4. **现有功能零回归**（底线）

**推进方式：绞杀者渐进式**——旧功能逐个迁移，每步可 `mise run test` 验证。原始代码整体保留在 `./old/`，新系统在仓库根重建。

---

## 2. 核心本体：组件与生命周期

### 2.1 生命周期六层

「组件生命周期」拆为六层，每层的归属是硬边界：

| 层 | 定义 | 归属 |
|---|---|---|
| **配方 Recipe** | 制品由什么构成：二进制版本 + 编译进哪些模块（如 caddy 是否含 coraza） | **上游官方** |
| **制品 Artifact** | 配方产出的可执行文件 | 上游构建 |
| **制品源 Source** | 制品从哪来（`official` / `custom` / `system`） | 分档（见 §5） |
| **运行态 Runtime** | 进程启停、健康、日志、崩溃恢复 | **GateBox** |
| **配置 Config** | 读写配置、热加载 | **GateBox** |
| **版本 Version** | 读当前版本、查上游最新版（只读） | **GateBox** |
| **升级 Upgrade** | 用新制品替换旧制品 | 边界（搬运，不造配方） |

### 2.2 边界规则（ADR-027）

> **GateBox 管理「进程」与「配置」，观测「版本」，但永不改写「配方」。升级 = 用上游发布的制品替换旧制品——GateBox 只当搬运工，不当配方师。**

推论：

- **Coraza（WAF）**：启用 = 改变配方（xcaddy 拼模块）。因此 Coraza 只能以**已发布的制品变体**存在（官方或用户自建并发布的制品），设备端**不编译**。角色上，用户「自己编译 coraza-caddy 放 GitHub」是合法的——编译发生在 GateBox 之外，GateBox 只从配置的 `custom` 源搬运。
- **Docker**：必须区分「组件」与「被管对象」——
  - `dockerd` 是**组件**：配方/制品/运行态归系统包管理器，GateBox 仅经 API 连接（`attached`）；
  - **容器**是**被管对象**（不是组件）：GateBox 对其运行态有完全控制权。
  - 「不接管 Docker」精确表述为：**不接管 dockerd，但全权接管容器。**

### 2.3 组件 vs 被管对象

| | 组件（Component） | 被管对象（Managed Object） |
|---|---|---|
| 例子 | caddy、acme.sh、dockerd、mosdns、插件 | 容器、镜像、网络、卷、compose 项目、代理规则、域名 |
| 谁定义其运行逻辑 | 上游（配方） | GateBox |
| GateBox 角色 | 供应的搬运 + 运行态管理 + 配置读写 | 完全所有 |

---

## 3. 组件运行时模型（ADR-028）

### 3.1 Descriptor（静态元数据）

```go
type Descriptor struct {
    ID           string    // caddy / acme / docker / mosdns / coraza
    Name         string
    Kind         Kind      // core | caddy-module | process | config-only
    Source       Source    // channel(official|custom|system) + index + artifact
    Provision    string    // managed | attached
    Runtime      string    // manage | observe
    Upgrade      string    // replace | system | none
    Capabilities []string  // runnable|health|config|upgradable|logs|operable
    Tier         string    // core | optional
    DefaultEnabled bool
    Removable    bool
    Bundled      bool      // 离线安装包是否附带制品
}
```

### 3.2 运行时接口（可选接口，能力驱动 UI）

不使用万能大接口。组件按能力实现可选接口，UI 的可用操作由 `Capabilities` 驱动：

```go
type Runnable     interface { Start(ctx) error; Stop(ctx) error; Restart(ctx) error }
type Health       interface { Status(ctx) (Status, error) } // running|stopped|error + healthy + metrics
type Configurable interface { ReadConfig(ctx) ([]byte, error); WriteConfig(ctx, []byte) error; Reload(ctx) error }
type Upgradable   interface { CheckUpdate(ctx) (VersionInfo, error); Upgrade(ctx, ver string) error; Rollback(ctx) error }
type Loggable     interface { Logs(ctx, LogOpts) (io.ReadCloser, error) }
type Operable     interface { Operations() []Operation; Operate(ctx, opID string, in map[string]string) (Result, error) }

type Registry interface { Get(id string) (Component, bool); List() []Descriptor }
```

- 核心组件（caddy/acme/ddns/docker/mosdns/flame）与插件**同一接口**，区别在 `Kind` / `Tier` / 生命周期策略。
- `system` 档组件（docker）只实现 `Health + Operable` → UI 自动不显示启停/升级键。

### 3.3 状态模型

```
State ∈ { running | stopped | error | unknown }
Status { State, Healthy bool, Message, Since, Metrics map[string]any }
```

---

## 4. 插件模型（ADR-029）

### 4.1 数据驱动，不引入解释器

v1 采用 **manifest + 通用引擎**：插件是**数据**，不是需要 GateBox 重编译的代码。`kind` 预留扩展位。

| kind | 形态 | 引擎行为 | 例子 |
|---|---|---|---|
| `caddy-module` | 替换 caddy 制品 + 注入片段 | 切换 active 制品（复用校验失败回退）+ 写 Caddyfile 片段 | Coraza |
| `process` | 独立进程 | 下载二进制 → 落 `$DATA_DIR/tools/<id>/` → 启停 + 健康 + 声明式操作 | Tailscale / MosDNS / flame |
| `config-only` | 只注入配置 | 注册 manifest + 写配置 | 轻量指令扩展 |

> 预留（未来，非 v1）：Traefik 式 Yaegi/WASM（`kind` 扩展）、Terraform 式子进程 gRPC 协议。

### 4.2 manifest schema（v1）

```yaml
apiVersion: gatebox/v1
kind: caddy-module            # caddy-module | process | config-only
id: coraza
name: Coraza WAF
version: 0.1.0
summary: OWASP CRS 规则的 Caddy WAF
requires:
  gatebox: ">=0.3.0"
  components: [caddy]
source:
  channel: official
artifact:
  amd64: { url: "...", sha256: "..." }
  arm64: { url: "...", sha256: "..." }
runtime:
  swap: [caddy]               # caddy-module
  process: { cmd: [...], health: {...}, workDir: "..." }   # process
config:
  schema: [...]               # 声明式设置表单
  inject:
    - component: caddy
      target: caddyfile
      snippet: "coraza_waf { ... }"
operations:                   # process 的声明式操作
  - { id: status, label: 状态, cmd: ["tailscale","status","--json"], readOnly: true }
contributions:
  nav: [{ path: /waf, label: WAF, icon: shield }]
  page: { type: iframe | settings }   # v1 仅 导航 + iframe + 声明式设置表单
```

### 4.3 状态机与持久化

```
available ──install──▶ installed ──enable──▶ enabled
    ▲                      │                    │
    └─────────remove───────┴──────disable───────┘  (disabled: 保留制品、停用效果)
                         任一环节失败 ──▶ error
```

- BoltDB bucket：`plugins`（安装状态 + 用户配置）、`component_state`（版本/来源缓存）。
- 卸载 = 回到 `available`（删制品 + 移除注入配置）。

### 4.4 分发：静态索引 + 多源（无服务端，ADR-029）

v1 **不需要常驻服务端**。用一个可静态托管的索引，实现「在线安装/升级」：

- 索引文件 `index.json` + 签名（minisign/cosign），可托管于 GitHub Pages / 对象存储 / CDN：
  ```json
  { "id": "...", "name": "...", "kind": "...", "version": "...", "channel": "...",
    "arch": "...", "artifact": { "url": "...", "sha256": "...", "size": 0 }, "manifest": {} }
  ```
- 制品放 GitHub Releases（含用户自编译的 coraza-caddy）。
- 客户端流程：拉索引 → 按 `channel` 比较版本 → 校验签名 + sha256 → 安装 / 替换 / 回滚。
- **协议抽象为接口**，未来若引入动态注册表服务，客户端无感。

**安全**：安装第三方制品 = 执行代码。必须签名 + 校验和 + 显式用户确认（与 `docker.sock` 提示同级告知）。

---

## 5. 制品源与升级（ADR-027）

### 5.1 制品源三通道

| 通道 | 说明 | 例子 |
|---|---|---|
| `official` | 上游官方 release | caddy、ddns-go、mosdns、flame |
| `custom` | 用户/团队 fork release | 自编译 coraza-caddy |
| `system` | 系统包管理器 | docker / dockerd |

**「最新版本」永远相对于源通道**：`custom` 源的最新版由该源的 Release 决定，不由上游决定。

### 5.2 升级策略三档

| 策略 | GateBox 行为 | 例子 |
|---|---|---|
| `replace` | 下载 → 校验 → 原子替换 → 健康检查 → 失败回滚（复用 ADR-002 模式） | caddy(任意源)、acme.sh、compose、ddns-go、mosdns、flame |
| `system` | 只显示版本，提示用包管理器，不自我替换 | docker / dockerd |
| `none` | 不可升级 | — |

---

## 6. 核心 vs 插件（ADR-030）

产品主闭环 = **「应用（容器/手工）→ 经域名由 Caddy 反代 → 自动 HTTPS」**。据此分类：

| 组件 | 主闭环必要性 | 档 | 生命周期 |
|---|---|---|---|
| **Caddy** | 代理引擎，不可缺 | core | managed / replace |
| **acme.sh** | 自动 HTTPS，产品招牌 | core | managed / replace |
| **docker + docker-compose** | 应用来源，产品定位 | core | docker=**attached**；compose=managed |
| ddns-go | 外网解析，可手动替代 | optional | managed / replace |
| flame | 导航，增强 UX | optional | managed / replace |
| mosdns | 内网解析，增强 | optional | managed / replace |
| tailscale | 组网，增强 | optional | managed / replace |
| coraza | WAF，增强 | optional | caddy-module |

- `core`：默认安装、默认启用、`Removable=false`。
- `optional`：按需安装；`Bundled=true` 者随离线包附带（ddns/flame）但默认停用。
- **正交提醒**：`core` 是产品重要性，`managed/attached` 是生命周期归属。**docker = `core + attached`**。

---

## 7. 系统架构图

```
GateBox（控制面：Go 后端 + Vue3 前端 + BoltDB）
    │  组件运行时：统一 Descriptor / 可选接口 / 注册表
    │  制品源：official | custom | system（索引 + 签名 + 下载）
    │  插件引擎：caddy-module | process | config-only
    │
    ├── [core]   caddy        反向代理 + 站点生成 + 文件证书加载
    ├── [core]   acme.sh      证书签发/续期
    ├── [core]   dockerd      attached，容器 API（被管对象：容器/镜像/网络/卷）
    ├── [core]   docker-compose 编排 CLI
    ├── [opt]    ddns-go      外网 DDNS
    ├── [opt]    flame        导航（iframe 内嵌）
    ├── [opt]    mosdns       内网 DNS
    ├── [opt]    tailscale    跨网组网
    └── [opt]    coraza       WAF（caddy-module）
```

**自动化联动**（已有 + 保留）：docker label → 派生 Service → Caddyfile → `/load`；命中受管根域的域名进入 acme 证书 + ddns 上报闭环；mosdns 内网解析。

---

## 8. 后端分层与命名标准（ADR-031）

### 8.1 目录

```
backend/
├── cmd/gatebox/main.go          # 入口 + 装配
└── internal/
    ├── server/      # 路由注册 + 中间件链（装配层）
    ├── handler/     # HTTP 翻译层
    ├── service/     # 用例编排层
    ├── repository/  # 本系统持久化（BoltDB）
    ├── model/       # 领域实体
    ├── component/   # 组件运行时：接口 + 注册表 + 状态 + 生命周期引擎
    ├── plugin/      # 插件引擎：manifest 解析 + 三形态实现
    ├── source/      # 制品源：索引 / 签名 / 下载 / 版本解析
    ├── adapter/     # 外部系统适配：caddy / docker / acme / ddns / mosdns / systemd
    ├── gateway/     # 领域：Caddyfile 生成 + label 派生
    ├── config/      # 配置加载
    └── web/         # 前端静态资源嵌入
```

**骨架基准**：后端采用 [go-nunu](https://github.com/go-nunu/nunu)（MIT）的分层词汇（`handler/service/repository/model`，`cmd/server`，Wire DI 可选），但**不引入 Casbin/RBAC/代码生成器**。

### 8.2 唯一判层规则

| 你要写的东西 | 放哪 | 硬规则 |
|---|---|---|
| 路由 / 参数解析 / 状态码 / 响应 | `handler` | 只做翻译，**禁止业务判断与外部调用** |
| 一个用户动作（如「建代理并同步 caddy」） | `service` | 用例编排，事务/回滚边界在此 |
| 读写本系统数据 | `repository` | 只出入 `model`，**禁止返回 HTTP/DTO** |
| 调用外部组件 | `adapter` 或 `plugin` | 每个外部系统一个包 |
| 组件/插件的统一抽象与生命周期 | `component` / `plugin` | 不直接依赖具体 adapter |
| 制品获取与版本解析 | `source` | 不依赖业务 |
| 领域数据 | `model` | **不依赖任何内部包** |
| 跨领域纯工具 | `pkg` | 无业务 |

### 8.3 依赖方向（lint 强制）

`handler → service → {repository, component, source} → model`；`server` 装配一切；`model` 依赖为零。用 `golangci-lint depguard` 将反向 import 变为构建错误。

### 8.4 命名

Go 包名全小写单词；类型 PascalCase；JSON 全 camelCase；测试 `_test.go`。

---

## 9. 前端分层与命名标准（ADR-032）

### 9.1 目录

```
frontend/src/
├── design/      # tokens：颜色/字号/间距/圆角的唯一来源
├── lib/         # 与业务无关：http、ws、format、message 封装
├── app/         # 骨架：main、router 实例、layout、stores、composables、通用组件
├── modules/     # 业务域：gateway / containers / domain / network / plugins / system
│   └── <domain>/{api.ts, views/, components/}
└── shared/      # 跨模块类型/常量（只被依赖，不依赖任何层）
```

**骨架基准**：保留 **Ant Design Vue 4.x**，布局与 CRUD 范式参照 `antdv-pro`（`nunu-layout-admin` 所用）；`modules/<domain>` 按领域聚合的目录约定参照 Vue Admin 的四层规范。**不引入 Element Plus，不引入完整 admin 模板。**

### 9.2 唯一判层规则

| 你要写的东西 | 放哪 |
|---|---|
| 某业务页面 | `modules/<域>/views/` |
| 页面私有组件 | `modules/<域>/components/` |
| 该域 API 调用 | `modules/<域>/api.ts` |
| 跨域通用组件 | `app/components/` |
| 布局 / 路由 / store | `app/` |
| 业务无关工具 | `lib/` |
| **颜色 / 字号 / 间距 / 圆角** | `design/tokens`（**禁止字面量**） |

### 9.3 设计 token 与「字号大一点」

所有视觉值来自 `design/tokens`（色彩、`fontSize.sm/base/lg`、间距 8px 网格、圆角）。规则：`.vue` 内**禁止** `font-size: 15px`、`color: #xxx` 等字面量，由 `stylelint` 强制。**「字号大一点」= 改 token 或换到下一档，不是逐页讨论项。**

### 9.4 依赖方向（lint 强制）

`modules → app → lib`；`shared` 单向；用 `eslint no-restricted-imports` 强制。禁止组件直接 import 其他 module 的 `views/`。

### 9.5 约定

Vue 组件 PascalCase 且与文件名同名；composable 一律 `useXxx`。

---

## 10. API 契约标准

- 统一前缀 `/api/v1`。
- 成功：直接返回资源（`201` / `204` 语义化）。
- 失败：`{"error":{"code":"SNAKE_CODE","message":"中文"}}`。
- 分页：`{"items":[],"total":n,"page":p,"pageSize":s}`（仅分页列表）。
- 时间：RFC3339；字段：全 camelCase。
- 由后端产出 OpenAPI，前端据此生成 TS 类型（消除手工漂移，并对外提供集成能力）。

---

## 11. 权限（极简，ADR-030）

- 单管理员 + 密码哈希 + 登录态中间件（JWT 或 session）。
- 定义一张 `Capability` 常量表**预留**未来扩展，**不引入 Casbin/RBAC**。
- 未来加能力只需在表里加项，不阻塞当前开发。

---

## 12. 运行时目录 `$DATA_DIR`

```
$DATA_DIR/
├── conf/                          # gatebox.db · secret.key · gatebox.conf
├── tools/                         # 所有组件/插件运行目录（一组件一目录）
│   ├── caddy/
│   │   ├── caddy                  # 当前激活二进制（= GATEBOX_CADDY_BIN 默认）
│   │   ├── versions/<ver>/        # 保留版本，供升级/回滚（ADR-002）
│   │   ├── user/                  # 用户扩展片段（import）
│   │   └── logs/ · data/
│   ├── acme/{acme.sh, certs/<fqdn>/{fullchain.pem,key.pem}}
│   ├── docker-compose/docker-compose
│   ├── ddnsgo/{ddns-go, .ddns_go_config.yaml}
│   ├── mosdns/ · tailscale/ · flame/
│   └── <plugin-id>/               # 插件制品同样落这里
├── appData/<projectName>/         # compose 项目（1 项目 = 1 目录）
├── Caddyfile                      # 最近一次成功 /load 的配置源备份
└── README.md
```

**规则**：组件与插件的可执行制品**统一落 `$DATA_DIR/tools/<id>/` 并在此运行**；`versions/` 支撑升级与回滚；不另设运行时 `plugins/` 目录。

---

## 13. 代码仓库目录（ADR-031）

```
gatebox/
├── go.work                  # 仅纳入活跃模块 ./backend（old 同 path 不能共存）
├── backend/                 # 新后端（§8）
├── frontend/                # 新前端（§9）
├── plugins/                 # 内置插件 manifest + schema + 文档
├── registry/                # 索引模板与 schema（索引本体托管于独立仓库）
├── tools/                   # 内置制品存档（离线安装用）：tools/<id>/<os>-<arch>/<binary>
├── old/                     # 原代码全量（old/backend 带独立 go.mod）
├── docs/                    # PRD · architecture · glossary · adr/
├── scripts/                 # install.sh / build.sh
└── configs/                 # systemd / openrc / procd 服务单元模板
```

- `old/backend` 自带独立 `go.mod`，为**冻结快照**；因其与新 module 同 path（`github.com/JiangBeta/gatebox`），**不能同时纳入 `go.work`**——故 `go.work` 仅纳入 `./backend`。查阅/构建旧码：`cd old/backend && GOWORK=off go build ./...`。
- `old/frontend` 不纳入新构建。
- 仓库 `tools/` 仅作**离线引导制品**（首次安装无网络时使用）；在线升级走 §5 制品源。现有平铺结构（`caddy`/`ddns-go`/`docker-compose`/`flare-amd64`/`flare-arm64`/`caddy.bak`）重建时规范为 `tools/<id>/<os>-<arch>/`。

---

## 14. 强制机制（跨开发者协作的胜负手）

约定不靠口头，靠工具报错：

- **后端**：`gofmt` / `go vet` / `golangci-lint depguard`（分层边界）。
- **前端**：`eslint no-restricted-imports`（依赖方向）+ `stylelint`（禁止样式字面量）。
- **API**：OpenAPI 生成 + 类型检查。
- **CI**：以上全部为门禁，未过不得合并。

---

## 15. 开发工作方法与分期

沿用既有方法：以「页面 & 功能」为开发单位，逐单位「讨论 → 撰写单位设计 → 开发 → 验证」（AI 验证 + 人工验证）。**当前阶段：先文档后代码。**

| 阶段 | 内容 | 交付物 |
|---|---|---|
| **P0 地基** | 目录重建 + `old/` + `go.work` + 标准文档 + lint/CI 门禁 | 可构建空骨架 + 门禁生效 |
| **P1 组件运行时** | `component` 接口/注册表 + `source` 索引/签名/下载 + 四个核心组件适配（caddy/acme/compose/docker） | 组件页可用、版本显示、升级（replace）闭环 |
| **P2 插件** | manifest + 三形态引擎 + 状态机 + 内置 Coraza/Tailscale 样例 | 插件页可装/启/停/卸 |
| **P3 前端重建** | `design/lib/app/modules` 骨架 + 网关/容器/域名页迁移 | 三大页零回归 |
| **P4 网络/首页/设置** | mosdns/tailscale 页 + flame 内嵌 + 极简权限 | 6 入口全通 |
| **P5 部署交付** | `install.sh` + 多架构 tarball + systemd/openrc/procd | 可安装可升级 |

> 每个阶段结束跑：后端 `mise run test` / `go vet` / `gofmt -l`；前端 `pnpm build`；人工验证对应页面。

---

## 16. 文档导航

- [PRD](PRD.md) · [术语表](glossary.md) · [ADR](adr/)（ADR-001 ~ ADR-032）
- 本文件为 v3 架构总纲；单位级设计见 `docs/{infra,docker,gateway,domain,network,home,deploy}.md`
