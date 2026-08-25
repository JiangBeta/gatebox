// Package models 定义领域实体。
package models

import "time"

// DNS 供应商枚举(与 ADR-003/006 一致)
const (
	ProviderCloudflare = "cloudflare"
	ProviderDNSPod     = "dnspod" // dnspod.cn 国内版
	ProviderAliyun     = "aliyun"
)

// DNSCredential DNS 凭证:统一管理、加密存储、双向下发。
type DNSCredential struct {
	ID        string            `json:"id"`
	Provider  string            `json:"provider"` // cloudflare | dnspod | aliyun
	Name      string            `json:"name"`
	Fields    map[string]string `json:"fields"` // 供应商特定字段(token / id+token / accessKeyId+secret)
	CreatedAt time.Time         `json:"createdAt"`
}

// Domain 根域名(rootDomain),域名管理的基本单位。
type Domain struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"` // rootDomain,如 neob.cn
	CredentialID string    `json:"credentialId"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Cert 证书(只读,来自证书管理器)。
type Cert struct {
	FQDN      string    `json:"fqdn"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	Serial    string    `json:"serial"`
	KeyAlgo   string    `json:"keyAlgo"`
}
