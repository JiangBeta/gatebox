# GateBox UI 规范文档

> 版本：1.0.0
> 最后更新：2026-09-02
> 状态：已确认

## 1. 技术栈

| 层 | 选型 |
|---|---|
| UI 框架 | Ant Design Vue 4.x |
| 图标库 | @ant-design/icons-vue |
| 按需加载 | unplugin-vue-components |
| 虚拟滚动 | ant-design-vue 内置（大数据表格） |
| 主题配置 | ConfigProvider + 自定义主题 |

## 2. 颜色规范

### 2.1 主色

```typescript
// frontend/src/theme/color.ts
export const colors = {
  primary: '#1677ff',      // Ant Design 默认蓝
  primaryHover: '#4096ff',
  primaryActive: '#0958d9',
  primaryBg: '#e6f4ff',
}
```

### 2.2 功能色

```typescript
export const functionalColors = {
  success: '#52c41a',
  successBg: '#f6ffed',
  warning: '#faad14',
  warningBg: '#fffbe6',
  error: '#ff4d4f',
  errorBg: '#fff2f0',
  info: '#1677ff',
  infoBg: '#e6f4ff',
}
```

### 2.3 中性色

```typescript
export const neutralColors = {
  text: '#000000d9',
  textSecondary: '#00000073',
  textTertiary: '#00000040',
  textQuaternary: '#00000026',
  border: '#d9d9d9',
  borderSecondary: '#f0f0f0',
  bg: '#ffffff',
  bgLayout: '#f5f5f5',
  bgContainer: '#ffffff',
  bgElevated: '#ffffff',
}
```

## 3. 字体规范

### 3.1 字体族

```typescript
export const fontFamily = `
  -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue',
  Arial, 'Noto Sans', sans-serif, 'Apple Color Emoji', 'Segoe UI Emoji',
  'Segoe UI Symbol', 'Noto Color Emoji'
`
```

### 3.2 字体大小

```typescript
export const fontSize = {
  xs: '12px',      // 辅助文字
  sm: '14px',      // 正文小字
  base: '14px',    // 正文（Ant Design 默认）
  lg: '16px',      // 小标题
  xl: '20px',      // 标题
  xxl: '24px',     // 大标题
}
```

### 3.3 行高

```typescript
export const lineHeight = {
  tight: '22px',
  base: '22px',
  loose: '32px',
}
```

## 4. 间距规范

### 4.1 基准网格

使用 **8px 基准网格**，所有间距为 8 的倍数：

```typescript
export const spacing = {
  xs: '4px',    // 0.5x
  sm: '8px',    // 1x
  md: '16px',   // 2x
  lg: '24px',   // 3x
  xl: '32px',   // 4x
  xxl: '48px',  // 6x
}
```

### 4.2 组件间距

```typescript
export const componentSpacing = {
  // 表单项之间
  formItem: '24px',
  // 卡片之间
  card: '16px',
  // 表格行之间
  tableRow: '0',
  // 按钮之间
  button: '8px',
}
```

## 5. 圆角规范

```typescript
export const borderRadius = {
  sm: '4px',     // 小元素（标签、按钮）
  base: '6px',   // 默认（输入框、卡片）
  lg: '8px',     // 大元素（模态框）
  xl: '12px',    // 特殊元素
}
```

## 6. 阴影规范

```typescript
export const boxShadow = {
  sm: '0 1px 2px 0 rgba(0, 0, 0, 0.03), 0 1px 6px -1px rgba(0, 0, 0, 0.02), 0 2px 4px 0 rgba(0, 0, 0, 0.02)',
  base: '0 6px 16px 0 rgba(0, 0, 0, 0.08), 0 3px 6px -4px rgba(0, 0, 0, 0.12), 0 9px 28px 8px rgba(0, 0, 0, 0.05)',
  lg: '0 6px 16px 0 rgba(0, 0, 0, 0.08), 0 3px 6px -4px rgba(0, 0, 0, 0.12), 0 9px 28px 8px rgba(0, 0, 0, 0.05)',
}
```

## 7. 动画规范

```typescript
export const animation = {
  // 过渡时间
  duration: {
    fast: '0.1s',
    base: '0.2s',
    slow: '0.3s',
  },
  // 缓动函数
  easing: {
    base: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
    in: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
    out: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
    inOut: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
  },
}
```

## 8. 组件规范

### 8.1 模态框（Modal）

