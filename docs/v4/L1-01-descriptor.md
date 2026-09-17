# L1-U1 · 组件描述符契约与注册

> 上层：[architecture.md §2/§4](../architecture.md) · [ADR-040](../adr/ADR-040.md)（四契约） · [ADR-041](../adr/ADR-041.md)（唯一 Descriptor）
> 状态：设计稿（待评审）· 依赖：无（L1 地基）

## 1. 目标与非目标

**目标**

1. 收敛为**唯一 `component.Descriptor`**：身份 + 功能清单 + 四契约（配置 / 信息 / 观测 / 生效）。
2. 内建组件（caddy/acme/docker/…）与插件（manifest）**编译为同一 `Descriptor`**。
3. 暴露 `GET /api/v1/descriptors`，作为前端派生引擎与功能地图的数据源。

**非目标**

- 不做依赖图推导（U2）、不做 schema 端点（U3）、不做前端渲染（U4/U5）。
- 不改生命周期语义（承载，ADR-027/028）；`Capabilities` 保留。

## 2. 现状与问题

| 现有物 | 位置 | 问题 |
|---|---|---|
| `component.Descriptor` | `internal/component/component.go:28` | 仅生命周期元数据；无功能/四契约 |
| `core` 私结构 + `CoreRegistry` | `internal/component/registry.go` | 内建组件硬编码，缺功能/信息声明 |
| `component` 可选接口 | `component.go:79-105` | 已有 Runnable/Health/Configurable/…；缺 `Activity` |
| `extension.Provider` / `Capability` | `internal/extension/registry.go` | 扩展点能力表；与组件描述分离 |
| `plugin.Manifest` → `providerFor` | `internal/plugin/plugin.go:534` | 只编译 capabilities/renderer；无描述符 |

U1 在不破坏上述结构的前提下，把「描述」收敛到 `Descriptor`：能力表继续承担扩展点分发，功能与四契约进入描述符。

## 3. 类型设计

全部落在 `internal/component`（`graph`/`reconcile`/`observe` 可依赖 `component`）。

### 3.1 功能（Function）

```go
// Function 组件对外提供的一项能力（功能地图的语义节点）。
type Function struct {
    ID          string `json:"id"`                    // 稳定标识，如 reverse-proxy
    Label       string `json:"label"`                 // 中文展示名
    Description string `json:"description,omitempty"`
    Icon        string `json:"icon,omitempty"`
}
```

**L1 初始功能词表**（可扩展，命名用 kebab-case）：

| Function ID | Label | 提供者 |
|---|---|---|
| `reverse-proxy` | 反向代理 | caddy |
| `static-serve` | 静态文件服务 | caddy |
| `http-listen` | HTTP/HTTPS 接入 | caddy |
| `cert-issue` | 证书签发/续期 | acme |
| `container-runtime` | 容器运行时 | docker |
| `label-publish` | 容器标签产出 | docker |
| `compose-orchestrate` | 编排 | docker-compose |
| `ddns-publish` | 外网 DDNS 上报 | ddns-go |
| `dns-resolve` | 内网 DNS 解析 | mosdns |
| `nav-portal` | 导航门户 | flame |
| `mesh-network` | 组网 | tailscale |
| `waf` | WAF | coraza |

### 3.2 信息类型（InfoType）

```go
type InfoType string

const (
    InfoService    InfoType = "service"    // 代理服务定义
    InfoDomain     InfoType = "domain"     // 域名行 / 根域
    InfoCredential InfoType = "credential" // DNS 凭证
    InfoFragment   InfoType = "fragment"   // Caddy 片段
    InfoVariable   InfoType = "variable"   // 统一变量
    InfoPort       InfoType = "port"       // 协议端口
    InfoLabel      InfoType = "label"      // docker label（运行信息）
    InfoCert       InfoType = "cert"       // 证书文件（运行信息）
    InfoIP         InfoType = "ip"         // 解析出的 IP（运行信息）
)
```

信息类型是**开放字符串**：未知类型不报错，仅不参与已知图推导。

### 3.3 配置契约（ConfigField）

精简起步（ADR-041 §10），属性 `advanced` / `summaryFields` 支持，其余按需后置。

