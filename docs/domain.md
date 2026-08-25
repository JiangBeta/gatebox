# GateBox · 域名功能设计 & 开发计划

> 状态：设计已定（ADR-012、ADR-013），待开发
> 关联：`docs/PRD.md`、`docs/adr/ADR-003 / 004 / 006 / 012 / 013`、`docs/glossary.md`

## 1. 定位与范围

域名页是控制面一级导航之一，负责：

- 管理**根域名（rootDomain）**与 **DNS 凭证**
- 只读展示**证书状态**与 **DDNS 上报状态**（由 caddy / ddnsgo 托管，本工具不建独立实体）

边界（关键）：

- **二级域名不建实体**，由「网关」页的 ProxyRoute.host 产生；本页只统计数量 / 罗列明细。
- **证书由 Caddy ACME 自动管理**（代理即签证），本页只读展示；包「证书管理器」抽象留升级口。
- **DDNS 上报的触发点在「网关」页**（创建路由时产生二级域名并上报）；本页只登记 rootDomain 与凭证、展示状态。

## 2. 领域模型（BoltDB）

| 实体 | 字段 | 说明 |
|---|---|---|
| **Domain** | `id`、`name`(rootDomain)、`credential_id` → DNSCredential、`created_at` | 管理单位 = rootDomain；apex 合法 |
| **DNSCredential** | `id`、`provider`(cloudflare \| dnspod \| aliyun)、`name`、`fields`(JSON, 加密)、`created_at` | 统一管理、加密存储、双向下发（ADR-003） |

- 二级域名：不建表，由 `ProxyRoute.host` 聚合推导。
- 证书 / DNS 记录：不建表，读 caddy / ddnsgo。

## 3. 页面设计（4 Tab）

### Tab 1 · 概览

**卡片（6 张）**：
1. 域名数量（rootDomain 数）
2. 证书 · 总数（当前有效证书数）
3. 证书 · 即将过期
4. 证书 · 已过期
5. DNS 供应商数（供应商去重）
6. DNS 凭证数（按 token 计数）

**列表列**：域名(rootDomain) · 证书(聚合状态) · 二级域名(数量) · 创建时间 · 最近签发时间

> 证书聚合状态：该 rootDomain 下所有证书的最坏状态——有已过期→红；有快过期→黄；全未申请→未申请；否则成功。

### Tab 2 · 域名管理

- 「+ 添加域名」→ 弹层：`input rootDomain` + `凭证 select`
- 列表：域名 · 证书状态 · 二级域名（**罗列明细**，可展开/弹层）· 操作（编辑 / 删除）

### Tab 3 · SSL 证书（只读）

- 列表（每二级域名一行）：名称(二级域名) · 有效期(状态 + 到期日) · 颁发机构 · 创建日期 · 查看详情
- 查看详情（只读弹层）：名称 · 颁发机构 · 序列号 · 私钥算法 · 到期日

### Tab 4 · DNS 凭证

- 添加凭证：选供应商（卡片式）→ 新建凭证（凭证名称 + 供应商 + 按供应商动态的 token 字段 + 验证按钮 + 保存/取消）
- 凭证列表：名称 · 供应商(logo+简写) · 创建时间 · 编辑 / 删除
- 供应商管理、Email 管理：**后置**（Email 属独立 acme 的「联系邮箱」，选 A 后无用途，随独立 acme 引入）

## 4. 核心流程

**添加域名**：填 rootDomain + 选凭证 → 存 Domain(credential_id)。不立即写 ddnsgo / caddy（尚无二级域名）。

**创建路由（网关页，跨页联动）**：产生二级域名 → 按其 rootDomain 取凭证 → 写 ddnsgo YAML（上报该二级域名 A 记录）+ 重启 ddnsgo → 生成 Caddyfile（tls dns 用该凭证）→ `/load` → Caddy 自动签证书。

**删除域名**：校验其下无路由引用（无二级域名）→ 删除 Domain。

**查看证书 / DDNS 状态**：经「证书管理器」读 caddy；经 ddnsgo 配置 / 日志读上报状态。