```typescript
export const modalConfig = {
  // 尺寸
  width: {
    sm: '400px',
    base: '520px',
    lg: '720px',
    xl: '900px',
  },
  // 内边距
  padding: '24px',
  // 标题字体
  titleFontSize: '16px',
  titleFontWeight: '600',
  // 按钮间距
  footerGap: '8px',
}
```

### 8.2 表格（Table）

```typescript
export const tableConfig = {
  // 行高
  rowHeight: '54px',
  // 表头高度
  headerHeight: '54px',
  // 内边距
  cellPadding: '16px',
  // 分页配置
  pagination: {
    pageSize: 20,
    showSizeChanger: true,
    showQuickJumper: true,
    showTotal: (total: number) => `共 ${total} 条`,
  },
}
```

### 8.3 表单（Form）

```typescript
export const formConfig = {
  // 标签宽度
  labelWidth: '100px',
  // 标签位置
  labelPlacement: 'left',
  // 表单项间距
  itemGap: '24px',
  // 验证规则触发时机
  validateTrigger: 'onChange',
}
```

### 8.4 按钮（Button）

```typescript
export const buttonConfig = {
  // 尺寸
  size: {
    sm: '32px',
    base: '40px',
    lg: '48px',
  },
  // 圆角
  borderRadius: '6px',
  // 字体
  fontSize: '14px',
  fontWeight: '500',
}
```

## 9. 布局规范

### 9.1 页面布局

```typescript
export const layoutConfig = {
  // 侧边栏宽度
  siderWidth: '200px',
  // 头部高度
  headerHeight: '80px',
  // 内容区内边距
  contentPadding: '24px',
  // 内容区背景色
  contentBg: '#f5f5f5',
}
```

### 9.2 栅格系统

使用 Ant Design 的 Grid 栅格系统：

```vue
<template>
  <a-row :gutter="[16, 16]">
    <a-col :xs="24" :sm="12" :md="8" :lg="6">
      <!-- 内容 -->
    </a-col>
  </a-row>
</template>
```

### 9.3 响应式断点

```typescript
export const breakpoints = {
  xs: '480px',
  sm: '576px',
  md: '768px',
  lg: '992px',
  xl: '1200px',
  xxl: '1600px',
}
```

## 10. 图标规范

```typescript
export const iconConfig = {
  // 图标大小
  size: {
    sm: '14px',
    base: '16px',
    lg: '20px',
  },
  // 图标颜色
  color: 'inherit',
  // 图标间距
  gap: '8px',
}
```

## 11. 交互规范

### 11.1 加载状态

- **按钮加载**：使用 `loading` 属性，显示旋转图标
- **表格加载**：使用 `loading` 属性，显示骨架屏
- **页面加载**：使用 `Spin` 组件，显示全屏加载

### 11.2 错误处理

- **表单验证错误**：在表单项下方显示红色提示文字
- **API 错误**：使用 `message.error()` 显示错误提示
- **网络错误**：显示重试按钮

### 11.3 成功反馈

- **操作成功**：使用 `message.success()` 显示成功提示
- **表单提交成功**：关闭模态框，刷新列表
- **批量操作成功**：显示成功数量

## 12. 可访问性规范

- 所有交互元素支持键盘导航
- 表单元素关联标签
- 颜色对比度符合 WCAG 2.1 AA 标准
- 图片提供 alt 属性
- 模态框支持 Esc 关闭

## 13. 性能优化

### 13.1 按需加载

使用 unplugin-vue-components 自动按需加载：

```typescript
// vite.config.ts
import Components from 'unplugin-vue-components/vite'
import { AntDesignVueResolver } from 'unplugin-vue-components/resolvers'

export default {
  plugins: [
    Components({
      resolvers: [AntDesignVueResolver()],
    }),
  ],
}
```

### 13.2 虚拟滚动

大数据表格（>100 行）使用虚拟滚动：

```vue
<template>
  <a-table
    :columns="columns"
    :data-source="data"
    :scroll="{ y: 400 }"
    :pagination="false"
  />
</template>
```

### 13.3 图片优化

- 使用 WebP 格式
- 压缩图片大小
- 使用懒加载

## 14. 迁移检查清单

### 14.1 按需加载配置

- [ ] 安装 `unplugin-vue-components`
- [ ] 配置 `AntDesignVueResolver`
- [ ] 测试按需加载效果

### 14.2 主题配置

