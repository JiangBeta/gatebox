# V4.1 记录与对账

> 上层：[ADR-042](../adr/ADR-042.md) §7（观测模型）、§8（对账）
> 状态：草案

---

## 1. 记录：三层 + 两形态

**三层**（越往下越接近原始证据）：

| 层 | 实体 | 记什么 |
|---|---|---|
| 编排层 | **Run** / **Step** | 卡在哪一步、成败、耗时 |
| 动作层 | **调用** | 施加的能力：输入、命令/API、输出、退出码、耗时 |
| 证据层 | **证据** | 插件原始输出：CLI stdout/stderr、日志、状态快照 |

**两形态**：**链视图**（显示每步最后状态）+ **链日志**（历次 Run，可下钻）。

---

## 2. 数据结构

```yaml
# —— 事务 Run ——
kind: Run
id: 2001
chain: 发布服务
trigger: user
state: running            # running | done | failed | skipped
startedAt: ...
endedAt: ...
ctx:                      # Run 上下文（步骤间传值）
  upstream: { host: nas, port: 6880 }
steps:
  - no: 0
    object: { kind: 编排, name: aria2 }
    component: docker·编排
    ability: 启动
    state: done
    ms: 2800
    calls:
      - ability: 启动
        input:  { compose: "..." }
        cmd:    "docker compose up -d"
        output: "Started aria2-pro"
        exit:   0
        ms:     2800
        evidence: [ { type: stdout, ref: blob://... } ]
  - no: 1
    object: { kind: 路由, name: aria }
    component: acme·证书
    ability: 触发任务
    state: failed
    ms: 8300
    calls: [ { exit: 1, evidence: [ { type: cli, ref: blob://... } ] } ]

# —— 访问 RequestRun（流量）——
kind: RequestRun
id: 88421
route: aria
at: ...
source: { ip: 1.2.3.4 }
stations:
  - { station: 入口, ok: true, ms: 12 }
  - { station: TLS,  ok: true, ms: 8 }
result: { status: 200, totalMs: 86 }
```

- 事务 Run 与访问 RequestRun **共用骨架**（都是"一次执行 + 若干站/步 + 证据"）；
- 区别：事务 Run 有 `chain`、有上下文与调用；RequestRun 由日志还原，站点较粗。

---

## 3. 保留与推送

| 项 | 方案 |
|---|---|
| 事务 Run/Step | 落盘；**条数 + 天数双限**（如最近 500 条 / 7 天） |
| 调用输入输出 / 证据 | 落盘但**截断**（如每段 64KB）；大输出只存引用 |
| 访问 RequestRun | **聚合**为主（按路由/时间桶统计），明细只留最近 N 条 |
| 实时推送 | 事件流（run/step/state/activity）；WebSocket 留给终端/exec |
| 日志 | 不落库，按需读取（关联到 Run/Step） |

---

## 4. 对账（Reconcile）

**语义**：让「现状」逼近「期望」的唯一动作。

```
C_resource : D × A → A'     资源对账：对象 spec ↔ status
C_service  : D × A → S      服务对账：渲染并原子加载 Caddy 配置
```

| 路径 | 做什么 | 例 |
|---|---|---|
| 资源对账 | 比较对象的期望与现状，差则让组件执行 | 容器端口变了 → 更新服务上游 |
| 服务对账 | 由对象渲染 Caddy 配置并原子加载 | 路由/证书变化 → 编译 → 重载 |

**触发**：
- **被动**：蓝图/对象变化（用户操作）；
- **主动**：定时兜底（如每 5m 全量对账）、外部事件（容器变化、证书将过期）。

**失败**：标记 + 退避重试，**不全局回滚**；下轮继续收敛。
**漂移**：定期比较期望与现状，发现手改/外部变化即纠正。

---

## 5. 对账与链的关系

- 一次**收敛** = 一次 Run；链是收敛的可读形态；
- 资源对账产出的 Run 覆盖对象（服务/路由/域名…）；
- 服务对账产出的 Run 覆盖"编译 + 加载"；
- 定时链（IP 巡检、证书续期、全量对账）走同一套 Run 记录。

---

## 6. 待定

1. 访问明细的采样率与聚合粒度。
2. 证据的存储实现（对象存储 / 本地分片）。
3. 事件流的断线重放策略。
