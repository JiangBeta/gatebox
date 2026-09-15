# registry/

插件/制品**静态索引**的模板与 JSON Schema。索引本体（`index.json` + 签名）托管于独立仓库 / CDN，不随本仓库运行（ADR-029 / ADR-036）。

```
registry/
└── schema/        # index.json（catalog v1）的 JSON Schema
```

客户端流程：拉索引 → 按 `channel` 比较版本 → 校验签名 + sha256 → 安装 / 替换 / 回滚。

## catalog v1 形状（ADR-036）

```json
{
  "schema": "gatebox.catalog/v1",
  "publisher": { "id": "...", "keyID": "..." },
  "plugins": [
    {
      "id": "caddy-l4",
      "versions": [
        {
          "version": "0.1.0",
          "channel": "stable",
          "publishedAt": "2026-09-15T00:00:00Z",
          "manifest": {},
          "artifacts": [],
          "signature": "..."
        }
      ]
    }
  ]
}
```

- 每个版本内嵌完整 **manifest v2**（见 `plugins/schema/manifest.v2.schema.json`）与制品清单（带 `role`）。
- 签名在版本条目级；制品可打包为单一 tar.gz，亦可独立 URL（GitHub Releases）。
- 信任分级：`official | verified | community`，决定可用 UI 层（L0/L1/L2）与权限（ADR-036 §7/§8）。
- **本阶段不实现在线索引下载**，仅保留数据模型。