- [ ] 创建 `theme/color.ts` 配置文件
- [ ] 创建 `theme/spacing.ts` 配置文件
- [ ] 创建 `theme/typography.ts` 配置文件
- [ ] 配置 `ConfigProvider`

### 14.3 组件封装

- [ ] 封装模态框组件
- [ ] 封装表格组件
- [ ] 封装表单组件
- [ ] 封装按钮组件

### 14.4 页面迁移

- [ ] Docker 页面迁移
- [ ] 域名页面迁移
- [ ] 网关页面迁移
- [ ] 设置页面迁移
- [ ] 仪表盘页面迁移

### 14.5 测试验证

- [ ] 功能测试
- [ ] 视觉测试
- [ ] 性能测试
- [ ] 可访问性测试

---

## 附录 A：颜色对比

### 旧颜色（naive-ui）

| 颜色 | 值 | 用途 |
|------|-----|------|
| 主色 | `#18a058` | 绿色 |
| 成功 | `#18a058` | 绿色 |
| 警告 | `#f0a020` | 橙色 |
| 错误 | `#d03050` | 红色 |
| 信息 | `#2080f0` | 蓝色 |

### 新颜色（Ant Design Vue）

| 颜色 | 值 | 用途 |
|------|-----|------|
| 主色 | `#1677ff` | 蓝色 |
| 成功 | `#52c41a` | 绿色 |
| 警告 | `#faad14` | 橙色 |
| 错误 | `#ff4d4f` | 红色 |
| 信息 | `#1677ff` | 蓝色 |

---

## 附录 B：组件映射

### naive-ui → Ant Design Vue

| naive-ui | Ant Design Vue | 备注 |
|----------|----------------|------|
| NButton | AButton | 按钮 |
| NInput | AInput | 输入框 |
| NSelect | ASelect | 下拉选择 |
| NTable | ATable | 表格 |
| NModal | AModal | 模态框 |
| NDrawer | ADrawer | 抽屉 |
| NForm | AForm | 表单 |
| NFormItem | AFormItem | 表单项 |
| NTag | ATag | 标签 |
| NAlert | AAlert | 警告提示 |
| NSpin | ASpin | 加载 |
| NEmpty | AEmpty | 空状态 |
| NPopover | APopover | 气泡卡片 |
| NTooltip | ATooltip | 文字提示 |
| NPopconfirm | APopconfirm | 气泡确认框 |
| NMessage | message | 全局提示 |
| NNotification | notification | 通知 |
| NLayout | ALayout | 布局 |
| NMenu | AMenu | 菜单 |
| NTabs | ATabs | 标签页 |
| NSteps | ASteps | 步骤条 |
| NProgress | AProgress | 进度条 |
| NAvatar | AAvatar | 头像 |
| NBadge | ABadge | 徽标数 |
| NCollapse | ACollapse | 折叠面板 |
| NAccordion | (使用 Collapse) | 手风琴 |
| NCard | ACard | 卡片 |
| NDescription | Descriptions | 描述列表 |
| NResult | AResult | 结果 |
| NStatistic | AStatistic | 统计数值 |
| NDivider | ADivider | 分割线 |
| NGrid | Row/Col | 栅格 |
| NGridItem | Col | 栅格项 |
| NSpace | Space | 间距 |
| NText | Typography.Text | 文字 |
| NParagraph | Typography.Paragraph | 段落 |
| NTitle | Typography.Title | 标题 |
| NLink | ALink | 链接 |
| NText | Typography.Text | 文字 |
| NText | Typography.Text | 文字 |

---

## 附录 C：配置文件模板

### theme/index.ts

```typescript
import { colors, functionalColors, neutralColors } from './color'
import { fontSize, fontFamily, lineHeight } from './typography'
import { spacing, componentSpacing } from './spacing'
import { borderRadius, boxShadow } from './effects'
import { animation } from './animation'

export const theme = {
  colors: {
    ...colors,
    ...functionalColors,
    ...neutralColors,
  },
  typography: {
    fontSize,
    fontFamily,
    lineHeight,
  },
  spacing: {
    ...spacing,
    ...componentSpacing,
  },
  effects: {
    borderRadius,
    boxShadow,
    animation,
  },
}

export default theme
```

### theme/color.ts

