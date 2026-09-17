# GateBox 架构设计（v4）

> 状态：**已定稿**（框架本体 + 承载，编码前的唯一权威设计）
> 日期：2026-09-17
> 关联：`docs/PRD.md`、`docs/glossary.md`、`docs/adr/`（ADR-027 ~ ADR-041）、`JiangBeta/GateBoxStore`
> 旧版实现完整保留于 `./old/`（见 §14）。v3 总纲已由本文件取代（见 ADR-041）。

本文件是 **v4 总纲**：以「统一描述 / 可观测 / 声明式调和」为系统本体，定义组件的描述与生命周期、扩展平台与插件、分层与命名标准、目录蓝图与推进分期。**编码中凡遇「放哪一层 / 用什么字号 / 怎么命名」，以本文件为准。**

---

## 1. 定位与目标

GateBox 是为 HomeLab 打造的**极简、可视化 AppGateway**（「缝合怪」实现）：以统一界面整合成熟外部组件，让用户把 Docker 应用、静态站点、宿主服务通过域名访问，并自动完成代理 / 证书 / DNS。

v4 是一条总纲：**一份统一描述，支撑两条能力——读（可观测）与写（声明式调和）。**

**价值函数（冲突时的优先级）：**

1. **骨架清晰** —— 先立统一描述与组件运行时抽象（一切功能的地基）
2. **插件化扩展**
3. **低资源占用**（OpenWrt 等设备）—— 贯穿全程的硬约束
4. **现有功能零回归**（底线）

**推进方式：绞杀者渐进式**——旧功能逐个迁移，每步可 `mise run test` 验证。原始代码整体保留在 `./old/`。

---

## 2. 系统本体：统一描述 / 可观测 / 声明式调和

> 详见 [ADR-040](adr/ADR-040.md)（框架定义）与 [ADR-041](adr/ADR-041.md)（V4 结构）。异质组件（caddy / docker / acme / ddns-go / mosdns / 插件）无法照搬单一 config 模型，故以**统一描述符**为共同底座。

### 2.1 一份描述，两条能力

```
                        可扩展应用网关
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
   统一描述（模型层）  ──读──▶  可观测（读取面）        │
        │                      │                      │
        └──────────写─────────▶  声明式调和（执行面）  ──┘
                               │
        承载：BoltDB · Registry/Provider · 投影 · 组件适配器 · 制品源
```

**统一描述**（模型层）：让异种组件以同一方式被描述、成为整体的一部分，是系统唯一描述真相。
**可观测**（读取面）：描述与运行态的**投影**，回答「静态/动态 × 整体/局部」。
**声明式调和**（执行面）：由「意图 + 依赖图 + 生效契约」**推导**操作，而非预先写死。

三条能力共用同一份「组件描述符」，故天然不漂移。

### 2.2 统一描述：组件描述符

**组件（Component）** = 被纳入并统一管理、承担某项网关能力的**功能提供者**。内建（caddy/docker/acme）与外部插件**同构**（ADR-036 I1）；GateBox 不接管其生命周期实现，只描述、触发、观测。

**组件描述符** = 用统一方式重新描述组件的一份契约，构成 = **身份** + **功能清单** + **四契约**：

| 契约 | 内容 | 服务于 |
|---|---|---|
| **配置契约** | 可编辑字段 schema（类型/校验/文档） | 编辑（Form / YAML） |
| **信息契约** | `consumes` / `produces`（信息端口） | 功能地图与信息流 |
| **观测契约** | `state` / `activity` / `logs` / `metrics` | 可观测 |
| **生效契约** | `actions`（可执行操作）+ `effect`（收到变更时的自动动作）+ 幂等/顺序 | 声明式调和 |

**功能（Function）** = 网关对外提供的一项能力（代理 / TLS / 解析 / DDNS 上报 / 运行时 / 端口接入…），与组件**多对多**，是功能地图的语义主节点。

> 本框架**修订 ADR-020**：其「不做通用 key-value」仅适用于 Caddy 片段 / handler 参数等自由文本；顶层业务对象必须 schema 化。

### 2.3 事实与信息

**事实（Fact）** = 可寻址、被持久化的**期望态单元**。分三类：

