# GateBox

为 HomeLab 打造的极简、可视化 **AppGateway**：一个控制面统一「应用代理、域名证书、二级域名 DNS、Docker 容器运维」。

## 技术选型

- **前端**：Vue3 + Vite + Naive UI（i18n：默认中文，可扩展英文）
- **后端**：Go
- **数据库**：BoltDB
- **外部组件**：caddy / ddnsgo / docker / docker-compose（独立进程、独立升级，通过「改写配置 + 调 API」读写）

## 文档导航

- [PRD（产品需求文档）](docs/PRD.md)
- [术语表 Glossary](docs/glossary.md)
- [架构决策记录 ADR](docs/adr/)

## 当前状态

**规划阶段** —— 已完成需求分析与架构决策（见 `docs/adr/`），按工作计划逐需求开发中：

1. 页面布局 & 域名
2. Docker
3. 应用
4. 控制台 & 设置（含用户认证）

## 快速开始

> 尚未实现。开发完成后补全安装/构建/运行说明。