```go
type ConfigType string

const (
    ConfigText     ConfigType = "text"
    ConfigNumber   ConfigType = "number"
    ConfigSelect   ConfigType = "select"
    ConfigSwitch   ConfigType = "switch"
    ConfigTextarea ConfigType = "textarea"
    ConfigArray    ConfigType = "array"
    ConfigObject   ConfigType = "object"
    ConfigReference ConfigType = "reference"
)

type FieldOption struct {
    Label string `json:"label"`
    Value any    `json:"value"`
}

// ReferenceSpec reference 类型字段的引用语义（对齐 oxidns）。
type ReferenceSpec struct {
    Types       []string `json:"types,omitempty"`       // 被引用的事实/组件类别
    Prefix      string   `json:"prefix,omitempty"`      // "$" | ""
    AllowInvert bool     `json:"allowInvert,omitempty"`
}

type ConfigField struct {
    Key           string         `json:"key"`
    Label         string         `json:"label"`
    Type          ConfigType     `json:"type"`
    Required      bool           `json:"required,omitempty"`
    Default       any            `json:"default,omitempty"`
    Advanced      bool           `json:"advanced,omitempty"`
    Placeholder   string         `json:"placeholder,omitempty"`
    Description   string         `json:"description,omitempty"`
    Docs          string         `json:"docs,omitempty"`
    Options       []FieldOption  `json:"options,omitempty"`       // select
    Reference     *ReferenceSpec `json:"reference,omitempty"`     // reference
    Item          *ConfigField   `json:"item,omitempty"`          // array 元素
    Fields        []ConfigField  `json:"fields,omitempty"`        // object
    SummaryFields []string       `json:"summaryFields,omitempty"` // 折叠摘要
}
```

> **动态 schema 特例**：DNS 凭证的字段由 `dns-provider` 能力动态提供（`extension.ProviderField`）。U3 将把 `ProviderField` 泛化为 `ConfigField`，并在 `/schema/{factKind}` 中按 provider 聚合。

### 3.4 信息契约（InfoPort）

```go
// EffectAction 生效动作（枚举）。
type EffectAction string

const (
    EffectUpdateConfig EffectAction = "update-config" // 重写配置
    EffectHotReload    EffectAction = "hot-reload"    // 热重载（不中断）
    EffectRestart      EffectAction = "restart"       // 重启
    EffectReissue      EffectAction = "reissue"       // 重新签发
    EffectDeploy       EffectAction = "deploy"        // 部署（compose up 等）
)

// InfoPort 信息契约：消费或产出某类信息。
//   - Consumes 时 Effect 表示「收到该类信息变更时的自动动作」（生效契约）；
//   - Produces 时 Effect 为空。
type InfoPort struct {
    Info   InfoType       `json:"info"`
    Effect []EffectAction `json:"effect,omitempty"`
}
```

**生效契约 = `Consumes[].Effect`（自动动作）+ `Operable` 运行时接口（用户主动操作）**：`actions` 依赖运行态、由接口动态提供；`effect` 静态声明，供依赖图与调和器使用（幂等与顺序约束在 U2/U3 随图与调和器定义，L1 先只声明动作集合）。

### 3.5 观测契约（Observability）

```go
type MetricSpec struct {
    Name  string `json:"name"`
    Label string `json:"label"`
    Unit  string `json:"unit,omitempty"`
}

type Observability struct {
    State    bool         `json:"state"`    // 提供 Health.Status
    Activity bool         `json:"activity"` // 提供 Activity 接口（L2）
    Logs     bool         `json:"logs"`     // 提供 Loggable
    Metrics  []MetricSpec `json:"metrics,omitempty"`
}
```

### 3.6 Descriptor（扩展后）

```go
type Descriptor struct {
    // 身份（不含 Version：版本是运行态，由 Info.Current 提供）
    ID      string   `json:"id"`
    Name    string   `json:"name"`
    Summary string   `json:"summary,omitempty"`
    Kind    Kind     `json:"kind"`
    Tier    string   `json:"tier"` // core | optional
    Tags    []string `json:"tags,omitempty"`

    // 生命周期（承载，ADR-027/028）
    Source       Source   `json:"source"`
    Provision    string   `json:"provision"` // managed | attached
    Runtime      string   `json:"runtime"`   // manage | observe
    Upgrade      string   `json:"upgrade"`   // replace | system | none
    Capabilities []string `json:"capabilities,omitempty"` // 保留：UI 兼容 + 能力派生
    DefaultEnabled bool   `json:"defaultEnabled,omitempty"`
    Removable      bool   `json:"removable,omitempty"`
    Bundled        bool   `json:"bundled,omitempty"`

    // v4：功能 + 四契约
    Functions  []Function     `json:"functions,omitempty"`
    Config     []ConfigField  `json:"config,omitempty"`
    Consumes   []InfoPort     `json:"consumes,omitempty"`
    Produces   []InfoPort     `json:"produces,omitempty"`
    Observable Observability  `json:"observable"`
}
```

> `Descriptor` 补 `json` tag（camelCase，符合 API 标准）。`Capabilities` 与 `Functions` 并存：前者是旧 UI 的操作门控，后者是功能地图语义；不强制派生（内建组件显式声明两者）。

## 4. 注册与编译

### 4.1 内建组件声明