| 类别 | 定义 | 例 |
|---|---|---|
| **持久事实** | 可寻址、落库、期望态、调和可写 | Service(manual) / Domain / Credential / Fragment / Variable / PortBinding / ComposeInstance |
| **派生事实** | 有 ID、可作边端点、可展示，但**不落库**，由事实 + 运行态推导 | docker 从 label 派生的 Service |
| **运行信息** | 只在流上传递、表达「某类数据」 | 容器状态 / IP、证书文件、label |

> 事实集由**中央事实注册表**声明（事实属于系统，不属于组件）；**派生事实不可被调和写入**，只能改其源头（compose 文件）。

**信息（Info）** = 在组件间流转的类型化数据（`service` / `domain` / `cert` / `port` / `variable` / `label` / `ip`），是**边的载荷**。来源 = 事实的投影，或组件运行时产出。**分类按角色，不按值**（Domain 是事实，`domain` 信息是它的投影）。

### 2.4 功能地图

由 **组件 + 功能 + 信息流** 构成的**全景图**，提供**双层投影**：

- **组件视角**（系统/排障）：节点 = 组件，节点内列功能端口；
- **功能视角**（人/总览）：节点 = 功能，边 = 信息流，节点下标注实现组件。

**状态归属规则**：状态 / 活动 / 日志**挂在组件**；功能节点上的状态 = 其实现组件状态的**聚合**。

### 2.5 可观测（读取面）

| | 静态（是什么） | 动态（正在发生什么） |
|---|---|---|
| **整体** | 功能地图（结构） | 传播轨迹（Run / Step） |
| **局部** | 组件（配置 / 版本 / 健康） | 组件内部（当前活动 / 步骤 / 日志） |

由**观测契约**提供：

- `state`：健康枚举（`healthy / degraded / error / running / stopped / unknown`）
- `activity`：当前任务与所处步骤（「acme 执行到哪一步」）
- `logs`：组件内部痕迹 / 日志来源
- `metrics`：定量指标（QPS / 延迟 / 连接 / 缓存命中…）

> `metrics` 不可省，否则「可观测」退化为只有红绿灯；`activity` 是「执行位置」的数据来源。

### 2.6 声明式调和（执行面）

三个输入：

- **意图（Intent）**：只声明「哪个事实变了」（what changed），不含如何传播。
- **依赖图（Dependency Graph）**：由**信息契约端口 + 事实间引用**推导（类型骨架 + 实例绑定），**算出来的、不是画出来的**。
- **生效契约（Effect Contract）**：组件声明收到某类信息变更时做什么（`update-config / restart / hot-reload / reissue / deploy`），含幂等与顺序。

调和器（level-triggered，对齐 ADR-036 §6）：

```
采集期望态 → 推导依赖图 → 求变更事实的消费闭包
→ 按生效契约生成动作 → 拓扑排序（生产者先于消费者）→ 执行 → 记录 Run/Step
```

性质：**幂等、可重放、周期兜底**（捕捉外部漂移）。写操作只做两件事：**落库事实 + `Trigger()`（去抖合并）**，HTTP 接口同步返回「已受理 + runId」，进度经事件流推送。

**运行记录**分三层 + 日志：

| 层 | 内容 | 用途 |
|---|---|---|
| **Run** | `{runId, 触发意图, 触发类型(intent/periodic/manual), 起止, 总状态}` | 整体动态 |
| **Step** | `{component, action, status, 起止, error, 前驱}` | 图上节点 / 边着色（默认粒度） |
| **Event** | 步骤内细粒度事件（如 acme：DNS 记录已添加 / 等待验证 / 证书已下载） | 当前执行位置 |
| **Logs** | 独立连续流，用 `runId` / `stepId` 关联，**不结构化塞入 Run** | 下钻细节 |

**收敛**：幂等重放 + 失败重试 + 漂移检测；失败**不全局回滚**，标记 `degraded/error` 并在下轮收敛（ADR-040 §3.4）。

**自动传播示例**：删除某代理 → 图中该 Service 的 `domain` 边存在 → 触发 ddns 的 `effect`（删域名 + 重启）；「删域名」本身又是一次事实变更 → 递归推导 → 触发ddns 重启。同一机制跑两层，而非两段写死代码。

---

## 3. 组件与生命周期（承载）

### 3.1 生命周期六层

