import http from './http'

export interface Domain {
  id: string
  name: string
  credentialId: string
  createdAt: string
}

export interface DomainOverview {
  id: string
  name: string
  credentialId: string
  certStatus: 'success' | 'expiring' | 'expired' | 'unissued'
  subdomainCount: number
  createdAt: string
  lastIssuedAt: string
}

export interface OverviewResponse {
  domainCount: number
  credentialCount: number
  providerCount: number
  certTotal: number
  certExpiringSoon: number
  certExpired: number
  domains: DomainOverview[]
}

export const listDomains = () => http.get<Domain[]>('/domains').then((r) => r.data)
export const createDomain = (d: { name: string; credentialId: string }) =>
  http.post<Domain>('/domains', d).then((r) => r.data)
export const updateDomain = (id: string, d: { name: string; credentialId: string }) =>
  http.put<Domain>(`/domains/${id}`, d).then((r) => r.data)
export const deleteDomain = (id: string) => http.delete(`/domains/${id}`)
export const overview = () => http.get<OverviewResponse>('/domains/overview').then((r) => r.data)
