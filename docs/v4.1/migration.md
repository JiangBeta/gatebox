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
| 网关页 | 拆到 **配置中心 → 对象面**（路由/服务/中间件/入口点）+ **配置中心 → 组件面** |
| 域名页 | **对象面**（根域名 tab + 凭证/证书页） |
| Docker 页 | **对象面 → 部署**（编排/容器/镜像） |
| 服务页 | 并入对象面（服务） |
| 扩展页 | **配置中心 → 组件面**（插件/组件/安装） |
| 仪表盘 | **主视图**（流量链 + 监控叠加） |
| 设置 | 保留（常规/变量），另加 **账户**（`user` 对象，basic_auth 用） |

首批落地的对象页：

| 页面 | 位置 | 对象 kinds |
|---|---|---|
| 路由 | 配置中心 `/config?tab=routes` | `route` |
| 服务 | 配置中心 `/config?tab=services` | `service` |
| 中间件 | 配置中心 `/config?tab=middlewares` | `middleware` |
| 入口点 | 配置中心 `/config?tab=entrypoints` | `entrypoint` |
| 根域名对象 | 域名页 `/domain?tab=roots` | `domain` |
| 账户 | 设置 `/settings?tab=users` | `user` |
| 任务 | 任务中心 `/tasks` | Run 历史（SSE 实时刷新） |

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

### 4.1 首批落地范围

首批只迁 4 张表到 5 类对象（`backend/internal/migration/v41.go`）：

| 旧表 | 新 kind | 规则 |
|---|---|---|
| `domains` | `domain` | id = `Slug(name)`；`credentialId` **不迁**（见下） |
| `port_bindings` | `entrypoint` | id = `Slug(protocol)`，`network` 由协议推导 |
| `fragments` | `middleware` | `type: code` 逃生舱，Caddy 原文逐字透传 |
| `services` | `service` + `route` | 一个旧服务拆成 1 个服务 + N 条路由（按 `domains` 拆） |

`dns_credentials` / `variables` / `compose_instances` / `apps` 首批**不迁**：
前者的目标类型 `dns-credential` 虽已建模但尚无界面（见 4.2），其余待后续批次。

执行方式（`backend/cmd/migrate41`）：

```bash
migrate41 --dry-run              # 默认行为：只出报告，不写任何数据
migrate41 --apply                # 执行；自动备份到 <data-dir>/backups/
migrate41 --apply --force        # 覆盖已存在的同 id 对象（默认跳过 → 幂等）
migrate41 --apply --json         # 机器可读报告（stdout 只有一个 JSON 对象）
```

BoltDB 是单写者锁：**执行前必须先停 GateBox**。

三条与安全有关的约定：

- **备份在打开库之前做**。bbolt 是 mmap + 有 freelist，直接拷一个打开中的库文件可能拷到
  尚未落盘的页，所以 `--apply` 先复制 `data/db/gatebox.db` 到 `data/backups/` 再开库。
- **写入失败不自动回滚**。报告里直说「没有自动回滚」并给出恢复命令（停 GateBox →
  把备份复制回 `data/db/gatebox.db` → 启动）。已经写进去的对象可能正被别的进程读着，
  静默回滚比留痕迹更危险。
- **`--json` 时 stdout 只出 JSON**，人类可读文本走 stderr，所以
  `migrate41 --apply --json | jq` 不会被夹杂的文本弄坏。

### 4.2 迁移期已知缺口（真实数据试跑结论）

对生产库副本（8MB / 2 域名 / 2 服务 / 3 端口绑定）试跑 `--apply` 的结论：

1. **`domain.credentialId` 不迁**。该字段指向 `dns-credential`：kind 已在注册表里
   （`model/dns-credential.yaml`），但不在首批界面批次 —— 它的 `fields` 是随供应商
   变化的对象、`provider` 选项来自扩展注册表，需要 `uiHint: dns-credential` 定制渲染。
   首批界面**没有 DNS 凭证对象页，因此无法重选**，旧 id 照搬也必然悬空（旧 id 与
   V4.1 的 id 命名空间不同）。迁移报告里列出原凭证 id，该域名在 dns-credential 批次
   落地前只能走旧 DNS 路径（ADR-013 acme.sh）。首批 kind：route / service /
   middleware / entrypoint / domain / host / user / variable。
2. **旧片段悬空引用要报不要补**。真实库 `fragments` bucket 已空，但两个服务仍引用
   `frag-gzip` / `frag-websocket` / `frag-block-common` / `frag-log-config`。
   迁移不猜测这些片段的内容，报告为「片段已删」，中间件列表里跳过。
3. **残留测试数据要拦**。`port_bindings` 里有一条 `protocol: test`，不在类型目录中，
   报告后跳过，不让单条脏数据卡住整次迁移。
4. **组件是否安装不是数据合法性**。`entrypoint_protocols` 里 `tcp`/`udp`/`mysql` 标着
   `installed: false`（依赖 caddy-l4），但旧库里这些协议本来就在工作。迁移只校验
   「协议在不在目录」，不校验「组件装没装」——后者是运行期事实，交给渲染期报错。
5. **Apply 的引用校验不许放行**。存在性判定 = 库里已有 ∪ 本次计划里有：计划内部引用
   （route→service/entrypoint/domain）放行，`credentialId` 这种谁也满足不了的引用拒写。
6. **报告分三档，别混为一谈**：
   - `unmappable`（无法映射）：数据本身缺必要信息，**没写进去**，需手工补。
   - `conflicts`（冲突）：写进去了但 id 撞了（已自动加后缀），需人工确认。
   - `warnings`（需确认）：写进去了且数据完整，只是有地方值得看一眼。
7. **file_server 的 `route.backend` 是占位值**。静态文件没有上游，但 `route.backend`
   是 schema 必填，只能填 `127.0.0.1:1`。渲染器按**服务类型**分派（`file_server` 出
   `root` 指令、不读 backend），所以渲染是对的；但若有人把路由的「服务」引用清空，
   就会退化成 `reverse_proxy 127.0.0.1:1`。因此每条 file_server 路由都记一条
   `warnings`，提示确认服务引用没丢。渲染器对 `l4_proxy` 这类「目录已列但未实现」的
   类型**返回错误**（→ 对象状态「错误」），而不是悄悄渲染成反向代理。

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
