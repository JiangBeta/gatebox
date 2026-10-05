// Package typespec 解析 V4.1 对象模型（docs/v4.1/model/*.yaml）并对外提供类型目录。
//
// 交付方式（ADR-043 §1，裁定 C + A）：
//
//	模型 YAML 经 go:embed 编译进二进制，启动时解析进内存，只读；本轮无写接口。
//	「最小集」= 只装 caddy 内置类型（installed: true）；
//	插件提供的类型在文件里保留但 installed: false，界面置灰 + [安装]，实际安装留批次 2。
//
// 模型文件是设计期唯一真相，编译期副本在 models/ 下；漂移由 TestModelsInSyncWithDocs 守住。
package typespec

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
	"strings"
)

// 字段控件类型（取自 model/*.yaml 的 type 键）。
const (
	FieldText     = "text"
	FieldTextarea = "textarea"
	FieldPassword = "password"
	FieldNumber   = "number"
	FieldSelect   = "select"
	FieldSwitch   = "switch"
	FieldArray    = "array"
	FieldObject   = "object"
	FieldRef      = "reference"
)

// 引用目标的约定值（reference.types）。
//
//	proxy → 后端进程（容器/宿主），不是 V4.1 对象，故不以对象 kind 表达；
//	其余为 model/*.yaml 里的 kind。
const (
	RefProxy = "proxy"
	RefUser  = "user"
)

// Option 下拉/单选项。
type Option struct {
	Label string `yaml:"label" json:"label"`
	Value string `yaml:"value" json:"value"`
}

// UnmarshalYAML 兼容两种写法：`{label: X, value: Y}` 与裸字符串 `Y`。
func (o *Option) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		o.Label, o.Value = s, s
		return nil
	}
	type plain Option // 避免递归
	var p plain
	if err := node.Decode(&p); err != nil {
		return err
	}
	*o = Option(p)
	if o.Label == "" {
		o.Label = o.Value
	}
	return nil
}

// Reference 引用目标约束。
type Reference struct {
	Types []string `yaml:"types" json:"types"`
}

// Field 一个字段契约（schema / writeOnly / status / 类型参数共用同一结构）。
type Field struct {
	Key         string            `yaml:"key" json:"key"`
	Label       string            `yaml:"label" json:"label"`
	Type        string            `yaml:"type" json:"type"`
	Required    bool              `yaml:"required" json:"required,omitempty"`
	Default     any               `yaml:"default" json:"default,omitempty"`
	Placeholder string            `yaml:"placeholder" json:"placeholder,omitempty"`
	Description string            `yaml:"description" json:"description,omitempty"`
	Note        string            `yaml:"note" json:"note,omitempty"`
	Advanced    bool              `yaml:"advanced" json:"advanced,omitempty"`
	Dynamic     bool              `yaml:"dynamic" json:"dynamic,omitempty"`
	Full        bool              `yaml:"full" json:"full,omitempty"`
	Confirm     bool              `yaml:"confirm" json:"confirm,omitempty"`
	Pattern     string            `yaml:"pattern" json:"pattern,omitempty"`
	MinLength   int               `yaml:"minLength" json:"minLength,omitempty"`
	Exclusive   string            `yaml:"exclusive" json:"exclusive,omitempty"`
	Kind        string            `yaml:"kind" json:"kind,omitempty"` // status 字段：observed/derived/linked
	Enum        []string          `yaml:"enum" json:"enum,omitempty"`
	Tone        map[string]string `yaml:"tone" json:"tone,omitempty"`
	Item        *Field            `yaml:"item" json:"item,omitempty"`
	Fields      []Field           `yaml:"fields" json:"fields,omitempty"`
	Options     []Option          `yaml:"options" json:"options,omitempty"`
	Reference   *Reference        `yaml:"reference" json:"reference,omitempty"`
}

// TypeParams 按 type 追加的参数（middleware.yaml 用）。
type TypeParams struct {
	Type   string  `yaml:"type" json:"type"`
	Fields []Field `yaml:"fields" json:"fields,omitempty"`
}

