// Package models 定义领域实体。
package model

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

// 代理类型(ADR-018:收敛为 reverse_proxy/file_server,tcp_stream/L4 后置)。
const (
	RouteTypeReverseProxy = "reverse_proxy"
	RouteTypeFileServer   = "file_server"
)

// 域名行传输协议(ADR-018 修订:支持 https/http,L4 tcp/udp 后置)。
const (
	DomainProtoHTTPS = "https"
	DomainProtoHTTP  = "http"
)

// App 应用分租实体(gateway 三层模型的顶层,ADR-018 修订)。
// 纯分组,不绑定 rootDomain;一个 App 下 0..N 个 Service。
type App struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ProxyDomain Service 内联的一条域名行(不建实体)。
// 完整访问域名 = <subdomain>.<rootDomain>;协议决定走 https(签证书)或 http(内网);
// customPort 开则用 Port,关则用协议默认端口(https=443 / http=80)。
type ProxyDomain struct {
	Protocol   string `json:"protocol"`   // https | http
	Subdomain  string `json:"subdomain"`  // 二级域名,可空(空则用 rootDomain 主域)
	RootDomain string `json:"rootDomain"` // 来自域名管理
	CustomPort bool   `json:"customPort"`
	Port       int    `json:"port,omitempty"` // 自定义端口,1~65535
}

// Host 计算完整访问域名(一个 site 地址,供生成器与展示)。
// RootDomain 为空时(如 docker 派生行),Subdomain 即完整域名原样返回。
func (d ProxyDomain) Host() string {
	if d.RootDomain == "" {
		return d.Subdomain
	}
	if d.Subdomain == "" {
		return d.RootDomain
	}
	return d.Subdomain + "." + d.RootDomain
}

