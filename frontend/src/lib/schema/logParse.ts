/**
 * 日志对账：按序列模板的 marker 顺序扫原始日志 → 实际步骤。
 *
 * 为什么要「对账」而不是直接解析日志结构：插件（acme.sh 等）的输出格式不受控，
 * 但**步骤序列**由 model/log.yaml 声明式给出（marker + optional）。
 * 顺序扫一遍就能得到「哪一步在什么时候发生、耗时多久、哪些 optional 步骤没走」。
 *
 * 与原型 parseLog 同算法：cursor 只前进不后退，保证步骤与日志行一一对应。
 */

import type { Sequence, SequenceStep } from '@/api/objects'

/** 对账出的一步。state 只有 success / skipped——命中与否，没有第三种。 */
export interface ParsedStep {
  no: number
  step: string
  marker: string
  optional: boolean
  /** 命中行里的时间戳（HH:MM:SS）；未命中为 ''。 */
  at: string
  state: 'success' | 'skipped'
  /** 距下一步的耗时（ms）；下一步未命中时为 0。 */
  ms: number
}

/** 行内时间戳（acme.sh 打 `[2026-09-22 15:00:00]`，这里只取时分秒）。 */
function tsOf(line: string): string {
  const m = line.match(/(\d{2}:\d{2}:\d{2})/)
  return m ? m[1] : ''
}

/** HH:MM:SS → 当日秒数。跨天的日志算不出真实间隔，返回 0（宁可显示 0ms 也不要编一个）。 */
function secOf(t: string): number {
  const p = t.split(':').map(Number)
  return p.length === 3 ? p[0] * 3600 + p[1] * 60 + p[2] : 0
}

/**
 * parseLog 按模板 marker 顺序匹配日志行。
 *
 * text 为空 → 全部记 skipped（界面显示「未采集到原始输出」，而不是空图）。
 */
export function parseLog(text: string, tpl: SequenceStep[] | undefined): ParsedStep[] {
  const steps = tpl || []
  const lines = (text || '').split('\n')
  let cursor = 0
  const hits: Array<{ s: SequenceStep; ok: boolean; at: string }> = []
  for (const s of steps) {
    let idx = -1
    for (let i = cursor; i < lines.length; i++) {
      if (lines[i].includes(s.marker)) {
        idx = i
        break
      }
    }
    if (idx >= 0) {
      hits.push({ s, ok: true, at: tsOf(lines[idx]) })
      cursor = idx + 1
    } else {
      hits.push({ s, ok: false, at: '' })
    }
  }
  return hits.map((hit, i) => {
    const next = hits.slice(i + 1).find((x) => x.ok)
    const ms =
      hit.ok && next && hit.at && next.at
        ? Math.max(0, (secOf(next.at) - secOf(hit.at)) * 1000)
        : 0
    return {
      no: hit.s.no,
      step: hit.s.step,
      marker: hit.s.marker,
      optional: !!hit.s.optional,
      at: hit.at || '—',
      state: hit.ok ? 'success' : 'skipped',
      ms,
    }
  })
}

/**
 * pickSequence 按动作挑序列模板。
 *
 * 首次签发与续期步骤不同（续期复用账户、不等解析），挑错会让一半步骤显示「跳过」。
 * 认不出来时返回 null——界面据此说明「未知序列」，不硬套一个。
 */
export function pickSequence(
  sequences: Sequence[] | undefined,
  action?: string,
): Sequence | null {
  if (!sequences?.length) return null
  const act = (action || '').toLowerCase()
  if (act === 'renew' || act === 'reissue') {
    return sequences.find((s) => s.id === 'acme_renew') || null
  }
  return sequences.find((s) => s.id === 'acme_issue') || sequences[0]
}