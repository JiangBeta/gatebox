# AGENTS.md — OpenCode 开发指南

> 完整说明见 `CLAUDE.md`。本文件只列 agent 容易踩坑的高信号信息。

## 语言

沟通、思考、代码注释**一律中文**。

## 快速命令

```bash
mise trust && mise install     # 首次进入项目：安装 go 1.26 / node 22 / pnpm 11
mise run test                  # 后端全部测试（backend/）
mise run build                 # 构建二进制 → ./gatebox
mise run dev                   # 后端开发服务 0.0.0.0:8099
mise run web                   # 前端开发服务器 0.0.0.0
```

后端单包验证（工作目录 `backend/`）：
```bash
go build ./...
go test ./...
go vet ./...
gofmt -l ./internal ./cmd      # 有输出即不合规
```

前端（工作目录 `frontend/`）：
```bash
pnpm build          # 输出到 backend/internal/web/dist（go:embed 内嵌）
pnpm dev --host 0.0.0.0
```

## 致命顺序

1. **改了前端 → 必须重新编译后端**（`go:embed` 编译期嵌入 `dist`，热替换无效）。
2. `pnpm build` 输出到 `backend/internal/web/dist`，该目录是 Go embed 根，不要手动删。
3. CGO_ENABLED=0 已在 `mise.toml` 设定，不要手动覆盖。

## 架构要点

- Go + Vue3 + BoltDB 单二进制。入口 `backend/cmd/gatebox/main.go`。
- **控制面 + 外部组件**：caddy / ddnsgo / docker / docker-compose 是独立进程，GateBox 只改写配置 + 调 API/CLI，不接管生命周期。
- Docker 操作走**自研轻量 HTTP 封装**（`internal/docker/`），非 moby SDK。见 `docs/adr/ADR-014.md`。

## 测试注意

- **竞态检测**需 C 编译器（mise 不提供）：`CGO_ENABLED=1 go test ./... -race`
- **Docker 集成测试**对接真实 daemon，`/var/run/docker.sock` 不可用时自动跳过；`GATEBOX_SKIP_DOCKER_IT=1` 显式跳过。

## 前端技术栈

### UI 框架
- **Ant Design Vue 4.x**（从 naive-ui 迁移）
- **@ant-design/icons-vue**（图标库）
- **unplugin-vue-components**（按需加载）

### 主题配置
- 主色：`#1677ff`（Ant Design 默认蓝）
- 字体：系统字体（-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif）
- 间距：8px 基准网格
- 圆角：6px（默认）

### 组件封装优先级
1. 模态框（Modal/Drawer）
2. 表格（Table）
3. 表单（Form）
4. 按钮（Button）

### 迁移顺序
1. Docker 页面（最复杂，先迁移）
2. 域名页面
3. 网关页面
4. 设置页面
5. 仪表盘页面

### 性能优化
- **按需加载**：使用 unplugin-vue-components 自动按需加载
- **虚拟滚动**：Docker 容器/镜像列表使用虚拟滚动
- **代码分割**：路由懒加载、组件懒加载

### 开发规范
- 使用 Ant Design Vue 的 ConfigProvider 配置主题
- 使用 8px 基准网格统一间距
- 使用系统字体族
- 动画使用 Ant Design Vue 默认配置

### 文档
- `docs/ui-spec.md`：完整的 UI 规范文档
- `docs/component-mapping.md`：naive-ui → Ant Design Vue 组件映射

## 前端库坑（旧）

- **naive-ui 2.40 `n-drawer`**：只渲染 default 槽，忽略 `#header`/`#footer`，内容无内边距。头部/底部需手动放 default 槽用 flex 布局。见 `ComposeEditorModal.vue`。
- **CodeMirror**：`codemirror` 包只导出 `EditorView`/`basicSetup`，`ViewPlugin`/`Decoration`/`WidgetType` 需从 `@codemirror/view` 显式 import；`@codemirror/state` 等需显式 `pnpm add`（pnpm 不提升传递依赖）。

## 前端库坑（新）

- **Ant Design Vue 4.x 按需加载**：需要配置 unplugin-vue-components 和 AntDesignVueResolver
- **ConfigProvider 主题配置**：需要在 App.vue 中配置 ConfigProvider，传入主题配置
- **虚拟滚动**：大数据表格需要配置 scroll.y 属性
- **图标按需加载**：@ant-design/icons-vue 需要按需导入，避免全量导入

## 开发环境

- 服务**一律绑 `0.0.0.0`**，不要绑 `127.0.0.1`（需局域网访问）。
- 开发端口 **8099**（8080 被 traefik 占用，8090 被 gateway-manager 占用）。
- 前端 `vite.config.ts` 已配置 `/api` 代理到 `127.0.0.1:8099`（含 WebSocket）。

## 目录约定

```
backend/
  cmd/gatebox/main.go          # 入口
  internal/{config,models,store,caddy,ddns,docker,api,server}/
frontend/
  src/{api,views,components,stores,router,locales}/
tools/{caddy,ddnsgo,docker}/   # 外部组件（运行时数据）
```

## 文档

- `docs/PRD.md` · `docs/glossary.md` · `docs/adr/`（ADR-001 ~ ADR-017）
- `docs/domain.md` · `docs/docker.md` · `docs/layout.md`
