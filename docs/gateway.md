# GateBox · 网关功能设计 & 开发计划

> 状态：核心已实现（数据层 / caddy 生成器+Admin 客户端 / 健康采集 / Docker 派生 / 后端 API+日志 / 前端 Tab（原 3 Tab，ADR-035 起变量迁设置页）/ 创建编辑代理 / 启停日志 / Caddy 片段 / ddns 上报联动）；PRD v2 单位③负责「**收尾**」——Caddyfile validate、片段-代理自动关联、docker 自动代理打通（§9）
> 关联：`docs/PRD.md`、`docs/glossary.md`、`docs/domain.md`（跨页联动）、`docs/docker.md` §5.6/§12.3（跨单位接口）、`docs/adr/ADR-002 / 007 / 013 / 016 / 017 / 018(含修订) / 019 / 020 / 021`

## 1. 定位与范围

网关页是控制面一级导航，负责「应用代理」与「Caddy 片段」两大块：

- **代理**：应用（App）及其下服务（Service）的创建、编辑、启停、日志、健康；Docker 自动派生服务的只读展示。
- **Caddy 片段**：遵循 caddy Caddyfile 规范、带 GateBox `<%VAR%>` 变量的通用配置片段（含默认自带只读条目）。

边界（关键）：

- **证书、DNS 记录不建实体**——由 caddy / ddnsgo 托管，本页只触发与展示。
- **Docker 自动 service 是派生视图**——真相源 = docker label，增删改跳转 Docker 单位（ADR-018）。
- **不接管 caddy 生命周期**——启停/重启是 **service 级**操作，非 caddy 进程级。
- **L4（tcp/udp）本期后置**：`tools/caddy`（v2.11.4 标准版）无 caddy-l4 插件，需 xcaddy 重编译才支持；且 L4 按 IP:端口匹配、与「域名行（子域）」语义不同，单独一期。

## 2. 领域模型（BoltDB，三层）

| 实体 | 字段 | 说明 |
|---|---|---|
| **App（应用）** | `id`、`name`、`description`、`createdAt` | 纯分组实体，**不绑定 rootDomain**；创建代理一次性建应用 + 其下全部服务 |
| **Service（服务）** | `id`、`appId`(FK→App)、`name`、`description`、`type`、`domains[]`、`upstream[]`、`root`、`healthUri`、`fragmentIds[]`、`enabled`、`createdAt` | 原 ProxyRoute；`type ∈ {reverse_proxy, file_server}`；`source=manual` 落库、`source=docker` 派生不落库 |
| **域名行**（Service 内联） | `protocol(https/http)`、`subdomain`、`rootDomain`、`customPort{custom,port}` | 不建实体；一个 Service 多行 = 同一后端的多个别名 |

- 每行以下拉/单选 + 输入表达：`解析域名 Z-SIB` = 传输协议(S) + 二级域名(I) + rootDomain(S) + 添加域名(B)。
- **Caddy 片段（Fragment）**：`id/name/description/code(遵循 caddy Caddyfile 语法 + <%VAR%>)/defaultEnabled(default off)/defaultHidden(default off)/createdAt`。
  > 2026-09-07 移除原 `tag(handler|route)`：生成器把片段体整块写入，无 handler/route 分层语义，纯装饰字段已删（模型/API/内置目录同步清理，历史数据 tag 键自动忽略）。
  > **2026-09-15（ADR-033）**：片段作用域由 code 自描述——顶层指令写 site block 顶层；`reverse_proxy { <子指令> }`（不带参数）合并进受控反代块；带参数或混用报错。
  - 默认自带 **7 条目**只读硬编码：Gzip/Zstd、Basic Auth、忽略自带证书校验、按服务日志、阻止常见漏洞、静态资源缓存、支持 websocket。健康检查不再是片段，由 `Service.HealthURI` 字段承载。
- App/Service 均不绑定 rootDomain；rootDomain 只在「域名行」选择（来自域名管理）。

## 3. 页面设计（2 Tab，ADR-018 修订 + v2；变量已迁至设置页，ADR-035）

### Tab 1 · 代理

- 二级目录按钮：`+ 创建代理`（合并原「创建反代/静态文件」，代理类型在服务内单选）。
- 列表：**按应用折叠分组**（折叠头=应用名+描述），行=Service；「Docker 自动」派生 service 放固定只读分组（列表末尾，不属任何 App）。
- 服务行列：名称（悬停显示 description）· 域名（主 + 多余 popover）· 访问端口 · 类型（反代/静态/自动标签）· 服务地址 · 服务端口 · 健康（绿/红/未知）· 操作（停止/重启/日志/编辑/删除；Docker 自动仅「管理」跳 Docker）。

