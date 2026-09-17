# L2 · 运行态观测（Activity / observe / SSE）

> 上层：[architecture.md §2.5](../architecture.md) · [ADR-040 §2](../adr/ADR-040.md) · [ADR-041 §7](../adr/ADR-041.md)
> 状态：设计稿（已含决策）· 依赖：L1（描述符 + 依赖图）

## 1. 目标与非目标

**目标**

1. 落地**观测契约**：新增 `Activity` 可选接口，`state` 用 `Health.Status`、`logs` 用 `Loggable`、`metrics` 留 `Status.Metrics`。
2. 新增 **`internal/observe`**：状态/活动聚合 + 事件总线（SSE）。
3. 暴露观测 API：`/components/{id}/{status,activity,logs}`、`/events`(SSE)、`/runs`(只读)。
4. Run/Step 存储（BoltDB `runs`，条数+天数双限；Event 仅内存）。
5. 前端：功能地图节点**状态着色** + 组件内部（活动/日志）下钻。

**非目标**

- 不实现声明式调和（L3）；`runs` 由 L3 写入，L2 只定义存储与只读端点。
- 不改组件生命周期语义。

## 2. 观测契约（Go）

在 `component` 包新增：

```go
// Activity 组件当前活动（观测契约的动态部分）。
type Activity struct {
    Task     string  `json:"task,omitempty"`     // 当前任务，如 "issue-cert"
    Step     string  `json:"step,omitempty"`     // 当前步骤，如 "waiting-dns"
    Progress float64 `json:"progress,omitempty"` // 0..1，-1 表示不确定
    Actor    string  `json:"actor,omitempty"`    // 执行者（进程/组件/run）
    Since    int64   `json:"since,omitempty"`    // Unix 秒
}

// ActivityProvider 可选接口：实现则具备 activity 观测（ADR-041 §7）。
type ActivityProvider interface {
    Activity(context.Context) (Activity, error)
}
```

`Descriptor.Observable.Activity` 为 true 的组件才应实现该接口；`observe` 对未实现者返回 `{Task:"idle"}`。

## 3. `internal/observe`

```
internal/observe/
├── bus.go        # 事件总线（订阅/发布，环形缓冲）
├── collector.go  # 定时轮询组件 state/activity，变化时发布
└── events.go     # 事件类型定义
```

### 3.1 事件模型

```go
type EventKind string

const (
    EventState    EventKind = "state"    // 组件 state 变化
    EventActivity EventKind = "activity" // 组件 activity 变化
    EventRun      EventKind = "run"      // Run/Step 推进（L3 写入）
    EventLog      EventKind = "log"      // 可选：日志行（仅当订阅）
)

type Event struct {
    Seq        uint64          `json:"seq"`
    Kind       EventKind       `json:"kind"`
    Component  string          `json:"component,omitempty"`
    RunID      string          `json:"runId,omitempty"`
    Data       json.RawMessage `json:"data,omitempty"`
    At         int64           `json:"at"`
}
```

- `Bus`：进程内 pub/sub，`Publish(Event)` / `Subscribe(buf) (<-chan Event, cancel)`；环形缓冲保留最近 N 条，SSE 首连可回放。
- `Collector`：`time.Ticker`（默认 5s，复用 `gateway.HealthCollector` 的节奏）轮询各组件 `Health.Status` 与 `ActivityProvider.Activity`；**仅在值变化时**发布（避免 SSE 噪声）。

### 3.2 组件适配（L2 初始）

| 组件 | state | activity | logs |
|---|---|---|---|
| caddy | 现有 `probe`（admin `/config/`） | 不实现（`idle`） | `Loggable`（后续接文件/流） |
| acme | 现有 `probe` | **由 `model.CertLog` 最新一条映射**（`action`→Task，`status`→Step） | CertLog 文件 |
| docker | 现有 `probe`（socket） | 不实现 | 容器日志另走既有 WS |

> acme 是「看到执行到哪一步」的首个样板，直接复用既有 `CertLog`，无需新埋点。

## 4. Run/Step 存储（L2 定义，L3 写入）

