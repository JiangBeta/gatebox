package objects

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

// Store 仓储接口（便于测试替换）。
type Store interface {
	ListObjects(kind string) ([]repository.Obj, error)
	AllObjects() ([]repository.Obj, error)
	UpdateObjects(fn func(cur []repository.Obj) (puts, dels []repository.Obj, err error)) error
}

// Service 通用对象层的用例层：校验、id 生成、引用索引、重命名级联、删除保护。
//
// 事务边界：所有写操作都走 Store.UpdateObjects，把「读校验 + 写本对象 + 级联改引用」
// 收在同一个 Bolt 写事务里，因此不会出现「引用已级联但主对象写入失败」的中间态。
type Service struct {
	store   Store
	secrets SecretStore
	spec    *typespec.Registry
	now     func() time.Time
}

// New 构造对象层用例。
func New(store Store, spec *typespec.Registry) *Service {
	return &Service{store: store, spec: spec, now: time.Now}
}

// SetClock 注入时钟（测试用）。
func (s *Service) SetClock(fn func() time.Time) { s.now = fn }

// Spec 暴露模型注册表（渲染器与 API 都要用）。
func (s *Service) Spec() *typespec.Registry { return s.spec }

// --- 读 ---

// List 返回某 kind 的全部对象（按 key 升序）。
func (s *Service) List(kind string) ([]Object, error) {
	rows, err := s.store.ListObjects(kind)
	if err != nil {
		return nil, err
	}
	out := make([]Object, 0, len(rows))
	for _, r := range rows {
		o, err := decode(r)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	SortObjects(out)
	return out, nil
}

// Get 返回单个对象。
func (s *Service) Get(kind, id string) (Object, error) {
	list, err := s.List(kind)
	if err != nil {
		return Object{}, err
	}
	for _, o := range list {
		if o.ID == id {
			return o, nil
		}
	}
	return Object{}, ErrNotFound
}

// RefIndex 重建全库引用索引。
func (s *Service) RefIndex() (RefIndex, error) {
	rows, err := s.store.AllObjects()
	if err != nil {
		return nil, err
	}
	idx := RefIndex{}
	for _, r := range rows {
		o, err := decode(r)
		if err != nil {
			continue
		}
		for _, rt := range s.refsOf(o) {
			if rt.Array {
				for _, v := range o.Strings(rt.Field) {
					for _, t := range rt.Types {
						idx.Add(Ref{From: o.Kind, ID: o.ID, Field: rt.Field, Target: t, Value: v})
					}
				}
				continue
			}
			v := o.String(rt.Field)
			if v == "" {
				continue
			}
			for _, t := range rt.Types {
				idx.Add(Ref{From: o.Kind, ID: o.ID, Field: rt.Field, Target: t, Value: v})
			}
		}
	}
	return idx, nil
}

// ReferencedBy 返回引用了某对象的引用列表。
func (s *Service) ReferencedBy(kind, id string) ([]Ref, error) {
	idx, err := s.RefIndex()
	if err != nil {
		return nil, err
	}
	return idx.Target(kind, id), nil
}

// --- id ---

// Slug 由主键字段值生成稳定 id。
//
// 保留小写字母、数字与连字符（Unicode 字母也算），其余折叠成单个连字符。
// 关键不变量：id 不含 "/"，因此仓储层可用 "<kind>/<id>" 作复合键而无需分隔符转义。
func Slug(v string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(v)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// KeyOf 取对象的主键字段值。
func (s *Service) KeyOf(kind string, spec map[string]any) (string, error) {
	f, ok := s.spec.KeyField(kind)
	if !ok {
		return "", fmt.Errorf("对象类型 %q 无法确定主键字段", kind)
	}
	v, ok := lookup(spec, f.Key)
	if !ok {
		return "", keyErr(f, "必填但未提供")
	}
	key, ok := v.(string)
	if !ok {
		return "", keyErr(f, "必须是文本")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", keyErr(f, "不能为空")
	}
	return key, nil
}

// keyErr 主键字段不合法 = 客户端输入错误，必须走 ValidationError(→422)。
// 早前这里是裸 fmt.Errorf，被 handler 兜成 500：请求体里 spec 传错类型时
// 用户看到的是「服务名 为空 / HTTP 500」，既误导又像是服务端坏了。
func keyErr(f typespec.Field, msg string) error {
	return &ValidationError{Errors: []typespec.FieldError{{Field: f.Key, Message: msg}}}
}

// --- 写 ---

// Create 新建对象。id 由主键字段 slug 化而来。
func (s *Service) Create(kind string, spec map[string]any) (Object, error) {
	if err := s.assertWritable(kind); err != nil {
		return Object{}, err
	}
	key, err := s.KeyOf(kind, spec)
	if err != nil {
		return Object{}, err
	}
	id := Slug(key)
	if id == "" {
		return Object{}, fmt.Errorf("主键 %q 无法生成 id", key)
	}
	now := s.now().UTC()
	var created Object
	err = s.store.UpdateObjects(func(cur []repository.Obj) ([]repository.Obj, []repository.Obj, error) {
		if find(cur, kind, id) != nil {
			return nil, nil, fmt.Errorf("%s %q 已存在", s.title(kind), key)
		}
		if errs := s.validate(kind, spec, cur, nil); len(errs) > 0 {
			return nil, nil, &ValidationError{Errors: errs}
		}
		created = Object{
			Kind: kind, ID: id, Key: key, Spec: spec,
			Status: map[string]any{}, CreatedAt: now, UpdatedAt: now,
		}
		return []repository.Obj{mustEncode(created)}, nil, nil
	})
	if err != nil {
		return Object{}, err
	}
	return created, nil
}

// Update 更新源字段（不含主键变更；改名走 Rename）。status 不由客户端覆盖。
func (s *Service) Update(kind, id string, spec map[string]any) (Object, error) {
	if err := s.assertWritable(kind); err != nil {
		return Object{}, err
	}
	if err := s.assertEdgeRole(kind, id, spec); err != nil {
		return Object{}, err
	}
	key, err := s.KeyOf(kind, spec)
	if err != nil {
		return Object{}, err
	}
	var out Object
	err = s.store.UpdateObjects(func(cur []repository.Obj) ([]repository.Obj, []repository.Obj, error) {
		o, err := load(cur, kind, id)
		if err != nil {
			return nil, nil, err
		}
		// 改主键值必须走 Rename：它要同时换 id 并级联改写全部引用。
		// 这里若照单全收，就会留下 id≠Slug(key) 的对象，引用和 id 规则双双失守。
		if key != o.Key {
			return nil, nil, fmt.Errorf("%w：%q → %q", ErrKeyChange, o.Key, key)
		}
		if errs := s.validate(kind, spec, cur, &exclude{kind: kind, id: id}); len(errs) > 0 {
			return nil, nil, &ValidationError{Errors: errs}
		}
		o.Key = key
		o.Spec = spec
		o.UpdatedAt = s.now().UTC()
		out = o
		return []repository.Obj{mustEncode(o)}, nil, nil
	})
	if err != nil {
		return Object{}, err
	}
	return out, nil
}

// Rename 改主键：换 id 并把全部引用级联改到新 id。
//
// 引用改写严格按模型里声明的 reference 字段走（不是「值相等就改」），
// 因此 remark 里恰好写着旧 id 时不会被误改。
func (s *Service) Rename(kind, id, newKey string) (Object, int, error) {
	if err := s.assertWritable(kind); err != nil {
		return Object{}, 0, err
	}
	newKey = strings.TrimSpace(newKey)
	newID := Slug(newKey)
	if newID == "" {
		return Object{}, 0, fmt.Errorf("新主键 %q 无法生成 id", newKey)
	}
	if newID == id {
		o, err := s.Get(kind, id)
		return o, 0, err
	}
	keyField, _ := s.spec.KeyField(kind)
	var out Object
	cascaded := 0
	// 只写秘密（user 密码哈希）跟着改名搬走。
	var secret []byte
	err := s.store.UpdateObjects(func(cur []repository.Obj) ([]repository.Obj, []repository.Obj, error) {
		o, err := load(cur, kind, id)
		if err != nil {
			return nil, nil, err
		}
		if find(cur, kind, newID) != nil {
			return nil, nil, fmt.Errorf("%s %q 已存在", s.title(kind), newKey)
		}
		// 先取旧 id 上的只写秘密，改名成功后搬到新 id（事务外搬，避免 secret 写失败时对象已改）。
		if s.secrets != nil {
			if b, err := s.secrets.GetSecret(kind, id); err == nil {
				secret = b
			}
		}
		spec := cloneMap(o.Spec)
		if err := setPath(spec, keyField.Key, newKey); err != nil {
			return nil, nil, err
		}
		// 校验时把自己从存在性判定里排除，允许自引用。
		if errs := s.validate(kind, spec, cur, &exclude{kind: kind, id: id}); len(errs) > 0 {
			return nil, nil, &ValidationError{Errors: errs}
		}
		o.ID = newID
		o.Key = newKey
		o.Spec = spec
		o.UpdatedAt = s.now().UTC()
		out = o

		puts := []repository.Obj{mustEncode(o)}
		dels := []repository.Obj{{Kind: kind, ID: id}}
		for _, r := range cur {
			if r.Kind == kind && r.ID == id {
				continue
			}
			src, err := decode(r)
			if err != nil {
				continue
			}
			if !s.rewriteRefs(src, kind, id, newID) {
				continue
			}
			src.UpdatedAt = s.now().UTC()
			puts = append(puts, mustEncode(src))
			cascaded++
		}
		return puts, dels, nil
	})
	if err != nil {
		return Object{}, 0, err
	}
	if secret != nil && s.secrets != nil {
		if err := s.secrets.PutSecret(kind, newID, secret); err != nil {
			return Object{}, 0, err
		}
		_ = s.secrets.DeleteSecret(kind, id)
	}
	return out, cascaded, nil
}

// Delete 删除对象。
//
// force=false：被引用则拒绝，并回报引用来源（前端据此提示先解引用）。
// force=true：连带删除引用方，返回受影响的 kind 列表（不静默留下悬空引用）。
func (s *Service) Delete(kind, id string, force bool) ([]string, error) {
	if err := s.assertWritable(kind); err != nil {
		return nil, err
	}
	if err := s.assertEdgeDeletable(kind, id); err != nil {
		return nil, err
	}
	var cascade []string
	var orphan []repository.Obj
	err := s.store.UpdateObjects(func(cur []repository.Obj) ([]repository.Obj, []repository.Obj, error) {
		if _, err := load(cur, kind, id); err != nil {
			return nil, nil, err
		}
		refs, err := s.inboundRefs(cur, kind, id)
		if err != nil {
			return nil, nil, err
		}
		if len(refs) > 0 && !force {
			return nil, nil, &InUseError{Object: kind + "/" + id, Refs: refs}
		}
		dels := []repository.Obj{{Kind: kind, ID: id}}
		seen := map[string]bool{}
		for _, ref := range refs {
			k := ref.From + "/" + ref.ID
			if seen[k] {
				continue
			}
			seen[k] = true
			dels = append(dels, repository.Obj{Kind: ref.From, ID: ref.ID})
			cascade = append(cascade, ref.From)
		}
		// 连带删除的对象，其只写秘密也要清掉（不留孤儿密码哈希）。
		orphan = dels
		return nil, dels, nil
	})
	if err != nil {
		return nil, err
	}
	if s.secrets != nil {
		for _, d := range orphan {
			_ = s.secrets.DeleteSecret(d.Kind, d.ID)
		}
	}
	sort.Strings(cascade)
	return cascade, nil
}

// Put 直写一个对象（不查重、不级联引用）。
//
// 用途只有两个：① 迁移按计划幂等落库；② 修复工具。
// 常规写操作一律走 Create/Update/Rename/Delete——它们才有校验与引用完整性保证。
func (s *Service) Put(o Object) error {
	if o.Kind == "" || o.ID == "" {
		return fmt.Errorf("对象缺少 kind 或 id")
	}
	if o.Key == "" {
		o.Key = o.ID
	}
	if o.Status == nil {
		o.Status = map[string]any{}
	}
	now := s.now().UTC()
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	}
	o.UpdatedAt = now
	return s.store.UpdateObjects(func(cur []repository.Obj) ([]repository.Obj, []repository.Obj, error) {
		if old := find(cur, o.Kind, o.ID); old != nil && !old.CreatedAt.IsZero() {
			o.CreatedAt = old.CreatedAt
		}
		return []repository.Obj{mustEncode(o)}, nil, nil
	})
}

// SetStatus 回写 status（渲染器在 Run 结束后调用）。
func (s *Service) SetStatus(kind, id string, patch map[string]any) error {
	return s.store.UpdateObjects(func(cur []repository.Obj) ([]repository.Obj, []repository.Obj, error) {
		o, err := load(cur, kind, id)
		if err != nil {
			return nil, nil, err
		}
		o.Status = withExtra(o.Status, patch)
		o.UpdatedAt = s.now().UTC()
		return []repository.Obj{mustEncode(o)}, nil, nil
	})
}

// --- 引用工具（严格按模型声明的 reference 字段）---

func (s *Service) refsOf(o Object) []typespec.RefTarget {
	return s.spec.ReferenceTargets(o.Kind)
}

func (s *Service) inboundRefs(cur []repository.Obj, kind, id string) ([]Ref, error) {
	var refs []Ref
	for _, r := range cur {
		if r.Kind == kind && r.ID == id {
			continue
		}
		src, err := decode(r)
		if err != nil {
			continue
		}
		for _, rt := range s.refsOf(src) {
			if !hasType(rt.Types, kind) {
				continue
			}
			if rt.Array {
				for _, v := range src.Strings(rt.Field) {
					if v == id {
						refs = append(refs, Ref{From: src.Kind, ID: src.ID, Field: rt.Field, Target: kind, Value: v})
					}
				}
				continue
			}
			if src.String(rt.Field) == id {
				refs = append(refs, Ref{From: src.Kind, ID: src.ID, Field: rt.Field, Target: kind, Value: id})
			}
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].From != refs[j].From {
			return refs[i].From < refs[j].From
		}
		return refs[i].Field < refs[j].Field
	})
	return refs, nil
}