// Spec 一个对象类型的定义（model/<kind>.yaml）。
type Spec struct {
	Kind       string       `yaml:"kind" json:"kind"`
	Title      string       `yaml:"title" json:"title"`
	Group      string       `yaml:"group" json:"group"`
	Doc        string       `yaml:"doc" json:"doc,omitempty"`
	Source     string       `yaml:"-" json:"source,omitempty"` // 模型文件名（编译期填充）
	List       bool         `yaml:"list" json:"list"`
	Readonly   bool         `yaml:"readonly" json:"readonly,omitempty"`
	Singleton  bool         `yaml:"singleton" json:"singleton,omitempty"`
	Key        string       `yaml:"key" json:"key"` // 主键字段键（显式声明或由 Required 推断）
	Schema     []Field      `yaml:"schema" json:"schema"`
	WriteOnly  []Field      `yaml:"writeOnly" json:"writeOnly,omitempty"`
	Status     []Field      `yaml:"status" json:"status,omitempty"`
	TypeParams []TypeParams `yaml:"typeParams" json:"typeParams,omitempty"`
	// Sequences 步骤序列模板（log.yaml）。**不是字段**，是「怎么把原始日志
	// 还原成步骤」的声明：运行时按 marker 顺序匹配日志行 → 实际步骤。
	// 序列不同（签发 11 步 / 续期 8 步）是因为模板不同，不是代码分支。
	Sequences []Sequence `yaml:"sequences" json:"sequences,omitempty"`
	Example   Examples   `yaml:"example" json:"example,omitempty"`
}

// Sequence 一个步骤序列模板（log.yaml 的 sequences 段）。
type Sequence struct {
	ID    string         `yaml:"id" json:"id"`
	Label string         `yaml:"label" json:"label"`
	Steps []SequenceStep `yaml:"steps" json:"steps"`
}

// SequenceStep 模板里的一步。
//
// Marker 是「日志标志」——该步骤在原始日志中的特征行。对账时用它定位，
// 耗时按相邻命中时间差算。Optional 步骤未命中记「跳过」而不是失败。
type SequenceStep struct {
	No       int    `yaml:"no" json:"no"`
	Step     string `yaml:"step" json:"step"`
	Marker   string `yaml:"marker" json:"marker"`
	Optional bool   `yaml:"optional" json:"optional,omitempty"`
}

// Examples 是 model/*.yaml 的 example 段。
// list: true 的类型写成列表，singleton（acme / caddy-global）写成单个对象，两种都接受。
type Examples []ExampleValue

// UnmarshalYAML 兼容「列表」与「单对象」两种写法。
func (e *Examples) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		var list []ExampleValue
		if err := node.Decode(&list); err != nil {
			return err
		}
		*e = list
	case yaml.MappingNode:
		var one ExampleValue
		if err := node.Decode(&one); err != nil {
			return err
		}
		*e = Examples{one}
	default:
		*e = nil
	}
	return nil
}

// ExampleValue 示例条目：源字段平铺 + status 嵌套。
type ExampleValue struct {
	Fields map[string]any `json:"fields"`
	Status map[string]any `json:"status,omitempty"`
}

// UnmarshalYAML 把顶层 status 摘出来，其余键留在 Fields（源字段平铺）。
func (v *ExampleValue) UnmarshalYAML(node *yaml.Node) error {
	var m map[string]any
	if err := node.Decode(&m); err != nil {
		return err
	}
	if st, ok := m["status"].(map[string]any); ok {
		v.Status = st
		delete(m, "status")
	}
	v.Fields = m
	return nil
}

// TypeDef 目录里的一个类型。
type TypeDef struct {
	ID          string  `yaml:"id" json:"id"`
	Label       string  `yaml:"label" json:"label"`
	From        string  `yaml:"from" json:"from"`
	Installed   bool    `yaml:"installed" json:"installed"`
	Builtin     bool    `yaml:"builtin" json:"builtin,omitempty"`
	Description string  `yaml:"description" json:"description,omitempty"`
	Fields      []Field `yaml:"fields" json:"fields"`
}

// Ability 能力项（catalog.abilities）。
type Ability struct {
	ID   string   `yaml:"id" json:"id"`
	From []string `yaml:"from" json:"from"`
}

// Catalog 类型目录（catalog.yaml 的四张表）。
type Catalog struct {
	ServiceTypes        []TypeDef `yaml:"service_types" json:"serviceTypes"`
	MiddlewareTypes     []TypeDef `yaml:"middleware_types" json:"middlewareTypes"`
	EntrypointProtocols []TypeDef `yaml:"entrypoint_protocols" json:"entrypointProtocols"`
	Abilities           []Ability `yaml:"abilities" json:"abilities"`
}

// ServiceType 按 id 查服务类型。
func (c *Catalog) ServiceType(id string) (TypeDef, bool) { return findType(c.ServiceTypes, id) }

// MiddlewareType 按 id 查中间件类型。
func (c *Catalog) MiddlewareType(id string) (TypeDef, bool) { return findType(c.MiddlewareTypes, id) }

// Protocol 按 id 查入口协议。
func (c *Catalog) Protocol(id string) (TypeDef, bool) { return findType(c.EntrypointProtocols, id) }