```typescript
export const colors = {
  primary: '#1677ff',
  primaryHover: '#4096ff',
  primaryActive: '#0958d9',
  primaryBg: '#e6f4ff',
}

export const functionalColors = {
  success: '#52c41a',
  successBg: '#f6ffed',
  warning: '#faad14',
  warningBg: '#fffbe6',
  error: '#ff4d4f',
  errorBg: '#fff2f0',
  info: '#1677ff',
  infoBg: '#e6f4ff',
}

export const neutralColors = {
  text: '#000000d9',
  textSecondary: '#00000073',
  textTertiary: '#00000040',
  textQuaternary: '#00000026',
  border: '#d9d9d9',
  borderSecondary: '#f0f0f0',
  bg: '#ffffff',
  bgLayout: '#f5f5f5',
  bgContainer: '#ffffff',
  bgElevated: '#ffffff',
}
```

### theme/typography.ts

```typescript
export const fontFamily = `
  -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue',
  Arial, 'Noto Sans', sans-serif, 'Apple Color Emoji', 'Segoe UI Emoji',
  'Segoe UI Symbol', 'Noto Color Emoji'
`

export const fontSize = {
  xs: '12px',
  sm: '14px',
  base: '14px',
  lg: '16px',
  xl: '20px',
  xxl: '24px',
}

export const lineHeight = {
  tight: '22px',
  base: '22px',
  loose: '32px',
}
```

### theme/spacing.ts

```typescript
export const spacing = {
  xs: '4px',
  sm: '8px',
  md: '16px',
  lg: '24px',
  xl: '32px',
  xxl: '48px',
}

export const componentSpacing = {
  formItem: '24px',
  card: '16px',
  tableRow: '0',
  button: '8px',
}
```

### theme/effects.ts

```typescript
export const borderRadius = {
  sm: '4px',
  base: '6px',
  lg: '8px',
  xl: '12px',
}

export const boxShadow = {
  sm: '0 1px 2px 0 rgba(0, 0, 0, 0.03), 0 1px 6px -1px rgba(0, 0, 0, 0.02), 0 2px 4px 0 rgba(0, 0, 0, 0.02)',
  base: '0 6px 16px 0 rgba(0, 0, 0, 0.08), 0 3px 6px -4px rgba(0, 0, 0, 0.12), 0 9px 28px 8px rgba(0, 0, 0, 0.05)',
  lg: '0 6px 16px 0 rgba(0, 0, 0, 0.08), 0 3px 6px -4px rgba(0, 0, 0, 0.12), 0 9px 28px 8px rgba(0, 0, 0, 0.05)',
}
```

### theme/animation.ts

```typescript
export const animation = {
  duration: {
    fast: '0.1s',
    base: '0.2s',
    slow: '0.3s',
  },
  easing: {
    base: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
    in: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
    out: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
    inOut: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
  },
}
```

---

## 附录 D：迁移计划

### 阶段 1：基础设施（1-2 天）

1. 安装 Ant Design Vue 4.x
2. 配置按需加载
3. 创建主题配置文件
4. 配置 ConfigProvider
5. 测试基础功能

### 阶段 2：组件封装（2-3 天）

1. 封装模态框组件
2. 封装表格组件
3. 封装表单组件
4. 封装按钮组件
5. 测试组件功能

### 阶段 3：页面迁移（5-7 天）

1. Docker 页面迁移（2-3 天）
2. 域名页面迁移（1-2 天）
3. 网关页面迁移（1-2 天）
4. 设置页面迁移（0.5-1 天）
5. 仪表盘页面迁移（0.5-1 天）

### 阶段 4：测试验证（1-2 天）

1. 功能测试
2. 视觉测试
3. 性能测试
4. 可访问性测试

### 阶段 5：文档完善（0.5-1 天）

1. 更新 AGENTS.md
2. 创建迁移指南
3. 更新开发文档

**总计：9-15 天**

---

## 附录 E：性能优化清单

### 按需加载

- [ ] 配置 unplugin-vue-components
- [ ] 测试打包体积
- [ ] 验证按需加载效果

### 虚拟滚动

- [ ] 识别大数据表格
- [ ] 配置虚拟滚动
- [ ] 测试性能

### 图片优化

- [ ] 压缩图片
- [ ] 使用 WebP 格式
- [ ] 配置懒加载

### 代码分割

- [ ] 路由懒加载
- [ ] 组件懒加载
- [ ] 第三方库分割

### 缓存优化

- [ ] 配置 HTTP 缓存
- [ ] 使用 Service Worker
- [ ] 优化静态资源

---

**文档结束**

此文档记录了 GateBox 项目的所有 UI 规范和迁移计划。所有开发者在进行前端开发时应遵循此文档。
