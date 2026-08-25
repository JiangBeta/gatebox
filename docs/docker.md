# GateBox · Docker 功能设计 & 开发计划

> 状态：设计已定（讨论 Q1~Q24 全部 settled），待开发
> 关联：`docs/PRD.md`、`docs/glossary.md`、`docs/adr/ADR-001`、`docs/layout.md`
> 本文档中标注「实测」的结论均来自本机真实执行（Docker 29.6.2 / API 1.55 / Compose 5.4.0 / cgroup v2），非推断。

## 1. 定位与范围

Docker 页是控制面一级导航之一，负责**容器运维**与**应用编排**：

- 管理对象：**容器、编排、镜像、网络、存储卷**
- 状态读取：运行时状态、日志、CPU / 内存
- 控制：启停、增删改查、exec
- 编排：Docker Compose 可视化配置（表单 ⇄ YAML 实时双向同步）
- 有限的 docker daemon 配置管理（镜像加速器等）

**边界（关键）**：

- **不生成 Caddyfile**。Docker 单位只负责「label ⇄ 表单的双向转换 + 落盘 + 从容器读出 label 并暴露」，代理配置的生成完全归「网关」单位（Q6）。
- **不接管外部组件生命周期**（ADR-001）。docker daemon、docker compose 均为独立进程/CLI，本工具只调用其 API/CLI 并改写其配置。
- **容器 / 镜像 / 网络 / 卷不建实体**——它们是 docker 拥有的运行时对象，GateBox 只读不存。**只有 ComposeInstance、Registry、镜像 tag 缓存落库**。

## 2. 领域模型

### 2.1 三层结构（Q1）

```
ComposeInstance (1) ──< Container (N) ──< App (M, M ≤ N)
```

- 一个 compose 项目起 N 个容器，其中只有需要被代理的容器升格为 **App**（术语表：可被代理的最小单元）。
- 这一层次是「一个 compose 起 3 个容器、其中 2 个各自代理到不同域名」的唯一自洽建模，也是应用商店（一个商店应用 = 一个 compose）与多主机（compose 是部署单位）的落点。
- **推论**：PRD 第 7 节的 `appData/<appName>/` 应正名为 `appData/<projectName>/`。

### 2.2 BoltDB 实体

| 实体 | 字段 | 说明 |
|---|---|---|
| **ComposeInstance**<br>bucket `compose_instances` | `projectName`(主键)、`displayName`、`hostID`、`managed`、`configFiles`、`editable`、`lastDeployedYAML`、`lastDeployedAt`、`createdAt` | 见下方标识规则 |
| **Registry**<br>bucket `registries` | `id`、`name`、`url`、`scheme`(http\|https)、`username`、`secret`(AES 加密)、`createdAt` | 私有仓库凭证，加密方式复用 DNSCredential（ADR-003） |
| **ImageTagCache**<br>bucket `image_tag_cache` | `repo`(主键)、`tags`、`platforms`、`fetchedAt` | tag/架构下拉的本地缓存，TTL 数小时 |

**标识规则（Q12）**：

- **主键 = `projectName`**，格式 `[a-z0-9][a-z0-9_-]{1,62}`（docker compose 硬约束），**不引入随机 ID**——它同时是 docker 项目标识、磁盘目录名、未来 Caddyfile 引用名，多一层映射只是负担。
- **`displayName`** 承载「家庭影院」这类任意字符的展示名，创建时由 projectName 生成默认值，可随意改。
- **`projectName` 创建后不可改，UI 不提供改名入口**。改项目名在 docker 眼里等于换了个全新项目，旧容器不会被 down、直接变孤儿——这个硬约束应诚实暴露，而非用假的「改名」按钮掩盖。

**多主机预留（Q21）**：仅 `ComposeInstance` 带 `hostID`（默认 `"local"`）；容器/镜像/网络/卷是运行时对象、不落库，**不加字段**。

### 2.3 容器三态（Q3 / Q14 / Q24）

| 态 | 判定 | 能力 |
|---|---|---|
| **托管** | GateBox 在 `appData/<project>/` 下创建 | 全功能：控制 / 日志 / exec / 编辑 YAML / 删除 |
| **外部编排** | `docker compose ls -a` 发现的非托管项目 | 控制 / 日志 / exec / **只读**查看 YAML；编辑需显式「接管」 |
| **游离** | 无 compose project label（`docker run` 起的） | 控制 / 日志 / exec；另有显式「转为编排」 |

**发现手段用 `docker compose ls -a`，不用扫容器 label**（Q24，实测）：`ls -a` 给出同样的 `CONFIG FILES` 绝对路径，且**能列出已停止的项目**；只扫 running 容器的 label 会让用户 stop 掉的项目从界面上凭空消失。

