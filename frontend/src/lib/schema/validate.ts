import type { ConfigField, ConfigFieldChild } from './types'
import { OBJECT_PRESENT, type FormValue } from './values'

function isEmpty(v: unknown): boolean {
  return v === '' || v === undefined || v === null
}

/** 校验表单值，返回错误文案列表（空数组 = 通过）。 */
export function validateFormValues(
  fields: ConfigField[],
  values: Record<string, FormValue>,
): string[] {
  const errs: string[] = []
  for (const f of fields) validateField(f, values[f.key], errs)
  return errs
}

function validateField(f: ConfigFieldChild, v: FormValue, errs: string[]): void {
  switch (f.type) {
    case 'array': {
      const arr = Array.isArray(v) ? v : []
      if (f.required) {
        const nonEmpty = arr.some((it) => !isEmpty((it as { value: unknown }).value))
        if (!nonEmpty) errs.push(`${f.label} 必填`)
      }
      if (f.item) {
        for (const it of arr) validateField(f.item, (it as { value: FormValue }).value, errs)
      }
      return
    }
    case 'object': {
      const obj = (v ?? {}) as Record<string, unknown>
      if (f.required && obj[OBJECT_PRESENT] !== true) {
        errs.push(`${f.label} 未启用`)
      }
      for (const c of f.fields ?? []) validateField(c, obj[c.key], errs)
      return
    }
    case 'number': {
      if (f.required && isEmpty(v)) errs.push(`${f.label} 必填`)
      else if (!isEmpty(v) && Number.isNaN(Number(v))) errs.push(`${f.label} 必须是数字`)
      return
    }
    default:
      if (f.required && isEmpty(v)) errs.push(`${f.label} 必填`)
  }
}
