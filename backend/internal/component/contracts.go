package component

// 本文件定义 v4 的「功能 + 四契约」（ADR-040 / ADR-041 §3）。
//
//	配置契约 ConfigField   → 编辑（Form/YAML）
//	信息契约 InfoPort      → 功能地图与信息流
//	观测契约 Observability → 可观测
//	生效契约 InfoPort.Effect → 声明式调和
//
// 注意：这些类型只在 /api/v1/descriptors 等新端点使用；Descriptor 本身不加 json
// tag，以保持既有 /api/v1/components 输出（前端读 PascalCase）零回归。

// Function 组件对外提供的一项能力（功能地图的语义节点）。
type Function struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

// InfoType 信息类型（在组件间流转的数据类别）。开放字符串，未知类型不报错。
type InfoType string

const (
	InfoService     InfoType = "service"    // 代理服务定义
	InfoDomain      InfoType = "domain"     // 域名行 / 根域
	InfoCredential  InfoType = "credential" // DNS 凭证
	InfoFragment    InfoType = "fragment"   // Caddy 片段
	InfoVariable    InfoType = "variable"   // 统一变量
	InfoPortBinding InfoType = "port"       // 协议端口
	InfoLabel       InfoType = "label"      // docker label（运行信息）
	InfoCert        InfoType = "cert"       // 证书文件（运行信息）
	InfoIP          InfoType = "ip"         // 解析出的 IP（运行信息）
)

// EffectAction 生效动作（枚举）。
type EffectAction string

const (
	EffectUpdateConfig EffectAction = "update-config" // 重写配置
	EffectHotReload    EffectAction = "hot-reload"    // 热重载（不中断）
	EffectRestart      EffectAction = "restart"       // 重启
	EffectReissue      EffectAction = "reissue"       // 重新签发
	EffectDeploy       EffectAction = "deploy"        // 部署（compose up 等）
)

// ValidEffect 判断生效动作是否为已知枚举。
func ValidEffect(a EffectAction) bool {
	switch a {
	case EffectUpdateConfig, EffectHotReload, EffectRestart, EffectReissue, EffectDeploy:
		return true
	}
	return false
}

// InfoPort 信息契约：消费或产出某类信息。
//   - Consumes 时 Effect 表示「收到该类信息变更时的自动动作」；
//   - Produces 时 Effect 为空。
type InfoPort struct {
	Info   InfoType       `json:"info"`
	Effect []EffectAction `json:"effect,omitempty"`
}

// ConfigType 配置字段类型（精简起步，ADR-041 §10）。
type ConfigType string

const (
	ConfigText      ConfigType = "text"
	ConfigPassword  ConfigType = "password"
	ConfigNumber    ConfigType = "number"
	ConfigSelect    ConfigType = "select"
	ConfigSwitch    ConfigType = "switch"
	ConfigTextarea  ConfigType = "textarea"
	ConfigArray     ConfigType = "array"
	ConfigObject    ConfigType = "object"
	ConfigReference ConfigType = "reference"
)

// FieldOption select 类型的一个选项。
type FieldOption struct {
	Label string `json:"label"`
	Value any    `json:"value"`
}

// ReferenceSpec reference 类型字段的引用语义。
type ReferenceSpec struct {
	Types       []string `json:"types,omitempty"`       // 可引用的事实/组件类别
	Prefix      string   `json:"prefix,omitempty"`      // "$" | ""
	AllowInvert bool     `json:"allowInvert,omitempty"` // 允许 ! 取反
}

// ConfigField 配置契约：可编辑字段（类型 / 校验 / 文档）。
type ConfigField struct {
	Key           string         `json:"key"`
	Label         string         `json:"label"`
	Type          ConfigType     `json:"type"`
	Required      bool           `json:"required,omitempty"`
	Default       any            `json:"default,omitempty"`
	Advanced      bool           `json:"advanced,omitempty"`
	Placeholder   string         `json:"placeholder,omitempty"`
	Description   string         `json:"description,omitempty"`
	Docs          string         `json:"docs,omitempty"`
	Options       []FieldOption  `json:"options,omitempty"`       // select
	Reference     *ReferenceSpec `json:"reference,omitempty"`     // reference
	Item          *ConfigField   `json:"item,omitempty"`          // array 元素
	Fields        []ConfigField  `json:"fields,omitempty"`        // object
	SummaryFields []string       `json:"summaryFields,omitempty"` // 数组项折叠摘要
}

// MetricSpec 观测契约中的定量指标。
type MetricSpec struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Unit  string `json:"unit,omitempty"`
}

// Observability 观测契约。
type Observability struct {
	State    bool         `json:"state"`
	Activity bool         `json:"activity"`
	Logs     bool         `json:"logs"`
	Metrics  []MetricSpec `json:"metrics,omitempty"`
}