// Ability 按 id 查能力。
func (c *Catalog) Ability(id string) (Ability, bool) {
	for _, a := range c.Abilities {
		if a.ID == id {
			return a, true
		}
	}
	return Ability{}, false
}

func findType(list []TypeDef, id string) (TypeDef, bool) {
	for _, t := range list {
		if t.ID == id {
			return t, true
		}
	}
	return TypeDef{}, false
}

// --- 对象 kind 常量（model/*.yaml 的 kind 键）---

const (
	KindRoute      = "route"
	KindService    = "service"
	KindMiddleware = "middleware"
	KindEntrypoint = "entrypoint"
	KindDomain     = "domain"
	KindHost       = "host"
	KindUser       = "user"
	KindVariable   = "variable"
	KindCatalog    = "catalog"
)

// --- 展开类型参数 ---

// ExpandType 在基础 schema 上按对象 type 追加该类型的字段。
// catalog 与 middleware.typeParams 同源（catalog 是并集，middleware 保留展开列表）；
// 本函数以 catalog 为准，middleware.typeParams 仅作补充。
func (r *Registry) ExpandType(kind, typeID string) ([]Field, error) {
	spec, ok := r.specs[kind]
	if !ok {
		return nil, fmt.Errorf("typespec: 未知对象类型 %q", kind)
	}
	var td TypeDef
	var found bool
	switch kind {
	case KindService:
		td, found = r.catalog.ServiceType(typeID)
	case KindMiddleware:
		td, found = r.catalog.MiddlewareType(typeID)
	}
	if !found {
		return nil, fmt.Errorf("typespec: %s 没有类型 %q", kind, typeID)
	}
	out := make([]Field, 0, len(spec.Schema)+len(td.Fields))
	out = append(out, spec.Schema...)
	out = append(out, td.Fields...)
	// middleware.yaml 里仍保留 typeParams 展开列表：以 catalog 为准去重。
	for _, tp := range spec.TypeParams {
		if tp.Type != typeID {
			continue
		}
		for _, f := range tp.Fields {
			if !hasField(out, f.Key) {
				out = append(out, f)
			}
		}
	}
	return out, nil
}

func hasField(list []Field, key string) bool {
	for _, f := range list {
		if f.Key == key {
			return true
		}
	}
	return false
}

// EffectiveFields 返回某 kind（可带 type）渲染表单时应展示的字段序列（已扁平化）。
//
// 扁平化理由（ADR-042 §12 不变量 3）：编辑面「源字段平铺」，
// object/array 的子字段用 "父键.子键" 表达，前端一次性渲染成同级控件。
func (r *Registry) EffectiveFields(kind, typeID string) []Field {
	if typeID != "" {
		if f, err := r.ExpandType(kind, typeID); err == nil {
			return Flatten(f)
		}
	}
	return r.SchemaFields(kind)
}

// SchemaFields 返回 spec.schema（含 object/array 递归展开的子字段）。
func (r *Registry) SchemaFields(kind string) []Field {
	spec, ok := r.specs[kind]
	if !ok {
		return nil
	}
	return Flatten(spec.Schema)
}

// Flatten 把嵌套 object/array 的 fields 提到与父字段同级，
// 键名用 "父键.子键" 表达，便于前端一次性渲染成扁平表单（ADR-042 §12「平铺」）。
func Flatten(fields []Field) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		out = append(out, f)
		out = append(out, flattenNested(f.Key, f.Fields)...)
	}
	return out
}

func flattenNested(prefix string, subs []Field) []Field {
	var out []Field
	for _, s := range subs {
		key := s.Key
		if prefix != "" {
			key = prefix + "." + s.Key
		}
		s.Key = key
		out = append(out, s)
		out = append(out, flattenNested(key, s.Fields)...)
	}
	return out
}

// --- 主键 ---

// KeyField 返回主键字段。
//
// 优先级：spec.key 显式声明 → 第一个 required 的 text → 第一个 required → 第一个字段。
// entrypoint 的主键是 protocol（select 而非 text），所以必须有显式声明或 required 兜底。
func (r *Registry) KeyField(kind string) (Field, bool) {
	spec, ok := r.specs[kind]
	if !ok {
		return Field{}, false
	}
	if spec.Key != "" {
		if f, ok := lookupField(spec.Schema, spec.Key); ok {
			return f, true
		}
	}
	for _, f := range spec.Schema {
		if f.Required && f.Type == FieldText {
			return f, true
		}
	}
	if spec.Key == "" {
		for _, f := range spec.Schema {
			if f.Required {
				return f, true
			}
		}
	}
	if len(spec.Schema) > 0 {
		return spec.Schema[0], true
	}
	return Field{}, false
}