在 `CoreRegistry` 的 `core` 结构体上直接填充 `desc.Functions/Config/Consumes/Produces/Observable`（最小改动，不新建机制）。L1 初始声明：

| 组件 | Functions | Consumes → Effect | Produces |
|---|---|---|---|
| caddy | reverse-proxy, static-serve, http-listen | service / port / fragment / variable / domain / cert → update-config + hot-reload | — |
| acme | cert-issue | domain / credential → reissue + update-config | cert |
| docker | container-runtime, label-publish | variable / port → update-config | label, service（派生） |
| docker-compose | compose-orchestrate | variable / port → update-config + deploy | label |
| ddns-go | ddns-publish | domain / credential → update-config + restart | — |
| mosdns | dns-resolve | domain → update-config + restart | — |

> **证书需求不单独建模**：acme `produces: [cert]`、caddy `consumes: [cert]`，依赖图由「同类型产出/消费」自动连边；acme 先于 caddy 的调和顺序是该边的拓扑结果（U2）。

### 4.2 插件 manifest → Descriptor

扩展现有 `providerFor`（`plugin.go`）为两步：

```
manifest ──providerFor──▶ extension.Provider（能力/渲染器/配置同步，不变）
         └─descriptorFor─▶ component.Descriptor（v4）
```

- 输入：manifest 的 `provides` / `consumes` / `produces` / `observability` / `effect`（additive，可选）+ `id/name/version/kind`。
- **缺失字段 → 最小描述符**：仅身份 + 由 `contributions` 派生的功能（如 `proxy-protocols` → `reverse-proxy`），`Observable` 全 false，无语义契约。保证旧插件零改动加载。
- `plugin` 包新增对 `component` 的依赖（符合依赖方向：`plugin → component`）。

### 4.3 与 `_core` provider 的关系

`extension.CoreProvider()` 注册的是**扩展点能力**（proxy-protocols / dns-provider），由 `_core` 伪组件承载。U1 不把 `_core` 作为功能地图节点；HTTP 渲染等行为归属 **caddy 组件**（其 `Functions` 已声明 `reverse-proxy`/`http-listen`）。能力表与功能表是「实现」与「语义」两层，通过 U2 的图绑定衔接。

## 5. API 契约

`GET /api/v1/descriptors` → `[]Descriptor`（数组，非分页），字段 camelCase。

```json
[
  {
    "id": "caddy", "name": "Caddy", "kind": "core", "tier": "core",
    "functions": [{ "id": "reverse-proxy", "label": "反向代理" }],
    "consumes": [{ "info": "service", "effect": ["update-config", "hot-reload"] }],
    "produces": [],
    "observable": { "state": true, "activity": false, "logs": true, "metrics": [] },
    "capabilities": ["health", "config", "upgradable", "logs", "restartable"]
  }
]
```

- 由新的 `handler.RegisterDescriptors(mux, reg, mgr)` 注册（`server.New` 装配）。
- 插件来源：`plugin.Manager` 提供 `Descriptors() []component.Descriptor`（仅 enabled 插件）。
- 内建来源：`component.CoreRegistry.Descriptors() []component.Descriptor`。
- 前端据 `kind`/`tier` 与契约渲染；无 `functions` 的组件退化为「仅生命周期卡」。

## 6. 兼容与迁移

- `Descriptor` 新增字段全部 `omitempty`（`Observable` 除外），旧客户端读取不受影响。
- `Capabilities` 保留，现有 `Info` 视图/组件页零改动。
- `component.Component` 接口（仅 `Descriptor()`）不变；`Activity` 接口在 L2 新增。
- 前端暂不消费 `/descriptors`（U4/U5 接入）；U1 仅需接口可用 + 测试。

## 7. 测试与验收

- 单元测试：`descriptor_test.go`
  - 内建组件描述符完整性（caddy/acme/docker 均有 functions + observable）。
  - `descriptorFor` 对「完整插件 / 缺失契约字段的旧插件 / 非法 effect 值」的行为。
  - `InfoPort.Effect` 只允许枚举值（解析时校验，非法则丢弃并记日志）。
- 契约测试：`GET /api/v1/descriptors` 返回 camelCase、含插件与内建。
- 门禁：`go build ./...` / `go vet ./...` / `gofmt -l ./internal ./cmd` / `mise run test`。

## 8. 已定决策

1. **Function 词表粒度**：`http-listen` **独立**于 `reverse-proxy`（端口是另一类信息）。
2. **`Version` 不进描述符**：版本是运行态，由 `Info.Current` 提供（已从 `Descriptor` 移除 `Version`）。
3. **证书需求不单独建模**：acme `produces: [cert]`、caddy `consumes: [cert]`；acme→caddy 的边与顺序由 U2 的图推导。
4. **`effect` L1 只声明动作集合**：幂等/顺序约束在 U2 与调和器设计中定义。
