# V4.1 迁移与兼容 / 边界情况

> 上层：[ADR-042](../adr/ADR-042.md) · 前置：[ADR-040](../adr/ADR-040.md) / [ADR-041](../adr/ADR-041.md)（V4）
> 状态：草案

---

## 1. 总原则

1. **不推倒重来**：V4 已有实现（descriptor / graph / observe / reconcile）按职责**复用或扩展**，能修的修，不能修的才换。
2. **对象模型为唯一真相**：文件降级为产物（见 [blueprint.md](blueprint.md)）。
3. **兼容期并存**：旧 label / 旧页面 / 旧 API 在一段过渡期内仍可用，逐步下线。

---

## 2. V4 资产处置

| V4 资产 | 处置 | 说明 |
|---|---|---|
| `component.Descriptor`（ID/Name/Capabilities…） | **扩展** | 保留注册与探测；补"角色 / 能力 / service_types / middleware_types / entrypoint_protocols" |
| `graph`（事实注册表 / 引用解析） | **复用为影响分析** | 不再当前端主视图；用于"引用/被引用"与删除预演 |
| `observe`（事件总线 / 采集器） | **复用** | 扩展为 Run/Step/调用/证据，事件流不变 |
| `reconcile`（意图 / 拓扑排序 / Run 存储） | **复用** | 扩展为"链驱动"；Run 表沿用 |
| v4 端点 `/descriptors` `/graph` `/runs` `/events` | **保留** | `/graph` 降级为影响分析用 |

---

## 3. 前端页面迁移

| 现有页面 | 去向 |
|---|---|
| 功能地图 `/topology` | **删除**（被主视图 + 流程视图取代） |
| 网关页 | 拆到 **配置中心 → 对象面**（路由/服务）+ **配置中心 → 组件面** |
| 域名页 | **对象面**（根域名/凭证/证书） |
| Docker 页 | **对象面 → 部署**（编排/容器/镜像） |
| 服务页 | 并入对象面（服务） |
| 扩展页 | **配置中心 → 组件面**（插件/组件/安装） |
| 仪表盘 | **主视图**（流量链 + 监控叠加） |
| 设置 | 保留（Caddy 全局 / 系统 / 凭证） |

---

## 4. 数据模型迁移（BoltDB → 对象）

| 现有 bucket | 新对象 |
|---|---|
| `domains` | 根域名 |
| `dns_credentials` | DNS 凭证 |
| `services` | 服务 / 路由（拆分） |
| `fragments` | 中间件 |
| `variables` | 变量 |
| `port_bindings` | 入口点 / 端口 |
| `compose_instances` | 部署（手动模式） |
| `apps` | 分组标签（或废弃） |
| `cert_logs` / `runs` | 记录（证书日志 / Run） |
| `derived_disabled` | 派生对象的启用状态 |

迁移需一次性的**映射 + 校验**（引用完整性、域名冲突）。

---

## 5. API 演进

| 新增 | 用途 |
|---|---|
| `/objects`（按 kind） | 对象册 CRUD |
| `/routers` `/services` `/deployments` `/entrypoints` `/middlewares` | 标准对象 |
| `/chains` `/runs` | 链与记录 |
| `/traffic`（投影 + 实况） | 主视图数据 |
| `/catalog`（类型目录） | 配置中心自适应 |

旧端点保留过渡期，内部改由对象模型驱动。

---

## 6. 插件 manifest 扩展

在 v2 manifest 上**新增可选字段**（属 additive，见 ADR-037 §5）：

```yaml
provides:
  service_types:        [ { id: php, label: PHP 应用, fields: [...] } ]
  middleware_types:     [ { id: php_worker, label: PHP Worker } ]
  entrypoint_protocols: [http, https]
  abilities:            [ { id: process.start, role: 控制 } ]
consumes: [域名, 凭证]
produces: [证书, 解析记录]
```

缺失这些字段的旧插件编译为**最小自述**，向后兼容。

---

## 7. 兼容期

| 旧机制 | 兼容策略 |
|---|---|
| docker label（`caddy.*` / `gatebox.*`） | 过渡期继续解析为路由/服务；提示"迁移到对象声明" |
| 手写 Caddyfile | 只读导入为对象（尽力解析）；不保证全量 |
| `gatebox.conf` / `daemon.json` | 作为引擎设置保留，不进对象册 |

---

## 8. 边界情况

| 情况 | 处理 |
|---|---|
| **域名冲突** | 两个路由抢同一 FQDN → 保存时校验拒绝，指出冲突方 |
| **删除影响分析** | 删除前列出引用者（凭证→根域名→路由→证书/解析），要求确认 |
| **依赖缺失** | 卸载插件后用到其类型的服务 → 标"依赖缺失"，提供 [重装] / [改类型] |
| **端口冲突** | 入口点端口被占用 → 校验拒绝 / 提示换端口 |
| **编译失败** | 配置非法 → Run 标记失败并给出差异与错误位置，不加载 |
| **权限** | 当前单管理员；预留 RBAC |
| **凭证安全** | 沿用 AES-GCM 整体加密；不进导出、不回传 |
| **多主机不可达** | 主机离线 → 相关对象标"未知"，链步骤跳过并告警 |

---

## 9. 迁移顺序（建议）

1. 后端：对象模型 + 类型目录 + 链驱动（复用 reconcile/observe）。
2. 数据：bucket → 对象的映射与一次性校验。
3. API：新增端点，旧端点保留。
4. 前端：配置中心（组件 + 对象）→ 主视图 → 流程视图。
5. 下线：功能地图 → 旧页面 → 旧 label 支持。