| 层 | 定义 | 归属 |
|---|---|---|
| **配方 Recipe** | 制品由什么构成：二进制版本 + 编译进哪些模块 | **上游官方** |
| **制品 Artifact** | 配方产出的可执行文件 | 上游构建 |
| **制品源 Source** | 制品从哪来（`official` / `custom` / `system`） | 分档（见 §6） |
| **运行态 Runtime** | 进程启停、健康、日志、崩溃恢复 | **GateBox** |
| **配置 Config** | 读写配置、热加载 | **GateBox** |
| **版本 Version** | 读当前版本、查上游最新版（只读） | **GateBox** |
| **升级 Upgrade** | 用新制品替换旧制品 | 边界（搬运，不造配方） |

### 3.2 边界规则（ADR-027）

> **GateBox 管理「进程」与「配置」，观测「版本」，但永不改写「配方」。升级 = 用上游发布的制品替换旧制品——GateBox 只当搬运工，不当配方师。**

推论：

- **Coraza（WAF）**：启用 = 改变配方（xcaddy 拼模块）。因此只能以**已发布的制品变体**存在（官方或用户自建并发布的制品），设备端**不编译**。
- **Docker**：`dockerd` 是**组件**（`attached`，仅经 API 连接）；**容器**是**被管对象**。精确表述：**不接管 dockerd，但全权接管容器。**

### 3.3 组件 vs 被管对象

| | 组件（Component） | 被管对象（Managed Object） |
|---|---|---|
| 例子 | caddy、acme.sh、dockerd、mosdns、插件 | 容器、镜像、网络、卷、compose 项目、代理规则、域名 |
| 谁定义其运行逻辑 | 上游（配方） | GateBox |
| GateBox 角色 | 供应的搬运 + 运行态管理 + 配置读写 | 完全所有 |

---

## 4. 组件模型与描述符

### 4.1 Descriptor（唯一描述）

扩展 v3 的 `component.Descriptor` 为「身份 + 功能 + 四契约」；`extension.Provider` 的能力声明并入「功能」；**插件 manifest 编译为同一 `Descriptor`**。生命周期元数据保留：

```go
type Descriptor struct {
    // 身份
    ID, Name, Version string
    Kind              Kind   // core | caddy-module | process | config-only
    Tier              string // core | optional
    // 生命周期（承载，ADR-027/028）
    Source    Source // channel(official|custom|system) + index + artifact
    Provision string // managed | attached
    Runtime   string // manage | observe
    Upgrade   string // replace | system | none
    // 功能 + 四契约（ADR-040）
    Functions  []Function       // 提供哪些功能
    Config     []ConfigField    // 配置契约
    Consumes   []InfoPort       // 信息契约
    Produces   []InfoPort       // 信息契约
    Observable Observability    // 观测契约
    Effect     EffectSpec       // 生效契约
}
```

### 4.2 运行时接口（可选接口，能力驱动 UI）

不使用万能大接口。组件按能力实现可选接口，UI 的可用操作由描述符的功能 / 契约驱动：

```go
type Runnable     interface { Start(ctx) error; Stop(ctx) error; Restart(ctx) error }
type Health       interface { Status(ctx) (Status, error) } // state + healthy + metrics
type Activity     interface { Activity(ctx) (Activity, error) } // 当前任务/步骤（ADR-041 §7）
type Configurable interface { ReadConfig(ctx) ([]byte, error); WriteConfig(ctx, []byte) error; Reload(ctx) error }
type Upgradable   interface { CheckUpdate(ctx) (VersionInfo, error); Upgrade(ctx, ver string) error; Rollback(ctx) error }
type Loggable     interface { Logs(ctx, LogOpts) (io.ReadCloser, error) }
type Operable     interface { Operations() []Operation; Operate(ctx, opID string, in map[string]string) (Result, error) }

type Registry interface { Get(id string) (Component, bool); List() []Descriptor }
```

- 核心组件与插件**同一接口**，区别在 `Kind` / `Tier` / 生命周期策略。
- `system` 档组件（docker）只实现 `Health + Operable + Activity` → UI 自动不显示启停/升级键。

### 4.3 状态模型

```
State ∈ { running | stopped | error | unknown }
Status  { State, Healthy bool, Message, Since, Metrics map[string]any }
Activity { Task string, Step string, Progress float64, Actor string, Since }
```

