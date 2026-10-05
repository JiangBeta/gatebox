import axios from 'axios'

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

/**
 * 后端错误。后端统一返回 {error, code, ...}，页面要按 code 分支
 * （如删除被引用 → 409 IN_USE → 弹「连带删除」确认），
 * 所以不能只把 error 变成裸 Error，否则 code 和 refs 全丢了。
 */
export class ApiError extends Error {
  /** HTTP 状态码（网络错误时为 undefined）。 */
  readonly status?: number
  /** 业务错误码：IN_USE / KEY_CHANGE_REQUIRES_RENAME / VALIDATION_FAILED … */
  readonly code?: string
  /** 完整响应体（details / refs 等附加信息都在这）。 */
  readonly body?: Record<string, unknown>

  constructor(message: string, opts: { status?: number; code?: string; body?: Record<string, unknown> } = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = opts.status
    this.code = opts.code
    this.body = opts.body
  }
}

http.interceptors.response.use(
  (resp) => resp,
  (err) => {
    const url = String(err?.config?.url || '')
    // 会话失效：跳登录页（登录/状态接口本身除外）。
    if (err?.response?.status === 401 && !url.startsWith('/auth/')) {
      if (!window.location.hash.startsWith('#/login')) window.location.hash = '#/login'
    }
    const body = (err?.response?.data ?? undefined) as Record<string, unknown> | undefined
    const raw = body?.error
    const msg =
      (typeof raw === 'string' ? raw : (raw as { message?: string } | undefined)?.message) ||
      err.message ||
      '请求失败'
    const code = typeof body?.code === 'string' ? body.code : undefined
    return Promise.reject(
      new ApiError(String(msg), { status: err?.response?.status, code, body }),
    )
  },
)

export default http
