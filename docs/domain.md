# GateBox · 域名功能设计 & 开发计划

> 状态：v1 核心已落地（数据层 / 凭证 / 域名管理 / 概览聚合 / acme.sh 证书签发（ADR-013 修订））；PRD v2 单位④做**证书接入重构 + DDNS Tab + 凭证统一**，见 §9
> 关联：`docs/PRD.md`、`docs/glossary.md`、`docs/adr/ADR-003 / 004 / 006 / 012 / 013(修订) / 016`、`docs/gateway.md`（跨页联动）

## 1. 定位与范围

域名页是控制面一级导航之一，负责：

- 管理**根域名（rootDomain）**与 **DNS 凭证**（证书签发与 DDNS 上报**共用同一凭证**）
- 展示**证书状态**（acme.sh 签发产物）与 **DDNS 上报状态**（ddns-go）

**边界（关键，v2）**：

- **二级域名不建实体**，由「网关」页服务（Service）的**域名行**产生；本页只统计数量 / 罗列明细。
- **证书由独立 acme.sh 签发**（DNS-01，落盘 `tools/acme/certs/<fqdn>/`），Caddy **仅按文件加载证书**（ADR-013 修订，替代原「Caddy ACME 自动」）；本页读 acme 证书目录展示，不建独立实体。
- **DDNS 上报的触发点在「网关」页**（创建路由时产生二级域名并上报）；本页登记 rootDomain、管理公共凭证、展示 DDNS 状态。
- **DNS 凭证 = 统一凭证**：同一份 DNS 供应商凭证同时供 acme.sh DNS-01 与 ddns-go 上报使用，模型唯一（`DNSCredential`），双向下发（ADR-003）。

## 2. 领域模型（BoltDB）

| 实体 | 字段 | 说明 |
|---|---|---|
| **Domain** | `id`、`name`(rootDomain)、`credential_id` → DNSCredential、`created_at` | 管理单位 = rootDomain；apex 合法 |
| **DNSCredential** | `id`、`provider`(cloudflare \| dnspod \| aliyun)、`name`、`fields`(JSON, 加密)、`created_at` | **统一凭证**：acme.sh provider + ddns-go provider 共用 |

- 二级域名：不建表，由 Service 的 `domains[].{subdomain, rootDomain}` 聚合推导。
- 证书 / DDNS 记录：不建表，读 acme.sh 证书目录 / ddns-go 配置。

## 3. 页面设计（4 Tab，v2）

### Tab 1 · 概览

**卡片（6 张）**：① 域名数量 ② 证书·总数 ③ 证书·即将过期 ④ 证书·已过期 ⑤ DNS 凭证数 ⑥ DDNS 记录数。

**列表列**：域名(rootDomain) · 证书(聚合状态) · 二级域名(数量) · DDNS(状态) · 最近签发时间。

> 证书聚合状态：该 rootDomain 下所有证书的最坏状态——有已过期→红；有快过期→黄；全未申请→未申请；否则成功（ADR-013 §决策 4）。

### Tab 2 · 域名管理

- 「+ 添加域名」→ 弹层：`input rootDomain` + `凭证 select`
- 列表：域名 · 证书状态 · 二级域名（罗列明细，可展开/弹层）· 操作（编辑 / 删除）

### Tab 3 · 证书（只读，acme.sh）

- 列表（每二级域名一行）：名称(二级域名) · 有效期(状态 + 到期日) · 私钥算法 · 创建日期 · 查看详情
- 详情（只读弹层）：名称 · 证书文件路径 · 私钥算法 · 到期日 · 签发时间
- 数据来源：扫描 `tools/acme/certs/<fqdn>/` 解析 fullchain.pem / key.pem（`AcmeCertManager`）。

### Tab 4 · DDNS（v2 新增）

- **DDNS 状态**：ddns-go 上报记录列表（二级域名 → 目标 IP → 供应商 → 状态）；ddns-go 不可达时展示降级提示。
- **DNS 凭证管理**：添加凭证（供应商卡片 → 凭证名 + 供应商 + 动态 token 字段 + 验证 + 保存）；列表（名称 · 供应商 · 创建时间 · 编辑 / 删除）。
- 说明文案：凭证同时用于证书签发（acme.sh）与 DNS 上报（ddns-go）。

> 决策点：凭证独立 Tab vs 并入 DDNS。本设计暂并入 DDNS Tab；凭证功能膨胀后再拆独立 Tab。

## 4. 核心流程

**添加域名**：填 rootDomain + 选凭证 → 存 Domain(credential_id)。不立即写 ddns-go / acme（尚无二级域名）。

**创建路由（网关页，跨页联动）**：产生二级域名 → 按其 rootDomain 取凭证 → 写 ddns-go YAML（上报该二级域名 A 记录）+ 重启 ddns-go → 生成 Caddyfile（tls 引用 acme 证书文件）→ 触发 acme.sh 签发（`Issuer.Ensure`，DNS-01）→ `/load`。

