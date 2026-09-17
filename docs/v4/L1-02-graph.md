# L1-U2 · 事实注册表与依赖图

> 上层：[architecture.md §2.3/§2.4/§2.6](../architecture.md) · [ADR-040](../adr/ADR-040.md) · [ADR-041](../adr/ADR-041.md)（类型骨架 + 实例绑定）
> 状态：设计稿（待评审）· 依赖：U1（`component.Descriptor` 的信息契约）

## 1. 目标与非目标

**目标**

1. 建立**中央事实注册表**：声明哪些 `model.*` 是事实、属于哪类信息、引用哪些其他事实。
2. 建立 **`internal/graph`**：由「组件信息契约（类型骨架）+ 事实引用（实例绑定）」推导依赖图。
3. 暴露 `GET /api/v1/graph`（组件视角 / 功能视角 / 事实下钻）。

**非目标**

- 不做调和执行（L3）、不做运行态观测（L2）。
- 不落库依赖图（推导物，按需计算）。
- 不引入外部图库（图规模为 HomeLab 量级，手写推导足够）。

## 2. 概念模型

```
信息（Info）  = 类型化的数据类别（U1 定义：service/domain/cert/…）
事实（Fact）  = 某个信息类别的具体实例（kind:id），如 service:abc、domain:neob.cn
组件（Component）= 功能的提供者，声明 consumes/produces（信息契约）
```

依赖图由两步合成（ADR-041 §4）：

1. **类型骨架**：`组件 P produces T` × `组件 C consumes T` ⇒ 组件边 `P --T--> C`（P≠C）。
2. **实例绑定**：每条组件边附上支撑它的**事实实例**（类型 T、由 P 产出）。

事实来源分两类：

| 类别 | 产出者（Origin） | 例 |
|---|---|---|
| **源事实** | `user`（用户意图）/ 内建 | Service(manual)、Domain、Credential、Fragment、Variable、Port、Compose |
| **派生事实** | 组件 | docker 从 label 派生的 Service |
| **运行信息** | 组件 | acme 产出的 cert 文件、docker 产出的 label |

> 派生事实**不可写**（`Persisted=false`），调和只能改其源头（U1/ADR-040 §1.4）。

## 3. 事实注册表

新增 `internal/graph/facts.go`。事实注册表是**中央声明**（事实属于系统，不属于组件）。

```go
// RefKind 引用种类。
type RefKind string

const (
    RefID      RefKind = "id"      // 按目标 ID 引用
    RefName    RefKind = "name"    // 按名称引用（如 rootDomain、variable key）
    RefDerived RefKind = "derived" // 运行态派生（如 service→container）
)

// FactReference 某事实对其他事实的引用。
type FactReference struct {
    Field      string  `json:"field"`      // 来源字段（文档/调试用）
    TargetKind string  `json:"targetKind"` // 目标事实 kind
    Kind       RefKind `json:"kind"`
}

// FactKind 事实类别（注册表一项）。
type FactKind struct {
    Kind       string             `json:"kind"`       // service | domain | ...
    Info       component.InfoType `json:"info"`       // 映射的信息类别（U1）
    Persisted  bool               `json:"persisted"`  // 持久事实 / 派生事实
    Source     string             `json:"source"`     // "user" 或组件 id（默认产出者）
    References []FactReference    `json:"references"`
}
```

**L1 初始事实表**：

| FactKind | Info | Persisted | Source | References |
|---|---|---|---|---|
| `service` | service | ✔（manual） | user | `appId → app`；`domains[].rootDomain → domain`（name）；`fragmentIds[] → fragment`（id） |
| `domain` | domain | ✔ | user | `credentialId → credential`（id） |
| `credential` | credential | ✔ | user | — |
| `fragment` | fragment | ✔ | user | — |
| `variable` | variable | ✔ | user | —（被按名引用，见 §5.3） |
| `port` | port | ✔ | user | — |
| `compose` | —（无信息类别，仅编排事实） | ✔ | user | `projectName → service`（derived） |
| `service`（派生） | service | ✘ | docker | 同 service |
| `cert` | cert | ✘ | acme | — |
| `label` | label | ✘ | docker | — |

> `app` 是纯分组，**不建图节点**（可作节点属性展示）。

## 4. 事实快照（Snapshot）

图构建的输入。由 handler 从 `repository`（持久事实）+ `adapter`（派生/运行信息）装配，`graph` 包只做纯推导（可测、无副作用）。

```go
type Fact struct {
    Ref    FactRef             // {Kind, ID}
    Info   component.InfoType
    Origin string              // "user" | 组件 id
    Value  any                 // model.* （供引用提取）
}

type Snapshot struct {
    Facts []Fact
    Desc  []component.Descriptor // U1 描述符（类型骨架）
}
```

**引用解析**（`graph` 内，按事实注册表声明）：对每个 `Fact.Value` 用注册表的 `References` 提取目标标识，生成 `fact → fact` 边 `RefEdge{From, To, Field}`。

## 5. 依赖图构建

```go
type Edge struct {
    From, To  string        // 组件 id 或函数 id
    Info      component.InfoType
    Instances []FactRef     // 支撑该边的实例（类型骨架为空时亦保留边）
    Cycle     bool          // 回边（几何判定见 U5）
}

type Node struct {
    ID        string
    Kind      string        // "component" | "function"
    Label     string
    Tier      string
    Functions []string
    Consumes  []InfoCount
    Produces  []InfoCount
}

type Graph struct {
    View  string
    Nodes []Node
    Edges []Edge
    FactRefs []RefEdge      // 事实→事实引用（下钻用）
}
```

