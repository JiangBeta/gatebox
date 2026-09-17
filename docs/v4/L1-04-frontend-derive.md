# L1-U4 · 前端派生引擎（lib/schema）

> 上层：[architecture.md §10](../architecture.md) · [ADR-031/032](../adr/ADR-032.md)（分层与门禁） · [ADR-041 §10](../adr/ADR-041.md)
> 状态：设计稿（待评审）· 依赖：U3（`/api/v1/schema/{factKind}`）

## 1. 目标与非目标

**目标**

1. 新建业务无关的 **`src/lib/schema/`**：把后端下发的 `ConfigField[]` 变成「表单值 ↔ 配置对象 ↔ 校验」的纯逻辑引擎。
2. 新建通用渲染器 **`src/app/components/schema/`**：`schema-form` 及按类型的字段控件。
3. 一个**验证闭环**：用最简单的事实（`variable`）跑通「schema → 表单 → 保存」。

**非目标**

- 不迁移现有 gateway / compose 复杂表单（P3 做）。
- 不做功能地图（U5）、不做 YAML 补全/lint（后置）。
- 不实现通用条件表达式（`type` 联动等）。

## 2. 分层与落位（遵守 ADR-032）

```
src/
├── lib/schema/            # 纯逻辑引擎（基础层：禁止 import @/app、@/modules）
│   ├── types.ts           # ConfigField / FactSchema 类型（镜像后端）
│   ├── defaults.ts        # 默认值
│   ├── values.ts          # 表单值模型（UI 中间态）与转换
│   ├── validate.ts        # 校验
│   └── index.ts
├── app/components/schema/ # 通用渲染器（app 层：可 import lib）
│   ├── SchemaForm.vue
│   ├── SchemaField.vue
│   ├── SchemaArrayField.vue
│   ├── SchemaObjectField.vue
│   ├── SchemaReferenceField.vue
│   └── SchemaAdvancedSection.vue
├── app/composables/
│   └── useFactSchema.ts   # 拉取 /api/v1/schema/{factKind}
└── modules/<域>/           # 域定制（uiHint 命中者）
```

- `lib/schema` **纯函数、可单测、无 Vue/无 HTTP**（引用数据经 props 传入）。
- 渲染器在 `app/components/schema/`，通用；域定制在 `modules/*`。
- **不依赖** `src/lib/http.ts`（已确认是死代码）；API 调用留在 `app/composables` / `modules/*/api.ts`。

## 3. TypeScript 类型（镜像后端）

`lib/schema/types.ts`（L1 手写；v3 §10 的 OpenAPI 生成落地后改为生成）：

```ts
export type ConfigType =
  | 'text' | 'password' | 'number' | 'select' | 'switch'
  | 'textarea' | 'array' | 'object' | 'reference'

export interface FieldOption { label: string; value: string | number }
export interface ReferenceSpec { types?: string[]; prefix?: '$' | ''; allowInvert?: boolean }

export interface ConfigField {
  key: string
  label: string
  type: ConfigType
  required?: boolean
  default?: unknown
  advanced?: boolean
  placeholder?: string
  description?: string
  docs?: string
  options?: FieldOption[]
  reference?: ReferenceSpec
  item?: ConfigField
  fields?: ConfigField[]
  summaryFields?: string[]
}

export interface FactSchema {
  factKind: string
  uiHint?: string
  fields: ConfigField[]
}
```

> 后端 `/schema` 返回 camelCase（U3 §8），与上述字段一一对应。

## 4. 表单值模型与转换

表单值不是配置对象：数组项需稳定 id、object 需 presence 标记、空值语义要与「未配置 vs 配置为空」区分（沿用 v3 对「默认值不物化」的经验）。

```ts
// 表单内部值（UI 中间态）
export type FormValue = unknown
export interface ArrayItemValue { id: string; value: FormValue }

// lib/schema/values.ts
export function createFormValues(fields: ConfigField[], config: object): Record<string, FormValue>
export function serializeFormValues(fields: ConfigField[], values: Record<string, FormValue>): object
```

规则：

- `text/number/select/...`：直传；`default` 仅作占位，**不写入**（未触碰的默认值不落配置）。
- `array`：`ArrayItemValue[]`（id 用单调计数），序列化时剥 id。
- `object`：`{ __present: boolean, ...children }`；序列化时子项全空且非必填则省略对象。
- `switch`：布尔；`select` 用哨兵区分「未配置」。
- 空串 / `undefined` 序列化时跳过。

校验（`lib/schema/validate.ts`）：`required`、`number` 可解析、`select` 值在选项内、object/array 递归。

## 5. 渲染器清单

