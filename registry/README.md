# registry/

插件/制品**静态索引**的模板与 JSON Schema。索引本体（`index.json` + 签名）托管于独立仓库 / CDN，不随本仓库运行（ADR-029）。

```
registry/
└── schema/        # index.json 的 JSON Schema
```

客户端流程：拉索引 → 按 `channel` 比较版本 → 校验签名 + sha256 → 安装 / 替换 / 回滚。