---

## 5. 扩展平台与插件（承载）

> ADR-029 的「三种 kind」升级为**五个部件 + 两类贡献原型**。核心只认扩展点、不认插件身份。
> 三个不变式：**I1** 核心无插件身份；**I2** 核心→插件只经投影 API；**I3** 插件→核心只经贡献声明。

### 5.1 五个部件

| 部件 | 职责 |
|---|---|
| **Manifest v2** | 强类型：`artifacts[]`（带 role）、`contributes{capabilities,ui,backend,data}`、`permissions`（v4 additive 扩展见 §5.6） |
| **能力注册表** | 核心与插件统一注册「能力」；网关/容器/前端只查表 |
| **投影 API** | 核心状态只读视图（domains/certs/ports/services）+ `revision` + 变更通知 |
| **扩展契约** | `template`（声明式）或 `sidecar`（进程，JSON over stdin/HTTP） |
| **槽位/页面注册表** | 前端具名合并点 + 三档渲染 |

### 5.2 两类贡献原型

| 原型 | 贡献点 | 作用 | 例 |
|---|---|---|---|
| **Provider（能力型）** | `capabilities` + `backend.renderer` + `ui.slots` | 扩展核心行为 | Caddy-L4 |
| **Consumer（数据型）** | `data.subscribe` + `backend.config-sync`/`reconcile` + `ui.page` | 消费核心数据、作用于外部 | ddns-go |

核心内置能力注册为 `_core` provider，与插件同构。在 v4 中，两类原型都**编译为 `Descriptor`**：能力→功能，`data.subscribe`→信息契约，`config-sync`/`reconcile`→生效契约。

### 5.3 扩展点（point）

| point | 语义 |
|---|---|
| `proxy-protocols` | 可代理的协议类别（`class`、`label`、`networks`、`requiresPrimaryDomain`） |
| `renderer` | 中性规则 → Caddyfile 片段（`for`、`scope`、`impl`: template\|sidecar） |
| `validator` | 校验器（`bin` 缺省 = 当前 active caddy 制品） |
| `config-sync` | 从投影渲染插件配置并落盘 + reload（`projection`、`target`、`template`） |
| `reconcile` | 核心通知侧车，侧车拉投影自行收敛（`entry`、`projection`、`interval`） |
| `component-variant` | 核心组件配方变体：插件声明中性**特征**，内核按特征并集解析变体制品（ADR-038） |
| `dns-provider` | DNS 凭证供应商：字段 schema + acme hook 名 + env 映射，驱动动态表单（ADR-039 §5） |

### 5.4 中性规则与渲染分发

核心只定义 `ProxyRule{Protocol, Upstream, Ports, Nets}`：HTTP 走内置渲染器（site block）；非 HTTP 按协议查注册表 → 调用其 renderer → 全局块片段。**核心不解析片段内容，不认识 `layer4`。**

### 5.5 前端渲染三档

| 层 | 形态 | 隔离 | 本阶段 |
|---|---|---|---|
| L0 元数据驱动 | 核心按 slot/schema 渲染 | — | 实现 |
| L1 iframe + postMessage 桥 | 强隔离 | 实现 |
| L2 远程 ESM 组件 | 同源（等价 XSS） | 预留，按信任级门禁 |

### 5.6 v4 additive 契约字段（可选，minor）

保持 `apiVersion: gatebox/v2`，新增可选字段（缺失则编译为最小描述符，向后兼容）：

```yaml
provides:      [ reverse-proxy ]          # 功能（Function）
consumes:      [ domain, credential ]     # 信息契约：消费
produces:      [ cert ]                   # 信息契约：产出
observability: { state: true, activity: true, logs: true, metrics: [qps, latency] }
effect:
  - on: domain
    do: [ update-config, restart ]        # 生效契约
requires: { components: [caddy], gatebox, extensionApi, os, arch }
```

### 5.7 状态机与持久化

```
available ──install──▶ installed ──enable──▶ enabled
    ▲                      │                    │
    └─────────remove───────┴──────disable───────┘
                         任一环节失败 ──▶ error
```

