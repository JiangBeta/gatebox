# V4.1 蓝图（BP）：结构与模板

> 上层：[ADR-042](../adr/ADR-042.md) §10（蓝图）· §9.1（部署单元）
> 状态：草案

---

## 1. 蓝图是什么

整个网关「应该长什么样」的**唯一真相**。持久、可 diff、可回滚。工具的原生配置（Caddyfile / compose / ddns 配置）全是它**生成出来的产物**。

---

## 2. 三册

```
蓝图 BP
├─ 组件册   — 引用已装组件/插件的自述（只读）
├─ 对象册   — 用户声明（源对象）
└─ 链册     — 事务链（内置 + 自定义）
```

### 2.1 组件册（只读）

内容来自组件自述（ADR-042 §4、§4.1），**不由用户写**：

```yaml
components:
  - { id: caddy,     from: built-in, role: [入口, 执行],
      service_types: [reverse_proxy, file_server, redirect, respond],
      middleware_types: [encode, basic_auth, headers, websocket, rewrite, code] }
  - { id: caddy-l4,  from: plugin, installed: false,
      service_types: [tcp_proxy, udp_proxy] }
```

### 2.2 对象册（用户声明）

```yaml
objects:
  # 公共
  dns_credentials:
    - { name: dnspod-main, provider: dnspod.cn, token: "AAA" }
  domains:
    - { name: neob.cn, credential: dnspod-main }
  entrypoints:
    https: [443, 9443]
  middlewares:
    - { name: gzip, type: encode, formats: [gzip, zstd] }

  # 部署（docker 内容）
  deployments:
    - { name: aria2, host: nas, compose: { manual: "..." } }

  # 声明（GateBox 内容）
  services:
    - { name: aria2-web, type: reverse_proxy, backend: { host: nas, deployment: aria2, port: 6880 } }
  routers:
    - { name: aria, host: aria, roots: [neob.cn], entrypoint: https, port: 443,
        service: aria2-web, middlewares: [gzip], tls: auto }
```

### 2.3 链册

```yaml
chains:
  - name: 发布服务
    kind: builtin                # builtin | custom
    trigger: user
    steps:
      - { object: 服务/aria2-web, component: 核心·对象派生, ability: 派生 }
      - { object: 路由/aria,      component: 核心·配置编译, ability: 编译 }
      - { object: Caddy,         component: caddy·反向代理, ability: 重载 }
```

用户自定义链：在同一册里新增，只能引用**已装组件的能力**（ADR-042 §4.1 的类型目录）。

---

## 3. 物理组织与交换

- **存储**：逻辑上是**单一蓝图**，落库（BoltDB）为准；
- **交换**：可导出/导入为 YAML（便于备份、评审、迁移）；
- 导出时**组件册不导出**（它来自已装组件，属环境而非声明）；
- 导入时做校验（类型存在、引用完整、无冲突）。

---

## 4. 版本 / 差异 / 回滚

```yaml
revision: 1042                       # 每次有效变更 +1
author: admin
at: 2026-09-20T12:00:00Z
change: "新增路由 aria"
```

- **diff**：任意两版之间，按对象显示"增/删/改"；
- **回滚**：把某一版重新应用（生成新版本，不重写历史）；
- **审计**：谁在何时改了什么。

---

## 5. 与外部源头（纳入 / 托管）

| 源头 | 处理 |
|---|---|
| GateBox 管理的 compose / Caddy 配置 | **产物**，由对象生成，不直接编辑 |
| 已存在的外部 compose 项目 | **只读展示**；显式「接管」后纳入对象册托管 |
| 旧 `gatebox.conf` / `daemon.json` | 引擎设置，不属对象册 |

**原则**：蓝图是声明真相；外部文件要么是产物，要么被纳入后成为对象。

---

## 6. 模板生态

模板 = 预制的"部署单元"（同时封装 docker 内容与 GateBox 声明的默认值，ADR-042 §9.1）。

```yaml
id: flare
version: 1.2.0
publisher: gatebox-store
inputs:
  - { key: domain, type: domain, required: true }
  - { key: title,  type: text,   default: "我的导航" }
service: { type: reverse_proxy, containerPort: 5005 }
router:  { protocol: https, port: 443, tls: auto, middlewares: [gzip] }
compose: |
  services:
    flare:
      image: soulteary/flare:latest
      ports: ["{{hostPort}}:5005"]
      volumes: ["./data:/app/data"]
```

| 事项 | 方案 |
|---|---|
| 来源 | GateBoxStore 官方（签名分发）+ 用户自建 |
| 版本 | 模板有版本；实例记录所用版本 |
| 参数校验 | 按 `inputs` 的 type/required 校验 |
| 升级策略 | 模板升级**不自动改已部署实例**；提示"有新版本"，用户确认后重新渲染（保留 inputs） |
| 兼容 | 渲染出的对象与手工写的完全一致，走同一条发布链 |

---

## 7. 待定

1. 多文件导入时的引用完整性校验细节。
2. 自定义链的图形化编辑（还是先只支持 YAML）。
3. 模板签名与信任链（复用插件索引的 Ed25519）。
