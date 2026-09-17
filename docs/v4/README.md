# GateBox 架构 V4 · 设计导航

> 总纲：[`docs/architecture.md`](../architecture.md)（v4） · 框架：[ADR-040](../adr/ADR-040.md) · V4 结构：[ADR-041](../adr/ADR-041.md)
> 本文是 V4 的**逐单位设计索引**。单位级设计遵循总纲 §16：逐单位「讨论 → 撰写单位设计 → 开发 → 验证」。

## 分期

| 层 | 目标 | 状态 |
|---|---|---|
| **L1 静态自描述** | 唯一描述符 + 事实注册表 + 依赖图 + 配置契约下发 + 前端派生引擎 + 只读功能地图 | **已实现（测试全绿）** |
| **L2 运行态观测** | 观测契约（`Activity`）+ `observe` 包 + `/components/{id}/{status,activity,logs}` + `/events`(SSE) + Run/Step 存储 | **已实现** · [L2-01-observability.md](L2-01-observability.md) |
| **L3 执行可追踪** | `reconcile` 包 + 意图 + 调和器 + 传播轨迹；首个闭环 = 网关域（替换 `reloadCaddy`） | **已实现（首个闭环）** · [L3-01-reconcile.md](L3-01-reconcile.md) |

顺序：先内建组件（caddy/acme/docker/ddns），后插件；首个验证域 = 网关。

## L1 单位清单

| # | 单位 | 交付 | 依赖 | 设计文档 |
|---|---|---|---|---|
| **U1** | 组件描述符契约与注册 | `component.Descriptor`（功能 + 四契约）+ 内建组件声明 + 插件 manifest 编译 + `GET /api/v1/descriptors` | — | [L1-01-descriptor.md](L1-01-descriptor.md) ✅ |
| **U2** | 事实注册表与依赖图 | `internal/graph`：事实清单 + 引用解析 + 依赖图推导 + `GET /api/v1/graph` | U1 | [L1-02-graph.md](L1-02-graph.md) ✅ |
| **U3** | 配置契约与 schema 下发 | `ConfigField` 落 Go + `GET /api/v1/schema/{factKind}` | U1 | [L1-03-schema.md](L1-03-schema.md) ✅ |
| **U4** | 前端派生引擎 | `lib/schema/` + `app/components/`：schema → Form / YAML / 校验 | U3 | [L1-04-frontend-derive.md](L1-04-frontend-derive.md) ✅ |
| **U5** | 功能地图（只读） | `@vue-flow/core` + 双层投影 + 手写分层布局 + 详情联动 | U1, U2 | [L1-05-topology.md](L1-05-topology.md) ✅ |

## L1 实现落点

**后端**
- `internal/component/contracts.go`（四契约类型）、`contracts_core.go`（内建声明）、`component.go`（Descriptor 扩展）、`registry.go`（`Descriptors()`）
- `internal/plugin/descriptor.go`（`DescriptorFor` + `Manager.Descriptors()`）
- `internal/graph/{facts.go,graph.go}`（事实注册表 + schema + 依赖图）
- `internal/handler/{descriptors.go,graph.go}`（`/descriptors`、`/graph`、`/schema/{factKind}`）

**前端**
- `src/lib/schema/`（纯引擎 + vitest）、`src/lib/graph/layout.ts`（布局 + vitest）
- `src/app/components/schema/*`、`src/app/components/topology/*`、`src/app/composables/{useFactSchema,useReferenceOptions}.ts`
- `src/modules/topology/views/Index.vue`、`src/api/{schema,graph}.ts`、`src/design/tokens.css`

> **实现偏差**：① 功能地图入口实现为**一级路由 `/topology` + 侧栏「功能地图」**（而非组件页第三视图），零回归且更贴合 V4 一级 surface，见 L1-05；② `pnpm typecheck` 用 `tsconfig.v4.json` **限定 V4 分层**（lib/app/modules/api/shared/design），存量 `src/views` 的类型债务留待 P3 迁移，见 L1-04。

## L2/L3 实现落点

**后端**
- `internal/component/observe.go`：`Activity`/`ActivityProvider`/`FactView`/`ReconcileInput`/`ReconcileResult`/`Reconciler`
- `internal/observe/{events,bus,collector}.go`：事件总线（环形回放）+ 采集器（变化才发布）
- `internal/reconcile/{types,order,store,reconciler}.go`：意图去抖、拓扑排序、BoltDB `runs`（条数+天数双限）、调和器
- `internal/repository/runs.go`：`runs` bucket 读写
- `internal/handler/observe.go`：`/events`(SSE)、`/components/{id}/{status,activity,logs}`、`/runs`、`/runs/{id}`、`POST /reconcile`
- `internal/handler/reconcile_trigger.go`：触发钩子；Domain/Credential/Port 写操作已接入（修复触发缺口）
- 装配：`server.New` 建总线 + RunStore + 调和器（注册 `caddy` → 网关全量调和）+ 采集器

**前端**
- `src/shared/observe.ts`、`src/api/{observe,events}.ts`、`src/app/composables/useEvents.ts`（单一 SSE 订阅）
- 功能地图：节点状态着色、活动展示、运行记录抽屉、立即调和

> **L2/L3 实现状态（第二批）**：
> - ✅ acme / caddy 拆为**独立调和步骤**（`ReconcileAcme` → `ReconcileCaddy`），顺序由 `acme --cert--> caddy` 边给出；service 变更同时影响 domain（acme 重签）。
> - ✅ 写操作收敛：`reloadCaddy` 在调和器装配时提交全量意图（异步收敛），未装配时同步回退（零回归）。
> - ✅ manifest `config-sync`（template）已在 `providerFor` 编译（此前缺口）。
> - ✅ `/components/{id}/logs` 接真实来源：caddy（`$DATA_DIR/logs/caddy/*.log` 最新）、acme（最近 CertLog.LogFile）；支持 `?tail=N`。
> - ✅ 前端传播轨迹：Run/Step 事件驱动图上节点徽标 + 边高亮。
>
> **调和开关**：`GATEBOX_RECONCILE`（默认开；`0/false/off` 关闭 → 写操作回退同步 `reloadCaddy`）；`GATEBOX_RECONCILE_PERIOD`（默认 `5m`，`0` 关闭周期兜底）。
>
> **仍未完成（需先定设计）**：① Variable → compose 回填——`interpolateContainerVars` 为**破坏性替换**，写盘后 `${KEY}` 占位符已丢失，必须先保留 compose 模板（存储/schema 变更，ADR-035 §7 已知债务）；② docker 部署/运行态动作未接入调和（仍走既有异步同步）；③ 存量 `src/views` 类型债务（56 处）留待 P3 迁移，`typecheck` 暂限定 V4 分层。

依赖图：

```
U1 ──┬─▶ U2 ──┐
     ├─▶ U3 ──┼─▶ U5
     │        └─▶ U4
```

## 约定

- 每单位文档含：目标与非目标 · 类型/接口设计 · API 契约 · 兼容与迁移 · 测试与验收 · 开放问题。
- 后端验收：`go build ./...` / `go vet ./...` / `gofmt -l` / `mise run test`。
- 前端验收：`pnpm typecheck` / `pnpm build`（+ 相关视图亮/暗双主题人工验证）。
- 契约变更须同步：主仓库 `plugins/schema`（若涉及）→ GateBoxStore vendor → CI 校验一致。