| 组件 | 职责 |
|---|---|
| `SchemaForm.vue` | 顶层：按字段渲染；`advanced` 字段折入 `SchemaAdvancedSection`；提交前 `serialize` + `validate` |
| `SchemaField.vue` | 按 `ConfigType` 分派到 AntD 控件（Input / InputNumber / Select / Switch / Textarea / …） |
| `SchemaArrayField.vue` | 增删行；`item.type==='object'` 时可折叠并显示 `summaryFields` 摘要 |
| `SchemaObjectField.vue` | 递归子表单 + presence 开关 |
| `SchemaReferenceField.vue` | 引用选择（下拉/弹窗），支持 `prefix`/`allowInvert`，选项经 props 注入 |
| `SchemaAdvancedSection.vue` | 高级折叠（有已配置的 advanced 字段时默认展开） |

字段文档：`description`/`docs` 用 AntD `Tooltip`/`Popover`（ⓘ）呈现。

## 6. 引用字段与数据源

- `SchemaReferenceField` 只负责渲染与取值，**不拉数据**：接收 `options: FieldOption[]` 与 `allowInvert`。
- 选项来源由调用方（`modules/*` 或 `app/composables`）从既有 API 加载：
  - `reference.types: ['domain']` → `/api/v1/domains`
  - `['fragment']` → `/api/v1/gateway/fragments`
  - `['credential']` → `/api/v1/credentials`
  - `['app']` → `/api/v1/gateway/apps`
- L1 用一个 `referenceOptionsLoader` 注册表（`app/composables/useReferenceOptions.ts`）按 type 懒加载；未知类型回退为自由文本输入。

## 7. 双模编辑器（仅限有文本源的事实）

ADR-041 §10：**只有有文本源的事实**做 Form ↔ YAML 双模。

- `uiHint` 注册表（前端）声明 `dualMode`：
  - `fragment-code` → 双模（Caddyfile 文本，复用 `components/CodeEditor.vue` `language="json"` 兜底，后续可加 caddyfile 高亮）
  - `compose-editor` → 双模（已有 `ComposeEditorModal.vue` 经验）
  - 其余（`gateway-service` 等）→ **纯 Form + 只读预览**（不发明第二真相源）
- L1 **不迁移** compose/fragment 的现有编辑器，只提供通用 `SchemaYamlEditor` 基建供后续用。

## 8. uiHint 注册表与域定制

```ts
// app/components/schema/registry.ts
const uiHints: Record<string, { component?: Component; dualMode?: boolean }> = {
  'gateway-service': { /* component: 域定制（P3） */ },
  'fragment-code':   { dualMode: true },
  'compose-editor':  { dualMode: true },
}
```

- 命中且注册了 `component` → 用域组件；否则回退 `SchemaForm`。
- 未知 `uiHint` 安全回退（不报错）。

## 9. 数据获取

`app/composables/useFactSchema.ts`：

```ts
export function useFactSchema(factKind: string, provider?: string) {
  // GET /api/v1/schema/{factKind}[?provider=]
  // 返回 { schema, loading, error }
}
```

`SchemaForm` 接收 `schema` + `modelValue`，通过 `update:modelValue` 回传**序列化后的配置对象**；保存仍走各域既有 REST（U3 §1）。

## 10. 工具链（建议）

现状：无测试框架、无 typecheck 脚本。建议为纯逻辑引擎补最小基建：

- 新增 devDeps：`vitest` + `vue-tsc`；scripts：`"test": "vitest run"`、`"typecheck": "vue-tsc --noEmit -p tsconfig.v4.json"`。
- 门禁纳入：`pnpm typecheck`；U4 的 `lib/schema` 纯函数必须有单测。
- **实现偏差**：`tsconfig.v4.json` 把 typecheck 限定到 V4 分层（`lib/app/modules/api/shared/design`），存量 `src/views` 的既存类型错误（table record 断言、`appName` 缺字段等）留待 P3 迁移时修复，避免本单元触碰旧码引入回归。

> 若暂不引入 vitest，则 U4 验收降级为「typecheck + 人工验证 variable 闭环」，但引擎无自动化测试，风险自担。

## 11. 测试与验收

- `lib/schema`（vitest）：
  - `createFormValues` / `serializeFormValues` 往返（含数组 id 剥离、object presence、默认值不物化）。
  - `validate`：required / number / select 越界 / 递归。
  - reference 前缀 `$` 与取反 `!$` 的取值/回填。
- 手动闭环：**variable 事实**（`key`+`value`）——`/schema/variable` → `SchemaForm` → 保存 → 重载回显。
- 门禁：`pnpm lint` / `pnpm lint:style` /（新增）`pnpm typecheck` /（新增）`pnpm test` / `pnpm build`；亮暗双主题人工验证。

## 12. 已定决策

1. **引入 `vitest + vue-tsc`**：补齐纯逻辑测试与 typecheck 门禁（`pnpm test` / `pnpm typecheck`）。
2. **`SchemaArrayField` 做 `summaryFields` 折叠摘要**（数组项高频场景）。
3. **引用选择器先下拉**（`Select showSearch`），弹窗搜索后置。
4. **`lib/schema` 为纯函数**，Vue 响应式留在 `app` 组件。