**记住见过的外部项目**（Q24）：首次发现即把 `{projectName, configFiles}` 存库。实测确认 `compose down` 后（容器被删）项目在 `ls -a` 里也彻底消失——落库后即使容器全删仍可列出（标记「未部署」）并一键 `up` 回来，避免「点了停止，项目人间蒸发」。

## 3. 页面设计（5 Tab）

Tab 顺序 **容器 / 编排 / 镜像 / 网络 / 存储卷**，**默认落在「容器」**（Q11）。编排虽是聚合根，但打开 Docker 页最高频的意图是「我的服务还活着吗」，首屏应服从使用频率而非模型地位。合并 Tab 会制造三级 Tab，违反 `layout.md` 的层级约定。

### 3.1 Tab 1 · 容器

**列表列**：名称 · 来源(托管/外部/游离 tag) · 端口 · CPU · 内存 · 创建时间 · 运行时间 · 操作

- **端口**：正常显示数量（「端口：6」），hover 弹层显示明细（`0.0.0.0:1234 -> 1234/tcp`），每条用 tag 区隔协议。
- **CPU**（Q8）：`(Δtotal_usage / Δsystem_cpu_usage) × online_cpus × 100`。实测 `online_cpus` 字段存在（值 8），直接用。
- **内存**（Q8）：**对齐 `docker stats` CLI 而非 ctop**——cgroup v2 用 `usage - stats.inactive_file`，cgroup v1 用 `usage - stats.cache`，按 cgroup 版本分支。
  > 实测：本机 cgroup v2，`memory_stats.stats` 下**无 `cache` 字段**，有 `file` / `active_file` / `inactive_file`。同一容器三种口径能差 1.7 倍（30.6MB / 29.8MB / 17.7MB）。选 `docker stats` 口径是因为**用户一定会开终端敲 `docker stats` 对照，对不上就是 bug 工单**。

**操作**：日志（弹层，可查找/下载）· 控制台（exec）· 启动/重启/停止/删除 · 目录（后置）

### 3.2 Tab 2 · 编排

**列表列**：应用名(displayName) · projectName · 来源 · 服务数 · 状态 · 创建时间 · 操作

**操作**：部署 / 停止 / 重启 / 编辑 / 删除 / 恢复上次成功配置

**创建应用**：Form ⇄ YAML 双向同步编辑器（见 §7.2）

### 3.3 Tab 3 · 镜像

**列表列**：ID · 名称(全称) · Digest · 架构 · 大小(MB) · 是否使用 · 创建时间 · 操作(查看/删除)

**二级操作**：

- **拉取镜像**：`input 镜像名` + `select 架构` + `select 版本` + 拉取 / 终止
- **仓库管理**：见 §3.3.1
- **导入镜像**：上传 tar，一次最多 10 个

**架构/版本下拉（Q10）**——**尽力而为，永远可手填**：

- 只走**标准 Registry V2 API**（`/v2/{name}/tags/list` + `auth.docker.io` 匿名 token + OCI image index 的 `Accept` 头），**不用 hub.docker.com 专有 API**——后者要 API key、无匿名分支，且镜像加速站只转发标准 V2 API。
- 结果按 `repo → {tags, platforms}` 缓存进 BoltDB，避免每敲一个字符就打上游、撞 Docker Hub 的 abuse 限流。
- 查询失败**静默降级**为自由输入（`n-select` 开 `filterable` + `tag`），架构默认填宿主机架构。「查不到」是一次降级，不是一堵墙。

#### 3.3.1 仓库管理与 daemon 配置（Q9）

你原始需求里的「仓库管理」表格（含 `协议 http/https` 字段）实际横跨两个性质完全不同的对象，UI 上分成两块：

| | 私有仓库（GateBox 实体） | daemon 配置 |
|---|---|---|
| 归属 | BoltDB `registries` | `/etc/docker/daemon.json` |
| 字段 | 名称 / URL / 协议 / 用户名 / 密码 | `registry-mirrors`、`insecure-registries`、`max-concurrent-downloads` |
| 生效 | 拉取时组装 base64url 的 `X-Registry-Auth` header | 写盘 + `SIGHUP` reload |
| 中断 | 无 | **无**（实测：这三项均在官方 SIGHUP 可热重载的 13 项白名单内，不重启进程、不影响运行中容器） |

