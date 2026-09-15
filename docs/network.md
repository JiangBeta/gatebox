# GateBox · 网络功能设计 & 开发计划

> 状态：**内网 DNS（mosdns）已实现**；Tailscale 仍待讨论（接入深度未定，本文给出候选方案与决策点）
> 关联：`docs/PRD.md`（§3 能力 5/6、§9.4）、`docs/glossary.md`
> 前置：单位①（导航 `/network` 占位已在 infra.md §7 定义）

## 1. 定位与范围

网络页是控制面一级导航之一，对应 PRD v2 两个能力：

- **内网 DNS（mosdns）**：内网域名记录自动指向——已发布的网关应用，在内网用域名访问时**不绕公网**，由内网 DNS 直接解析到家用网段地址。
- **组网（Tailscale）**：跨网访问——异地设备经 Tailscale 访问 HomeLab 服务时**不暴露公网端口**。

**边界**：

- 两个能力均由**外部进程**承担（mosdns 是 DNS server / 分流代理；tailscaled 是 VPN 客户端），GateBox 只读写其配置 + 调 CLI/API（ADR-001 原则）。
- 内网 DNS 与「域名」模块联动：网关发布应用 → 域名页产生的二级域名 → 内网解析记录自动下发（联动点在域名/网关单位）。
- 本页**不涉及**公网 DDNS（外网访问走 ddns-go，见 `docs/domain.md`）。

## 2. 接入深度：两个候选（决策点 D1）

| 维度 | 方案 A · 引导 + 状态（轻） | 方案 B · 完全接管（重） |
|---|---|---|
| mosdns | 安装引导、配置模板（hosts 段留空）、服务状态展示 | 改写 mosdns 配置（内网解析段）+ 重启生效 |
| Tailscale | 安装引导、节点/连接状态展示、`tailscale status` | 状态 + 操作（up / down / 退出 / subnet router 引导） |
| 联动 | 不自动下发记录，UI 提示手工加 | 域名行 → 内网 A 记录自动写入 mosdns hosts，随发布/删除联动 |
| 边界 | 只读 + 引导，无写回风险 | 需处理配置所有权（与用户手改共存） |

> **D1 待讨论**。PRD v2 现状：能力描述为「内网访问通过 DNS 自动指向（mosdns）」「跨网访问时使用 VPN（Tailscale）」。推荐起步选 **方案 A**（先落地页面与状态），记录自动下发（方案 B 联动）在本期评估后决定。

## 3. 页面设计（2 Tab）

### Tab 1 · 内网 DNS（mosdns）

- **状态卡**：mosdns 进程状态（运行/停止/未安装）、监听端口（默认 53/udp）、配置路径、上次修改时间。
- **内网解析记录**（方案 B 才可写）：已发布二级域名 → 内网 IP 列表（A）；「+ 新增 / 删除 / 复制模板行」。
- 未安装 / 不可达：降级提示 +「前往部署」引导（跳部署交付单位⑦或给出 CLI 步骤）。

### Tab 2 · 组网（Tailscale）

- **状态卡**：`tailscale status` 聚合（本机节点名 / IP（100.x）/ Tailnet / 在线节点数 / 版本 / 登录用户）。
- **子网 / 出口**：subnet router / exit node 状态只读展示（方案 A）；操作按钮（方案 B）。
- 未登录：显示「需要 `tailscale up`」引导，代码不代持登录（凭据交互）。

## 4. 后端设计（草案）

模块（`backend/internal/`）：

- `internal/mosdns`：配置读写（YAML，hosts 段）+ 状态探测（进程 / 端口 `53`）+ 重启（服务管理器 CLI：systemctl / openrc / procd，见展开）。
- `internal/network`：Tailscale CLI 封装（`tailscale status --json`、`up` / `down`、`list` 节点）。

