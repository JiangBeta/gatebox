/**
 * ESLint 扁平配置 —— 前端分层边界门禁（ADR-032 / docs/architecture.md §9.4）。
 *
 * 依赖方向：modules → app → lib；shared 单向。
 * 违反层级 import 由 `no-restricted-imports` 报错。
 */
import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'
import globals from 'globals'

export default [
  { ignores: ['dist/**', 'node_modules/**'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: { parser: tseslint.parser },
    },
  },
  {
    languageOptions: {
      globals: { ...globals.browser },
    },
    rules: {
      'vue/multi-word-component-names': 'off',
    },
  },
  // 基础层（design/lib/shared）不得依赖业务层
  {
    files: ['src/design/**/*.{ts,vue}', 'src/lib/**/*.{ts,vue}', 'src/shared/**/*.{ts,vue}'],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [
          { group: ['@/app/*'], message: '基础层不得依赖 app 层（docs/architecture.md §9.4）' },
          { group: ['@/modules/*'], message: '基础层不得依赖 modules 层（docs/architecture.md §9.4）' },
        ],
      }],
    },
  },
  // 模块之间不得直接引用彼此的 views
  {
    files: ['src/modules/**/*.{ts,vue}'],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [
          {
            group: ['@/modules/*/views', '@/modules/*/views/*'],
            message: '模块之间不得直接引用 views，请经 lib/shared 或 app（docs/architecture.md §9.4）',
          },
        ],
      }],
    },
  },
]