- **凭证不写 `~/.docker/config.json`**——那会与用户 CLI 登录态打架，且凭证以 base64 明文落在用户主目录。走 Engine API 原生的 `X-Registry-Auth`，凭证加密存 BoltDB。
- **daemon 配置只开放上述白名单字段，其余一律只读展示**。这是这个功能的全部风险控制：不在 13 项内的配置（`storage-driver`、`log-driver`、`data-root` …）必须**重启 dockerd** 才生效，而 `live-restore` 默认关闭时，官方明确 daemon 终止会 **shuts down running containers**。
- **NixOS / OpenWrt 降级**：启动时探测 daemon.json 是否存在且可写、是否处于声明式托管。探测失败即**整块切只读**，从 `docker info` 读运行时真值展示，并提示「本系统由 Nix/UCI 托管，请在系统配置中修改」。
  > 本机实测 `/etc/docker/daemon.json` **不存在**（NixOS 声明式管理），而 PRD 第 10 节的目标发行版明确包含 nixos 与 openwrt——做成可写反而是个假功能。
- **附带**：`live-restore` 也在可热重载白名单内，值得在设置页给一个开关并推荐开启——开了之后将来重启 dockerd 才不会停掉用户所有容器。

### 3.4 Tab 4 · 网络

**列表列**：名称 · 驱动 · IPv4 · 创建时间 · 操作(查看/删除)

**创建网络**：`input 名称` + `select 驱动`(bridge/macvlan/ipvlan/overlay/host) + `开关 隔离外部访问` + `开关 允许手动附加容器`

### 3.5 Tab 5 · 存储卷（Q4）

**列表列**：名称 · 驱动 · 挂载点 · 是否使用中 · 创建时间 · 操作(查看/删除)

**二级操作**：一键清理未使用

> 卷不在你的原始需求里，但 compose 有 top-level `volumes:`、删容器会产生孤儿卷，而**磁盘被孤儿卷和悬空镜像吃满是 HomeLab 最常见的故障之一**。它和镜像是同一个问题的两半。

## 4. 核心流程

### 4.1 部署编排（Q16 / Q16' / Q22 / Q23）

```
① 校验（不落盘）  docker compose -f - --project-directory <dir> config -q  ← stdin 读入
② 本地增补校验    端口占用 / 挂载路径存在性 / 镜像本地是否存在
③ 未定义变量确认  阻断式警告弹窗
④ 写盘            校验全通过才写 docker-compose.yaml
⑤ 部署            docker compose --progress json -p <project> up -d   → WS 流式推送
⑥ 成功            写入 lastDeployedYAML
```

**校验前置、不落坏文件**：`config -q` 从 stdin 读，纯只读。磁盘上永远不会出现非法 YAML——不是靠回滚补救，而是让它压根不发生。
> 实测约束：必须传 `--project-directory`，否则 `.env` 加载与 `./data` 这类相对路径会以进程 cwd 为基准解析错。

**`config -q` 的能力边界（实测，必须知道）**：

| ✅ 能查出（exit 1） | ❌ 查不出（exit 0） |
|---|---|
| YAML 语法错误 | 镜像不存在 |
| schema 违规（未知字段、`ports` 写成数字） | 宿主机端口已被占用 |
| 未声明的 network / volume 引用 | 挂载的宿主机路径不存在 |
| `depends_on` 指向不存在的 service | **未定义变量 `${TYPO}` —— 只告警，静默变成空串** |

**故补三项本地校验（Q22）**：端口占用（读现有容器端口映射 + 宿主机监听）、宿主机挂载路径存在性、镜像本地是否存在。三项均为纯本地、毫秒级、零网络依赖，却能拦掉最常见的部署失败。**不做联网校验镜像/tag 是否存在**——要撞 Docker Hub 限流、离线内网直接失效，让 pull 阶段去报更诚实。

**未定义变量提升为阻断式警告**：不静默放行，弹「这些变量未定义，将被替换为空字符串，确认继续？」。变量名敲错会导致「部署成功但配置是空串」，是最阴的一类失败。

**失败处理**：

| 失败形态（实测） | 行为 | 处理 |
|---|---|---|
| **拉镜像失败** | compose 先拉全部镜像，任一失败即中止，**一个容器都不创建** | 干净失败，可安全重试 |
| **启动期失败**（端口冲突等） | 已起的服务**保持运行**，失败的停在 `Created`，**无任何自动回滚** | **什么都不做**（Q23） |

- **不自动回滚 YAML**：用户刚敲的配置不该被系统抹回旧版——他想的是「改一下再试」。
- **不自动回滚容器状态**：只有用户知道那些已跑起来的服务此刻值不值钱（可能是刚恢复的数据库正在初始化）。自动 `down` 会误伤；只删失败容器则让用户失去 `docker inspect` 看现场的机会。
- UI **逐服务**展示状态（而非整个项目一个红叉）+ 完整错误输出，提供「重试部署」和「停止整个项目」两个显式动作。
- **`lastDeployedYAML` 是纯数据不是机制**（Q16'）：up 成功后覆盖写入一个字符串字段，UI 给一个用户主动点击的「恢复上次成功配置」按钮。没有自动回滚、没有 `.bak` 满地跑。