**证书签发**（`internal/acme/Issuer.Ensure`）：证书缺失或剩余有效期 ≤30 天 → `acme.sh --issue --dns <provider> -d <fqdn>`（Let's Encrypt，ec-256）→ `--install-cert` 落盘 `certs/<fqdn>/{fullchain.pem,key.pem}`；失败进入 10min 冷却，不阻塞 reloadCaddy（ADR-013 §3）。

**续期**：acme.sh 自身 `--cron`（安装脚本配 systemd timer）负责；GateBox 只在访问/重载时对过期或缺失证书触发重签。

**删除域名**：校验其下无路由引用（无二级域名）→ 删除 Domain。

## 5. 后端设计

模块（`backend/internal/`）：

- `models`：Domain、DNSCredential
- `store`：BoltDB bucket `domains` / `dns_credentials`；凭证 AES 加密存 `fields`
- `cert`：**证书管理器接口** + `AcmeCertManager` 实现（读 acme.sh 证书目录，ADR-013 §决策 5）
  ```go
  type CertManager interface {
      List(ctx context.Context) ([]Cert, error)
      Get(ctx context.Context, fqdn string) (*Cert, error)
  }
  ```
- `acme`：`Issuer.Ensure`（签发 + 安装 + 冷却，已实现）
- `ddns`：ddns-go YAML 生成 / 重启（`ddns.NewManager`，`tools/ddnsgo/.ddns_go_config.yaml`）+ **DDNS 状态读取**
- `api`：HTTP handler

## 6. API 概要（`/api/v1`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/domains` | 列表 / 添加 |
| GET/PUT/DELETE | `/domains/:id` | 详情 / 编辑 / 删除 |
| GET | `/domains/overview` | 概览卡片 + 聚合列表 |
| GET/POST | `/credentials` | 凭证列表 / 添加（acme + ddns 共用） |
| PUT/DELETE | `/credentials/:id` | 编辑 / 删除 |
| POST | `/credentials/verify` | 凭证可用性验证 |
| GET | `/certificates` | 只读证书列表（读 acme 目录） |
| GET | `/certificates/:fqdn` | 证书详情 |
| GET | `/ddns/records` | DDNS 上报记录列表（v2 新增） |

## 7. 数据与配置落点

| 项 | 位置 |
|---|---|
| acme.sh 主程序 | `tools/acme/acme.sh`（或 PATH，`GATEBOX_ACME_BIN`） |
| 证书产物 | `tools/acme/certs/<fqdn>/{fullchain.pem, key.pem}` |
| ddns-go 配置 | `tools/ddnsgo/.ddns_go_config.yaml` |
| Caddyfile tls 引用 | `tls <DATA_DIR>/tools/acme/certs/<fqdn>/fullchain.pem key.pem`（`internal/caddy/tls.go`） |

## 8. 后置 backlog

- acme 管理 UI：手动重新申请 / 私钥复用与下载 / 申请历史 / 多 CA（ZeroSSL 需 EAB）/ Email 管理。
- 内网 DNS 记录自动下发（mosdns，单位⑤）。
- 供应商元数据管理、凭证配额与错误详情展示。
- 多主机控制（数据模型预留 host 维度）。

## 9. PRD v2 变更（单位④）：证书接入重构 + DDNS + 凭证统一

> 对应 PRD §14 单位④。前置：单位①③。

1. **证书实现改 acme.sh**：代码已先行（`internal/acme`、`internal/caddy/tls.go`、ADR-013 修订）；文档同步；`cert` 包实现切为 `AcmeCertManager`。
2. **新增 DDNS Tab**：`GET /api/v1/ddns/records` + 前端 Tab 4。
3. **凭证统一**：`DNSCredential` 唯一；ddns 使用侧（`ddns.NewManager`）接入凭证生成供应商配置段；UI 注明「acme + ddns 共用」。
4. **概览卡片**补 DDNS 计数与列表列。

### 单位④ 开发计划（含验证方案）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | cert 管理器切 acme.sh | `AcmeCertManager` 读证书目录 + 状态聚合 | 单测：目录解析 fixture（有效/过期/缺失） | 看证书 Tab 状态 |
| 2 | DDNS 状态端点 | `GET /ddns/records` | 单测：mock ddns-go → 记录渲染 | 建记录看列表 |
| 3 | DDNS Tab 前端 | 状态列表 + 凭证管理 + 空态/降级 | 组件测试：状态色、降级提示 | 停 ddns-go 看提示 |
| 4 | 凭证统一（ddns 使用侧） | ddns manager 接入凭证生成配置段 | 单测：凭证 → ddns-go YAML 结构 | 凭证改后 DDNS 生效 |
| 5 | 概览补 DDNS 计数 | 卡片 + 列表列 | 组件测试：聚合 | 造数据核对 |
| 6 | 端到端 | 凭证→域名→路由→证书→DDNS 回显 | `caddy validate` 通过 | 完整闭环 |