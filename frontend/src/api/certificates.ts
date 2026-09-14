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