# 术语表 Glossary

统一语言（Ubiquitous Language）。全项目文档、代码、UI 文案一致使用以下术语。

| 术语 | 定义 |
|---|---|
| **控制面** | GateBox 本体（Go 后端 + Vue3 前端 + BoltDB） |
| **外部组件** | caddy、acme.sh、docker、docker-compose、ddns-go、mosdns、tailscale、flame——独立进程、独立升级；GateBox 只改写其配置 + 调其 API/CLI |
| **App（应用）** | 网关代理的**分组实体**（名称 + 描述，落库），下挂 0..N 个 Service；**不与 rootDomain 绑定**；「应用」与「服务」是两个层级 |
| **Service（服务）** | App 下的代理单元（即原 ProxyRoute），`type ∈ {reverse_proxy, file_server}`（`tcp_stream`/L4 后置）；含 0..N 个域名行；`source ∈ {manual, docker}`；`Docker 自动`为派生 service、不落库 |
| **域名行** | Service 内联的一条域名配置（不建实体）：`传输协议(S: https/http)` + `二级域名(I)` + `rootDomain(S)` + `使用端口(custom)`；一个 Service 可有多个域名行 = 指向同一后端的多个别名 |
| **Domain（域名）** | 根域名 rootDomain（如 `gatebox.cn`、`neob.cn`），域名管理的基本单位、独立实体 |
| **二级域名** | rootDomain 下的子域名（如 `ddnsgo.neob.cn`），由网关页 Service 的域名行产生，不建独立实体，仅统计数量 |
| **证书管理器** | 证书访问的抽象层：当前实现 = **AcmeCertManager**（读 acme.sh 证书目录 `tools/acme/certs/<fqdn>/`，ADR-013）；未来切 CA/客户端只换实现 |
| **ComposeInstance** | 一个 compose 项目：定义（YAML 文件）+ 运行态。部署的基本单位，`1 ComposeInstance ──< N Container ──< M Service`（M ≤ N，M = 需被代理的容器，网关侧为派生 Service） |
| **projectName** | ComposeInstance 主键，同时是 docker 项目名与 `appData/` 目录名。格式 `[a-z0-9][a-z0-9_-]{1,62}`，**创建后不可改** |
| **displayName** | ComposeInstance 的展示名，可用中文等任意字符，仅用于 UI |
| **托管容器** | GateBox 在 `appData/<projectName>/` 下创建的编排所起的容器，全功能可编辑 |
| **外部编排** | 由 `docker compose ls -a` 发现的非托管项目，默认只读，需显式「接管」才可编辑 |
| **游离容器** | 无 compose project label 的容器（`docker run` 起的），只读控制 + 可「转为编排」 |
| **DNS 凭证** | cloudflare / dnspod.cn / aliyun 的 API key（**统一模型**：acme.sh 证书签发与 ddns-go 上报共用，加密存储于 `conf/secret.key`） |
| **Upstream（后端）** | 代理转发目标，`IP:端口`；网关单位本期单上游（字段数组化向后兼容）。弃用「router」，统一「后端/upstream」 |
| **DATA_DIR** | 用户指定的运行时数据根目录，install.sh 创建并写入 `conf/gatebox.conf` |
| **source** | Service 的来源：`docker`（label 自动发现，派生不落库）/ `manual`（手动 IP:端口，宿主机服务并入此项） |
| **代理类型** | 网关「服务」的二级分类：`反向代理` / `静态文件` / `Docker 自动`（决定流量去哪），与「Caddy 片段」两分 |
| **Caddy 片段（Fragment）** | 一段遵循 caddy Caddyfile 语法的配置片段（可含 `<%VAR%>`），决定流量怎么处理。按 code 自描述作用域：顶层指令写 site block 顶层；`reverse_proxy { <子指令> }` 包裹片段合并进受控反代块（ADR-033）。变量位用 GateBox 语义（最小变量 `<%GB_APP%>`/`<%GB_SERVICE%>`/`<%GB_HOST_PORT%>`/`<%GB_DATA_DIR%>` + 组合变量 `<%GB_STATIC_ROOT%>`/`<%GB_LOG_FILE%>`，定界符 `<% %>` 与 caddy env `{$…}` 区分） |
| **片段自动关联** | 新建/编辑 Service 时自动把「默认启用」片段写入其 `FragmentIDs[]`；后端目标 https 时生成器按 `UpstreamProto` 自动应用「忽略证书校验」（不落 `FragmentIDs`，ADR-033 修订 PRD v2 单位③） |
| **其他选项** | Service 表单底部按 `defaultHidden` 过滤出的 Caddy 片段 toggle（默认启用者默认勾选）；健康检查/忽略证书校验由字段与生成器承载，不出现在此处；每个 Service 落库自己的片段引用 |
| **默认启用** | Caddy 片段的一个开关（默认关）：开启后，在每个 Service 的「其他选项」中该片段**默认勾选** |
| **默认隐藏** | Caddy 片段的一个开关（默认关）：开启后，在「其他选项」中**不显示**该片段 |
| **默认自带条目** | **只读硬编码**的 Caddy 片段清单（ADR-033，7 个：Gzip/Zstd、Basic Auth、忽略自带证书校验、按服务日志、阻止常见漏洞、静态资源缓存、支持 websocket），非可编辑实例；健康检查由 `Service.HealthURI` 字段承载，不再作为片段；用户另可自建片段 |
| **site 地址** | caddy label 值的反代站点地址（ADR-026）：`[proto://]host[:port]`；裸 host=**https/443**、`http://`=**http/80**、`:port`=自定义访问端口；可逗号分隔多站点 |
| **站点（域名行）** | 一个反代站点地址；**每个站点派生一个独立 Service**（ADR-026 修订）——同一容器可经不同域名发布不同端口/不同片段的服务 |
| **`{{upstreams}}`** | caddy-docker-proxy 的 upstream 模板（ADR-026 自解释）：`{{upstreams [https] [N]}}`，`N`=容器内部端口（反向查宿主映射），无则用唯一宿主端口；解释为 `127.0.0.1:<宿主端口>` |
| **`gatebox.*` label** | GateBox 扩展命名空间（ADR-026）：`gatebox.upstream_port`（服务级逃生舱宿主端口）、`gatebox.fragments[_N]`（站点片段名逗号列表，行级覆盖继承）、`gatebox.description`（派生 Service 说明） |
| **行级覆盖继承** | 派生语义（ADR-026 修订）：每个站点优先读取自己的 `caddy_N.reverse_proxy` / `gatebox.fragments_N`；缺省继承服务级 `caddy.reverse_proxy` / `gatebox.fragments`（= 站点 0） |
| **ExtraDirectives** | 派生 Service 的透传指令行（force 自 `caddy.*` 逃生舱子 label，ADR-026），derived-only、不落库，site block 三段式按序拼装 |
| **派生告警** | 派生时的温和降级提示（ADR-026）：引用缺失片段、全局级 `caddy.*` 键、多宿主端口未标注等场景，容器仍可代理但网关分组行展示告警 |
| **同步到网关** | 把运行中容器 label 派生进整体 Caddyfile 并 `POST /load`（ADR-026）：编排动作自动触发 + docker 页手动按钮 `POST /api/v1/docker/sync-caddy` |

