package typespec

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Models 内嵌 V4.1 对象模型 YAML。
//
// 模型文件是设计期唯一真相（docs/v4.1/model/），编译期副本放在 models/ 下，
// 因为 go:embed 不能引用包目录之外的路径。漂移由 TestModelsInSyncWithDocs 守住。
//
//go:embed all:models
var Models embed.FS

// Registry 所有对象类型定义 + 类型目录（只读，进程内唯一）。
type Registry struct {
	mu      sync.RWMutex
	specs   map[string]*Spec
	catalog Catalog
	order   []string // kind 按 group、title 排序，供前端分页
	groups  map[string][]string
}

// Load 解析内嵌模型，构造 Registry。
//
// 无 kind 的文件（如 _naming.yaml）会被跳过；解析失败即启动失败——
// 模型是内置数据源，不允许静默降级成空目录（否则所有下拉都变空）。
func Load() (*Registry, error) {
	sub, err := fs.Sub(Models, "models")
	if err != nil {
		return nil, err
	}
	return LoadFS(sub)
}

// LoadFS 从任意文件系统加载（测试用）。
func LoadFS(fsys fs.FS) (*Registry, error) {
	r := &Registry{
		specs:  map[string]*Spec{},
		groups: map[string][]string{},
	}
	var files []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	hasCatalog := false
	for _, f := range files {
		raw, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", f, err)
		}
		// 先判 kind：只有带 kind 的文件才是对象定义。
		kind := yamlKind(raw)
		if kind == "" {
			continue
		}
		if kind == KindCatalog {
			var c Catalog
			if err := yaml.Unmarshal(raw, &c); err != nil {
				return nil, fmt.Errorf("解析 %s: %w", f, err)
			}
			if len(c.ServiceTypes) == 0 && len(c.MiddlewareTypes) == 0 && len(c.EntrypointProtocols) == 0 {
				return nil, fmt.Errorf("解析 %s: 类型目录为空", f)
			}
			r.catalog = c
			hasCatalog = true
			continue
		}
		var sp Spec
		if err := yaml.Unmarshal(raw, &sp); err != nil {
			return nil, fmt.Errorf("解析 %s: %w", f, err)
		}
		if sp.Kind == "" {
			return nil, fmt.Errorf("解析 %s: 缺少 kind", f)
		}
		if sp.Doc == "" {
			sp.Doc = headComment(raw, sp.Kind)
		}
		sp.Source = path.Base(f)
		sp.Source = path.Base(f)
		spec := sp // 复制，避免循环变量别名
		r.specs[spec.Kind] = &spec
	}
	if !hasCatalog {
		return nil, fmt.Errorf("类型目录缺失：内嵌模型中未找到 kind: %s", KindCatalog)
	}
	if len(r.specs) == 0 {
		return nil, fmt.Errorf("内嵌模型中没有可用的对象类型定义")
	}
	r.index()
	return r, nil
}

// index 排序并分组。
func (r *Registry) index() {
	r.order = make([]string, 0, len(r.specs))
	for k := range r.specs {
		r.order = append(r.order, k)
	}
	sort.Slice(r.order, func(i, j int) bool {
		a, b := r.specs[r.order[i]], r.specs[r.order[j]]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.Title < b.Title
	})
	for _, k := range r.order {
		g := r.specs[k].Group
		r.groups[g] = append(r.groups[g], k)
	}
}

// yamlKind 提取顶层 kind（不整体反序列化，省一次 reflect）。
func yamlKind(raw []byte) string {
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.Kind
}

// headComment 取文件首个标题注释行（`# 对象：路由 Route（…）`），作为说明文案。
func headComment(raw []byte, skip string) string {
	for _, line := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if t == "" || strings.HasPrefix(t, "─") || strings.HasPrefix(t, "=") {
			continue
		}
		if strings.HasPrefix(t, skip+":") {
			continue
		}
		return t
	}
	return ""
}

// Spec 按 kind 取对象定义。
func (r *Registry) Spec(kind string) (*Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sp, ok := r.specs[kind]
	return sp, ok
}

// Kinds 返回全部对象 kind（按 group、title 排序）。
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// Groups 返回「一级页面 → kind 列表」。
func (r *Registry) Groups() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string][]string, len(r.groups))
	for g, ks := range r.groups {
		out[g] = append([]string(nil), ks...)
	}
	return out
}

// Catalog 返回类型目录。
func (r *Registry) Catalog() Catalog {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.catalog
}

// Manifest 是 /api/v1/catalog 的响应：类型目录 + 对象 kind 清单。
type Manifest struct {
	Catalog Catalog             `json:"catalog"`
	Kinds   []string            `json:"kinds"`
	Groups  map[string][]string `json:"groups"`
}

// Manifest 汇总类型目录与对象清单。
func (r *Registry) Manifest() Manifest {
	return Manifest{Catalog: r.Catalog(), Kinds: r.Kinds(), Groups: r.Groups()}
}

// ValidateRef 校验一次引用是否存在（供对象层组合 uses）。
func (r *Registry) ValidateRef(refKind, id string, exists func(refKind, id string) bool) error {
	if refKind == RefProxy {
		return nil // 后端进程不是 V4.1 对象，由运行时判定
	}
	if _, ok := r.specs[refKind]; !ok {
		return fmt.Errorf("引用目标类型 %q 未定义", refKind)
	}
	if exists != nil && !exists(refKind, id) {
		return fmt.Errorf("引用的 %s %q 不存在", refKind, id)
	}
	return nil
}
