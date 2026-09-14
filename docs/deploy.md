# GateBox · 部署交付设计 & 开发计划

> 状态：设计已定（PRD v2 单位⑦）
> 关联：`docs/PRD.md`（§5 技术需求 / §7 文件架构 / §12 部署交付）、`docs/infra.md`（§5 gatebox.conf）、`docs/adr/ADR-011 / 013`
> 前置：单位①~⑥（功能代码）；本单位为最后一期（可并行做 build.sh 地基）

## 1. 定位与范围

把 GateBox（单二进制）+ 全部外部组件交付为**可安装的多发行版服务**。

- **技术需求**（PRD v2 §5）：架构 amd64、arm；系统 debian / ubuntu / openwrt / archlinux / armbian；尽量简约、低资源。
- **交付物**：`install.sh` + 多架构 tarball（本体 + 外部组件）；系统服务单元（systemd / openrc / procd）；`build.sh`（前端 → go:embed → 单二进制）。

边接：

- **不打包 docker daemon** / docker-compose 由系统包管理器提供（ADR-011）。
- **acme.sh** 需要它的账号/域名密钥目录（默认 `~/.acme.sh`），install 时检测并在服务用户下初始化。
- **flame**（flare 单二进制）装入 `tools/`，其配置目录由 install 初始化（单位⑥联动依赖）。

## 2. 组件归属

| 组件 | 来源 | 运行形态 | 服务单元 | 说明 |
|---|---|---|---|---|
| gatebox | 自研（`build.sh` 产物） | 单二进制 `$DATA_DIR/gatebox` | ✓ systemd/openrc/procd | 控制面 |
| caddy | 官方标准版（v2.11.x，无 DNS 插件） | `tools/caddy/caddy` | ✓ | 代理（文件证书，ADR-013） |
| acme.sh | 官方脚本 | `tools/acme/acme.sh` + 家目录 | ✓ cron/timer | 证书签发/续期（ADR-013） |
| ddns-go | 官方单二进制 | `tools/ddnsgo/ddns-go` | ✓ | 外网 DDNS |
| docker-compose | 系统包管理器（docker compose v2 插件或 standalone） | 系统 | —（随 docker） | 编排 CLI |
| mosdns | 官方单二进制 | `tools/mosdns/` | ✓ | 内网 DNS（单位⑤） |
| flame（flare） | 官方单二进制（本机已放 `flare-amd64`/`flare-arm64`） | `tools/flare-<arch>` | ✓ | 导航，端口默认 5005 |
| tailscale | 发行版包 / 官方 | 系统 `tailscaled` | 系统服务 | 组网（单位⑤，GateBox 只读状态） |

- **声明原则**：tools 二进制按 `tools/<name>/` 归置（当前仓库 `tools/` 平铺为开发期临时形态；install 负责规整到目标布局）。

## 3. 目录与服务布局（安装后）

```
<DATA_DIR>/
├── gatebox                     # 本体二进制
├── conf/{gatebox.db,secret.key,gatebox.conf}   # infra.md §3
├── tools/                      # caddy / acme / ddnsgo / mosdns / flare-<arch> / docker-compose
├── appData/<projectName>/
├── Caddyfile                   # 最近成功 /load 备份
└── README.md
```

服务：

- GateBox 本体服务（读 `conf/gatebox.conf`）；caddy / ddns-go / mosdns / flare 各自独立服务单元；`acme.sh --cron` 定时器。
- 服务用户：专用低权用户 `gatebox`（非 root）——`docker.sock` 组授权 + caddy 加载目录写权限为最小集。

## 4. `scripts/build.sh`

流程（CI/本地均可）：

1. `frontend/pnpm build` → `backend/internal/web/dist`（go:embed 内嵌）。
2. `backend/go build -ldflags "-X main.version=..." -o <arch>/gatebox`；交叉编译 `GOOS=linux GOARCH=amd64|arm64`（CGO_ENABLED=0）。
3. 收集外部组件（从 release 下载或拷贝 `tools/` 已验证二进制）→ 组装 `gatebox-<ver>-<arch>.tar.gz`：
   - `gatebox`、`install.sh`、`configs/`、`conf/gatebox.conf.example`、外部组件二进制、`docs/` 精简 README。