**并发（Q16）**：同 `projectName` 加**应用层互斥锁**，第二个请求直接拒绝并提示「该应用正在部署中」，**不排队**（排队会让用户搞不清最终生效的是哪一版）。
> 实测：Compose 5.4.0 **没有跨进程项目级锁**，并发 `up` 直接竞态，败者拿到难读的 `container name "/xxx" is already in use`。

### 4.2 接管外部编排（Q14）

外部编排**默认只读**（up/down/restart/查看 YAML/日志/exec）。编辑需用户点「接管」并确认：**将重写该文件、注释与格式会丢失、建议先提交 git**。

**检测到以下情况连接管入口都不给**，只提示「该项目由多文件组成，请在源文件中编辑」：

- `configFiles` 为逗号分隔的多个文件（`-f base.yml -f override.yml`）
- compose 使用了 `include:` / `extends:`

> 「原地读写」的**读**是白拿的，**写**却要赌用户文件的复杂度——而赌注是别人 git 仓库里的文件。本机实测唯一的外部项目就指向 `/home/beta/Projects/dockerSets/Hosts/niaoyun.yaml`。

### 4.3 游离容器转编排（Q3）

`docker inspect` 反推 compose → **diff 预览** → 用户确认（明确告知会 `down` + `up` **重建容器**、存在数据风险）→ 落盘 `appData/<project>/` → 部署。

这是破坏性操作，与 4.2 的「原地接管」风险等级差一个数量级，UI 上必须区分开，不能用同一个「接管」按钮掩盖。

### 4.4 拉取镜像（Q20）

进度是**多层并行**的（每 layer 独立经历 `Waiting → Downloading → Verifying → Extracting → Complete`）：

- **总进度按字节加权**：`Σ layer_current / Σ layer_total`，Download 阶段占 80%、Extract 占 20%。
- `progressDetail.total` 缺失时**整体降级**为「完成层数 / 总层数」，而不是显示一个跳来跳去的假百分比。
- UI：总进度条 + 可展开的每层明细（像 docker CLI 那样）。
- **「终止」= 断开 HTTP 连接**（context cancel）——Docker API 没有取消拉取的端点。提示需如实说明「已下载的层会保留」。

### 4.5 删除语义（Q15）

| 对象 | 规则 |
|---|---|
| **容器** | 停止的才能删；「同时删除匿名卷」勾选，**默认不勾** |
| **编排（托管）** | 三项分列（破坏性差着数量级）：`停止并删除容器`（必选、不可取消）/ `删除数据目录 appData/<project>`（默认不勾，**红字**）/ `删除关联的命名卷`（默认不勾，**红字**） |
| **编排（外部）** | **只 down，永不删除用户的文件**，UI 上不出现「删除数据目录」选项 |
| **镜像** | 使用中禁止删除，并**列出占用它的容器**（而非只报一句「镜像正在使用」） |
| **网络** | 有容器连接时禁止删除，同样列出容器 |

## 5. 关键技术约束

### 5.1 Docker 客户端：自研轻量 HTTP 封装（Q7）

**不用 moby SDK**，用标准库 `net/http` + unix socket 直连 Engine API，路径锁定版本号（`/v1.44/...`，server 向后兼容至 1.40）。

理由**不是体积**（实测 strip 后仅差 1.36 MB），而是 **API 稳定性**：

- `github.com/docker/docker/client` **这条路径已经不能用了**——moby 已将 client/api 拆为独立模块 `github.com/moby/moby/client`，当前版本 **v0.5.1**（0.x，无稳定性承诺），且方法签名刚整体换过一轮（`ContainerList` 返回值变了、`ContainerExecAttach` 改名为 `ExecAttach`）。**PRD 里写的「moby SDK」这个选型，指向的包已不存在**。
- Engine API 的 **HTTP 契约**带版本号、有明确向后兼容承诺、十年没断过，比 Go SDK 稳定得多。
- SDK 会拖进 20 个模块，其中 **8 个是 OpenTelemetry 链**，而 GateBox 不做分布式追踪。
- 多主机将来要支持 tcp+TLS / ssh 传输，transport 层在自己手里更顺。

**代价**（需自行实现）：约 25~30 个端点，其中 4 个流式（logs / stats / pull / exec）；stdcopy 的 8 字节帧头解析；exec 的连接 hijack（标准库 `http.Hijacker` 原生支持）。

**Compose 仍走 CLI**——它没有 Engine API 等价物。

### 5.2 实时通道（Q5 / Q18）

| 数据 | 通道 |
|---|---|
| 容器列表 CPU/内存 | 后端**单例采集器**聚合成内存快照 → 前端轮询 |
| 日志 follow / exec / 镜像拉取 / compose 部署 | 各开一条 **WebSocket** |

