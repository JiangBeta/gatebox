# L1-U5 · 功能地图（只读）

> 上层：[architecture.md §2.4/§10](../architecture.md) · [ADR-041 §8/§10](../adr/ADR-041.md)
> 状态：设计稿（待评审）· 依赖：U1（描述符）、U2（`/api/v1/graph`）

## 1. 目标与非目标

**目标**

1. 用 **`@vue-flow/core`** 渲染只读**功能地图**：`/api/v1/graph` → 节点/边 → 画布。
2. 支持**双层投影切换**（组件视角 / 功能视角，ADR-040 §1.6）。
3. **手写分层布局**（不引 dagre/elk）；节点位置按内容指纹持久化。
4. 点击节点**联动**到既有详情（组件页 / 插件详情）。
5. 边按**信息类型**着色与标注实例数。

**非目标**

- 图上**不编辑**（编辑走表单）。
- 不做状态/活动着色（L2）、不做传播轨迹回放（L3）。
- 不做环边的几何动画（L1 仅标注）。

## 2. 依赖与落位

新增前端依赖：`@vue-flow/core` + `@vue-flow/background` + `@vue-flow/controls`（不引 minimap，HomeLab 图规模小）。

```
src/
├── lib/graph/                    # 纯逻辑（基础层）：图布局与几何
│   └── layout.ts                 # 分层布局算法（可单测）
├── app/components/topology/      # 通用画布（app 层）
│   ├── TopologyCanvas.vue        # <VueFlow> + Background/Controls + Panel
│   ├── nodes/ComponentNode.vue   # 组件节点
│   ├── nodes/FunctionNode.vue    # 功能节点
│   └── TopologyLegend.vue
└── modules/topology/             # 域：页面与数据
    ├── api.ts                    # GET /api/v1/graph
    ├── views/Index.vue           # 视图切换 + 画布 + 侧栏
    └── components/NodeDetailDrawer.vue
```

> ADR-032 §9.1 的 modules 列表需补 `topology`（一行更新，随本单元落地）。

## 3. 数据流

```
GET /api/v1/graph?view=component|function
   → GraphModel（nodes/edges）
   → lib/graph/layout.ts（分层布局 → 坐标）
   → Vue Flow nodes/edges（含自定义节点）
   → 交互（点击 → 详情；拖拽 → 持久化坐标）
```

- 视图切换只改 `view` 参数并重新布局；组件↔功能映射由后端给出（U2 §5.4）。
- 数据缓存于 `modules/topology` 的局部状态；切换视图懒加载。

## 4. 图模型与自定义节点

对齐 U2 §6.1 的 `Graph` JSON。

**节点类型**（`nodeTypes`）：

| 类型 | 数据 | 展示 |
|---|---|---|
| `component` | `{id,label,tier,functions,consumes,produces}` | 图标 + 名称 + 功能徽章 + 消费/产出信息计数 |
| `function` | `{id,label,implementor?}` | 功能图标 + 名称 + 实现组件标注 |

- 节点视觉复用现有组件图标（`utils/brandIcons.ts` / AntD 图标）。
- 状态点（running/healthy）**L2** 接入；L1 不显示。
- 每个节点显示「消费/产出」的信息类型徽章与实例数（来自 `consumes/produces`）。

**边**：

| 信息类型 | 颜色（design token） | 线型 |
|---|---|---|
| `service` | primary | 实线 |
| `cert` | warning | 实线 |
| `label` | muted | 虚线 |
| 其他 | 默认边框色 | 实线 |

- 边标签：信息类型 + 实例数（`cert ×2`）；`MarkerType.ArrowClosed` 表示信息流向（生产者→消费者）。
- 边样式集中在一处 `edgeStyle(info)`，颜色一律取自 `@/design/tokens`（stylelint 门禁）。

## 5. 双层投影切换

- 顶部 `Segmented`（AntD）：`组件视角` / `功能视角`。
- 切换即 `view=` 变化 + 重布局；保留各自的位置存储 key。
- 功能视角下点击功能节点 → 侧栏列出实现该功能的组件（可跳到组件详情）。

## 6. 手写分层布局（`lib/graph/layout.ts`）

不引自动布局库；采用已验证的分层思路：

```
estimateNodeHeight(node)           # 普通卡 96px；含信息徽章的卡按行数估算
buildLayers(edges, nodes)          # BFS 求深度 → 同深度为一列；x = depth * xGap
resolveAvailableY(column, desired) # 列内冲突消解（迭代下推，上限 N 次）
recenterRoots(...)                 # 根节点对齐子节点视觉中心，减少折线
```

- 仅依赖邻接关系（纯函数，输入 `nodes/edges`，输出 `Map<id, {x,y}>`）。
- 回边（`from.depth >= to.depth`）不参与分层，作为 `cycle` 边直连标注。
- 参数（`xGap/yGap/节点宽高`）常量集中在 `layout.ts`。

## 7. 位置持久化

- 用户拖拽后存 `localStorage`；key = `contentFingerprint(node)`（非索引，避免数据变化错位）。
- 组件视角 / 功能视角各自 key 前缀；「重置布局」清空并重新自动布局。
- React Flow 的 `fitView` 仅在没有自定义位置时启用。

## 8. 交互与联动

- 点击组件节点 → 打开 `NodeDetailDrawer`：描述符摘要 + 消费/产出信息 + 实例列表 + 跳转既有组件页/插件详情。
- 点击边的标签 → 侧栏列出该边的**实例事实**（如 `cert` 边的域名列表）。
- 图上**只读**：不提供增删改；编辑入口在详情抽屉内跳转表单。
- 空态：无节点时显示引导（「尚未配置任何事实」）。

## 9. 视觉规范

- 画布背景 `Background`（dots，gap 20）；`Controls` 只读。
- 节点卡片：AntD 风格 + design tokens；主色/边框/圆角取自 token，**禁止字面量**。
- 图例 `TopologyLegend`：信息类型颜色 + 视角说明。
- 深浅色主题适配（跟随全局主题）。

## 10. 导航入口

功能地图是 V4 的一级 surface，建议：

- 新增 `modules/topology` + 侧边栏入口「功能地图」。
- 备选：先作为「组件页」的一个视图模式（对齐 oxidns 插件页 grid/table/topology 三态），稳定后再提升为一级入口。

（取舍见 §12 开放问题）

## 11. 测试与验收

- `lib/graph/layout.test.ts`（vitest，纯函数）：
  - 直线链 → 各节点 x 递增、y 稳定。
  - 分叉/汇聚 → 根节点居中、同列不重叠（`resolveAvailableY` 生效）。
  - 自环/回边不参与分层。
  - 同输入结果确定（逐字节）。
- 手动验收：`GET /graph` 两种 view 均正确渲染；拖拽后刷新位置保留；点击节点联动详情；亮暗双主题。
- 门禁：`pnpm lint` / `pnpm lint:style` / `pnpm typecheck` / `pnpm test` / `pnpm build`。

## 12. 已定决策

1. **入口形态**：先作为组件页的第三视图；**实现时改为一级路由 `/topology` + 侧栏「功能地图」**（更贴合 V4 一级 surface，且对既有页面零改动零回归）。
2. **功能视角粒度**：**不合并**同名功能的多实现；实现组件作为节点属性/边。
3. **实例列表获取**：L1 **只用图内 `instances`**，不额外请求事实列表。
4. **`@vue-flow` 体积**：**路由/组件懒加载**，不进首屏。