### 5.1 类型骨架

```
producers[T] = { d.ID | T ∈ d.Produces }
consumers[T] = { d.ID | T ∈ d.Consumes }
∀ T, P ∈ producers[T], C ∈ consumers[T], P ≠ C:  Edge{P → C, Info: T}
```

### 5.2 实例绑定

```
Edge(P→C, T).Instances = { f.Ref | f.Info == T ∧ f.Origin == P }
```

例（L1 预期结果）：

| 边 | 依据 |
|---|---|
| `docker --service--> caddy` | docker `produces: [service]`，caddy `consumes: [service]`；实例 = 派生 Service |
| `acme --cert--> caddy` | acme `produces: [cert]`，caddy `consumes: [cert]`；实例 = 已签发证书 |
| `docker --label--> caddy` | docker `produces: [label]`，caddy `consumes: [label]` |

**注意**：源事实（domain/credential）由 `user` 产出，不产生组件边；其消费者（acme/caddy/ddns/mosdns）通过**节点上的 `Consumes` 计数**体现，而非空出边。
**调和顺序**：`acme --cert--> caddy` 即给出「acme 先于 caddy」的拓扑约束（L3 使用）。

### 5.3 按名引用（Variable）

变量引用写法随上下文而变（网关 `<%KEY%>`、容器 `${KEY}`，ADR-035）：

- 事实注册表把 `variable` 的引用**不作为事实边**，而在组件侧扫描：caddy 的 fragment/service 文本中 `<%KEY%>`、compose 文本中 `${KEY}`。
- L1 由 handler 在装配 Snapshot 时把「被引用的变量名」作为 `RefEdge{From: "variable:<key>", To: "<事实>:<id>", Field: "ref"}` 注入；U2 只消费，不解析目标格式。

### 5.4 功能视角

由组件视角聚合：节点 = 所有出现过的 Function；边 = 对每条组件边 `P→C`，`∀ f∈P.Functions, g∈C.Functions: f --T--> g`（实例数累加）。

### 5.5 确定性

节点与边**稳定排序**（组件按 id、边按 `(from,to,info)`），保证输出可 diff、前端布局稳定。

## 6. API 契约

### 6.1 图

`GET /api/v1/graph?view=component|function` → `Graph`（camelCase）

```json
{
  "view": "component",
  "nodes": [
    { "id": "caddy", "kind": "component", "label": "Caddy", "tier": "core",
      "functions": ["reverse-proxy", "static-serve", "http-listen"],
      "consumes": [{ "info": "service", "count": 4 }, { "info": "cert", "count": 2 }],
      "produces": [] }
  ],
  "edges": [
    { "from": "acme", "to": "caddy", "info": "cert",
      "instances": [{ "kind": "cert", "id": "app.neob.cn" }], "cycle": false }
  ],
  "factRefs": []
}
```

### 6.2 事实下钻（可选，L1 末）

`GET /api/v1/graph?root=<kind>:<id>&depth=N` → 以某事实为中心的子图（`factRefs` 正反向展开）。用于「删这个代理影响谁」的预演（L3 复用）。

- 由新的 `handler.RegisterGraph(mux, graphBuilder)` 注册；`server.New` 装配。
- 快照装配放在 `handler`（可访问 `repository` + 已有派生来源，如 `gw.DerivedServices`），`graph` 包保持纯函数。

## 7. 缓存与失效

- **L1 直接按请求计算**（HomeLab 规模，事实量 < 数百）。
- 预留：以「事实 revision + 描述符版本」为 key 缓存推导结果；写操作在 L3 接入 `reconcile.Trigger` 时顺带失效。
- 不落库。

## 8. 测试与验收

- `facts_test.go`：注册表完整（每个 `model.*` 事实均有登记或显式排除）；引用提取（Service→App/Domain/Fragment、Domain→Credential）。
- `graph_test.go`：
  - 仅骨架（空事实）时边存在但 `instances` 为空。
  - 带 fixture 事实时 `docker→caddy`、`acme→caddy` 边与实例正确。
  - 自产自消（P==C）被跳过；源事实不产生组件边。
  - 输出确定性（同输入两次结果逐字节一致）。
  - `view=function` 聚合正确。
- 契约测试：`GET /api/v1/graph` 返回 camelCase、稳定排序。
- 门禁：`go build ./...` / `go vet ./...` / `gofmt -l ./internal ./cmd` / `mise run test`。

## 9. 已定决策

1. **L1 接入范围**：持久事实 + docker 派生（docker 可用时），降级优雅（不可达则空实例）；`cert` / `label` 运行信息留 L2。
2. **`compose` 不进图节点**：无信息类别，仅作编排事实；其 `label` 产出由 docker 承担。
3. **组件边不穿透多跳引用**：`service → domain → credential` 的全链保留在 `factRefs`（事实下钻用），组件边只连直接消费者。
4. **环检测放 U5**：U2 只输出结构与 `cycle:false` 占位，几何判定在 U5 布局时计算。
5. **`Consumes.count` 按 Origin 分组**：区分 manual / docker 派生等同 Info 不同来源的实例。