### Tab 2 · Caddy 片段

- 二级目录：`+ 创建片段`（通用 JSON 编辑器，复用 ComposeEditorModal 的 CodeMirror）。
- 列表列：名称 · 来源（内置/自定义）· 默认启用 · 默认隐藏 · 说明 · 操作（修改/删除，被 Service 引用时禁删）。
- 片段编辑/新增页：编辑器上方提供「变量」button（列出 §5.1 全部可用变量：系统变量 + 用户变量），**点击即在光标处插入该 `<%VAR%>` 文本**；并保留**内联新增用户变量**（改调统一 API，ADR-035 §9）。

> **变量管理入口已迁至「设置 → 变量」**（ADR-035）：网关页不再有「变量」Tab，只剩「代理 / Caddy 片段」两 Tab。片段编辑器的变量选择器与内联新增保留，数据来自设置页维护的统一变量存储。

### 创建代理（终案图，ADR-018 修订 §1/§2）

```
┌─────────────────────────────────────────────────────┐
│ 代理规则                                         X  │
├─────────────────────────────────────────────────────┤
│ 基本信息                                            │
│ 应用名称*（I）           应用描述（I）              │
│ --------------------------------------------------- │
│ 服务信息       + 新建服务（B）                      │
│ ┌─────────────────────────────────────────────────┐ │
│ │ 服务名称*（I）           服务描述（I）          │ │
│ │ ----------------------------------------------- │ │
│ │ 域名信息       + 新建域名（B）                  │ │
│ │ 解析域名（Z-SIB）     使用端口（Z-KI）删除（B） │ │
│ │ ----------------------------------------------- │ │
│ │ 服务端信息                                      │ │
│ │ 代理类型（Z-B）                                 │ │
│ │ 代理详情（按情况显示不同的 form）               │ │
│ │ ----------------------------------------------- │ │
│ │ 其他选项（见下）                                │ │
│ └─────────────────────────────────────────────────┘ │
├─────────────────────────────────────────────────────┤
│                                 取消（B） 确定（B） │
└─────────────────────────────────────────────────────┘
```

**1.1 基本信息**
- 应用名称*：提示「一般用服务/产品名，如 app01」；应用描述：提示「一句话概括该应用」。
- 一个创建会话 = **一个应用 + 其下全部服务**（`+ 新建服务` 增卡片）批量提交。

**1.2 服务信息**
- 服务名称*：提示「用服务名，如 web / api」；服务描述。
- `+ 新建服务`：应用下加一个服务卡片；每卡片独立域名行 + 代理类型 + 详情 + 其他选项。

**1.3 域名信息**
- `+ 新建域名`：加一行。
- 解析域名：`传输协议(S: https 默认/http)` + `二级域名(I: 空则用主域名)` + `rootDomain(S: 选已登记 / 未登记点右侧 添加域名 跳域名管理)`。
- 使用端口：`自定义端口(K: 默认关)` + `端口(I: 正整数 ≤65535，K 关时灰色不可输入，不填为协议端口)`。

**1.4 服务端信息**
- 代理类型 Z-B：反向代理 / 静态文件。
- 反向代理：目标协议(S: http默认/https) · 目标地址(I: IP/域名) · 目标端口(I: 正整数 ≤65535，不填为协议端口)。
- 静态文件：网站根目录 Z-SI（路径选择 S: 默认路径/自定义；根目录 I：默认= `<%GB_STATIC_ROOT%>/<%GB_SERVICE%>` 灰色不可改，自定义=绝对路径可输入）+ 显示文件列表(K: 默认关，开则加 `browse`)。

**1.5 其他选项**
- 开关：`禁用当前规则`（= service.enabled，整条规则失效）· `阻止常见漏洞` · `静态资源缓存` · `支持 websocket`（后三者写 handlerIds）+ 所有「默认隐藏=关」的片段 toggle（「默认启用」预勾选）。

## 4. 核心流程