func lookupField(fields []Field, key string) (Field, bool) {
	for _, f := range fields {
		if f.Key == key {
			return f, true
		}
		if sub, ok := lookupField(f.Fields, key); ok {
			return sub, true
		}
	}
	return Field{}, false
}

// --- 引用目标 ---

// ReferenceTargets 返回某 kind 全部引用字段的（字段键, 目标类型）。
// 嵌套字段用点号路径（"backend.host"），与 Flatten 的键名一致。
func (r *Registry) ReferenceTargets(kind string) []RefTarget {
	spec, ok := r.specs[kind]
	if !ok {
		return nil
	}
	var out []RefTarget
	for _, f := range spec.Schema {
		out = append(out, collectRefs(f, "")...)
	}
	return out
}

// RefTarget 一个引用字段。
type RefTarget struct {
	Field string   // 字段键（数组引用即该键）
	Types []string // 目标类型
	Array bool     // 是否引用数组
}

func collectRefs(f Field, prefix string) []RefTarget {
	key := f.Key
	if prefix != "" {
		key = prefix + "." + f.Key
	}
	var out []RefTarget
	if f.Reference != nil {
		out = append(out, RefTarget{Field: key, Types: f.Reference.Types, Array: f.Type == FieldArray})
	}
	for _, s := range f.Fields {
		out = append(out, collectRefs(s, key)...)
	}
	return out
}

// ReferencedBy 反查：谁引用了 kind。
func (r *Registry) ReferencedBy(kind string) []string {
	var out []string
	for k := range r.specs {
		if k == kind {
			continue
		}
		for _, rt := range r.ReferenceTargets(k) {
			for _, t := range rt.Types {
				if t == kind {
					out = append(out, k)
					break
				}
			}
		}
	}
	return out
}

// --- 校验 ---

// FieldError 单个字段的校验错误。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// Validate 校验 spec 的源字段值（不含 status：status 由后端自动记录）。
//
// 覆盖：必填 / pattern / minLength / 控件类型 / options 取值 / 引用存在性 /
// dynamic select 是否在类型目录且已安装。
func (r *Registry) Validate(kind string, spec map[string]any, exists func(refKind, id string) bool) []FieldError {
	def, ok := r.specs[kind]
	if !ok {
		return []FieldError{{Message: fmt.Sprintf("未知对象类型 %q", kind)}}
	}
	fields := def.Schema
	// 带 type 时把目录里的类型参数并入校验范围。
	if dk := dynamicKey(def); dk != "" {
		if rawType, ok := lookupAny(spec, dk); ok {
			if typeID, _ := rawType.(string); typeID != "" {
				if extra, err := r.ExpandType(kind, typeID); err == nil {
					fields = extra
				}
			}
		}
	}
	var errs []FieldError
	for _, f := range fields {
		errs = append(errs, r.validateField(kind, f, "", spec, exists)...)
	}
	return errs
}

// dynamicKey 返回该 kind 里承担「类型」语义的字段键。
func dynamicKey(def *Spec) string {
	for _, f := range def.Schema {
		if f.Dynamic && f.Type == FieldSelect {
			return f.Key
		}
	}
	return ""
}

