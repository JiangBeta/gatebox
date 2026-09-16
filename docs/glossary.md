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