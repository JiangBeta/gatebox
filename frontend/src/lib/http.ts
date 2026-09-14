/**
 * HTTP 基础设施（业务无关）。统一 baseURL、超时与错误归一化。
 *
 * 约定（docs/architecture.md §10）：错误响应体 {"error":{"code","message"}}。
 */
import axios, { type AxiosInstance } from 'axios'

const http: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

http.interceptors.response.use(
  (res) => res,
  (err) => {
    const body = err?.response?.data
    const message = body?.error?.message || err?.message || '请求失败'
    const code = body?.error?.code || 'UNKNOWN'
    const e = new Error(message) as Error & { code?: string; status?: number }
    e.code = code
    e.status = err?.response?.status
    return Promise.reject(e)
  },
)

export default http
