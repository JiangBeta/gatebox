# GateBox · 首页 & 设置功能设计 & 开发计划

> 状态：设计已定（PRD v2 单位⑥）
> 关联：`docs/PRD.md`（§8 页面结构 / §9.5）、`docs/layout.md`、`docs/glossary.md`
> 前置：单位①（导航入口）、单位③（网关数据）、单位④（域名数据）用于仪表盘聚合

## 1. 定位与范围

首页与设置页：GateBox 的**入口面**与**系统信息出口**。

- **首页**：Tab 1 导航（**内嵌 flame**）、Tab 2 仪表盘（聚合各能力状态）。
- **设置**：关于 / 主题 / 语言 / 系统信息；认证后置（PRD v2 §11，单用户 admin）。

边界：

- flame 是外部组件（**内嵌页面展示**，GateBox 只做部署与配置联动，不实现导航渲染逻辑）。
- 仪表盘只**聚合**其它单位的 overview 数据，不重算业务状态。
- 主题 / 语言为**纯前端**设置（localStorage 持久化），不落 DB。

## 2. 首页设计（2 Tab，PRD v2 §8）

### Tab 1 · 导航（内嵌 flame）

- **内嵌方式**：`<iframe>` 加载导航服务 HTTP 地址（配置项 `flame_url`，默认如 `http://127.0.0.1:5005`；跨域问题用同源代理 `/flame/*` 转发缓解）。
- **运行形态**：**单二进制**（`tools/flare-<arch>`，Go 单文件导航页，本机已放 `flare-amd64` / `flare-arm64`），由系统服务管理器拉起；其应用配置落 `tools/flare/`（目录/文件式配置）。
- **部署联动**（配合单位⑦ install.sh）：安装时初始化配置与运行目录；GateBox 在网关成功 `/load` 后，把已发布应用聚合写为导航项（**联动实现见 §6 F2 研究**——cache flame 配置格式（`data/apps/*.yaml`）或暴露的 API，先做「手动同步按钮 / 导出配置」占位）。
- 未部署 / 不可达：降级提示 +「部署引导」（跳单位⑦步骤）。

### Tab 2 · 仪表盘

聚合卡片（点卡片跳对应一级页）：

| 卡片 | 数据来源 | 内容 |
|---|---|---|
| 网关 | `/apps` + `/services/health` | 代理服务数 · 健康绿/红/未知数 · Docker 自动数 |
| 容器 | `/docker/info` + `/docker/containers` | 运行 / 总计容器数 · 编排项目数 · 镜像数 |
| 域名 | `/domains/overview` | 域名数 · 证书状态（总数/即将过期/已过期）· DDNS 记录数 |
| 系统 | `conf/gatebox.conf` + 运行时 | GateBox 版本 · 运行时长 · DATA_DIR · 监听端口 |

新增聚合端点：`GET /api/v1/dashboard/overview`（后端并行汇总上述，前端一张卡片一次请求）。

> 占位策略：数据源不可达（如 docker 未装）→ 卡片显示「不可用」。

## 3. 设置页设计

### 关于

- GateBox 版本（LOGO 右侧小字灰色 + NEW Badge 逻辑：`version` 大于已见版本显示红色 NEW，localStorage 记录已见版本）。
- 官网 / 文档 / GitHub 链接（侧边栏底部小字同源）。
- 外部组件版本与运行状态：caddy / acme.sh / ddns-go / mosdns / tailscale（`tools/` 下各 `* --version` 探测 + 进程探测）——服务进程可见性（ADR-018 修订点 2 曾定的「设置-关于」落点）。
- 许可证 / 致谢。

### 外观与区域

- **主题**：深 / 浅 / auto（Ant Design Vue `ConfigProvider` theme + `localStorage:gb-theme`）。
- **语言**：zh-CN / en（`localStorage:gb-lang`；i18n 骨架已有，本期默认中文、英文翻译按需后置）。

### 系统

- DATA_DIR 与关键路径只读展示；`conf/gatebox.conf` 编辑入口（后置，避免误改）。
- 「重启服务」操作（走系统服务管理器，后置——ADR-001 不接管外部生命周期原则同样适用于本进程触发自身重启，需谨慎）。

## 4. 侧边栏 & 顶部联动（v2，源自 layout.md §2/§5）

- 侧边栏底部（退出上方小字）：**Github 链接 / 文档链接 / 🌐 语言切换 / 🌓 主题（深/浅/auto）**。
- 侧边栏最底部：**退出**（认证后置——本期无认证，退出按钮隐藏或显示「单用户无需登录」占位）。
- LOGO 右侧：版本号小字灰色 + 更新 NEW Badge。

## 5. 后端设计

模块与 API（`backend/internal/`）：

- `internal/dashboard`：overview 聚合（注入各数据源接口，避免 api 层直调）。
- 配置项追加：`flame_url`（`conf/gatebox.conf`，见 infra.md §5 扩展）。
- `internal/api` 追加：`GET /dashboard/overview`、`GET /system/versions`（组件版本探测）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/dashboard/overview` | 仪表盘聚合 |
| GET | `/system/versions` | 外部组件版本 + 进程状态 |

## 6. 决策点（开发前讨论）

- **F1** 导航服务 iframe 跨域与认证：标注「公开模式（只读）」展示；认证联动后置。
- **F2** 「应用 → 导航项」联动实现：研究 flame/flare 数据落点（tools/flare/ 下 apps 配置文件或 API，token 情况）；候选 ① 写配置 + 服务重启 ② 生成配置导入 ③ 仅手动同步按钮；本期先做 ③。
- **F3** 退出 / 重启服务入口：无认证阶段是否展示。

## 7. 单位⑥ 开发计划（含验证方案）

| # | 任务 | 交付物 | AI 验证 | 人工验证 |
|---|---|---|---|---|
| 1 | 导航 Tab（iframe 内嵌 flame） | iframe + flame_url 配置 + 降级 | 组件测试：URL 拼接、不可达降级 | 打开首页看 flame |
| 2 | 仪表盘聚合端点 | `GET /dashboard/overview` | 单测：各数据源 mock 聚合 | 点卡片跳转 |
| 3 | 仪表盘前端 | 卡片 + 跳转 + 不可用态 | 组件测试：聚合渲染 | 造数据核对 |
| 4 | 设置页（关于/系统） | 版本 + 组件状态 + DATA_DIR | 组件测试：版本 NEW Badge、组件状态 | 核对版本 |
| 5 | 主题 / 语言 / 侧边栏 | ConfigProvider 主题 + 切换持久化 | 组件测试：三态主题、语言切换 | 切主题/语言 |
| 6 | 联动（F2 初步） | 手动同步按钮 / 导出 | 组件测试：触发同步 | flame 出现导航项 |
| 7 | 端到端 | 首页 → 跳转 → 设置 | — | 走查 |