- BoltDB bucket：`plugins`（安装状态 + 用户配置 + 制品清单 + 权限）、`component_state`（版本/来源缓存）、`runs`（v4 新增，运行记录，见 §12）。
- 卸载语义三档：`disable`（留制品配置）/ `uninstall`（删制品 + 注销贡献 + 配方重算 + 保留数据）/ `purge`（删数据，二次确认）。

### 5.8 分发：GateBoxStore + 静态索引（无服务端）

- 索引 `index.json`（`catalog.v1`）+ Ed25519 签名，托管于 `JiangBeta/GateBoxStore`（raw 直链），制品放 GitHub Releases。
- 每个插件打包为单一签名 tar.gz（manifest + `binary`/`sidecar`/`ui`/`assets`）。
- 客户端：磁盘 manifest 加载（内置）→ 在线拉索引 → 按 `channel` 比较版本 → 校验 sha256 → 安装/替换/回滚；**签名验签预留**。
- **制品矩阵**：`index.json` 含 `variants[]`（核心组件配方变体，ADR-038）。

**安全**：安装第三方制品 = 执行代码。必须签名 + 校验和 + 显式用户确认。插件权限经 **plugin token + scope** 强制（ADR-039 §2）。

---

## 6. 制品源与升级（ADR-027）

### 6.1 制品源三通道

| 通道 | 说明 | 例子 |
|---|---|---|
| `official` | 上游官方 release | caddy、ddns-go、mosdns、flame |
| `custom` | 用户/团队 fork release | 自编译 coraza-caddy |
| `system` | 系统包管理器 | docker / dockerd |

**「最新版本」永远相对于源通道。**

### 6.2 升级策略三档

| 策略 | GateBox 行为 | 例子 |
|---|---|---|
| `replace` | 下载 → 校验 → 原子替换 → 健康检查 → 失败回滚 | caddy、acme.sh、compose、ddns-go、mosdns、flame |
| `system` | 只显示版本，提示用包管理器 | docker / dockerd |
| `none` | 不可升级 | — |

---

## 7. 核心 vs 插件（ADR-030）

产品主闭环 = **「应用（容器/手工）→ 经域名由 Caddy 反代 → 自动 HTTPS」**。

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

**正交提醒**：`core` 是产品重要性，`managed/attached` 是生命周期归属。**docker = `core + attached`**。

---

## 8. 系统架构图与自动化联动

```
GateBox（控制面：Go 后端 + Vue3 前端 + BoltDB）
    │  统一描述：Descriptor（身份 + 功能 + 四契约）+ 事实注册表
    │  可观测：观测契约 + Run/Step/Event + SSE
    │  声明式调和：意图 + 依赖图 + 调和器（internal/{graph,reconcile,observe}）
    │  承载：组件运行时 · 制品源 · 扩展平台 · 插件引擎
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

**自动化联动（v4：显式依赖图 + 调和器）**：docker label → 派生 Service（事实）→ 依赖图推导 → 调和 caddy（Caddyfile → `/load`）→ 命中受管根域则调和 acme（签发）→ 作为投影供插件（ddns-go）经 `config-sync`/`reconcile` 收敛。**核心不直接驱动 ddns，只图驱动调和**（ADR-036/040/041）。

---

## 9. 后端分层与命名标准

### 9.1 目录

```
backend/
├── cmd/gatebox/main.go          # 入口 + 装配
└── internal/
    ├── server/      # 路由注册 + 中间件链（装配层）
    ├── handler/     # HTTP 翻译层
    ├── service/     # 用例编排层（v4：校验 + 落库 + 提交意图）
    ├── reconcile/   # 【v4 新增】意图 + 调和器 + Run/Step
    ├── graph/       # 【v4 新增】事实注册表 + 引用解析 + 依赖图推导
    ├── observe/     # 【v4 新增】观测聚合 + 事件总线（SSE）
    ├── repository/  # 本系统持久化（BoltDB）
    ├── model/       # 领域实体
    ├── component/   # 组件运行时：Descriptor + 接口 + 注册表 + 生命周期
    ├── plugin/      # 插件引擎：manifest 解析 + 编译为 Descriptor
    ├── extension/   # 扩展平台：能力注册表 + 投影 + 扩展契约
    ├── source/      # 制品源：索引 / 签名 / 下载 / 版本解析
    ├── adapter/     # 外部系统适配：caddy / docker / acme / ddns / mosdns / systemd
    ├── gateway/     # 领域：Caddyfile 生成 + label 派生
    ├── config/      # 配置加载
    └── web/         # 前端静态资源嵌入
