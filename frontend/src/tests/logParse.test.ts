/**
 * 日志对账测试（parseLog / pickSequence）。
 *
 * 重点测三件事：
 *   1. marker 顺序匹配：后面的步骤不能匹配到更靠前的日志行（cursor 单进）。
 *   2. 未命中的 optional 步记「跳过」而不是失败。
 *   3. 耗时按相邻命中时间差算；无时间戳时给 0（不编数字）。
 */
import { describe, expect, it } from 'vitest'
import { parseLog, pickSequence } from '../lib/schema/logParse'
import type { Sequence } from '../api/objects'

const TPL = [
  { no: 1, step: '注册账户', marker: 'Registering account', optional: true },
  { no: 2, step: '选择 CA', marker: 'Using CA' },
  { no: 3, step: '下载证书', marker: 'Cert success.' },
]

const LOG = [
  '[2026-09-22 15:00:00] Registering account',
  '[2026-09-22 15:00:01] Using CA: LetsEncrypt',
  '[2026-09-22 15:00:08] Cert success.',
  '[2026-09-22 15:00:09] Reload successful',
].join('\n')

describe('parseLog', () => {
  it('按 marker 顺序命中并算出耗时', () => {
    const steps = parseLog(LOG, TPL)
    expect(steps).toHaveLength(3)
    expect(steps.map((s) => s.state)).toEqual(['success', 'success', 'success'])
    expect(steps[0].at).toBe('15:00:00')
    // 第 1 步到第 2 步 1 秒；最后一步没有「下一步」，耗时 0
    expect(steps[0].ms).toBe(1000)
    expect(steps[1].ms).toBe(7000)
    expect(steps[2].ms).toBe(0)
  })

  it('未命中的 optional 步记 skipped，缺时间戳时耗时不编造', () => {
    const steps = parseLog('[2026-09-22 15:00:00] Cert success.', TPL)
    expect(steps.map((s) => s.state)).toEqual(['skipped', 'skipped', 'success'])
    expect(steps[2].ms).toBe(0)
    expect(steps[0].at).toBe('—')
  })

  it('cursor 单进：先出现的 marker 不会被后面的步骤重复吃掉', () => {
    const tpl = [
      { no: 1, step: 'A', marker: 'same' },
      { no: 2, step: 'B', marker: 'same' },
    ]
    const steps = parseLog('same\nsame', tpl)
    expect(steps[0].state).toBe('success')
    expect(steps[1].state).toBe('success')
  })

  it('空日志 → 全部 skipped（不返回空数组，界面据此说明「未采集」）', () => {
    const steps = parseLog('', TPL)
    expect(steps).toHaveLength(3)
    expect(steps.every((s) => s.state === 'skipped')).toBe(true)
  })

  it('模板为空 → 空步骤数组', () => {
    expect(parseLog(LOG, undefined)).toEqual([])
  })
})

describe('pickSequence', () => {
  const seqs: Sequence[] = [
    { id: 'acme_issue', label: '证书签发（首次）', steps: TPL },
    { id: 'acme_renew', label: '证书续期', steps: TPL.slice(1) },
  ]

  it('renew 动作挑续期模板', () => {
    expect(pickSequence(seqs, 'renew')?.id).toBe('acme_renew')
  })

  it('ensure / 未知动作挑签发模板', () => {
    expect(pickSequence(seqs, 'ensure')?.id).toBe('acme_issue')
    expect(pickSequence(seqs, undefined)?.id).toBe('acme_issue')
  })

  it('认不出来返回 null（界面据此说明「未知序列」，不硬套）', () => {
    expect(pickSequence(undefined, 'renew')).toBeNull()
    expect(pickSequence([], 'renew')).toBeNull()
  })
})