// rewriteRefs 把 obj 里指向 (kind, oldID) 的引用改成 newID，返回是否改动过。
func (s *Service) rewriteRefs(obj Object, kind, oldID, newID string) bool {
	changed := false
	for _, rt := range s.refsOf(obj) {
		if !hasType(rt.Types, kind) {
			continue
		}
		if rt.Array {
			raw, ok := lookup(obj.Spec, rt.Field)
			if !ok {
				continue
			}
			items, ok := raw.([]any)
			if !ok {
				continue
			}
			for i, it := range items {
				if str, ok := it.(string); ok && str == oldID {
					items[i] = newID
					changed = true
				}
			}
			continue
		}
		if str, ok := lookupString(obj.Spec, rt.Field); ok && str == oldID {
			if err := setPath(obj.Spec, rt.Field, newID); err == nil {
				changed = true
			}
		}
	}
	return changed
}

func hasType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// --- 校验 ---

type exclude struct{ kind, id string }

// existsIn 判定某 kind 的 id 在当前库里存在（可选排除自身）。
func (s *Service) existsIn(cur []repository.Obj, ex *exclude, kind, id string) bool {
	for _, r := range cur {
		if r.Kind != kind || r.ID != id {
			continue
		}
		if ex != nil && ex.kind == kind && ex.id == id {
			continue
		}
		return true
	}
	return false
}

