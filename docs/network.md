# GateBox · 网络功能设计 & 开发计划

> 状态：**设计待讨论**（PRD v2 单位⑤；接入深度未定，本文给出候选方案与决策点）
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