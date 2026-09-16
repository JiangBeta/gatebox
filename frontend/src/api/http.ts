import axios from 'axios'

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

http.interceptors.response.use(
  (resp) => resp,
  (err) => {
    const url = String(err?.config?.url || '')
    // 会话失效：跳登录页（登录/状态接口本身除外）。
    if (err?.response?.status === 401 && !url.startsWith('/auth/')) {
      if (!window.location.hash.startsWith('#/login')) window.location.hash = '#/login'
    }
    const raw = err?.response?.data?.error
    const msg = typeof raw === 'string' ? raw : raw?.message || err.message || '请求失败'
    return Promise.reject(new Error(msg))
  },
)

export default http