func (s *Service) validate(kind string, spec map[string]any, cur []repository.Obj, ex *exclude) []typespec.FieldError {
	return s.spec.Validate(kind, spec, func(refKind, id string) bool {
		if refKind == typespec.RefProxy {
			// 后端进程不是 V4.1 对象，本轮不做存在性判定（ADR-043 §3）。
			return true
		}
		return s.existsIn(cur, ex, refKind, id)
	})
}

func (s *Service) assertWritable(kind string) error {
	sp, ok := s.spec.Spec(kind)
	if !ok {
		return fmt.Errorf("对象类型 %q 未定义", kind)
	}
	if sp.Readonly {
		return fmt.Errorf("%s（%s）由类型目录求并集得出，只读", sp.Title, kind)
	}
	return nil
}

func (s *Service) title(kind string) string {
	if sp, ok := s.spec.Spec(kind); ok {
		return sp.Title
	}
	return kind
}

// --- 内部 ---

func decode(r repository.Obj) (Object, error) {
	var o Object
	if err := json.Unmarshal(r.Body, &o); err != nil {
		return Object{}, fmt.Errorf("解码对象 %s/%s 失败: %w", r.Kind, r.ID, err)
	}
	if o.Kind == "" {
		o.Kind = r.Kind
	}
	if o.ID == "" {
		o.ID = r.ID
	}
	return o, nil
}