**stats 必须在后端收敛**：若让每个浏览器直连，10 容器 × 3 标签页 = 30 条长连接打到 dockerd。单例采集器让 N 个客户端始终只对应 M 条 docker 流。

**采集器按需启停 + 引用计数，冷却期 30 秒**（Q18）：GateBox 是常驻服务（不同于开着才采的 ctop），没人看 Docker 页时不该 24 小时消耗 dockerd。采集用 `stats?stream=true` 长流，而非轮询 `stream=false`（后者每次请求 docker 内部都要采样等待）。

**历史数据只存内存环形缓冲**（每容器最近 60 个采样点），**不落盘**。BoltDB 是 B+ 树，扛不住时序数据的持续写入；持久化曲线需要降采样与保留策略，是另一个量级的工程。重启丢曲线对 HomeLab 可接受。

### 5.3 Compose CLI 调用约定（实测）

- **`--progress` 是全局选项**：`docker compose --progress json up -d`。写成 `up --progress json` 会报 `unknown flag`。
- **用 `--progress json`**：输出结构化 JSON Lines（`{"id":"Container x","status":"Working|Done","text":"Creating"}`，错误也是 JSON 行），远比 `plain` 适合流式推送前端。`tty` 含大量 ANSI 控制字符，不可直接转发。
- **进度和错误全部走 stderr，stdout 是空的**。只读 stdout 会得到一片空白——隐蔽但致命。

### 5.4 exec 控制台（Q19）

- **shell 自动探测** `bash → sh`，同时给一个**可编辑的命令输入框**（预填探测结果）。探测全失败时明确提示「该镜像不含 shell」，而不是丢一个空白终端（distroless/scratch 镜像连 sh 都没有）。
- **身份**默认容器默认用户，高级选项可切 `root`（很多镜像默认非 root，进去啥也干不了）。
- **安全提示为弹层顶部常驻一行小字**，不做每次弹窗——弹窗会被无脑点掉。
- 会话不持久化，30 分钟无输入自动断开。

### 5.5 与网关单位的接口（Q6 / Q13）

**代理寻址用 `127.0.0.1:宿主机映射端口`**（GateBox 自动分配高位端口并绑 `127.0.0.1`，不暴露公网）。容器 IP 作为高级选项。

> **为什么不用容器 IP 或服务名**：caddy 是 `$DATA_DIR/tools/caddy/caddy`——**宿主机二进制、systemd 服务，不是容器**。docker 的内嵌 DNS（`127.0.0.11`）只在容器内部可用，宿主机上的 caddy 解析不了 `jellyfin:8096`。而容器 IP **每次 `up` 重建都会变**，用它就得背上一整套 docker events 监听 + 自动重新生成 Caddyfile 的机制——**为省一个本机端口，换来「容器重启后网站 502」这个经典故障源**。绑 `127.0.0.1` 同样不暴露公网，稳定性在 HomeLab 远比端口洁癖值钱。

**App 的稳定标识 = `project + service`**（游离容器回退 `container_name`）。容器 ID 每次重建必变，`container_name` 很多人不设，而这一对 label 永远存在且永不变。

**暴露给网关单位的接口**：

```go
// 不含容器 ID 和容器 IP —— 从接口层面杜绝下游依赖易变值
type ProxyableContainer struct {
    Project       string            // com.docker.compose.project
    Service       string            // com.docker.compose.service
    ContainerName string
    HostPort      int
    Labels        map[string]string // caddy.* / gatebox.*
    State         string
}
```

## 6. 后端设计

### 6.1 模块（`backend/internal/`）

| 包 | 职责 |
|---|---|
| `docker/client` | 自研 Engine API 封装（unix socket / 将来 tcp+TLS / ssh，按 hostID 解析） |
| `docker/stats` | 单例 stats 采集器（引用计数 + 环形缓冲） |
| `docker/compose` | compose CLI 调用、校验、部署、项目发现、项目锁 |
| `docker/registry` | 标准 Registry V2 API 客户端（tag/架构查询 + 缓存） |
| `docker/daemon` | daemon.json 白名单读写 + 可写性探测 + reload |
| `models` | ComposeInstance、Registry、ImageTagCache |
| `store` | 上述 bucket 存取；Registry secret 复用 AES 加密 |
| `api` | HTTP handler + WebSocket handler |

