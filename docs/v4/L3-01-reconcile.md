# L3 · 执行可追踪（声明式调和 / reconcile）

> 上层：[architecture.md §2.6](../architecture.md) · [ADR-040 §3](../adr/ADR-040.md) · [ADR-041 §5/§6](../adr/ADR-041.md)
> 状态：设计稿（已含决策）· 依赖：L1（描述符 + 依赖图）、L2（观测 + bus + runs）

## 1. 目标与非目标

**目标**

1. 新增 **`internal/reconcile`**：意图 + 调和器（level-triggered）+ Run/Step/Event 记录。
2. 组件实现 `Reconciler` 可选接口；**首个闭环 = 网关域**（caddy / acme / 端口 / 变量 / 片段），替换 `reloadCaddy`。
3. 修掉既有触发缺口：改 Domain / Credential 不重载、Port 需手动重启、改 Variable 不回填 compose。
4. 暴露 `POST /api/v1/reconcile` 与 `run` 事件（SSE），前端呈现**传播轨迹**。

**非目标**

- 不引入 DB 回滚（失败以 `degraded` + 重试表达）。
- 不做通用可视化编排（用户不能自定义边；边由描述符 + 事实推导）。
- 不迁移 docker 部署/运行态动作（保持既有 `syncCaddyAsync`，后续接入）。

## 2. 概念（与 ADR-040 §3 对齐）

| 概念 | 落地 |
|---|---|
| **意图（Intent）** | `{Op, Kind, ID}`：只声明某事实变化 |
| **期望态（Desired State）** | 事实快照（L1 `graph.Snapshot`） |
| **依赖图** | L1 `graph.Build`（类型骨架 + 实例绑定） |
| **生效契约** | `Descriptor.Consumes[].Effect`（update-config/hot-reload/restart/reissue/deploy） |
| **调和器** | `reconcile.Reconciler`：消费闭包 → 拓扑排序 → 执行 → 记录 |
| **收敛** | 幂等重放 + 失败重试 + 周期兜底 |

## 3. `internal/reconcile`

```
internal/reconcile/
├── intent.go      # Intent + 去抖合并
├── reconciler.go  # 调和器主循环
├── order.go       # 拓扑排序（生产者先于消费者）
└── store.go       # Run/Step 读写（BoltDB runs bucket）
```

### 3.1 组件调和接口（`component` 包）

```go
// FactView 调和输入中的只读事实视图。
type FactView struct {
    Kind   string
    ID     string
    Info   InfoType
    Origin string
    Value  any
}

type ReconcileInput struct {
    Facts    []FactView
    Affected []InfoType // 本次受影响的信息类型
}

type ReconcileResult struct {
    State  string   // success | degraded | skipped
    Detail string
    Events []string
}

// Reconciler 可选接口：实现则可被调和器驱动（ADR-041 §5）。
type Reconciler interface {
    Reconcile(context.Context, ReconcileInput) (ReconcileResult, error)
}
```

### 3.2 调和器算法

```go
type Intent struct {
    Op   string // add | update | delete
    Kind string // 事实类别（service/domain/credential/port/variable/fragment/compose）
    ID   string
}

func (r *Reconciler) Trigger(i Intent) string   // 去抖合并，返回 runId
func (r *Reconciler) Run(ctx context.Context) (Run, error)
```

`Run` 步骤：

1. **快照**：handler 装配 `graph.Snapshot`（持久事实 + docker 派生）。
2. **建图**：`graph.Build(snap, "component")`。
3. **求消费闭包**：变更事实（intent.Kind 的信息类型）→ 沿边向消费者展开，得到受影响组件集合。
4. **拓扑排序**（`order.go`）：按 `produces→consumes` 的边排序（如 `acme --cert--> caddy` ⇒ acme 先于 caddy）；同层可并行。
5. **执行**：对每个受影响且实现 `Reconciler` 的组件调用 `Reconcile`；记录 `Step`，向 `observe.Bus` 发布 `run` 事件。
6. **落库**：写 `runs` bucket（L2 定义）。
7. **失败**：`Step.State=error` → `Run.State=degraded`；**不中断其他分支**，不全局回滚；下轮周期兜底重试。

### 3.3 触发

- **意图触发**：`service` 写操作（create/update/delete 域名/凭证/端口/变量/片段）在落库后调用 `reconciler.Trigger(intent)`；HTTP 接口**同步返回 `{runId}`**（已受理），进度经 SSE。
- **去抖合并**：500ms 窗口内的多个 intent 合并为一次 run。
- **周期兜底**：默认 5min 全量 reconcile（漂移检测）；可经配置关闭。
- **手动**：`POST /api/v1/reconcile` 触发全量。

