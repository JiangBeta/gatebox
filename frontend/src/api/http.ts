import axios from 'axios'

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

http.interceptors.response.use(
  (resp) => resp,
  (err) => {
    const raw = err?.response?.data?.error
    const msg = typeof raw === 'string' ? raw : raw?.message || err.message || '请求失败'
    return Promise.reject(new Error(msg))
  },
)

export default http