### 6.2 API 概要（`/api/v1`，全部支持可选 query `?host=local`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/docker/containers` | 列表（含 stats 快照） |
| GET | `/docker/containers/{id}` | 详情 |
| POST | `/docker/containers/{id}/{start\|stop\|restart}` | 控制 |
| DELETE | `/docker/containers/{id}` | 删除（`?removeVolumes=`） |
| GET | `/docker/containers/{id}/shells` | 探测可用 shell |
| POST | `/docker/containers/{id}/convert` | 游离容器转编排 |
| WS | `/docker/containers/{id}/logs` | 日志 follow |
| WS | `/docker/containers/{id}/exec` | 控制台 |
| GET | `/docker/stats` | stats 快照（轮询端点） |
| GET | `/docker/compose` | 项目列表（`compose ls -a` + DB 合并） |
| GET | `/docker/compose/{project}` | 详情（读盘 YAML） |
| POST | `/docker/compose/validate` | 校验（stdin `config -q` + 本地增补校验） |
| POST | `/docker/compose/{project}/{up\|down\|restart}` | 控制 |
| DELETE | `/docker/compose/{project}` | 删除（`?removeData=&removeVolumes=`） |
| POST | `/docker/compose/{project}/adopt` | 接管外部项目 |
| POST | `/docker/compose/{project}/restore` | 恢复上次成功配置 |
| WS | `/docker/compose/{project}/deploy` | 部署 + 流式进度 |
| GET/DELETE | `/docker/images`、`/docker/images/{id}` | 列表 / 删除 |
| POST | `/docker/images/load` | 导入 tar（≤10 个） |
| WS | `/docker/images/pull` | 拉取 + 进度 |
| GET | `/docker/registries/{repo}/tags` | tag/架构查询（带缓存） |
| GET/POST | `/docker/registries` | 私有仓库列表 / 添加 |
| PUT/DELETE | `/docker/registries/{id}` | 编辑 / 删除 |
| GET/POST | `/docker/networks` | 列表 / 创建 |
| DELETE | `/docker/networks/{id}` | 删除 |
| GET | `/docker/volumes` | 列表 |
| DELETE | `/docker/volumes/{name}` | 删除 |
| POST | `/docker/volumes/prune` | 清理未使用 |
| GET/PUT | `/docker/daemon/config` | daemon 白名单字段（含 `writable` 标记） |
| GET | `/docker/proxyable` | **跨单位接口**：供网关单位消费 |

## 7. 前端设计

### 7.1 依赖增量

| 依赖 | 体积 | 加载方式 |
|---|---|---|
| CodeMirror 6（核心 + YAML 语言包） | ~300KB | **动态导入**，点击编辑区才加载 |
| `yaml`（npm，非 js-yaml） | ~16KB | 常规 |
| xterm.js | ~250KB | **懒加载**，打开控制台才拉取 |

合计约 **550KB 懒加载资源**，首屏不受影响。

### 7.2 编排编辑器：Form ⇄ YAML 双向同步

**并发控制：代数版本锁（Generation Lock）**

维护全局整型 `stateVersion`。任一同步任务开始时取 `const currentVersion = ++stateVersion`，执行转换计算后**立即判断** `if (currentVersion !== stateVersion) return;`——执行期间有新操作产生则丢弃旧任务。无需布尔锁与固定防抖等待，杜绝「表单 → YAML → 编辑器 → 解析 → 表单」死循环。

**触发时机**：

- 表单 → 编辑器：`@blur` 或显式「同步至 YAML」按钮。**禁止 `@input` 实时触发序列化**。
- 编辑器 → 表单：CodeMirror `onChange` + **400ms 防抖**。

**表单字段覆盖范围（Q17）**：

| 组 | 字段 |
|---|---|
| **基础** | `image`、`container_name`、`restart`、`ports`、`environment`、`volumes`、`networks` |
| **高级**（默认折叠） | `devices`、`network_mode`、`user`、`command`/`entrypoint`、`cap_add`、`logging`、`healthcheck`、`build`、`labels`、`depends_on`、`deploy.replicas` |

> 高级组里前四项是**补进来的 HomeLab 刚需**：`devices` 是 Jellyfin/Emby/Frigate **硬件转码**的前提（`/dev/dri:/dev/dri`）；`network_mode: host` 是 Home Assistant 设备发现与 DLNA/mDNS 的必需（且与 `ports`/`networks` **互斥，表单需联动禁用**）；`user` 对应 linuxserver.io 全家桶的 PUID/PGID 惯例；`logging` 用于限制日志大小——**容器日志撑爆磁盘是 HomeLab 经典故障**。

- **top-level `networks`** 做最小表单（声明 + `external` 开关）——Q13 的「加入 caddy 网络」需要它；**top-level `volumes`** 保持透传。
- **`_rawConfigs` 只读展示（JSON 预览），不可编辑**——它是兜底桶，做成可编辑等于在表单页再造一个 YAML 编辑器；改未覆盖字段的正道是切到 YAML 页签，那本来就是逃生舱。

**转换算法规范**：

