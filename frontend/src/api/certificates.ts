import http from './http'

export interface Cert {
  fqdn: string
  issuer: string
  notBefore: string
  notAfter: string
  serial: string
  keyAlgo: string
}

export interface CertDetail extends Cert {
  publicKey: string
  privateKey: string
}

export const listCertificates = () => http.get<Cert[]>('/certificates').then((r) => r.data)

export const getCertDetail = (fqdn: string) => http.get<CertDetail>(`/certificates/${encodeURIComponent(fqdn)}`).then((r) => r.data)

export const renewCert = (fqdn: string) => http.post(`/certificates/${encodeURIComponent(fqdn)}/renew`, null, { timeout: 300000 }).then((r) => r.data)

export const deleteCert = (fqdn: string) => http.delete(`/certificates/${encodeURIComponent(fqdn)}`).then((r) => r.data)

export interface CertLog {
  id: string
  fqdn: string
  action: string
  status: string
  message: string
  logFile?: string
  createdAt: string
}

export const listCertLogs = (fqdn?: string) =>
  http.get<CertLog[]>('/certificates/logs', fqdn ? { params: { fqdn } } : undefined).then((r) => r.data)

export const getCertLog = (id: string) => http.get<{ log: string }>(`/certificates/logs/${encodeURIComponent(id)}`).then((r) => r.data)

// --- SSL 证书页:二级域名汇总 + ACME 注册邮箱 ---

/** 二级域名来源(Caddy 手动服务 / Docker 编排派生)。 */
export interface SubdomainSource {
  type: 'caddy' | 'docker'
  /** Caddy: 服务 ID(打开编辑抽屉用)。 */
  serviceId?: string
  serviceName?: string
  /** Caddy: 所属应用名。 */
  appName?: string
  /** Docker: 编排项目名(打开编辑抽屉用)。 */
  projectName?: string
  /** Docker: 项目展示名。 */
  displayName?: string
}

/** 一个受管二级域名(可能尚无证书)。 */
export interface SubdomainCert {
  fqdn: string
  subdomain?: string
  rootDomain?: string
  protocol?: string
  sources: SubdomainSource[]
  hasCert: boolean
  issuer?: string
  notBefore?: string
  notAfter?: string
  serial?: string
  keyAlgo?: string
}

/** 所有受管二级域名(manual + docker 派生),附已有证书信息。 */
export const listSubdomains = () => http.get<SubdomainCert[]>('/certificates/subdomains').then((r) => r.data)

export const getACMEEmail = () => http.get<{ email: string }>('/certificates/email').then((r) => r.data)

export const setACMEEmail = (email: string) =>
  http.put<{ email: string; note: string }>('/certificates/email', { email }).then((r) => r.data)