4. 输出校验：`sha256sum` + 产物清单。

## 5. `scripts/install.sh`

流程（幂等，可重跑）：

1. **检测**：发行版（debian/ubuntu/armbian → apt；archlinux → pacman；openwrt → opkg）、架构、`docker` 存在性。
2. **装配**：解压到 `$DATA_DIR`（默认 `/var/lib/gatebox`，可 `--data-dir` 覆盖）；生成 `conf/gatebox.conf`（写入 data_dir、端口、各组件路径，默认 `0.0.0.0:8099`）。
3. **建用户** `gatebox` + 目录授权（`/var/run/docker.sock` 组）。
4. **初始化**：acme.sh 账户注册指引（首次签发前）；flare 配置目录；Caddyfile 空模板 + `tools/caddy/user/` 预置。
5. **装服务**：按发行版选择 unit（`configs/services/systemd/gatebox.service` 等 + caddy/ddns-go/mosdns/flare/acme-cron 对应单元）。
6. **启动**：启动各服务并 `systemctl/enable`（openwrt 用 procd `/etc/init.d` 模板）。
7. **回滚**：`install.sh --uninstall`（停服、删除单元、保留 `$DATA_DIR` 与 `conf/`）。

## 6. 服务单元模板（`configs/services/`）

| 平台 | 文件 |
|---|---|
| systemd | `systemd/gatebox.service`、`systemd/caddy.service`、`systemd/ddns-go.service`、`systemd/mosdns.service`、`systemd/flare.service`、`systemd/acme-cron.timer` |
| openrc | `openrc/gatebox`、`openrc/caddy` … |
| procd（OpenWrt） | `procd/gatebox` …（`/etc/init.d` 脚本 + `uci` 后端，门槛高，见 §8 风险） |

要点：GateBox 服务含 `EnvironmentFile=$DATA_DIR/conf/gatebox.conf` 式导入；各组件服务 `User=gatebox` 最小权限；acme-cron `--cron --home <acme home>` 每日续期检查。

## 7. 验证方案

| 类型 | 内容 |
|---|---|
| 构建 | `build.sh` 在干净环境产出两个架构 tarball；`go vet ./...`、gofmt 合规 |
| 安装 | docker-in-docker / 容器内逐发行版跑 `install.sh` → 服务起来；幂等重跑不报错 |
| 人工 | debian/ubuntu 真机全链路：安装 → 加应用 → 证书 → DDNS → 导航页出现；升级二进制热替换 |
| 回滚 | `--uninstall` 后 `conf/` 保留、服务停净 |

## 8. 风险与决策点

- **OpenWrt（procd）**：无 systemd，二进制体积与内存约束大；本期可列为**尽力支持**（优先 debian/ubuntu/arch/armbian），openwrt 提供 `install.sh` 分支与 procd 模板，后续真机验证。
- **跨架构二进制下载源**：caddy/ddns-go/mosdns/flare 的发行源需在 build.sh 固化版本+校验和，防供应链漂移。
- **D1 决策点（单位⑤）**：mosdns/tailscale 的服务单元是否纳入 install，取决于接入深度决定。

## 9. 单位⑦ 开发计划（含验证方案）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | build.sh | 多架构 tarball + 校验 | 两架构产物可构建 | 解压冒烟 |
| 2 | 服务单元模板 | systemd 全组件 + acme-cron timer | 模板语法（`systemd-analyze verify` 如可用） | 装服务起停 |
| 3 | install.sh（debian/ubuntu/arch/armbian） | 安装/升级/卸载 + conf 初始化 | 容器内逐发行版幂等 | 真机全链路 |
| 4 | openwrt 分支 + procd 模板 | install 分支 + init.d 脚本 | 语法校验 | openwrt 真机（尽力） |
| 5 | acme-cron 与续期 | timer + `--cron` 验证 | 单测：cron 命令组装 | 次日证书续期 |