1. **Caddy label 双向转换**：State → Labels 时首个路由用无后缀 `caddy` 键，后续用 `caddy_1`、`caddy_2`；非 Caddy 的原生 label 合并写入 `_rawLabels`。Labels → State 时过滤 `caddy` 前缀键、按索引重组为 `caddyRoutes`，`caddy`/`caddy_0` 的值按空格拆为 `domain` 与 `path`，无法识别的高级指令归入 `customDirectives`。
2. **`build` 智能简写**：解析时字符串自动转 `{context, dockerfile:'Dockerfile'}`；**仅当** `dockerfile === 'Dockerfile'` 且 `args` 为空时，导出降级为纯字符串。
3. **`healthcheck` 规范化**：`test` 归一为 `{testType, command}`；导出时重组回 `[testType, ...command.split(' ')]`。
4. **无损透传优先级**：导出时**先展开 `_rawConfigs` 作为基础对象，再覆盖写入 UI 识别的标准字段**——确保 UI 修改能覆盖旧值，而 `ulimits` / `sysctls` 等未修改的高级参数被完整保留。

**错误处理**：

- YAML 语法错误时**绝对禁止覆盖现有 formState**，仅在编辑器顶部展示红色错误条，表单维持上一次成功解析的状态。
- 序列化异常时捕获并回退，不更新编辑器内容。

**渲染优化**：`_rawConfigs` 展示区用 `v-if`（折叠时不渲染 DOM、不调 `JSON.stringify`）；`ports`/`environment` 超 20 项时「仅显示前 10 条，剩余折叠」；CodeMirror 关闭 `lineWrapping`、`autocompletion`、`hover`，仅开 `highlightActiveLine` + `matchBrackets`，`tabSize: 2`。

**性能基线**：首屏（不含 CodeMirror）JS 执行 < 300ms；编辑器激活并渲染 100 行 YAML < 500ms；解析 2000 行（约 50 服务）`parse + normalize` < 100ms 且不卡顿；编辑器激活后内存增量 < 20MB。

## 8. 开发计划（含验证方案）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | **Docker 客户端** | 自研 Engine API 封装：容器/镜像/网络/卷端点 + stdcopy 帧解析 + hijack | 单测：stdcopy 帧解析（含跨帧边界）、错误映射；集成：对真实 socket 拉取容器列表 | — |
| 2 | **stats 采集器** | 单例采集 + 引用计数 + 30s 冷却 + 环形缓冲 | 单测：CPU/内存公式（cgroup v1/v2 双分支，用实测 JSON 做 fixture）、引用计数启停 | — |
| 3 | **数据层** | ComposeInstance / Registry / ImageTagCache 模型 + BoltDB 存取 + secret 加密 | 单测：CRUD 往返、secret 加解密往返、密文不落明文、projectName 格式校验 | — |
| 4 | **容器 Tab** | 列表 API + 三态判定 + 前端列表（端口 hover / CPU / 内存 / 操作） | 集成：三态判定正确；组件测试：端口 hover 渲染、内存口径显示 | 对照 `docker stats` 核对数字 |
| 5 | **日志 & exec** | WS 日志 follow（查找/下载）+ xterm.js 控制台 + shell 探测 | 集成：日志流帧解析；单测：shell 探测降级链 | 进容器敲命令、下载日志 |
| 6 | **编排：发现与只读** | `compose ls -a` 发现 + DB 合并 + 列表 + YAML 只读查看 | 集成：已停止项目仍列出、down 后仍从 DB 列出（标记未部署） | 停掉外部项目，确认未消失 |
| 7 | **编排：校验与部署** | stdin `config -q` + 三项本地校验 + 变量告警 + `--progress json` 流式 + 项目锁 | 单测：本地校验（端口占用/路径缺失/镜像缺失）；集成：两种失败形态的行为、并发被拒 | 造端口冲突，看逐服务状态 |
| 8 | **编排：编辑器** | Form ⇄ YAML 双向同步 + 代数版本锁 + 四项转换算法 | 单测：转换算法**往返幂等**（含 `_rawConfigs`/`_rawLabels` 无损）、代数锁丢弃过期任务、语法错误不覆盖 formState | 编辑复杂 compose，核对无字段丢失 |
| 9 | **编排：接管与转换** | 外部接管（多文件/include 检测）+ 游离容器转编排（diff 预览） | 单测：多文件/include 检测；集成：inspect → compose 反推 | 接管外部项目、转换游离容器 |
| 10 | **镜像 Tab** | 列表 + 删除（占用检查）+ WS 拉取进度 + tar 导入 | 单测：进度聚合（含 total 缺失降级）；集成：使用中镜像拒删并列出容器 | 拉取大镜像看进度、终止 |
| 11 | **仓库 & daemon** | 私有仓库 CRUD + `X-Registry-Auth` + daemon 白名单 + 可写性探测 | 单测：`X-Registry-Auth` base64url 编码、白名单过滤、NixOS 探测降级 | 本机应显示为只读并给出提示 |
| 12 | **网络 & 卷 Tab** | 网络 CRUD + 卷列表/删除/清理未使用 | 集成：有容器连接的网络拒删并列出容器 | 建网络、清理未使用卷 |
| 13 | **跨单位接口** | `GET /docker/proxyable` + 端口自动分配（127.0.0.1 绑定） | 单测：稳定标识为 `project+service`、响应不含容器 ID/IP；端口分配不冲突 | — |
| 14 | **端到端** | 创建应用 → 部署 → 容器可见 → 代理就绪 | 全链路集成：创建 → 部署 → `proxyable` 返回预期条目 | 走完整闭环，重启容器确认代理不断 |