- **创建代理**：填应用 + 至少一服务（域名行选 rootDomain+子域）→ 落库 App + Services → 重生成 Caddyfile → `/load` → 触发 ddnsgo 上报 + caddy 签证书（DNS-01）。
- **编辑**：点服务行 → 打开**应用级抽屉**（含该应用全部服务）编辑。
- **停止/启用**：切 service `enabled` → 重生成 Caddyfile（排除/恢复 site block）→ `/load`。
- **删除服务**：校验 `enabled=false` → 删库 → 同步清 ddnsgo 该二级域名记录 → 重生成 Caddyfile → `/load`。
- **删除应用**：级联删其下全部服务（各自沿用「须已停用」约束 + DNS 清理）。
- **查看日志**：WebSocket follow，按 host 过滤 access log。
- **健康状态**：单例采集器轮询 caddy Admin API → 快照，前端轮询快照端点。

## 5. 后端设计

模块（`backend/internal/`）：

- `models`：App、Service（原 ProxyRoute）、HandlerInstance。
- `store`：bucket `apps` / `services` / `handlers`；片段敏感字段 AES 加密。
- `caddy`：Caddyfile 生成器（snippet/停用排除/import user）+ Admin API 客户端（`POST /load`、`GET /reverse_proxy/upstreams`）。
- `gateway`：docker service 派生、健康采集器（单例轮询）、列表聚合（按 App 分组）。
- `api`：HTTP + WebSocket handler。

