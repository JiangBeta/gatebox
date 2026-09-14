# old/ —— 重构前原始代码（冻结快照）

本目录保留 v3 重构前的全部代码，**仅供回溯、查证与对照**，不参与新系统构建，也不再修改。

## 结构

```
old/
├── backend/    # 原 Go 后端（独立 go.mod，module github.com/JiangBeta/gatebox）
└── frontend/   # 原 Vue3 前端
```

## 构建旧后端

`old/backend` 与新的 `backend/` 使用相同 module path，因此不能同时纳入根 `go.work`。单独构建时需关闭 workspace：

```bash
cd old/backend
GOWORK=off go build ./...
GOWORK=off go test ./...
```

前端依赖已随目录迁移；如需运行请在 `old/frontend` 内自行 `pnpm install`。

## 迁移对照

新系统的分层与设计见 [`docs/architecture.md`](../docs/architecture.md) 与 ADR-027 ~ ADR-032。