func mustEncode(o Object) repository.Obj {
	b, err := json.Marshal(o)
	if err != nil {
		// Object 只含 map/slice/time，Marshal 不会失败。
		panic("objects: 编码对象失败: " + err.Error())
	}
	return repository.Obj{Kind: o.Kind, ID: o.ID, Body: b}
}

func find(cur []repository.Obj, kind, id string) *Object {
	for i := range cur {
		if cur[i].Kind == kind && cur[i].ID == id {
			o, err := decode(cur[i])
			if err != nil {
				return nil
			}
			return &o
		}
	}
	return nil
}

func load(cur []repository.Obj, kind, id string) (Object, error) {
	for _, r := range cur {
		if r.Kind == kind && r.ID == id {
			return decode(r)
		}
	}
	return Object{}, ErrNotFound
}

// setPath 按点号路径写值（中间缺失时自动建 map）。
func setPath(m map[string]any, path string, val any) error {
	parts := strings.Split(path, ".")
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = val
			return nil
		}
		sub, ok := cur[p].(map[string]any)
		if !ok {
			sub = map[string]any{}
			cur[p] = sub
		}
		cur = sub
	}
	return nil
}

func lookupString(m map[string]any, path string) (string, bool) {
	v, ok := lookup(m, path)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// withExtra 合并状态补丁。**补丁里的 nil 表示删除该键**。
//
// 约定必须明确：同步回写既要设 state/error/skipReason，又要清掉上一轮留下的
// 同名字段。只会「加字段」的合并会让旧错误信息一直挂在界面上。
func withExtra(dst map[string]any, patch map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range patch {
		if v == nil {
			delete(dst, k)
			continue
		}
		dst[k] = v
	}
	return dst
}

// ValidationError 字段级校验失败。
type ValidationError struct{ Errors []typespec.FieldError }

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for _, f := range e.Errors {
		parts = append(parts, f.Field+": "+f.Message)
	}
	return "校验未通过：" + strings.Join(parts, "；")
}

// Details 返回结构化错误（API 直接回给前端定位到字段）。
func (e *ValidationError) Details() []typespec.FieldError { return e.Errors }

// InUseError 对象被引用，删除被拒。
type InUseError struct {
	Object string
	Refs   []Ref
}

func (e *InUseError) Error() string {
	parts := make([]string, 0, len(e.Refs))
	for _, r := range e.Refs {
		parts = append(parts, fmt.Sprintf("%s/%s 的 %s", r.From, r.ID, r.Field))
	}
	return fmt.Sprintf("%s 仍被引用（%s），请先解引用或强制删除", e.Object, strings.Join(parts, "、"))
}

// Port 端口取值（渲染器用）。
func (o Object) Port(key string) (int, bool) {
	f, ok := o.Number(key)
	if !ok {
		return 0, false
	}
	n := int(f)
	if float64(n) != f {
		return 0, false
	}
	return n, true
}

// PortOr 返回端口，缺省回退到 def。
func (o Object) PortOr(key string, def int) int {
	if n, ok := o.Port(key); ok {
		return n
	}
	return def
}