// validateField 校验单个字段。cur 是该字段所在的值容器，prefix 是错误路径前缀。
func (r *Registry) validateField(kind string, f Field, prefix string, cur map[string]any, exists func(string, string) bool) []FieldError {
	key := f.Key
	if prefix != "" {
		key = prefix + "." + f.Key
	}
	raw, present := lookupAny(cur, f.Key)

	if isEmpty(raw) || !present {
		if f.Required {
			return []FieldError{{Field: key, Message: "必填"}}
		}
		// 未填的 object 不再深入子字段（否则报一串子字段必填，噪音大）。
		return nil
	}

	var errs []FieldError
	switch f.Type {
	case FieldObject:
		sub, ok := raw.(map[string]any)
		if !ok {
			return []FieldError{{Field: key, Message: "类型应为对象"}}
		}
		for _, sf := range f.Fields {
			errs = append(errs, r.validateField(kind, sf, key, sub, exists)...)
		}
		return errs
	case FieldArray:
		items, ok := raw.([]any)
		if !ok {
			return []FieldError{{Field: key, Message: "类型应为数组"}}
		}
		for i, it := range items {
			ik := fmt.Sprintf("%s[%d]", key, i)
			if f.Item != nil {
				switch f.Item.Type {
				case FieldNumber:
					if _, ok := toFloat(it); !ok {
						errs = append(errs, FieldError{Field: ik, Message: "元素应为数字"})
					}
				case FieldText:
					if _, ok := it.(string); !ok {
						errs = append(errs, FieldError{Field: ik, Message: "元素应为文本"})
					}
				}
			}
			errs = append(errs, r.checkRef(key, ik, f, it, exists)...)
		}
		return errs
	case FieldSwitch:
		if _, ok := raw.(bool); !ok {
			errs = append(errs, FieldError{Field: key, Message: "类型应为开关"})
		}
	case FieldNumber:
		if _, ok := toFloat(raw); !ok {
			errs = append(errs, FieldError{Field: key, Message: "类型应为数字"})
		}
	case FieldSelect:
		s, ok := raw.(string)
		if !ok {
			errs = append(errs, FieldError{Field: key, Message: "类型应为文本"})
			return errs
		}
		if len(f.Options) > 0 && !hasOption(f.Options, s) {
			errs = append(errs, FieldError{Field: key, Message: fmt.Sprintf("取值 %q 不在候选内", s)})
		}
		if f.Dynamic {
			td, found := r.lookupDynamic(kind, s)
			switch {
			case !found:
				errs = append(errs, FieldError{Field: key, Message: fmt.Sprintf("类型 %q 不在类型目录中", s)})
			case !td.Installed:
				errs = append(errs, FieldError{Field: key, Message: fmt.Sprintf("类型 %q 依赖未安装的组件 %s", s, td.From)})
			}
		}
	}

	// 文本类约束。
	if s, ok := raw.(string); ok {
		if f.Pattern != "" {
			if re, err := regexp.Compile(f.Pattern); err == nil && !re.MatchString(s) {
				errs = append(errs, FieldError{Field: key, Message: "格式不合法"})
			}
		}
		if f.MinLength > 0 && len([]rune(s)) < f.MinLength {
			errs = append(errs, FieldError{Field: key, Message: fmt.Sprintf("至少 %d 个字符", f.MinLength)})
		}
	}
	errs = append(errs, r.checkRef(key, key, f, raw, exists)...)
	return errs
}

// checkRef 校验引用字段（reference / array+reference）指向的对象是否存在。
func (r *Registry) checkRef(_, errKey string, f Field, val any, exists func(string, string) bool) []FieldError {
	if f.Reference == nil || exists == nil {
		return nil
	}
	id, ok := val.(string)
	if !ok {
		// 引用字段的值必须是对象 id（字符串）。
		// 早前这里对非字符串直接 return nil，于是前端把 {id,value} 结构漏进来
		// （表单数组未序列化）也会被接受并落库，渲染时才炸 —— 不能静默放过。
		return []FieldError{{Field: errKey, Message: "引用值必须是对象 id（文本）"}}
	}
	if id == "" {
		return nil
	}
	var errs []FieldError
	for _, t := range f.Reference.Types {
		if !exists(t, id) {
			errs = append(errs, FieldError{Field: errKey, Message: fmt.Sprintf("引用的 %s %q 不存在", t, id)})
		}
	}
	return errs
}

func hasOption(opts []Option, value string) bool {
	for _, o := range opts {
		if o.Value == value {
			return true
		}
	}
	return false
}

// lookupDynamic 判定 kind 上下文里 dynamic select 的候选项。
func (r *Registry) lookupDynamic(kind, typeID string) (TypeDef, bool) {
	switch kind {
	case KindService:
		return r.catalog.ServiceType(typeID)
	case KindMiddleware:
		return r.catalog.MiddlewareType(typeID)
	case KindEntrypoint:
		return r.catalog.Protocol(typeID)
	}
	return TypeDef{}, false
}

// DynamicOptions 返回 dynamic select 的候选项（供前端 /catalog 使用）。
func (r *Registry) DynamicOptions(kind string) []TypeDef {
	switch kind {
	case KindService:
		return r.catalog.ServiceTypes
	case KindMiddleware:
		return r.catalog.MiddlewareTypes
	case KindEntrypoint:
		return r.catalog.EntrypointProtocols
	}
	return nil
}

// --- 小工具 ---

func lookupAny(m map[string]any, key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	if v, ok := m[key]; ok {
		return v, true
	}
	// 支持 "a.b" 路径（Flatten 产生的扁平键）。
	if i := strings.Index(key, "."); i > 0 {
		head, tail := key[:i], key[i+1:]
		if sub, ok := m[head].(map[string]any); ok {
			return lookupAny(sub, tail)
		}
	}
	return nil, false
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

func toString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	}
	return 0, false
}

func toSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}
