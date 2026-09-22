# V4.1 流量链：投影与实况还原

> 上层：[ADR-042](../adr/ADR-042.md) §6（链）· [views.md](views.md) §2.1（主视图）
> 状态：草案

主视图的数据来源。一句话：**流量链的"设计"从 Caddy 配置投影，"实况"从访问日志还原，二者叠在同一张图上。**

---

## 1. 流量链是什么

**一次用户访问经过的"站"（Station）序列。** 站 = 请求路径上的一格，对应「组件 + 能力」。

```
用户 → 入口 → 还原真实IP → WAF → TLS握手 → 路由匹配 → 转发 → 上游 → 响应
```

与事务链的区别：流量链由 **Caddy 定义**（GateBox 不拥有其运行），GateBox 只**读**（配置 + 日志）。

---

## 2. 两个来源

| 来源 | 得到什么 | 数据从哪 |
|---|---|---|
| **投影**（设计） | 这条路由"打算"怎么走 | Caddy 生效配置（GateBox 生成，天然已知） |
| **还原**（实况） | 这次访问"实际"怎么走、每站结果 | Caddy 访问日志（+ 插件日志） |

主视图 = 投影骨架（站点固定）+ 实况叠加（每站最后状态）。

---

## 3. 站点的投影规则

从对象直接推出站点序列：

| 站 | 来自对象 | 出现条件 |
|---|---|---|
| **入口** | EntryPoint（协议+端口） | 总有 |
| **还原真实 IP** | caddy realip 组件 | 装了 realip |
| **WAF 检查** | caddy + coraza 组件 | 装了 coraza |
| **TLS 握手** | Router.`tls` ≠ off | 协议为 https |
| **路由匹配** | Router（Host 规则） | 总有 |
| **中间件处理** | Router.middlewares（按序） | 挂了中间件 |
| **转发** | Service.`type=reverse_proxy` | 反代 |
| **上游** | Service.backend | 反代 |
| **响应** | — | 总有 |

**例（aria）**：

```yaml
router:  { name: aria, host: aria, roots: [neob.cn], entrypoint: https, port: 443,
           service: aria2-web, middlewares: [websocket], tls: auto }
service: { name: aria2-web, type: reverse_proxy, backend: { host: nas, container: aria2-pro, port: 6880 } }
```
→ 站点序列：`入口(443) → TLS → 路由匹配(Host=aria.neob.cn) → 中间件(websocket) → 转发 → 上游(192.168.1.20:6880) → 响应`

> **哪些中间件成为"站"**：会改变流程或产生可观测结果的处理（WAF、认证、重定向、限流）单列成站；纯变换（gzip）并入所属站，不单列。

---

## 4. 实况还原

Caddy 访问日志每条 = 一次访问。还原成一次 **RequestRun**：

```yaml
kind: RequestRun
route: aria
at: 2026-09-20T12:04:31Z
source: { ip: 1.2.3.4, ua: "..." }
stations:
  - { station: 入口,        ok: true, ms: 12 }
  - { station: WAF,         ok: true, ms: 3 }
  - { station: TLS,         ok: true, ms: 8 }
  - { station: 路由匹配,    ok: true, ms: 1 }
  - { station: 中间件,      ok: true, ms: 1 }
  - { station: 转发,        ok: true, ms: 42 }
  - { station: 上游,        ok: true, ms: 41 }
result: { status: 200, total_ms: 86 }
```

- **投影骨架固定**，实况只填每站状态（成败 / 耗时）；
- 站点耗时来自 Caddy 日志的细分字段；（若日志无细分，只填"总耗时"到转发站）
- 未命中的可选站（如无 WAF）不出现。

---

## 5. 旁路（不在 Caddy 请求路径内）

acme / ddns 是外部流程，**不站**在请求路径上，但影响某一站：

```
      ┌──────── acme.sh ────────┐
      │  签发/续期证书           │
      └───────────┬─────────────┘
                  │ 提供证书
TLS 站 ◀──────────┘
      ┌──────── ddns ───────────┐
      │  解析 → 本机 IP          │
      └───────────┬─────────────┘
                  │ 决定能否到达
入口站 ◀──────────┘
```

在主视图上画成**虚线旁路**，点开看它们的运行记录（事务链）。

---

## 6. 与事务链的挂钩

每个事务步骤持有坐标：**它改写了流量链的哪一站**。

| 事务组件 | 影响的流量站 |
|---|---|
| docker·编排 | 上游 |
| acme·证书 | TLS |
| ddns·更新解析 | 入口（可达性） |
| caddy·反向代理 | 路由匹配 / 中间件 / 转发 |
| coraza（组件） | WAF |

于是：在发布任务的链视图里，可以标出"这一步改了访问链的哪一站"；在主视图里，可以标出"这一站最近由哪次事务改动"。

---

## 7. 待定

1. 访问日志字段不足以还原细分站点时的降级规则。
2. 高频访问的采样/聚合（避免每次访问都落库）。
3. 多主机下，各主机 Caddy 的实况如何汇总。