## 5. 后端设计

模块（`backend/internal/`）：

- `models`：Domain、DNSCredential
- `store`：BoltDB bucket `domains` / `dns_credentials`；凭证 AES 加密存 `fields`
- `cert`：**证书管理器接口** + Caddy 实现
  ```go
  type CertManager interface {
      List(ctx context.Context) ([]Cert, error)      // 当前有效证书及状态
      Get(ctx context.Context, fqdn string) (*Cert, error)
  }
  ```
- `ddns`：ddnsgo YAML 生成 / 重启（基础能力，供网关功能复用）
- `api`：HTTP handler

API 概要（`/api/v1`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/domains` | 列表 / 添加 |
| GET/PUT/DELETE | `/domains/:id` | 详情 / 编辑 / 删除 |
| GET | `/domains/overview` | 概览卡片 + 聚合列表 |
| GET/POST | `/credentials` | 列表 / 添加 |
| PUT/DELETE | `/credentials/:id` | 编辑 / 删除 |
| POST | `/credentials/verify` | 凭证可用性验证 |
| GET | `/certificates` | 只读证书列表 |
| GET | `/certificates/:fqdn` | 证书详情 |

## 6. 开发计划（含验证方案）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | 数据层 | Domain / DNSCredential 模型 + BoltDB 存取 + 凭证 AES 加密 | 单测：CRUD 往返、`fields` 加解密往返、凭证密文不落明文 | — |
| 2 | 证书管理器 | `CertManager` 接口 + Caddy 实现（读 Admin API） | 单测：mock Caddy 响应 → 解析证书状态/到期日/颁发机构 | 打开 SSL 证书 Tab 看状态 |
| 3 | ddnsgo 基础 | ddnsgo YAML 生成 + 重启 | 单测：给定凭证/二级域名 → 生成合法 YAML（结构校验） | — |
| 4 | 后端 API | 上述 REST 端点 + 概览聚合 | 集成测试：happy path + 边界（无凭证、删除被引用域名报错） | — |
| 5 | 前端骨架 | 域名页 4 Tab 路由 + 布局 | 构建通过、路由可达 | 切 Tab 正常 |
| 6 | 概览页 | 卡片 + 聚合列表 | 组件测试：聚合状态计算（最坏状态） | 造数据看卡片/列表 |
| 7 | 域名管理页 | 添加/编辑/删除 + 二级域名罗列 | 组件测试：表单校验（rootDomain 格式、必选凭证） | 添加域名 → 列表可见 |
| 8 | SSL 证书页 | 只读列表 + 详情弹层 | 组件测试：详情字段渲染 | 打开详情核对字段 |
| 9 | DNS 凭证页 | 供应商选择 → 新建 → 验证 → 保存 + 列表 | 组件测试：按 provider 动态 token 字段渲染 | 添加/验证/删除凭证 |
| 10 | 端到端联动 | 添加域名 + 凭证 → 网关页创建路由 → 证书/DDNS 状态回显 | `caddy validate`：生成的 Caddyfile 可通过 | 走完整链路看证书签发 |

> 任务 3、10 的 ddnsgo 完整上报联动属「网关」功能（开发计划第 3 项），此处只做基础能力与跨页接口。

## 7. 验证方案汇总

- **AI 验证**：任务 1–4、6–9 的单测/集成/组件测试；任务 10 的 `caddy validate`（生成 Caddyfile 先校验、失败回退，见 ADR-002）。
- **人工验证**：装好 caddy / ddnsgo 后打开实际页面，走「添加凭证 → 添加域名 → 网关页建路由 → 看证书签发与 DDNS 上报」完整闭环。

## 8. 后置 backlog

- 独立 ACME 客户端（acme.sh 或自研），届时补回：手动重新申请 / 私钥复用与下载 / 申请历史 / 多颁发机构 / Email 管理。
- 内网 DNS 记录自动下发（adguardHome / MosDNS / RouterOS / OpenWRT）。
- 多主机控制（单一控制面管理多台主机，数据模型预留 host 维度）。