// Service 服务(原 ProxyRoute,ADR-018 修订:更名为 Service,host→domains[])。
// 仅 source=manual 落库;Docker 自动为运行时派生 Service、不落库。
// AppID 可与应用弱绑定:空 = 归属「默认」(ADR-018 修订)。
type Service struct {
	ID          string        `json:"id"`
	AppID       string        `json:"appId,omitempty"` // FK→App;空=默认归属
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Type        string        `json:"type"` // reverse_proxy | file_server
	Domains     []ProxyDomain `json:"domains"`
	// Upstream 后端 "地址:端口";反代用;按 protocol/addr/port 组合为单元素(数组化向后兼容)。
	Upstream []string `json:"upstream,omitempty"`
	// UpstreamProto 反代目标协议:http(默认)|https(https 走 caddy tls transport 到后端)。
	UpstreamProto string `json:"upstreamProto,omitempty"`
	Root          string `json:"root,omitempty"` // file_server 静态根目录
	// Browse file_server 是否显示文件列表(开则加 browse)。
	Browse bool `json:"browse,omitempty"`
	// HealthURI active health check 探测路径,默认 "/",空=关闭(ADR-020)。
	HealthURI   string   `json:"healthUri,omitempty"`
	Enabled     bool     `json:"enabled"`
	FragmentIDs []string `json:"fragmentIds,omitempty"` // 显式引用的 Caddy 片段(ADR-018 修订)
	// ExcludeFragmentIDs 在「其他选项」中被用户取消勾选的默认启用片段
	// (生成时从「默认启用全集」扣减,ADR-018 修订 Q8/Q3)。
	ExcludeFragmentIDs []string `json:"excludeFragmentIds,omitempty"`
	// ContainerState 仅 docker 派生 Service 使用(docker label 真相源,ADR-016/019):
	// 非 running 容器的派生行保留展示但 Enabled=false、不生成 site block
	// (网关「异常保留」,gateway.md §9.3)。manual 服务恒为空。
	ContainerState string `json:"containerState,omitempty"`
	// ExtraDirectives 仅 docker 派生 Service 使用:配自 caddy.* 逃生舱子 label
	// 的透传指令(ADR-026 §4),不落库;生成时在片段之后、受控反代之前拼装。
	// 空则恒为未设置(不参与 JSON)。manual 服务恒为空。
	ExtraDirectives []string `json:"extraDirectives,omitempty"`
	// DerivedWarning 仅 docker 派生 Service:派生告警(多宿主端口未标注 / 片段缺失 /
	// 全局级 label 忽略等,ADR-026),单行文本,展示于网关分组行;不落库。
	DerivedWarning string    `json:"derivedWarning,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// 内置 Caddy 片段 ID(只读清单,不落库为可编辑实例,ADR-018 修订 Q7)。
const (
	FragmentGzip        = "frag-gzip"
	FragmentBasicAuth   = "frag-basic-auth"
	FragmentSkipVerify  = "frag-skip-verify"
	FragmentHealthcheck = "frag-healthcheck"
	FragmentLogConfig   = "frag-log-config"
	FragmentBlockCommon = "frag-block-common"
	FragmentStaticCache = "frag-static-cache"
	FragmentWebsocket   = "frag-websocket"
)

// 注:FragmentTagHandler / FragmentTagRoute 已随 tag 字段移除(2026-09-07)。

// BuiltinFragmentToggle 内置片段的默认开关用户覆盖。
// 内置片段 body 不可改,但「默认启用/默认隐藏」允许用户自定并持久化(ADR-018 修订 Q7)。
type BuiltinFragmentToggle struct {
	DefaultEnabled bool `json:"defaultEnabled"`
	DefaultHidden  bool `json:"defaultHidden"`
}

// Fragment Template 内置片段的固定描述(只读)。
type FragmentTemplate struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	DefaultEnabled bool   `json:"defaultEnabled"`
	DefaultHidden  bool   `json:"defaultHidden"`
	// Code 是示例/默认 Caddyfile 片段体(可含 <%VAR%>),内置条目不可改。
	Code string `json:"code"`
}

// Fragment 实例(docs/gateway.md §2,ADR-018 修订 §4)。
// code 是遵循 caddy 规范的片段体(可含 <%VAR%> 变量),整体加密存储(可能含密钥)。
// 注:原 tag(handler|route) 字段已于 2026-09-07 移除——生成器按整块塞入 site block,
// 无 handler/route 分层语义;历史 JSON 中的 tag 键自动忽略。
type Fragment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// DefaultEnabled 默认关;开则在该 service 的「其他选项」里默认勾选(ADR-018 修订 Q8)。
	DefaultEnabled bool `json:"defaultEnabled"`
	// DefaultHidden 默认关;开则在「其他选项」中不显示(但随默认勾选生效)。
	DefaultHidden bool      `json:"defaultHidden"`
	Code          string    `json:"code"`
	CreatedAt     time.Time `json:"createdAt"`
}

// Variable 用户自定义 Caddy 片段变量(ADR-018 修订 §5.1)。
// key 不得以 GB_ 开头(保留前缀);引用写法 <%KEY%>。
type Variable struct {
	Key         string    `json:"key"` // 主键
	Value       string    `json:"value"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

// PortBinding 对外协议端口记录(网关 → 端口,ADR-026 多端口)。
// Protocol 是主键(如 "http"/"https"/"mysql")。
// HTTP/HTTPS 为系统内置项(Builtin=true,不可删除),其 Ports 驱动 caddy 监听
// (HTTP 取首端口作 http_port;HTTPS 取首端口作 https_port、后续为额外 https 端口)。
// 其它协议暂为管理态(L4 代理后置),记录并展示。
type PortBinding struct {
	Protocol    string    `json:"protocol"` // 主键,小写字母/数字
	Description string    `json:"description"`
	DefaultPort int       `json:"defaultPort"` // 协议约定默认端口(展示/新建提示)
	Ports       []int     `json:"ports"`       // 实际监听/使用端口(可多个)
	Enabled     bool      `json:"enabled"`     // 停用=false
	Builtin     bool      `json:"builtin"`     // 系统默认项(HTTP/HTTPS),不可删除
	CreatedAt   time.Time `json:"createdAt"`
}

// CertLog 证书操作日志(SSL 证书 → 日志)。
// LogFile 指向本次申请时 acme.sh 的完整原始输出文件(可 debug)。
type CertLog struct {
	ID        string    `json:"id"`
	FQDN      string    `json:"fqdn"`
	Action    string    `json:"action"` // ensure | renew | delete
	Status    string    `json:"status"` // success | fail
	Message   string    `json:"message"`
	LogFile   string    `json:"logFile,omitempty"` // acme.sh 原始输出文件路径
	CreatedAt time.Time `json:"createdAt"`
}
