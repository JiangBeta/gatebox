// Package objects 实现 V4.1 的通用对象层：一套按 kind 存取的 CRUD，
// 覆盖中间件 / 服务 / 路由 / 入口点 / 根域名 / 主机 / 用户（ADR-043 §3）。
//
// 形状固定为「源字段（可编辑）+ status（自动记录，只读）」两段（ADR-042 不变量 3）：
//
//	Object{spec map[string]any, status map[string]any}
//
// 引用一律存稳定 id、显示层再解析成 keyField（ADR-043 §3），因此删除与重命名都必须做引用检查/级联。
package objects

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ErrNotFound 对象不存在。
var ErrNotFound = errors.New("objects: not found")

// ErrDuplicate 主键重复。
var ErrDuplicate = errors.New("objects: 主键已存在")

// ErrReadonly 该对象类型只读（catalog），不接受写。
var ErrReadonly = errors.New("objects: 该对象类型只读")

// ErrKeyChange 试图用普通更新改主键值。改主键必须走 Rename（PATCH /key）：
// 换主键要连带改 id 和所有引用，普通 Update 做不到，也不该悄悄留下 id≠Slug(key) 的脏对象。
var ErrKeyChange = errors.New("objects: 改主键请用重命名接口")

// Object 一个 V4.1 对象。
type Object struct {
	Kind      string         `json:"kind"`
	ID        string         `json:"id"`
	Key       string         `json:"key"`
	Spec      map[string]any `json:"spec"`
	Status    map[string]any `json:"status"` // 无 omitempty：MarshalJSON 保证至少是 {}
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Clone 深拷贝（避免把 map 交出去后被调用方改写）。
func (o Object) Clone() Object {
	c := o
	c.Spec = cloneMap(o.Spec)
	c.Status = cloneMap(o.Status)
	return c
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneVal(v)
	}
	return out
}

func cloneVal(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneMap(t)
	case []any:
		s := make([]any, len(t))
		for i := range t {
			s[i] = cloneVal(t[i])
		}
		return s
	default:
		return v
	}
}

// --- 状态取值（枚举取自各 model/*.yaml 的 status.enum）---

// State 生效状态。route 与 service 的枚举不同，故两个常量集。
const (
	// route.status.state
	StateApplied  = "已生效"
	StateCertWarn = "证书告警"
	StatePending  = "未生效"
	StateDisabled = "已停用"
	StateError    = "错误"
	// service.status.state
	StateRunning    = "运行中"
	StateNotRunning = "未运行"
	StateDepMissing = "依赖缺失"
)

// Get 返回 spec 里的字段值（支持 "backend.host" 路径）。
func (o Object) Get(key string) (any, bool) { return lookup(o.Spec, key) }

// String 返回 spec 里的文本字段值。
func (o Object) String(key string) string {
	v, ok := o.Get(key)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Bool 返回 spec 里的开关值。
func (o Object) Bool(key string) bool {
	v, ok := o.Get(key)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// Number 返回 spec 里的数字字段值。
func (o Object) Number(key string) (float64, bool) {
	v, ok := o.Get(key)
	if !ok {
		return 0, false
	}
	return toFloat(v)
}

// Numbers 返回 spec 里的数字数组（如 entrypoint.ports）。
func (o Object) Numbers(key string) []float64 {
	v, ok := o.Get(key)
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]float64, 0, len(t))
		for _, it := range t {
			if f, ok := toFloat(it); ok {
				out = append(out, f)
			}
		}
		return out
	case []float64:
		return t
	}
	return nil
}

// Strings 返回 spec 里的字符串数组（引用数组）。
func (o Object) Strings(key string) []string {
	v, ok := o.Get(key)
	if !ok {
		return nil
	}
	items, ok := v.([]any)
	if !ok {
		if s, ok := v.([]string); ok {
			return s
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// SetState 写回 status.state。
func (o *Object) SetState(state string) {
	if o.Status == nil {
		o.Status = map[string]any{}
	}
	o.Status["state"] = state
}

// State 读 status.state。
func (o Object) State() string { return o.StatusString("state") }

// StatusString 读 status 里的字段。
func (o Object) StatusString(key string) string {
	s, _ := o.Status[key].(string)
	return s
}

// Enabled 读 spec.enabled，缺省视为 true。
func (o Object) Enabled() bool {
	if v, ok := o.Get("enabled"); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
}

// MarshalJSON 保证 spec/status 不为 null（前端直接遍历）。
func (o Object) MarshalJSON() ([]byte, error) {
	type alias Object
	c := alias(o)
	if c.Spec == nil {
		c.Spec = map[string]any{}
	}
	if c.Status == nil {
		c.Status = map[string]any{}
	}
	return json.Marshal(c)
}

// SortObjects 按 key 升序（列表稳定序）。
func SortObjects(list []Object) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Key < list[j].Key })
}

// --- 引用收集 ---

// Ref 一次引用。
type Ref struct {
	From   string `json:"from"`   // 引用方 kind
	ID     string `json:"id"`     // 引用方 id
	Field  string `json:"field"`  // 字段键
	Target string `json:"target"` // 被引用 kind
	Value  string `json:"value"`  // 被引用 id
}

// RefIndex 反查引用关系（谁引用了我）。
type RefIndex map[string][]Ref

// Add 记一条引用。
func (idx RefIndex) Add(r Ref) { idx[r.Target+"#"+r.Value] = append(idx[r.Target+"#"+r.Value], r) }

// Target 取某对象的引用列表。
func (idx RefIndex) Target(kind, id string) []Ref { return idx[kind+"#"+id] }

// To 列出该对象引用了谁。
func (idx RefIndex) To(kind, id string) []Ref {
	key := kind + "#" + id
	var out []Ref
	for k, refs := range idx {
		if k == key {
			continue
		}
		for _, r := range refs {
			if r.From == kind && r.ID == id {
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].Field < out[j].Field
	})
	return out
}

// Dump 便于调试的紧凑输出。
func (idx RefIndex) Dump() string {
	keys := make([]string, 0, len(idx))
	for k := range idx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s ← %d\n", k, len(idx[k]))
	}
	return b.String()
}

// --- 内部工具 ---

func lookup(m map[string]any, key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	if v, ok := m[key]; ok {
		return v, true
	}
	if i := strings.Index(key, "."); i > 0 {
		if sub, ok := m[key[:i]].(map[string]any); ok {
			return lookup(sub, key[i+1:])
		}
	}
	return nil, false
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
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}