- BoltDB bucket：`runs`（key = runId，value = Run JSON）。
- 结构（与 ADR-040 §3.3 对齐）：

```go
type Run struct {
    ID        string `json:"id"`
    Trigger   string `json:"trigger"` // intent | periodic | manual
    Intent    string `json:"intent,omitempty"`
    State     string `json:"state"` // running | success | degraded | error
    StartedAt int64  `json:"startedAt"`
    EndedAt   int64  `json:"endedAt,omitempty"`
    Steps     []Step `json:"steps"`
}
type Step struct {
    Component string `json:"component"`
    Action    string `json:"action"`
    State     string `json:"state"` // pending | running | success | error | skipped
    Detail    string `json:"detail,omitempty"`
    StartedAt int64  `json:"startedAt,omitempty"`
    EndedAt   int64  `json:"endedAt,omitempty"`
    Events    []string `json:"events,omitempty"` // 细粒度事件文本（仅内存，落库时裁剪）
}
```

- 保留策略：**最近 50 条 + 7 天**双限；`Step.Events` 落库时裁剪为最近 20 条。

## 5. API 契约

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/components/{id}/status` | `component.Status`（`Health` 接口） |
| GET | `/api/v1/components/{id}/activity` | `component.Activity`（未实现则 `{task:"idle"}`） |
| GET | `/api/v1/components/{id}/logs?tail=N` | 文本流（`Loggable`），默认 tail 200 行 |
| GET | `/api/v1/events` | **SSE**：`state`/`activity`/`run` 事件；`Last-Event-ID` 支持回放 |
| GET | `/api/v1/runs` · `/api/v1/runs/{id}` | Run 列表 / 详情（L2 只读；L3 写入） |

- SSE 帧：`event: <kind>` + `id: <seq>` + `data: <Event JSON>`；心跳 `: ping` 每 15s。
- 鉴权：沿用现有中间件（SSE 支持 Cookie 会话）；不新开权限。

## 6. 前端

- 新增 `src/api/observe.ts`（status/activity/logs）与 `src/api/events.ts`（`EventSource` 封装，自动重连 + `Last-Event-ID`）。
- `app/composables/useEvents.ts`：单例订阅，向 store 分发（避免每组件各开一条 SSE）。
- 功能地图（U5）节点按 `state` 着色（`running/healthy` 绿、`degraded` 黄、`error` 红、`unknown` 灰）；状态点 L2 启用。
- 节点详情抽屉新增「活动 / 日志」区块：activity 显示 `task · step`，logs 拉 tail 文本。
- 运行记录页（`modules/topology` 或 `modules/system`）列表 + 详情（L3 填充数据）。

## 7. 兼容与迁移

- `Activity` 为新增可选接口，既有组件不受影响；`Descriptor.Observable.Activity` 默认为 false。
- 现有 `HealthCollector`（gateway 上游健康）保留；`observe.Collector` 是组件级状态，二者不合并。
- `/events` 与既有 WebSocket 并存：SSE 只承载观测事件，WS 继续承载 exec/日志/部署进度。

## 8. 测试与验收

- `observe/bus_test.go`：发布/订阅、环形回放、退订。
- `observe/collector_test.go`：桩组件的 state 变化只发布一次；未实现 Activity 返回 idle。
- `handler` 测试：`/events` 返回 `text/event-stream` 且可收到一条事件；`/components/{id}/activity` 对未实现组件返回 idle；`/runs` 空列表。
- 前端：`useEvents` 在 mock EventSource 下分发正确（vitest）；`pnpm typecheck/test/lint:style/build`。
- 后端：`go build/vet/gofmt` + `mise run test`。

## 9. 已定决策

1. **推送用 SSE**（单向），WS 保留给双向交互（ADR-041 §7）。
2. **`observe.Collector` 独立于 `HealthCollector`**：前者组件级、可发布事件，后者网关上游级。
3. **acme activity 复用 `CertLog`**：以最小成本实现「执行到哪一步」的首个样板。
4. **Run 存储 `runs` bucket**：L2 只读端点，L3 写入；条数+天数双限，Step.Events 落库裁剪。
5. **前端单一 SSE 订阅**（`useEvents` 单例），避免连接风暴。
