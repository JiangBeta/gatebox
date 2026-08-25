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

// Registry 私有镜像仓库凭证(docs §3.3.1)。
//
// 整体 AES-GCM 加密存储,复用 DNSCredential 的方案(ADR-003)。
// Secret 是明文密码,仅在落库结构里存在;下发前端前由 API 层剥掉,
// 只回传 HasSecret。凭证不写 ~/.docker/config.json,拉取时走
// Engine API 原生的 X-Registry-Auth 头(docs §3.3.1)。
type Registry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`    // registry 地址,如 registry.example.com
	Scheme    string    `json:"scheme"` // http | https
	Username  string    `json:"username"`
	Secret    string    `json:"secret,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ComposeInstance 编排项目(docs §2.2)。主键是 projectName,不引入随机 ID。
type ComposeInstance struct {
	ProjectName string `json:"projectName"` // 主键,[a-z0-9][a-z0-9_-]{1,62},不可变
	DisplayName string `json:"displayName"` // 展示名,可随意改
	HostID      string `json:"hostID"`      // 多主机预留,默认 "local"
	Managed     bool   `json:"managed"`     // 是否 GateBox 托管(appData 下)

	// ConfigFiles 是 compose 文件绝对路径,逗号分隔多文件(docs §4.2)。
	// 多文件或用了 include:/extends: 的项目 editable=false,只读。
	ConfigFiles string `json:"configFiles"`
	Editable    bool   `json:"editable"`

	// LastDeployedYAML 是纯数据不是机制(docs §4.1 Q16'):up 成功后覆盖写入,
	// UI 给一个用户主动点击的「恢复」按钮。明文存储(无敏感信息)。
	LastDeployedYAML string    `json:"lastDeployedYAML,omitempty"`
	LastDeployedAt   time.Time `json:"lastDeployedAt,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}

// ValidateProjectName 校验 projectName 是否符合 docker compose 硬约束:
// 首字符 [a-z0-9],后续 [a-z0-9_-],总长 1~63(docs §2.2 Q12)。
// 它是 docker 项目标识、磁盘目录名、Caddyfile 引用名,格式必须在这里守住。
func ValidateProjectName(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	c := s[0]
	if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
