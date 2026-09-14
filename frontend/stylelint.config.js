/**
 * Stylelint 配置 —— 禁止样式字面量，强制使用 design token（ADR-032）。
 *
 * 规则：modules 下的 .vue 中，颜色/字号/圆角/间距等属性禁止写 #hex、rgb()、数字 px；
 * 一律引用 @/design/tokens。
 */
export default {
  extends: ['stylelint-config-recommended-vue'],
  // 基础规则：stylelint 要求配置非空（stylelint-config-recommended-vue 只在 overrides 里给规则）。
  rules: {
    'color-no-invalid-hex': true,
  },
  overrides: [
    {
      files: ['src/modules/**/*.vue'],
      rules: {
        'declaration-property-value-disallowed-list': {
          '/^(color|background|background-color|border-color|font-size|font-family|border-radius|margin|margin-top|margin-bottom|padding|gap)$/':
            [/^#/, /^rgb/, /^\d+px$/],
        },
      },
    },
  ],
}
