# V4.1 日志（读日志 · 观测）

> 上层：[ADR-042](../adr/ADR-042.md) §7（观测模型）· [records.md](records.md) · 模型：[model/log.yaml](model/log.yaml)
> 状态：原型已实现（静态 HTML）· 入口：`docs/v4.1/prototype/`

本文只讲「日志」这一视图：**读日志能力取回的插件原始输出**的呈现。展示**最后一份**日志。

---

## 1. 两个视图

| 视图 | 内容 | 说明 |
|---|---|---|
| **地图视图**（默认） | 触发卡 → 连线 → sequence 卡（步骤列表） | 可**缩放 / 拖动**的画布（参照 OxiDNS）；节点卡 + 箭头 + 边标签 |
| **文本视图** | 原始日志全文 | CodeEditor 外观（行号 gutter）+ **日志格式高亮**：时间戳灰、关键字色（success 绿 / Pending 黄 / error 红）、证书块紫 |

**触发卡**：`角色 / 动作 / 时间`——角色（用户 / 系统 / 定时器）、动作（手工触发 / 第一次启动 / 自动更新），来自 `Run.trigger`。

**sequence 卡**：标题 + `序列 N 条 · 结果 · 耗时`；每行 `#no · 步骤 · 运行时间 · 状态 · 耗时`（行间虚线）。步骤名 hover 显示**日志标志**。

---

## 2. 序列从哪来：模板 + 匹配（不硬编码）

步骤序列由**组件自述**声明模板，运行时与真实日志**对账**得到：

```
组件自述.sequence 模板 [{no, step, marker, optional}]
        +
读日志（证据，acme.sh 原始输出）
        │  parseLog：按 marker 顺序匹配日志行
        ▼
实际步骤（时间戳 / 耗时）；未命中的 optional 步骤 → 记「跳过」
```

- `marker` = **日志标志**（该步骤在日志中的特征行），用于定位与算耗时（相邻命中时间差）。
- 不同流程 → **不同模板**，序列自然不同：

| 流程 | 模板 | 步骤数 |
|---|---|---|
| 首次签发 | `acme_issue` | 11 |
| 证书续期 | `acme_renew` | 8（复用账户、无注册/等待） |
| ddns / docker / 配置编译… | 各自组件的模板 | 各自 |

- **降级**：无模板或匹配不到 → 地图提示「未能解析步骤，见文本视图」；文本视图始终可用。

---

## 3. 真实日志来源

`data/tools/acme/certs/logs/<时间戳>-<fqdn>.log`（`backend/internal/adapter/acme` 的 `writeIssueLog` 写入，acme.sh 的 `CombinedOutput`）。原型取其中一份（DNS-01）作为 `mock.logs.acme`。

---

## 4. 模型与入口

- **模型**：`model/log.yaml` — Run 字段（chain/trigger/state/时间/耗时）+ step 列 + **sequences 模板** + evidence。
- **组件自述**：`model/component.yaml` — `acme·证书` 的 `logMarkers.sequences: [acme_issue, acme_renew]`。
- **原型入口**：域名 → 域名 tab → 证书「日志」图标；或 hash `#domains/domain-name/log/<sequence>`（如 `/log/acme`、`/log/acme_renew`）。

---

## 5. 待定

1. 其他组件（ddns / docker）的 sequence 模板与日志标志。
2. 日志按 **Run 列表**（历次）查看，而非仅最后一份。
3. 大日志的截断 / 懒加载；证书 PEM 块折叠。
4. 地图视图与「链视图（每步最后状态）」的合并（同一链视图叠加运行状态）。