API 概要（`/api/v1`，草案）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/network/mosdns/status` | mosdns 状态 + 配置路径 |
| GET/POST/DELETE | `/network/mosdns/records` | 内网解析记录（方案 B） |
| POST | `/network/mosdns/reload` | 重写配置并重启（方案 B） |
| GET | `/network/tailscale/status` | Tailscale 状态聚合 |
| POST | `/network/tailscale/action` | up / down / 退出（方案 B） |

## 5. 联动语义（若选方案 B）

- **触发点**：网关每次成功 `/load` 后，把「域名行（host）」集合同步到 mosdns hosts 段：`host -> <网关内网 IP>`。
- **内网 IP 来源**：`127.0.0.1:HostPort` 的代理寻址（ADR-016）面向宿主机 caddy；内网解析目标 = **绑定的宿主机网卡 IP**（`GATEBOX_LAN_ADDR` 显式配置？或运行时探测默认路由网卡 IP）。
- **与 ddns-go 的关系**：外网（ddns-go，公网 IP）与内网（mosdns，LAN IP）**双轨解析**同一域名——公网用户解析到公网 IP 走 DDNS，内网用户解析到 LAN IP 直连 caddy 的 443。DNS 客户端的「域名分流」由用户侧决定（见决策点 D3）。
- **删除**：网关删除 service → 记录同步删除。

> 决策点 D2：内网 IP 来源（显式配置 / 探测默认路由网卡）；D3：mosdns 是否负责「内网/公网分流」还是仅 hosts 记录（HomeLab 常见做法：mosdns 同时做 DNS 分流，本页只写 hosts 段，不接管分流策略）。

## 6. 待讨论决策点清单

- **D1** 接入深度（方案 A / B / A 起步后演进 B）。
- **D2** 内网解析目标 IP 的确定方式。
- **D3** mosdns 的角色边界（仅 hosts 内网记录 vs 同时接管内外网 DNS 分流）。
- **D4** Tailscale 操作范围（状态展示 / up-down / subnet router / exit node / MagicDNS 联动）。
- **D5** 与单位⑦部署交付的关系（mosdns / tailscale 二进制由 install.sh 装入 `tools/`，服务单元管理由谁负责）。

## 7. 单位⑤ 开发计划（占位，待讨论后细分）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | mosdns 状态端点 | 状态探测 + 配置读写 | 单测：配置解析/生成 | 看状态卡 |
| 2 | Tailscale 状态端点 | `status --json` 聚合 | 单测：JSON 解析 fixture | 看节点列表 |
| 3 | 两个 Tab 前端 | 状态卡 + 空态/降级 | 组件测试 | 装/卸组件看降级 |
| 4 | 记录自动下发（D1=B） | hosts 段联动 + reload | 集成：发布 → 记录 → reload | 内网域名直达 |

## 8. 实现记录：内网 DNS（mosdns）管理页

**入口**：侧边栏「服务 → mosdns」（`/services/mosdns`）。参照 [sbwml/luci-app-mosdns](https://github.com/sbwml/luci-app-mosdns) 的功能形态，落到 GateBox「控制面只改写配置 + 调 API」的架构（ADR-001 / ADR-028）。

**运行目录**：`$DATA_DIR/tools/mosdns/`（`config.yaml` · `settings.json` · `hosts.txt` · `mosdns.log` · `cache.dump` · `rule/*.txt` · `geosite_*.txt` / `geoip_cn.txt`），与组件运行时的 `pid` 托管目录一致。进程启停复用 `POST /api/v1/components/{id}/{start,stop,restart}`。

**页面（7 Tab）**：

| Tab | 能力 |
|---|---|
| 状态 | 进程状态（5s 轮询）、版本 / 可升级提示、监听与 API 地址、各文件路径；启停 / 重启 / 刷新缓存 |
| 基础设置 | 内嵌「基础 / 高级 / Cloudflare」三栏：监听与日志、本地/远程/流媒体上游、Bootstrap、并发与连接复用、ECS、防泄漏、缓存（容量 / Lazy TTL / 落盘）、TTL、RR65、Apple 优化、广告拦截与规则来源；保存即重新生成 `config.yaml` |
| 内网解析 | hosts 记录（域名 → 一或多个 IP，IPv4/IPv6）增删改，落盘 `hosts.txt` |
| 规则 | 9 类规则文件编辑（白/黑/灰名单、DDNS、重定向、PTR、流媒体、广告拦截、Cloudflare IP 段） |
| 数据库 | GeoIP/GeoSite 数据更新（国内/国外/Apple 域名、国内 IP），可配 GitHub 代理 |
| 配置文件 | CodeMirror 手工编辑 `config.yaml`，YAML + 插件 tag/type 校验；可一键生成默认配置 |
| 日志 | WebSocket 实时跟随、等级/关键字过滤、自动滚动、下载、清空（UX 对齐「网关 → 代理 → 日志」） |

**配置生成**（对照 luci 的 `init.d` 移植）：hosts → cache → redirect → Apple/DDNS/白名单/拦截/灰名单/流媒体/国内/国外分流 → fallback(本地主用 / 远程备用)，可选 ECS、Cloudflare 改写、TTL 修正。引用的 `geosite_*.txt` / `geoip_cn.txt` / `rule/*.txt` 在生成配置时若缺失会自动建为空文件，保证 mosdns 一定能启动。

**设置持久化**：表单写入 `settings.json`（与 luci 的 UCI 等价），`config.yaml` 由其生成；`settings.json` 缺失时从 `config.yaml` 尽力还原。「配置文件」页手工编辑不影响 `settings.json`。

**数据来源**：数据库更新下载社区维护的纯文本列表（`Loyalsoldier/v2ray-rules-dat` 的 direct/proxy/apple 列表、`Loyalsoldier/geoip` 的 `text/cn.txt`），mosdns 可直接读取。

**后端**（`internal/adapter/mosdns` + `internal/handler/mosdns.go`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/mosdns/status` | 进程态 + 解析出的路径 / 端口 |
| GET/PUT | `/api/v1/mosdns/settings` | 读取 / 保存设置并生成配置 |
| GET/PUT | `/api/v1/mosdns/config` | 读取 / 写入原始 `config.yaml` |
| GET/PUT | `/api/v1/mosdns/hosts` | 读取 / 保存内网解析记录 |
| GET | `/api/v1/mosdns/rules` | 规则列表清单 |
| GET/PUT | `/api/v1/mosdns/rules/{name}` | 读取 / 保存单个规则文件 |
| GET | `/api/v1/mosdns/geodata` | 数据库文件状态 |
| POST | `/api/v1/mosdns/geodata/update` | 下载全部数据库文件 |
| POST | `/api/v1/mosdns/adblock/update` | 下载广告规则来源 |
| GET | `/api/v1/mosdns/logs` | 读取日志尾部 |
| GET | `/api/v1/mosdns/logs/stream` | WebSocket 实时日志 |
| DELETE | `/api/v1/mosdns/logs` | 清空日志 |
| POST | `/api/v1/mosdns/flush` | 经 `api.http` 调 `cache` 插件 `/flush` 清空缓存 |

> **官方制品限制**：GateBox 使用官方 `IrineSistiana/mosdns`，未包含 sbwml 的补丁插件，故：广告/拦截规则用官方 `domain_set`（须为 mosdns 域名规则格式，非 AdGuard `||domain^`）；**缓存预取（prefetch）**、`adblock_set`、`stats_api` 查询统计不生成（这些依赖上游 patch）。需要时请在「配置文件」页手工改用官方 `domain_set` / `ip_set` 等价配置。

