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
| **Caddy 片段（Handler）** | 一段**遵循 caddy JSON 规范的配置片段**（语法上可直接用），变量位用 GateBox 语义（`<%GB_APP%>`/`<%GB_SERVICE%>`/`<%GB_STATIC_ROOT%>`/`<%GB_HOST_PORT%>`，定界符 `<% %>` 与 caddy env `${…}` 区分）；分 `handler`（site block 中间件）与 `route`（route 级）两类标签；决定流量怎么处理 |
| **片段自动关联** | 新建/编辑 Service 时自动把「默认启用」片段写入其 `handlerIds[]`；后端目标 https 时生成器自动注入「忽略证书校验」（进「其他选项」勾选态，PRD v2 单位③） |
| **其他选项** | Service 表单底部的开关组：`禁用当前规则`（= 整条规则失效）+ 3 个安全开关（阻止常见漏洞/静态资源缓存/支持 websocket）+ 所有非「默认隐藏」的 Caddy 片段 toggle；每个 Service 落库自己的片段引用（route 级覆盖） |
| **默认启用** | Caddy 片段的一个开关（默认关）：开启后，在每个 Service 的「其他选项」中该片段**默认勾选** |
| **默认隐藏** | Caddy 片段的一个开关（默认关）：开启后，在「其他选项」中**不显示**该片段 |
| **默认自带条目** | **只读硬编码**的 Caddy 片段清单（Gzip/Br、Basic Auth、忽略自带证书校验、健康检查、日志配置、阻止常见漏洞、静态资源缓存、支持 websocket），非可编辑实例；用户另可自建片段 |
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

> 详见 `docs/architecture.md` 与 ADR-027 ~ ADR-032。

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
| **插件（Plugin）** | 数据驱动的可选扩展=manifest + 通用引擎，`kind ∈ {caddy-module, process, config-only}`（ADR-029） |
| **manifest** | 插件声明文件（`apiVersion/kind/id/version/requires/source/artifact/runtime/config/operations/contributions`） |
| **静态索引** | 可静态托管的插件/制品索引 `index.json`（+ 签名），客户端据此在线安装/升级；**无常驻服务端**（ADR-029） |
| **插件状态机** | `available → installed → enabled/disabled`，失败转 `error`；`disable` 保留制品、`remove` 删除（ADR-029） |
| **设计 token** | 前端颜色/字号/间距/圆角的唯一来源（`frontend/src/design/`）；`.vue` 禁止样式字面量（ADR-032） |
| **分层标准** | 后端 `handler/service/repository/component/source/model` + 前端 `design/lib/app/modules/shared` 的判层与依赖方向，由 lint 强制（ADR-031/032） |
| **old/** | 重构前原始代码的完整保留区（`old/backend` 独立 `go.mod`），供回溯查证（ADR-031） |