```

### 9.2 唯一判层规则

| 你要写的东西 | 放哪 | 硬规则 |
|---|---|---|
| 路由 / 参数解析 / 状态码 / 响应 | `handler` | 只做翻译，**禁止业务判断与外部调用** |
| 一个用户动作 | `service` | 校验 + 落库 + 提交意图；**事务边界在此**；不直接调 `adapter` |
| 变更如何传播 / 调和某个组件 | `reconcile` | 不依赖 `handler`/`adapter/gateway` |
| 事实注册 / 引用 / 依赖图 | `graph` | 只依赖 `model` + `component` |
| 状态 / 活动 / 日志聚合与推送 | `observe` | 只依赖 `component` + `model` |
| 读写本系统数据 | `repository` | 只出入 `model`，**禁止返回 HTTP/DTO** |
| 调用外部组件 | `adapter` 或 `plugin` | 每个外部系统一个包 |
| 组件抽象与生命周期 | `component` | 不直接依赖具体 adapter |
| 扩展点、能力注册表、投影契约 | `extension` | 不依赖具体插件实现 |
| 制品获取与版本解析 | `source` | 不依赖业务 |
| 领域数据 | `model` | **不依赖任何内部包** |

### 9.3 依赖方向（lint 强制）

```
handler → service → {reconcile, repository}
reconcile → {graph, component, observe, repository}
graph → {model, component}
observe → {component, model}
plugin → {component, extension}
component → {model, source}
extension → model
model → （零依赖）
server 装配一切
```

用 `golangci-lint depguard` 将反向 import 变为构建错误。

### 9.4 命名

Go 包名全小写单词；类型 PascalCase；JSON 全 camelCase；测试 `_test.go`。

---

## 10. 前端分层与命名标准

### 10.1 目录

```
frontend/src/
├── design/      # tokens：颜色/字号/间距/圆角唯一来源
├── lib/         # 与业务无关：http、ws、format、message
│   └── schema/  # 【v4 新增】schema → 渲染引擎（业务无关、可测）
├── app/         # 骨架：main、router、layout、stores、composables、通用组件
│   └── components/  # 含 schema-form / schema-yaml / schema-card 等通用渲染器
├── modules/     # 业务域：gateway / containers / domain / network / plugins / system
│   └── <domain>/{api.ts, views/, components/}
└── shared/      # 跨模块类型/常量（只被依赖，不依赖任何层）
```

**骨架基准**：保留 **Ant Design Vue 4.x**；图表库（如需）与地图库 `@vue-flow/core` 按需引入（ADR-041）。**不引入 Element Plus，不引入完整 admin 模板。**

### 10.2 唯一判层规则

| 你要写的东西 | 放哪 |
|---|---|
| 某业务页面 | `modules/<域>/views/` |
| 页面私有组件 | `modules/<域>/components/` |
| 该域 API 调用 | `modules/<域>/api.ts` |
| schema 解析 / 序列化 / 校验（业务无关） | `lib/schema/` |
| 通用 Form / YAML / 卡片 / 拓扑渲染器 | `app/components/` |
| 布局 / 路由 / store | `app/` |
| 业务无关工具 | `lib/` |
| **颜色 / 字号 / 间距 / 圆角** | `design/tokens`（**禁止字面量**） |

### 10.3 派生引擎（v4）

- **配置契约驱动**：后端 `/api/v1/descriptors` 与 `/api/v1/schema/{factKind}` 下发字段 schema，`lib/schema` 据此生成 Form / YAML / 补全 / 校验；字段集精简起步（`text/number/select/switch/textarea/array/object/reference` + `advanced/summaryFields`）。
- **双模范围**：只有**有文本源**的对象做 Form ↔ YAML（compose 已有、Fragment、后续 gatebox.conf）；无文本源者 = Form + 只读预览。
- **功能地图**：`@vue-flow/core` + 双层投影（组件/功能视角）+ 手写分层布局；节点位置按内容指纹存 localStorage；点击打开详情/编辑抽屉。
- **状态与活动**：`/api/v1/events`（SSE）驱动节点着色与「当前执行位置」展示。

### 10.4 设计 token 与「字号大一点」

所有视觉值来自 `design/tokens`。`.vue` 内**禁止** `font-size: 15px`、`color: #xxx` 等字面量，由 `stylelint` 强制。**「字号大一点」= 改 token 或换到下一档。**

