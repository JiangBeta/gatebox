import http from './http'

export interface AuthState {
  enabled: boolean
  authed: boolean
}

export const me = () => http.get<AuthState>('/auth/me').then((r) => r.data)
export const login = (password: string) => http.post('/auth/login', { password }).then((r) => r.data)
export const logout = () => http.post('/auth/logout').then((r) => r.data)
