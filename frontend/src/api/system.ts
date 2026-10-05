/**
 * 控制面自身信息（顶栏「升级 / 重启」）。
 *
 * 刻意没有 latestVersion / upgradeNotes：升级通道本轮不开放（ADR-043 §6），
 * 没有可信版本源就不编数字——那会让「有新版本」角标变成永远为假的装饰。
 */
import http from './http'

export interface SystemInfo {
  version: string
  caddyVersion?: string
  online: boolean
  /** docker 不可达时后端给 null，前端据此显示「不可达」而不是「0 个容器」。 */
  containers: { total: number; running: number } | null
  tasks: { running: number }
  restart: { graceful: boolean; estimate: string }
}

export async function getSystem(): Promise<SystemInfo> {
  const { data } = await http.get<SystemInfo>('/system')
  return data
}