### 10.5 依赖方向（lint 强制）

`modules → app → lib`；`shared` 单向；`eslint no-restricted-imports` 强制。禁止组件直接 import 其他 module 的 `views/`。

### 10.6 约定

Vue 组件 PascalCase 且与文件名同名；composable 一律 `useXxx`。

---

## 11. API 契约标准

- 统一前缀 `/api/v1`。
- 成功：直接返回资源（`201` / `204` 语义化）。
- 失败：`{"error":{"code":"SNAKE_CODE","message":"中文"}}`。
- 分页：`{"items":[],"total":n,"page":p,"pageSize":s}`。
- 时间：RFC3339；字段：全 camelCase。
- 由后端产出 OpenAPI，前端据此生成 TS 类型。

### 11.1 v4 新增端点

| 方法 | 路径 | 作用 |
|---|---|---|
| GET | `/descriptors` | 全部组件描述符（派生引擎数据源） |
| GET | `/graph?view=component\|function&root=<tag>` | 功能地图 / 依赖图 |
| GET | `/runs` · `/runs/{id}` | 运行记录列表 / 详情 |
| GET | `/components/{id}/status` · `/activity` · `/logs` | 局部观测 |
| GET | `/events` | **SSE**：Run/Step 推进与状态变化 |
| GET | `/schema/{factKind}` | 配置契约（驱动 Form/YAML） |

投影 `/api/v1/extensions/me/projection/*` 保持不变。

---

## 12. 运行时目录与持久化

### 12.1 `$DATA_DIR`

```
$DATA_DIR/
├── conf/                          # gatebox.db · secret.key · gatebox.conf
├── tools/                         # 所有组件/插件运行目录（一组件一目录）
│   ├── caddy/{caddy, versions/<ver>/, user/, logs/, data/}
│   ├── acme/{acme.sh, certs/<fqdn>/{fullchain.pem,key.pem}}
│   ├── docker-compose/docker-compose
│   ├── ddnsgo/ · mosdns/ · tailscale/ · flame/
│   └── <plugin-id>/
├── appData/<projectName>/         # compose 项目（1 项目 = 1 目录）
├── Caddyfile                      # 最近一次成功 /load 的配置源备份
└── README.md
```

### 12.2 BoltDB buckets（v4）

| bucket | 用途 |
|---|---|
| 事实 buckets | `domains` / `dns_credentials` / `apps` / `services` / `fragments` / `variables` / `port_bindings` / `compose_instances` … |
| `plugins` / `component_state` | 插件与组件状态（承载） |
| **`runs`**（v4 新增） | 最近 N 条 Run/Step（JSON）；Event 仅内存；Logs 不落库 |

保留策略：`runs` 按**条数 + 天数**双限；依赖图**不落库**（推导物）。

---

## 13. 权限（极简，ADR-030）

- 单管理员 + 密码哈希 + 登录态中间件。
- 一张 `Capability` 常量表**预留**未来扩展，**不引入 Casbin/RBAC**。

### 13.1 服务身份与 Docker 权限（ADR-034）

- GateBox 本体以专用低权用户 `gatebox` 运行，托管进程同权限。
- Docker 权限：API 访问 = 加入 `docker.sock` 属组；`docker.service` 的 reload/restart = polkit 最小授权（OpenRC/procd 回退 sudoers）。
- 重启语义：默认软重启（`reload`，不中断容器）；「强制重启」= `restart`（中断容器，UI 二次确认）。

---

## 14. 代码仓库目录

```
gatebox/
├── go.work                  # 仅纳入活跃模块 ./backend
├── backend/                 # 新后端（§9）
├── frontend/                # 新前端（§10）
├── plugins/schema/          # 扩展契约权威：manifest.v2 schema
├── registry/schema/         # 扩展契约权威：catalog.v1 schema（含 variants[]）
├── tools/                   # 内置制品存档（离线安装用）：tools/<id>/<os>-<arch>/<binary>
├── old/                     # 原代码全量（old/backend 带独立 go.mod）
├── docs/                    # PRD · architecture · glossary · adr/
├── scripts/                 # install.sh / build.sh
└── configs/                 # systemd / openrc / procd 服务单元模板
```