## PRD v2 新增术语

| 术语 | 定义 |
|---|---|
| **容器** | 一级导航名称（原「Docker」，ADR-025）；管理对象 = 容器、编排、镜像、网络、存储卷 |
| **DDNS** | 域名页 Tab：ddns-go 上报记录状态 + 统一 DNS 凭证管理（ADR-024） |
| **内网 DNS** | 网络页 Tab：mosdns 承载，已发布应用的内网解析记录自动指向（单位⑤，深度待讨论） |
| **组网** | 网络页 Tab：Tailscale 承载，跨网访问 HomeLab（单位⑤，深度待讨论） |
| **flame（导航）** | 首页 Tab：以单二进制（`tools/flare-<arch>`）承载的导航页，GateBox 内嵌 iframe 展示 + 配置联动（ADR-023） |
| **Caddyfile 备份** | `$DATA_DIR/Caddyfile`——最近一次成功 `/load` 的配置源备份（ADR-002 / 022） |

## v3 重构新增术语

> 详见 `docs/architecture.md` 与 ADR-027 ~ ADR-034。

| 术语 | 定义 |
|---|---|
| **组件（Component）** | GateBox 统一纳管的可执行单元（核心或插件），以 `Descriptor` 描述、按可选接口实现能力（ADR-028） |
| **被管对象（Managed Object）** | GateBox 完全所有的对象（容器、镜像、网络、卷、compose 项目、代理规则、域名）；与「组件」相对 |
| **配方（Recipe）** | 制品由什么构成：二进制版本 + 编译进哪些模块。**归上游，GateBox 永不改写**（ADR-027） |
| **制品（Artifact）** | 配方产出的可执行文件 |
| **制品源（Source）** | 制品获取渠道：`official`（上游官方）/ `custom`（用户/团队 fork）/ `system`（系统包管理器）。「最新版本」永远相对源解析 |
| **生命周期六层** | 配方 / 制品 / 制品源 / 运行态 / 配置 / 版本 / 升级；前两者归上游，运行态与配置归 GateBox，升级为搬运边界（ADR-027） |
| **managed / attached** | 组件的生命周期归属档：`managed` = GateBox 供应并管理；`attached` = 由用户/系统提供，GateBox 只读写配置/API（docker 为 attached） |
| **升级策略** | `replace`（搬运替换 + 校验回退）/ `system`（仅提示，符合包管理器）/ `none`（不可升级） |
| **core / optional** | 组件产品档：`core` 默认安装、不可卸载；`optional` 按需安装。与 `managed/attached` 正交 |
| **Bundled** | 离线安装包是否附带该 `optional` 组件的制品（如 ddns-go / flame） |
| **插件（Plugin）** | 数据驱动的可选扩展=manifest + 通用引擎 + 贡献注册，`kind ∈ {caddy-module, process, config-only}`；ADR-037 起为**唯一可安装单元**（ADR-029 / ADR-037） |
| **扩展平台（Extension Platform）** | 核心与插件解耦的机制：五个部件（Manifest v2 / 能力注册表 / 投影 API / 扩展契约 / 槽位注册表）+ 三个不变式（ADR-036） |
| **扩展点（Extension Point）** | 核心定义的抽象接入点：`proxy-protocols` / `renderer` / `validator` / `config-sync` / `reconcile`（ADR-036） |
| **能力注册表（Capability Registry）** | 核心与插件统一注册「能力」的表；网关/容器/前端只查表、不认插件身份（ADR-036） |
| **Provider / Consumer** | 由贡献点导出的两类插件原型：能力型（扩展核心行为）/ 数据型（消费核心数据）（ADR-036） |
| **投影（Projection）** | 核心状态的只读视图（domains/certs/ports/services），带 `revision`，经变更通知驱动插件自收敛（ADR-036） |
| **扩展契约（Extension Contract）** | 核心调用插件逻辑的两种实现：`template`（声明式）/ `sidecar`（进程，JSON over stdin/HTTP）（ADR-036） |
| **manifest** | 插件声明文件；v2 为强类型：`artifacts[](role)` + `contributions{capabilities,ui,backend,data}` + `permissions`（ADR-036） |
| **静态索引** | 可静态托管的插件/制品索引 `index.json`（+ 签名），客户端据此在线安装/升级；**无常驻服务端**。ADR-037 起本体托管于 `JiangBeta/GateBoxStore`（ADR-029 / ADR-037） |
| **插件状态机** | `available → installed → enabled/disabled`，失败转 `error`；`disable` 保留制品、`remove` 删除（ADR-029） |
| **设计 token** | 前端颜色/字号/间距/圆角的唯一来源（`frontend/src/design/`）；`.vue` 禁止样式字面量（ADR-032） |
| **分层标准** | 后端 `handler/service/repository/component/source/model` + 前端 `design/lib/app/modules/shared` 的判层与依赖方向，由 lint 强制（ADR-031/032） |
| **old/** | 重构前原始代码的完整保留区（`old/backend` 独立 `go.mod`），供回溯查证（ADR-031） |
| **服务用户（gatebox）** | 安装脚本创建的专用低权系统用户，GateBox 本体与其托管进程均以此身份运行（ADR-034） |
| **软重启** | 不中断服务的重载：Docker `reload`（SIGHUP）、Caddy `POST /load`、托管进程 stop+start、acme 重探测（ADR-034） |
| **硬重启** | 真正重启守护进程（如 `systemctl restart docker.service`），会中断容器，UI 须二次确认（ADR-034） |
| **polkit 授权** | systemd 下以 `rules.d` 规则最小授权 `gatebox` 管理 `docker.service`，替代 sudo（ADR-034） |

## 变量统一新增术语（ADR-035）

> 「设置 → 变量」的唯一数据源。引用写法随消费格式而变，命名值统一。

| 术语 | 定义 |
|---|---|
| **变量（Variable）** | GateBox 全局命名值：一条 `key → value`，由用户统一维护。是「值」的抽象，不绑定消费上下文 |
| **用户变量** | 可编辑的变量（CRUD），统一存储于 bucket `variables`，网关 Caddyfile 与容器 compose 均可用其值；**不引入作用域字段**，同一 key 只有一个值 |
| **系统变量** | 只读、上下文派生的变量：网关内置（`GB_APP`/`GB_SERVICE`/`GB_HOST_PORT`/`GB_DATA_DIR`/`GB_STATIC_ROOT`/`GB_LOG_FILE`）与容器内置（`GB_PROJ_NAME`/`GB_PROJ_FILE`/`GB_SER_<n>_PORT_<n>`）。由 `GET /api/v1/settings/variables/system` 下发描述符 |
| **引用写法** | 变量在目标格式中的占位语法，随上下文而变：网关 `<%KEY%>`、容器 `${KEY}`（compose 原生）。**不统一**，与 Caddy 环境变量 `{$KEY}` 区分（ADR-035 §4） |
| **环境变量** | compose 服务级 `environment:` 的键值（服务级、随容器定义），与全局「变量」是两回事；UI 与文档一律用「环境变量」指代 |
| **迁移冲突报告** | 两桶合并时，同名不同值或键名不合法的存量项不自动导入，列入报告由用户在设置页解决（ADR-035 §8） |
| **插值时机** | 网关变量在 Caddyfile 生成时实时替换；容器变量在保存 compose 时破坏性替换写盘。**当前不统一**，为已知债务（ADR-035 §7） |

## 插件拔插化新增术语（ADR-037 ~ ADR-039）

> 详见 `docs/adr/ADR-037.md`（分离）、`ADR-038.md`（配方变体）、`ADR-039.md`（运行时契约）与 `JiangBeta/GateBoxStore` 的 `docs/`。

| 术语 | 定义 |
|---|---|
| **拔插式（Hot-pluggable）** | 插件可独立安装/启用/停用/卸载/升级，且不要求重新编译主程序（ADR-037） |
| **分离判据（主闭环）** | 是否属于「应用 → 域名 → Caddy 反代 → 自动 HTTPS」产品主闭环；闭环内留内核，闭环外迁为插件（ADR-037 §1） |
| **可安装单元（Installable Unit）** | ADR-037 起 `plugin.Manager` 是唯一的安装面；`component` 注册表降级为核心组件目录，`extension` 只当贡献表（ADR-037 §2） |
| **核心插件扩展** | 给核心组件补充能力、可能替换其配方的插件（如 caddy-l4）。形态 = 中性特征声明 + 配方变体 + 可选声明式贡献，**无独立逻辑进程**（ADR-037 §3、ADR-038） |
| **独立进程插件** | 自带后端二进制与 UI 制品、经 sidecar 契约与 L0/L1 宿主接入的插件（如 mosdns）（ADR-037 §3、ADR-039） |
| **特征（Feature）** | 插件声明给核心组件增加的**中性标识**（Go 模块路径或等价 ID），如 `github.com/mholt/caddy-l4`；核心只认特征、不认插件身份（ADR-038 §1） |
| **配方变体（Recipe Variant）** | 一个核心组件的某次具体构建 = 组件版本 + 已启用特征并集 + 平台；由 Store 预构建或按需构建（ADR-038） |
| **变体键（Variant Key）** | `hash(component, version, os/arch, sorted(features))`，用于解析与缓存变体制品（ADR-038 §2） |
| **制品矩阵（Variant Matrix）** | `index.json` 中列出的已发布变体清单（`key/component/version/features/os/arch/url/sha256`）（ADR-038 §4） |
| **配方重算（Recipe Reconcile）** | 启用集合变化时重算特征并集 → 解析新变体 → 替换组件二进制 → 重启 → 健康检查 → 失败回滚（ADR-038 §3） |
| **互斥插件** | 提供整包组件二进制（而非可协商特征）的插件，与同组件其他变体插件不可同时启用，UI 须明示（ADR-038 §6） |
| **按需构建（On-demand Build）** | 变体矩阵未命中时，触发 Store CI（`xcaddy`）异步构建该组合，或导入 `custom` 制品（ADR-038 §4） |
| **生命周期三档** | `disable`（留制品配置）/ `uninstall`（删制品 + 注销贡献 + 配方重算 + 保留数据）/ `purge`（删数据，二次确认）（ADR-037 §4） |
| **扩展 API 版本（extensionApi）** | 内核维护的整数契约版本；新增可选字段=minor、破坏性变更=major（须写 ADR）；manifest 与索引据此校验兼容（ADR-037 §5） |
| **plugin token** | 安装时为插件生成的凭据，绑定 `permissions.api` scope；核心按 scope 校验，插件不共享管理员 session（ADR-039 §2） |
| **插件 UI 宿主** | 内核统一托管 `/plugins/<id>/*`（UI 制品）与反代 `/api/v1/plugins/<id>/*`（sidecar），使 iframe 与 API 同源（ADR-039 §3） |
| **postMessage 桥** | 插件 iframe 与内核的唯一宿主通道，只承载 `init/ready/resize/navigate/toast/setTitle`，**不传业务数据**（业务走 HTTP）（ADR-039 §3） |
| **L0 / L1 / L2** | 插件 UI 三档渲染：L0 元数据驱动（内核渲染）/ L1 iframe + postMessage（强隔离）/ L2 远程 ESM（同源、等价 XSS，后置）（ADR-036 §7、ADR-039 §3） |
| **dns-provider** | 让 DNS 凭证供应商可拔插的扩展点：插件声明字段 schema + acme hook 名 + env 映射；内核以 `GET /api/v1/credentials/providers` 聚合，前端按 schema 动态渲染（ADR-039 §5） |
| **GateBoxStore** | 独立公开仓库 `JiangBeta/GateBoxStore`：插件源码（`plugins/<id>/`）、schema（vendor）、构建脚本、CI 与静态索引 `index.json`；契约权威仍在主仓库（ADR-037 §6） |

## 应用网关统一框架（ADR-040）

> 详见 `docs/adr/ADR-040.md`。一份统一描述，支撑两条能力：读（可观测）、写（声明式调和）。

| 术语 | 定义 |
|---|---|
| **统一描述** | 让异种组件以同一方式被描述、成为整体一部分的模型层；由「组件描述符」承载，是系统的唯一描述真相 |
| **组件描述符（Component Descriptor）** | 用统一方式重新描述组件的一份契约 = 身份 + 功能清单 + 四契约（配置/信息/观测/生效）；核心与插件同构（ADR-040 §1.2） |
| **四契约** | 描述符的四份契约：**配置契约**（编辑）/ **信息契约**（图与流转）/ **观测契约**（可观测）/ **生效契约**（调和）（ADR-040 §1.2） |
| **信息契约（Info Contract）** | 组件声明 `consumes` / `produces` 的信息端口；是依赖图边与信息流的来源（ADR-040 §1.2） |
| **观测契约（Observability Contract）** | 组件声明如何被观测：`state` / `activity` / `logs` / `metrics`（ADR-040 §2） |
| **生效契约（Effect Contract）** | 组件声明「收到某类信息变更时做什么」（update-config / restart / hot-reload / reissue / deploy），含幂等与顺序约束；与 `actions`（用户主动操作）并列（ADR-040 §1.2 / §3.1） |
| **功能（Function）** | 网关对外提供的一项能力（代理 / TLS / 解析 / DDNS 上报 …）；与组件**多对多**，是功能地图的语义主节点（ADR-040 §1.3） |
| **事实（Fact）** | 可寻址、被持久化的期望态单元（`Service#id`、`Domain#id`…）；调和的输入（ADR-040 §1.4） |
| **持久事实** | 落库、可寻址、可被调和写入的事实（Service(manual) / Domain / Credential / Fragment / Variable / PortBinding / ComposeInstance）（ADR-040 §1.4） |
| **派生事实** | 有 ID、可作边端点、可展示，但**不落库**、由事实+运行态推导的事实（如 docker 从 label 派生的 Service）；**调和不能写它，只能改其源头**（ADR-040 §1.4） |
| **信息（Info）** | 在组件间流转的类型化数据（label / domain / cert / port / variable / ip），是边的载荷；分类按角色不按值（ADR-040 §1.5） |
| **功能地图（Function Map）** | 由 组件 + 功能 + 信息流 构成的全景图（ADR-040 §1.6） |
| **双层投影** | 功能地图的两种视图：**组件视角**（节点=组件，含功能端口）/ **功能视角**（节点=功能，边=信息流）；状态挂组件、聚合到功能（ADR-040 §1.6） |
| **意图（Intent）** | 对「某事实发生变化」的声明（what changed），不含如何传播；调和器的输入（ADR-040 §3.1） |
| **期望态（Desired State）** | 全部事实的集合，即「系统应处的状态」（ADR-040 §3.1） |
| **依赖图（Dependency Graph）** | 由信息契约端口 + 事实间引用**推导**出的消费关系图（算出来的，非手画）（ADR-040 §3.1） |
| **声明式调和（Reconcile）** | 由「意图 + 依赖图 + 生效契约」推导操作的 level-triggered 机制：求消费闭包 → 生成动作 → 拓扑排序 → 执行 → 记录（ADR-040 §3） |
| **调和器（Reconciler）** | 执行声明式调和的通用算法；幂等、可重放、周期兜底（ADR-040 §3.2） |
| **消费闭包** | 变更事实沿依赖图反向传播（谁消费它）得到的受影响组件集合（ADR-040 §3.2） |
| **运行记录（Run / Step / Event）** | 一次调和的执行轨迹三层：Run（整体）/ Step（组件动作，默认粒度）/ Event（步骤内细粒度事件）；Logs 独立关联（ADR-040 §3.3） |
| **传播轨迹** | 一次意图引发的 Run/Step 序列，即功能地图上「整体动态」象限的内容（ADR-040 §2 / §3.3） |
| **收敛（Convergence）** | 使现实逐步等于期望的机制：幂等重放 + 失败重试 + 漂移检测；不引入 DB 回滚，改以状态可见表达不一致（ADR-040 §3.4） |
| **L1 / L2 / L3** | 框架分期：**L1** 静态自描述（描述符+功能地图+静态信息流图）/ **L2** 运行态观测（状态/活动/日志聚合到图）/ **L3** 执行可追踪（Run/Step/Event+调和器）（ADR-040 §6） |

## 架构 V4 新增术语（ADR-041）

> 详见 `docs/adr/ADR-041.md`。V4 取代 V3 成为唯一现任总纲（`docs/architecture.md`）；主叙事 = 统一描述 / 可观测 / 声明式调和，V3 的成熟部分作为**承载**保留。

| 术语 | 定义 |
|---|---|
| **架构 V4** | 以 ADR-040 框架为组织原则的总纲版本；取代 V3（ADR-041） |
| **承载（Carrier）** | V4 中不重造、被吸收沿用的 V3 资产：组件生命周期六层、制品源三通道、插件分发、分层门禁、设计 token、运行时目录 |
| **事实注册表（Fact Registry）** | 声明「哪些 `model.*` 是事实」的中央清单（事实属于系统，不属于组件）；依赖图推导的输入（ADR-041 §4） |
| **类型骨架 + 实例绑定** | 依赖图 = 组件的**信息类型端口**（类型骨架）+ 事实间**引用**给出的具体实例边（实例绑定）（ADR-041 §4） |
| **`internal/graph`** | V4 新增包：事实注册表 + 引用解析 + 依赖图推导 |
| **`internal/reconcile`** | V4 新增包：意图 + 调和器 + Run/Step 记录 |
| **`internal/observe`** | V4 新增包：观测聚合（state/activity/logs/metrics）+ 事件总线 |
| **Activity（活动接口）** | V4 新增可选接口，观测契约的 `activity` 实现面（当前任务/步骤）；不并入万能大接口（ADR-041 §7） |
| **runs bucket** | V4 新增 BoltDB bucket：最近 N 条 Run/Step（JSON）；按条数+天数双限；Event 仅内存、Logs 不落库 |
| **SSE 事件流** | 观测事件（Run/Step 推进、state/activity 变化）的单向推送通道（`GET /api/v1/events`）；WebSocket 保留给双向交互 |
| **additive 契约字段** | v4 对 manifest v2 的可选扩展（`provides`/`consumes`/`produces`/`observability`/`effect`），属新增可选字段=minor（ADR-037 §5）；缺失则编译为最小描述符，向后兼容 |

## V4.1 统一模型（ADR-042）

> 详见 `docs/adr/ADR-042.md`。V4.1 重设计 V4 主视图。四层执行体系：**插件 / 组件 / 能力 / 链**；总纲：**`GateBox = 蓝图 × 对账 → 记录`**。

| 术语 | 定义 |
|---|---|
| **插件（Plugin）** | 一个可独立安装/升级、独立运行的**工具**；内部自治（黑盒），只透过输出/日志/状态露出。**系统插件**默认自带（caddy/acme.sh/docker）；**外部插件**后续安装（ddns-go/mosdns/flame/coraza）（ADR-042 §2） |
| **黑盒原则** | 插件的内部业务过程不建模，只通过**证据**（CLI 输出、日志、状态）露出，用于 debug 与呈现；GateBox 对插件只做**配置 / 触发 / 观测**（ADR-042 §2） |
| **执行者（Executor）** | 链上每一步的执行主体：**插件**或**核心**。插件组件操作工具；核心组件操作对象（ADR-042 §3） |
| **GateBox 核心（本体）** | GateBox 程序自身，控制中枢；做五件事：受理声明、加工（派生/编译/比对/校验）、编排、观测、呈现。**核心也有组件**（配置编译/对象派生/差异比对）（ADR-042 §3） |
| **组件（Component）** | **执行者内的一项功能**（一项任务的封装）：对外提供配置项与观测来源，内部用若干**能力**完成。例：docker·编排、ddns-go·更新解析、核心·配置编译（ADR-042 §4） |
| **组件自述（Manifest）** | 每个组件自带的声明：所属执行者、配置项、能力、消费与产出、前置。扩展接入＝提供一份自述（ADR-042 §4） |
| **能力（Capability）** | **GateBox 能施加的动作**，通用且有限，分三族：**控制**（写配置/校验/应用重载/启动/停止/删除/重启/触发任务）、**观测**（读状态/日志/指标/数据）、**加工**（编译/派生/比对/校验）。它是"怎么做"的最小可执行、可记录、可重试单位，**不是业务步骤**（ADR-042 §5） |
| **链（Chain）** | 由「组件 + 能力」的步骤**编排**成的一项任务。分**事务链**（BP 声明，配置时）与**流量链**（Caddy 服务态投影，访问时）（ADR-042 §6） |
| **链步骤** | 链的一行，五列：`对象 · 条件 · 组件 · 能力 · 说明`；条件不满足则**跳过**并记入 Run（ADR-042 §6） |
| **触发（Trigger）** | 链的一等属性，三类：**用户操作 / 定时器 / 事件**（ADR-042 §6） |
| **观测模型** | 让"管不着"也能 debug 的三层留痕：**编排层**（Run/Step）→ **动作层**（调用：输入/命令/输出/退出码/耗时）→ **证据层**（CLI 输出/日志/状态）（ADR-042 §7） |
| **链视图 / 链日志** | 同一模型的两种形态：**链视图**显示每步**最后一次状态**（编排叠加 + 观测叠加）；**链日志**是每条链独立的历次 Run 列表，可下钻（ADR-042 §7） |
| **对象（Object）** | **组件需要（消费）或产出的东西**；清单由组件自述**推导**、不手工维护，加插件自动扩展（"被需要"定义，取代"有 spec/status"的自证定义）。可寻址者才算（label/公网 IP 等运行信息不算）。分**源对象**（用户声明，进蓝图）与**派生对象**（组件产出，作现状）（ADR-042 §9） |
| **服务态** | Caddy 生效的配置与运行状态，属「现状」；因直接面向访问、必须**原子替换**，需要时单列 |
| **蓝图（Blueprint，BP）** | 整个网关「应该长什么样」的唯一真相；装组件（引用自述）、对象、链。持久、可 diff、可回滚（ADR-042 §10） |
| **对账（Reconcile）** | 让「现状」逼近「期望」的唯一动作；触发：被动（声明变化）+ 主动（定时/事件）；失败退避重试、不全局回滚（ADR-042 §8） |
| **记录（Run / Step / 调用 / 证据）** | 每次对账/访问留下的痕迹；Run 含 Step、Step 含调用与证据，并携带**上下文**用于步骤间传值（ADR-042 §11） |
| **承载建设（P0–P5）** | V3 的分期实现（地基/组件运行时/插件/前端/网络首页设置/部署），在 V4 中并入不再与 L1–L3 并列 |

## V4.1 对象命名与视图（ADR-042 §13 · `docs/v4.1/`）

> 对象命名对齐 Traefik；视图设计见 `docs/v4.1/views.md`。

| 术语 | 定义 |
|---|---|
| **入口点（EntryPoint）** | **协议 → 端口（一对多）** 的监听定义（`http: [80,8080]` / `https: [443,9443]` / `tcp` / `udp`）。对应 Traefik EntryPoints（ADR-042 §13） |
| **项目（Project）** | 多个服务组成的**组合**（一份 compose / 一份编排），可含多个服务。以 docker 项目为基础，泛化到其他项目 |
| **服务（Service）** | 编排中的**一项**（一个容器 / compose 的一个 service）；可有多个端口发布 → **包含多条路由** |
| **路由（Route）** | **一个转发，后端为 1 个端口**；含 域名 / 入口点+端口 / 中间件 / TLS / 类型 / 后端。六类型 `reverse_proxy / file_server / redirect / respond / tcp_proxy / udp_proxy`（后两者依赖 caddy-l4） |

> 层级：**项目 → 服务 → 路由**。网关页列的是**路由**。
| **中间件（Middleware）** | 请求/响应处理；结构化（encode/basic_auth/headers/websocket/rewrite）+ 自由文本逃生舱 `code`。对应 Traefik Middlewares |
| **主机（Host）** | `hosts` 表：名称 + 地址（手填）+ docker 接入点 + 角色（edge/worker）；后端地址由「容器端口 → 宿主端口 → 主机地址」推导 |
| **部署单元（Deployment）** | 内容分两部分：**① docker 内容** `compose`（模板生成 / 手填）+ **② GateBox 声明** `services`/`routers`/`middlewares`；链接 `backend: { deployment, port }`。compose 文件是产物，不写 label（ADR-042 §9.1） |
| **模板（Template）** | 预制部署单元：同时封装 docker 内容与 GateBox 声明的默认值 + 参数 `inputs`；来自 GateBoxStore（签名）或用户自建（`docs/v4.1/blueprint.md`） |
| **类型目录（Catalog）** | 所有组件自述的并集（service_types / middleware_types / entrypoint_protocols / abilities）；未安装的**置灰 + 一键安装**；模型里无写死的类型清单（ADR-042 §4.1） |
| **站（Station）** | 流量链上的一格（组件 + 能力），如 入口 / WAF / TLS / 路由匹配 / 上游（`docs/v4.1/traffic-chain.md`） |
| **RequestRun** | 一次访问的记录：由 Caddy 访问日志**还原**成站点序列（成败/耗时）（`docs/v4.1/records.md`） |
| **流量链** | 用户访问经过的站序列：**设计**从 Caddy 配置**投影**，**实况**从访问日志**还原**（`docs/v4.1/traffic-chain.md`） |
| **流程视图** | 各页（组件 / 路由）的第三视图，替代 OxiDNS 的静态拓扑：同一批组件、两种视角（发布任务 / 访问路由）（`docs/v4.1/views.md`） |