## 9. 验证方案汇总

- **AI 验证**：任务 1~13 的单测/集成/组件测试。其中三处必须重点覆盖：
  1. **CPU/内存公式**用实测 stats JSON 做 fixture，cgroup v1/v2 双分支；
  2. **转换算法往返幂等**——`YAML → State → YAML` 不得丢失任何字段（`_rawConfigs` 无损透传是核心）；
  3. **部署两种失败形态**的行为差异（拉镜像失败零创建 vs 启动期失败部分残留）。
- **人工验证**：对照 `docker stats` 核对数字；进容器 exec；造端口冲突看逐服务状态展示；编辑复杂 compose 核对字段无丢失；**重启容器后确认代理不断**（这是 Q13 选 `127.0.0.1:映射端口` 的核心收益点）。

## 10. 既有文档与代码的同步修改（2026-08-25 已完成）

| 位置 | 原 | 现 | 依据 |
|---|---|---|---|
| `docs/PRD.md` §5 | Docker 选型「moby SDK」 | 自研轻量 HTTP 封装 | Q7 / ADR-014 |
| `docs/PRD.md` §6 | 无三层模型 | `ComposeInstance ──< Container ──< App` | Q1 / ADR-015 |
| `docs/PRD.md` §7 | `appData/<appName>/`、`db/appgateway.db`、`cmd/appgateway` | `appData/<projectName>/`、`db/gatebox.db`、`cmd/gatebox` | Q1 / Q6 |
| `docs/PRD.md` §8 | 「stats 走 WebSocket 推送」 | stats 后端聚合 + 轮询；会话型走 WS | Q5 |
| `docs/PRD.md` §13 | 开发计划第 3 项写作「应用」 | 「网关」（与 layout.md §7 统一） | 遗留不一致 |
| `CLAUDE.md` | label 前缀 `appgateway.*`、moby SDK、旧路径 | `gatebox.*`、自研封装、新路径 | Q6 / Q7 |
| `backend/cmd/appgateway/` | 目录名 | `backend/cmd/gatebox/` | Q6 |
| `backend/internal/store/store.go` | `appgateway.db` | `gatebox.db`（既存 DB 文件一并重命名，数据未丢） | Q6 |
| `docs/glossary.md` | 无 Docker 单位术语 | 补 ComposeInstance 释义 + projectName / displayName / 三态容器 | Q3 / Q12 |
| `docs/adr/ADR-007` | `appgateway.*` | `gatebox.*` + 注明不装 caddy-docker-proxy 插件 | Q6 |
| `docs/adr/ADR-011` | `appgateway.db`、`appData/<appName>/` | `gatebox.db`、`appData/<projectName>/` | Q1 / Q6 |

**新增 ADR**：

- **ADR-014**：Docker 客户端自研轻量 HTTP 封装（取代 PRD 原 moby SDK 选型）
- **ADR-015**：Compose 三层领域模型与项目标识
- **ADR-016**：代理寻址与容器稳定标识

> 保留「AppGateway」作为品类名（README / CLAUDE.md / PRD 的定位句），它不是项目标识符，无需更名。
> 验证：`CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` 全通过。

## 11. 后置 backlog

- **容器目录管理**：应用所在目录的上传/下载/新增文件与文件夹（你已明确后置）。
- **多服务器管理**：远端 docker 主机。数据模型已预留 `hostID`，客户端层已按 hostID 解析。
- **应用商店**：casaos / 1panel / aapanel 等第三方商店的 compose 变体 → ComposeState 转换适配器。
- **手动登记外部 compose 文件路径**（Q24 的 (c)）：用于登记从未在本机启动过的 compose 文件。
- **编排版本历史**：最近 N 次成功部署的版本列表（当前只留 `lastDeployedYAML` 一份）。
- **stats 持久化曲线**：需降采样与保留策略，非 BoltDB 能承担。
- **UDP 代理、独立证书签发**（PRD 既有 backlog）。