API 概要（`/api/v1`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/apps` | 应用列表（含其下服务 + Docker 自动派生合并，按 App 分组） |
| POST | `/apps` | 批量创建应用 + 其下全部服务 |
| GET/PUT/DELETE | `/apps/:id` | 编辑（含服务增删）/ 级联删除（服务须已停用） |
| POST | `/services/:id/{stop,start}` | 启停服务（`enabled` 切换 + `/load`） |
| WS | `/services/:id/logs` | 日志 follow（按 host 过滤） |
| GET | `/services/health` | 健康快照（采集器聚合） |
| GET/POST | `/fragments` | 片段列表 / 创建（含默认自带只读条目） |
| PUT/DELETE | `/fragments/:id` | 编辑 / 删除（被 Service 引用时 409） |
| — | ~~`/variables`~~ | **已迁至 `GET/POST /settings/variables`**（ADR-035，统一变量存储） |
| — | ~~`/variables/:key`~~ | **已迁至 `PUT/DELETE /settings/variables/:key`** |

### 5.1 GateBox 变量系统（Caddy 片段占位；统一存储，ADR-035）

Caddy 片段是遵循 caddy Caddyfile 语法的模板，含变量位。网关侧引用写法为 `<% … %>`，与 caddy 自身的**环境变量占位符 `{$VAR}`**（可带默认 `{$VAR:default}`）**彻底区分**——`{$…}` 原样透传、不参与替换；`${…}` 并非 Caddy 语法（ADR-033 修订）。

> **变量存储与入口已统一**（ADR-035）：用户变量存于单一 bucket `variables`，由「设置 → 变量」页统一 CRUD，网关与容器共用同一份值；**引用写法随上下文而变**（网关 `<%KEY%>`、容器 `${KEY}`）。网关 API 不再暴露用户变量端点，只保留系统变量视图 `GET /api/v1/settings/variables/system`。

**① 系统变量（只读，闭集，不可删改）**：

| 变量 | 含义 | 归属 |
|---|---|---|
| `<%GB_APP%>` | 应用名 | 应用级 |
| `<%GB_SERVICE%>` | 服务名 | 服务级 |
| `<%GB_HOST_PORT%>` | 反代目标端口（Service 服务端目标端口） | 服务级 |
| `<%GB_DATA_DIR%>` | 实际 `data_dir` 绝对路径 | 全局（路径原语） |
| `<%GB_STATIC_ROOT%>` | 全局静态根（`<dataDir>/www`，Q5） | 全局（组合） |
| `<%GB_LOG_FILE%>` | 每服务日志文件路径 | 全局（组合） |

**② 用户变量（统一存储，设置页 CRUD）**：key 需匹配 `[A-Za-z_][A-Za-z0-9_]*` 且 **不得以 `GB_` 开头**（保留前缀）；值 = 任意字符串。引用写法为 `<%KEY%>`，生成器按 key 查表替换；同一值在容器侧以 `${KEY}` 引用（详见 ADR-035）。

- **解析时机**：Caddyfile 生成器渲染 site block 时，按「系统（按 App/Service 上下文）→ 用户全局（查表）」顺序替换 `<% … %>`；**未命中的 `<% … %>` 报配置错误**（防拼错静默生成非法路径）。
- **caddy env 透传**：`{$…}` 原样保留，交给 caddy `/load` 时读系统环境变量——用户可借用 caddy 侧能力。

---

## 6. 开发计划（含验证方案）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | 数据层 | App / Service / HandlerInstance 模型 + BoltDB + 敏感字段 AES | 单测：CRUD 往返、加解密往返 | — |
| 2 | caddy 生成器 | 根据 apps+services+fragments 生成完整 Caddyfile（含 `<%VAR%>` 插值） | 单测：给定 apps → 期望 Caddyfile；`caddy validate` 通过 | 看实际 caddy 生效 |
| 3 | caddy Admin 客户端 | `POST /load` + `GET /reverse_proxy/upstreams` | 单测：mock Admin API；集成：真实 `/load` 原子替换 | — |
| 4 | 健康采集器 | 单例轮询 caddy Admin API 聚合快照 | 单测：mock upstream → 绿/红/未知 | 造故障后端看变红 |
| 5 | Docker 自动派生 | `/docker/proxyable` → service 映射（含 displayName） | 单测：label → service、不含容器 ID/IP | 起带 label 容器看列表 |
| 6 | 后端 API | 上述 REST + WS 日志 | 集成：happy path + 边界（删除被引用片段 409） | — |
| 7 | 前端骨架 | 网关页 2 Tab + 列表按应用分组 | 构建通过、路由可达 | 切 Tab 正常 |
| 8 | 创建/编辑代理 | 应用+多服务批量表单（域名行动态增删、类型联动） | 组件测试：表单校验、联动 | 造数据看列表 |
| 9 | 启停 / 日志 | 停止/启用 + 日志 follow | 组件测试：状态切换、WS 连接 | 实际 stop/start + 看日志 |
| 10 | Caddy 片段页 | 创建/编辑/删除 + CodeMirror JSON 编辑器 + 默认条目 | 组件测试：JSON 校验、引用禁删 | 建片段挂到服务 |
| 11 | 端到端联动 | 创建应用 → Caddyfile → `/load` → 证书 → 健康变绿 | `caddy validate` + 集成测试 | 走完整闭环看证书/DDNS/健康 |

## 7. 验证方案汇总

- **AI 验证**：任务 1–6、8–10 的单测/集成/组件测试；任务 2 的 `caddy validate`；任务 11 集成测试。
- **人工验证**：装好 caddy / ddnsgo 后走「创建应用(含多服务) → 看 Caddyfile 生效 → 证书 → 健康变绿 → 停止/启用 → 看日志 → 删除应用级联」完整闭环。

## 8. 后置 backlog

- **L4（tcp/udp）透传**：需 xcaddy 重编译加 `github.com/mholt/caddy-l4`；按 IP:端口匹配，单列一期。
- 多上游负载均衡（本期单上游，字段已数组化）。
- 域名行更多协议（如自定义证书）、纯 HTTP 内网代理细分。
- 更多 Caddy 片段（`redir` / `request_header` / `request_body` …）并入通用片段体系。
- 日志历史搜索、file_server 文件上传/管理、静态根目录管理。
- 独立 ACME 客户端、多主机控制。

---

## 9. PRD v2 变更（单位③）：收尾与自动关联

> 对应 PRD §14 单位③。前置：单位①（目录、Caddyfile 备份上提）与单位②（容器→网关代理数据事件）。本节是 v2 增量，覆盖 §6 计划任务的「未完成」部分与验证。

### 9.1 完成情况（现状，来自 PRD §9.1）

- **已完成**：页面 Tab（原 3 Tab「代理 / Caddy 片段 / 变量」；ADR-035 起变量迁至设置页，现为 2 Tab）、手工代理、Caddy 片段、片段默认自动关联（后端 `seedDefaultFragments` + 前端预勾选，2026-09-07 核对已落地）、https 后端自动忽略证书校验（生成器条件应用 `reverse_proxy { transport http { tls_insecure_skip_verify } }`，ADR-033）。
- **待验证**：代理配置**未端到端走查**（Caddyfile validate 已补：`reloadCaddy` 前置校验 + 成功 `/load` 后写 `$DATA_DIR/Caddyfile` 备份）。
- **未完成**：
  1. ~Caddy 片段与代理的关联（自动关联已实现，端到端未走查）~；
  2. ~docker 自动代理打通~（已打通：`listGroups`/`reloadCaddy` 实时派生 + 10s 轮询推送；单位②事件总线后置）。

### 9.2 片段-代理的自动关联

**语义**（承接 ADR-018 修订点 5「默认启用预勾选」的落地不足）：新建/编辑 Service 时，**自动把「默认启用」的片段写入该 Service 的 `FragmentIDs[]`**，用户可在「其他选项」按需取消勾选。

> **2026-09-07 状态**：后端 `gatewayAPI.seedDefaultFragments`（含内置片段 toggle 覆盖、默认隐藏不可排除）+ 前端 `AppFormModal.preselectDefaults` 均已实现，待端到端人工走查确认。

**自动关联默认片段集**（ADR-033 定案，`seedDefaultFragments` 按类型无关 seed）：

| 片段 | 生效范围 | 说明 |
|---|---|---|
| Gzip/Zstd（encode） | 全部 | 默认勾选，可取消 |
| 阻止常见漏洞 | 全部 | 默认勾选，可取消 |
| 支持 websocket（`flush_interval -1`） | 反向代理 | 默认勾选；静态服务上生成器静默忽略 |
| 按服务日志 | 全部 | 默认勾选；落 `<dataDir>/logs/caddy/<应用>_<服务>.log` |
| 静态资源缓存 | 默认关 | 需在「其他选项」手动勾选 |
| Basic Auth | 默认关 + 默认隐藏 | 默认 admin/admin，待「设置-用户」完善 |
| 忽略后端证书校验 | **后端目标协议 = https**（自动判定，不可手选） | `reverse_proxy { transport http { tls_insecure_skip_verify } }`，**不落 FragmentIDs** |

> 「忽略后端证书校验」由生成器按 `Service.UpstreamProto == "https"` 条件应用（`caddyfile.go` `serviceFragments`），修复原 `transport http { tls }` 不跳过校验、以及非法 `tls_connection_policies` code 导致配置加载失败的缺陷（ADR-033）。
> 健康检查由 `Service.HealthURI` 字段承载（默认 `/`，编辑可改/关，ADR-020 §1），不再作为片段。

**边界**：手动自定义片段不自动关联；「默认隐藏」（`defaultHidden`）的片段仅在「其他选项」中不显示（ADR-033 起前端读后端字段，不再硬编码）。

> **docker 派生**（ADR-026）：派生 Service 同样自动带上「默认启用」片段集；`gatebox.fragments: name1,name2` 显式引用额外片段；上游 https 时生成器条件应用 `frag-skip-verify`。引用缺失温和降级（忽略 + 告警）。

### 9.3 docker 自动代理打通

- 网关侧复用 `proxyableContainers` 派生「Docker 自动」service（已实现，§5）；两处补齐：
  1. **即时刷新**：`listGroups`/`reloadCaddy` 每次实时重派生（后端 `deriveDockerServices`），前端代理 Tab **10s 轮询**推送变化。单位②事件总线（docker.md §12.3）**后置**——按「人工操作即 load、自动变化不 load」策略，容器自带 label 变化不自动触发 `/load`。
  2. **异常保留**：`proxyableContainers(ctx, cli, s, includeStopped)` 网关侧重置 `true`，非 running 容器派生为 `Enabled=false`、`containerState!=running` 的行——列表保留并标红「容器×」，**不生成 site block、不进 ddns 上报**；docker 端点（`/docker/proxyable`）保持 running-only 语义。
  3. **手动同步**（ADR-026）：编排页「同步到网关」按钮 → `POST /api/v1/docker/sync-caddy`；编排部署成功 / down / restart / adopt / convert / upgrade 后自动同步一次。
- 派生 label 语义（ADR-026）：协议/访问端口编码进 site 地址、upstream 三级优先解析、`gatebox.fragments` 片段引用、受管根域自动匹配（进 ddns/acme 闭环），详见 `docs/adr/ADR-026.md`。
- 验证：起带 `gatebox.*`/`caddy.*` label 的容器 → 网关列表出现 → 停容器 → 标红保留 → 恢复 → 变绿。

### 9.4 单位③ 开发计划（含验证方案）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | 自动关联默认片段 | 创建代理默认写 FragmentIDs + 表单预勾选 | 单测：新建服务默认片段集；组件测试：预勾选状态 | 创建代理看「其他选项」预勾选 |
| 2 | 后端 https 自动忽略证书校验 | 生成器条件渲染 `transport http { tls }` | 单测：http/https 两种目标协议 → 期望 Caddyfile | 代理 PVE 8006 后端可见 |
| 3 | docker 自动代理打通 | 实时派生 + 前端 10s 轮询 + 异常保留 | 单测：stopped 容器派生 `Enabled=false` 保留展示 | 起/停带 label 容器看列表 |
| 4 | Caddyfile validate 闭环 | 生成后 `caddy validate` 前置校验（缺失降级跳过）+ 成功 `/load` 写 `$DATA_DIR/Caddyfile` 备份 | 单测：validate 二进制缺失/非法配置；备份写盘断言 | 非法配置后端可见 400 且旧配置继续 |
| 5 | 端到端人工验证 | 创建应用→证书→健康→停用→删除级联 | 见 §7 | 完整链路走查 |

### 9.5 派生行操作与域名协议选择（2026-09-15）

- **派生（Docker 自动）行的操作列**：`自动代理(闪电，去容器页) + 停止 / 启动 / 重启 / 日志`，全部**只作用于 caddy**，与容器生命周期无关。
  - 停止 = 写入本地「派生禁用集合」（`derived_disabled` bucket，稳定键 `docker:<项目>~<服务>~<host>~<port>`），生成器对命中项 `Enabled=false`、不生成该 site block；启动 = 移除。
  - 重启 = 仅 `reloadCaddy`（重新生成 + load）。日志 = 读该服务的 `GB_LOG_FILE`（派生服务按 `?service=<名称>` 定位）。
  - 派生 Service 现下发稳定 `ID`（`docker:` 前缀），供操作列与本地覆盖引用。
- **域名行协议选择**：由「端口页」协议驱动（`http/https` 恒可用；其它协议是否可用取决于能力注册表中是否存在 `class=non-http` 的 `proxy-protocols` 能力，即是否有对应能力型插件启用）。选 `https` 即该域名在**全部 https 端口**（443、9443…）生效；`customPort/port` 不再由表单设置。域名行「+」复用共享 `PortFormModal`（与端口页同一弹层），新增后自动选中新协议。
- **目标协议**：非 HTTP 协议选项在无对应能力插件时置灰，提示「需启用支持 TCP/UDP 的扩展」（前端不引用具体插件名）。
- **忽略自带证书校验**：片段改为「其他选项」中可见；`UpstreamProto=https` 时自动勾选且**禁用**（由生成器强制应用，不可手动取消）。

### 9.6 L4（TCP/UDP）代理与能力型插件（ADR-036）

> **核心不含任何 L4/`layer4`/插件 ID 硬编码**——L4 是「能力型插件（Provider）」通过扩展点接入的示例。

- **插件形态**：`kind=caddy-module` 的扩展（如 Caddy L4）安装时用含目标模块的 caddy 制品替换 active 制品（备份原文件），重启 Caddy 后生效。校验统一用**当前 active caddy 制品**，核心不挑选特定插件制品。
- **能力声明**：插件在 manifest `contributions.capabilities` 声明 `point: proxy-protocols`（`class=non-http`、`networks` 等）；启用后该能力进入**能力注册表**，停用即消失。
- **渲染分发**：核心只产出中性规则 `ProxyRule{Protocol, Upstream, Ports, Nets}`。HTTP 规则走内置渲染器生成 site block；非 HTTP 规则按协议查注册表 → 调用插件声明的 `renderer`（`scope=global`）→ 得到全局块片段写入全局块。**核心不解析片段内容。**
  ```
  {
      <内置：http_port / https_port / log>
      <插件 renderer 产出的全局片段，如 layer4 {...}>
  }
  ```
- **L4 网络下沉到「端口」页**：协议记录含「网络」列（`TCP / UDP / TCP & UDP`；http/https 恒 TCP）。选协议即完成 tcp/udp 选择，创建代理与编排都不再单独选目标网络。
- **前端（能力驱动）**：协议下拉从 **能力注册表 API** 取选项；当 `proxy-protocols` 存在 `class=non-http` 且启用时，非 http/https 协议可选，否则置灰并提示。前端**不判断任何插件 ID**。
- **docker 派生 L4 label**：非 HTTP 协议的站点地址写为 `<proto>://`（如 `caddy: mqtt://`），端口/网络取端口页该协议记录；派生出 `Domain.Protocol=<proto>`，核心按协议分发到对应 renderer。
- **边界**：`caddy-module` 安装覆盖主 caddy 制品，多个此类插件**不可并存**（后装覆盖前装）；运行中的 Caddy 必须为该制品，否则含对应指令的 `/load` 会失败。
