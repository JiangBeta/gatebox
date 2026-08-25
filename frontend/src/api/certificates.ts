import http from './http'

export interface Cert {
  fqdn: string
  issuer: string
  notBefore: string
  notAfter: string
  serial: string
  keyAlgo: string
}

export const listCertificates = () => http.get<Cert[]>('/certificates').then((r) => r.data)
