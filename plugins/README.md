# plugins/

内置插件的 **manifest + schema + 文档**（随 GateBox 分发）。第三方插件不入本仓库，走静态索引分发（ADR-029 / ADR-036）。

```
plugins/
├── manifests/     # 内置插件 manifest 样例（caddy-l4 / coraza / realip …）
└── schema/        # manifest 的 JSON Schema（manifest.v2.schema.json）
```

- 插件是**数据**：`manifest v2 + 通用引擎`；运行时制品统一落 `$DATA_DIR/tools/<id>/`。
- 三种形态：`caddy-module` / `process` / `config-only`。
- **贡献（contributions）**（ADR-036）：
  - `capabilities[]`：`point` + `data`（如 `proxy-protocols`）；
  - `backend[]`：`renderer`（中性规则 → Caddyfile 片段）/ `config-sync`（投影 → 插件配置）/ `reconcile`（通知侧车）；
  - `ui{}`：`nav` / `routes` / `slots` / `page`（前端贡献，L0 元数据驱动）；
  - `data{}`：`subscribe`（订阅核心投影）。
- **制品（artifacts）** 为带 `role` 的列表：`binary` / `sidecar` / `ui` / `assets`。
- **权限（permissions）**：`filesystem` / `network` / `api(scope)`，安装时显式授权。
- 引擎实现位于 `backend/internal/plugin/`；能力注册表与投影契约位于 `backend/internal/extension/`。
- 三个不变式：核心无插件身份；核心→插件只经投影；插件→核心只经贡献声明。
