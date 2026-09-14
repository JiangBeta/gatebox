# plugins/

内置插件的 **manifest + schema + 文档**（随 GateBox 分发）。第三方插件不入本仓库，走静态索引分发（ADR-029）。

```
plugins/
├── manifests/     # 内置插件 manifest（如 coraza.yaml / tailscale.yaml）
└── schema/        # manifest 的 JSON Schema（供校验与编辑器提示）
```

- 插件是**数据**：`manifest + 通用引擎`；运行时制品统一落 `$DATA_DIR/tools/<id>/`。
- 三种形态：`caddy-module` / `process` / `config-only`（ADR-029）。
- 引擎实现位于 `backend/internal/plugin/`。
