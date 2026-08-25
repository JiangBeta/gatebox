# 术语表 Glossary

统一语言（Ubiquitous Language）。全项目文档、代码、UI 文案一致使用以下术语。

| 术语 | 定义 |
|---|---|
| **控制面** | GateBox 本体（Go 后端 + Vue3 前端 + BoltDB） |
| **外部组件** | caddy、ddnsgo、docker、docker-compose——独立进程、独立升级 |
| **App（应用）** | 可被代理的最小单元，`source ∈ {docker, manual, host}` |
| **ProxyRoute（代理规则）** | 挂在 App 下的代理条目，`type ∈ {reverse_proxy, file_server, tcp_stream}` |
| **Domain（域名）** | 根域名 rootDomain（如 `gatebox.cn`、`neob.cn`），域名管理的基本单位、独立实体 |
| **二级域名** | rootDomain 下的子域名（如 `ddnsgo.neob.cn`），由网关页 ProxyRoute 的 host 产生，不建独立实体，仅统计数量 |
| **证书管理器** | 证书访问的抽象层：当前实现走 Caddy ACME，未来可切换独立 acme 客户端而不改上层 |
| **ComposeInstance** | 一个 compose 项目：定义（YAML 文件）+ 运行态。部署的基本单位，`1 ComposeInstance ──< N Container ──< M App`（M ≤ N） |
| **projectName** | ComposeInstance 主键，同时是 docker 项目名与 `appData/` 目录名。格式 `[a-z0-9][a-z0-9_-]{1,62}`，**创建后不可改** |
| **displayName** | ComposeInstance 的展示名，可用中文等任意字符，仅用于 UI |
| **托管容器** | GateBox 在 `appData/<projectName>/` 下创建的编排所起的容器，全功能可编辑 |
| **外部编排** | 由 `docker compose ls -a` 发现的非托管项目，默认只读，需显式「接管」才可编辑 |
| **游离容器** | 无 compose project label 的容器（`docker run` 起的），只读控制 + 可「转为编排」 |
| **DNS 凭证** | cloudflare / dnspod.cn / aliyun 的 API key（统一管理、加密存储） |
| **Upstream（后端）** | 代理转发目标，`IP:端口`，可多个（负载均衡） |
| **DATA_DIR** | 用户指定的运行时数据根目录，安装脚本创建并写入 DB |
| **source** | App 的来源：`docker`（label 自动发现）/ `manual`（手动 IP:端口）/ `host`（宿主机服务） |