## 4. 首个闭环：网关域

### 4.1 组件调和函数

| 组件 | Reconcile 行为 | 生效动作 |
|---|---|---|
| **caddy** | `gateway.Generate` 由事实生成 Caddyfile → `caddy validate` → `/load` → `persistCaddyfile` | `update-config + hot-reload` |
| **acme** | 对每个 HTTPS 域名 `Issuer.Ensure`（幂等、10min 冷却） | `reissue + update-config` |
| **config-sync 插件**（ddns-go） | 由投影渲染插件配置 → 落盘 → 重启进程 | `update-config + restart` |

> 拓扑顺序由 `acme --cert--> caddy` 边自动给出：先签发、再重载。

### 4.2 迁移（三阶段）

1. **并存**：实现 `caddy.Reconcile`/`acme.Reconcile`，与 `reloadCaddy` 并存，环境变量 `GATEBOX_RECONCILE=1` 启用调和路径。
2. **切换默认**：默认走调和器；`reloadCaddy` 保留为回退开关。
3. **收口**：移除写 handler 内的直接 `reloadCaddy` 调用，全部改为 `Trigger`；修复触发缺口（Domain/Credential/Port/Variable）。

### 4.3 触发缺口修复对照

| 场景 | 现状 | L3 |
|---|---|---|
| 改 Domain / Credential | 不重载 | intent → 调和 acme + caddy + config-sync |
| Port 保存 | 需手动重启 | intent → 调和 caddy（端口信息） |
| 改 Variable | compose 不回填 | intent → 调和 caddy + compose 插值 |
| Service 增删改 | 直接 `reloadCaddy` | intent → 调和 caddy + acme |

## 5. 一致性

- **幂等**：caddy 生成/load 幂等；acme `Ensure` 幂等（冷却）；config-sync 渲染+写幂等。
- **重试**：失败 Step 记入 Run；周期兜底自动重试；不引入 DB 回滚（ADR-040 §3.4）。
- **状态可见**：失败组件在 `observe` 中标记 `degraded`，功能地图节点变黄/红。
- **意图 vs 传播**：`deploy`/`up` 等用户动作是**意图**，其成功后的派生（label→service）才是传播；调和器只处理后者，避免自激循环。

## 6. API 契约

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/reconcile` | 手动全量调和，返回 `{runId}` |
| GET | `/api/v1/runs` · `/runs/{id}` | Run 列表 / 详情（含 Step） |
| GET | `/api/v1/events` | SSE：`run`/`step` 事件（L2 通道） |

- 写接口（如 `PUT /gateway/services/{id}`）响应新增可选 `runId` 字段（向后兼容，旧客户端忽略）。

## 7. 前端：传播轨迹

- `modules/topology`：Run 进行中时，受影响节点/边高亮并显示 Step 状态；完成后复原。
- `modules/system`（或 topology 子页）：Run 列表 → 详情时间线（Step + Events）。
- `app/composables/useReconcile.ts`：触发/订阅/取消。
- 失败可见：Step 失败节点标红并给出 `Detail`；点击跳转组件详情/日志。

## 8. 测试与验收

- `reconcile/order_test.go`：给定 `acme --cert--> caddy`，排序保证 acme 在 caddy 前；同层稳定。
- `reconcile/reconciler_test.go`：桩组件记录调用顺序；消费闭包正确（改 domain 影响 acme+caddy+ddns）；失败标 degraded 且不中断；去抖合并。
- `reconcile/store_test.go`：runs 落库/保留策略/裁剪。
- 集成：网关域「改域名 → acme 签发 → caddy 重载」端到端（无外部依赖时以桩 caddy/acme 验证顺序）。
- 回归：既有 `go test ./...` 全绿；`GATEBOX_RECONCILE=0` 时行为与迁移前一致。
- 前端：`pnpm typecheck/test/lint:style/build`。

## 9. 已定决策

1. **调和器在 `internal/reconcile`**，`service` 收缩为「校验 + 落库 + Trigger + 返回 runId」（ADR-041 §5）。
2. **组件调和通过可选接口 `Reconciler`**，不实现者不参与（内建组件先实现，插件经 config-sync/reconcile 契约）。
3. **拓扑序由依赖图给出**，不硬编码顺序。
4. **不引入 DB 回滚**：失败标 `degraded` + 周期重试 + 状态可见。
5. **灰度迁移**（`GATEBOX_RECONCILE`）+ 保留回退，确保零回归。
6. **首个闭环限网关域**；docker 部署动作仍走既有异步同步，后续单位再接入。
