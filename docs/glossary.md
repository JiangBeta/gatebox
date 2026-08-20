# 术语表 Glossary

统一语言（Ubiquitous Language）。全项目文档、代码、UI 文案一致使用以下术语。

| 术语 | 定义 |
|---|---|
| **控制面** | GateBox 本体（Go 后端 + Vue3 前端 + BoltDB） |
| **外部组件** | caddy、ddnsgo、docker、docker-compose——独立进程、独立升级 |
| **App（应用）** | 可被代理的最小单元，`source ∈ {docker, manual, host}` |
| **ProxyRoute（代理规则）** | 挂在 App 下的代理条目，`type ∈ {reverse_proxy, file_server, tcp_stream}` |
| **Domain（域名）** | 二级域名，联动 DDNS 上报与证书签发 |
| **ComposeInstance** | 表单生成的 docker-compose 定义 + 运行态 |
| **DNS 凭证** | cloudflare / dnspod.cn / aliyun 的 API key（统一管理、加密存储） |
| **Upstream（后端）** | 代理转发目标，`IP:端口`，可多个（负载均衡） |
| **DATA_DIR** | 用户指定的运行时数据根目录，安装脚本创建并写入 DB |
| **source** | App 的来源：`docker`（label 自动发现）/ `manual`（手动 IP:端口）/ `host`（宿主机服务） |