> **插件源码不在此仓库**：迁至 `JiangBeta/GateBoxStore`（`plugins/<id>/` + `schema/` + `cmd/gbx-store/` + `variants.yaml` + CI + `index.json`）。本仓库 `plugins/`、`registry/` 仅保留 **schema 权威**，Store vendor 一份并在 CI 校验一致（ADR-037 §6）。

- `old/backend` 自带独立 `go.mod`，为**冻结快照**；与新 module 同 path，**不能同时纳入 `go.work`**。查阅/构建旧码：`cd old/backend && GOWORK=off go build ./...`。
- 仓库 `tools/` 仅作**离线引导制品**；在线升级走 §6 制品源。

---

## 15. 强制机制（跨开发者协作的胜负手）

约定不靠口头，靠工具报错：

- **后端**：`gofmt` / `go vet` / `golangci-lint depguard`（分层边界）。
- **前端**：`eslint no-restricted-imports`（依赖方向）+ `stylelint`（禁止样式字面量）。
- **API**：OpenAPI 生成 + 类型检查。
- **CI**：以上全部为门禁，未过不得合并。

---

## 16. 开发工作方法与分期

以「页面 & 功能」为开发单位，逐单位「讨论 → 撰写单位设计 → 开发 → 验证」。**当前阶段：先文档后代码。**

### 16.1 v4 分期（纵轴 = 能力层，横轴 = 先内建后插件）

| 层 | 内容 | 交付物 |
|---|---|---|
| **L1 静态自描述** | 唯一 `Descriptor` + 事实注册表 + 依赖图推导 + `/descriptors` · `/graph` · `/schema/{kind}`；前端派生引擎 + 功能地图（只读） | 功能地图可看、表单由 schema 生成 |
| **L2 运行态观测** | 观测契约（`Activity` 接口）+ `observe` 包 + `/components/{id}/{status,activity,logs}` + `/events`(SSE) + Run/Step 存储 | 图上可见组件状态 / 活动 / 日志 |
| **L3 执行可追踪** | `reconcile` 包 + 意图 + 调和器 + 传播轨迹；**首个闭环 = 网关域**，替换 `reloadCaddy` 并修触发缺口 | 变更自动传播、可追踪、可重放 |

顺序：先内建组件（caddy/acme/docker/ddns），后插件；**首个验证域 = 网关**（Service/Domain/Credential/Port/Variable）。

### 16.2 承载建设（v3 的 P0–P5，已完成或进行中，并入不再并列）

| 阶段 | 内容 |
|---|---|
| P0 地基 | 目录重建 + `old/` + `go.work` + 标准文档 + lint/CI 门禁 |
| P1 组件运行时 | `component` 接口/注册表 + `source` 索引/签名/下载 + 核心组件适配 |
| P2 插件/扩展 | manifest v2 + 引擎 + 状态机 + 能力注册表 + 投影 API |
| P3 前端重建 | `design/lib/app/modules` 骨架 + 网关/容器/域名页迁移 |
| P4 网络/首页/设置 | mosdns/tailscale 页 + flame 内嵌 + 极简权限 |
| P5 部署交付 | `install.sh` + 多架构 tarball + systemd/openrc/procd |

> 每个阶段结束跑：后端 `mise run test` / `go vet` / `gofmt -l`；前端 `pnpm build`；人工验证对应页面。

---

## 17. 文档导航

- [PRD](PRD.md) · [术语表](glossary.md) · [ADR](adr/)（ADR-001 ~ ADR-041）
- 统一框架：[ADR-040](adr/ADR-040.md)（统一描述 / 可观测 / 声明式调和） · [ADR-041](adr/ADR-041.md)（架构 V4）
- 插件拔插化：[ADR-037](adr/ADR-037.md) · [ADR-038](adr/ADR-038.md) · [ADR-039](adr/ADR-039.md)
- 插件作者文档见 GateBoxStore 的 `docs/`（`architecture-v4` / `plugin-authoring` / `catalog` / `extension-api` / `plugin-ui`）
- 本文件为 v4 架构总纲；单位级设计见 `docs/{infra,docker,gateway,domain,network,home,deploy}.md`
