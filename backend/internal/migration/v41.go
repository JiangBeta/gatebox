// Package migration 把旧表（BoltDB model.*）映射成 V4.1 对象（ADR-043 §5 / migration.md §4）。
//
// 本轮只交付**计划与报告**：Build 产出 Plan（将迁 N 条 / 冲突 M 条 / 无法映射 K 条），
// Apply 才真正写对象，且执行前必须由调用方备份 gatebox.db。
//
// 幂等：对象键是 "<kind>\x00<id>"，重跑得到同一批 id；已存在的对象默认跳过（--force 才覆盖），
// 因此「执行 → 验收不满意 → 再跑一次」不会产生重复数据。
//
// 映射表（migration.md §4 + _naming.yaml renames 段）：
//
//	domains        → domain      （同名，主键 name 的 slug）
//	port_bindings  → entrypoint  （protocol 为主键）
//	fragments      → middleware  （旧片段是裸 Caddy 文本 → type=code 逃生舱，行为等价）
//	services       → service + route × N（ADR-043 §3：一个旧 Service 拆成「后端」+「每域名一条转发」）
package migration

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/objects"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

// Action 计划里单个对象的处置。
type Action string

const (
	ActionCreate    Action = "create"    // 目标不存在 → 新建
	ActionSkip      Action = "skip"      // 目标已存在 → 保留（幂等）
	ActionOverwrite Action = "overwrite" // 目标已存在 + force → 覆盖
)

// Planned 一条待写对象。
type Planned struct {
	Kind   string         `json:"kind"`
	ID     string         `json:"id"`
	Source string         `json:"source"` // 来源旧表/旧 id
	Action Action         `json:"action"`
	Object objects.Object `json:"-"`
}

// Conflict 冲突：两个旧记录映射到同一 (kind,id)。
type Conflict struct {
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`
	Sources []string `json:"sources"`
	Reason  string   `json:"reason"`
}

// Unmappable 无法映射（数据本身缺必要信息，迁移不做猜测）。
type Unmappable struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// Warning 已迁出、但需要人工确认的项。
//
// 与 Unmappable 的区别：数据本身完整、也真的写进去了，只是有个地方值得
// 用户看一眼（如 file_server 路由的 backend 是占位值）。
type Warning struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// Plan 迁移计划。
type Plan struct {
	Planned    []Planned    `json:"planned"`
	Conflicts  []Conflict   `json:"conflicts"`
	Unmappable []Unmappable `json:"unmappable"`
	Warnings   []Warning    `json:"warnings"`
	// ReadCounts 各旧表读到的条数（报告用）。
	ReadCounts map[string]int `json:"readCounts"`
}

// Stats 报告口径：迁 N / 冲突 M / 无法映射 K。
type Stats struct {
	Create     int `json:"create"`
	Skip       int `json:"skip"`
	Overwrite  int `json:"overwrite"`
	Conflicts  int `json:"conflicts"`
	Unmappable int `json:"unmappable"`
	Warnings   int `json:"warnings"`
}

// Stats 汇总。
func (p Plan) Stats() Stats {
	var s Stats
	for _, pl := range p.Planned {
		switch pl.Action {
		case ActionCreate:
			s.Create++
		case ActionSkip:
			s.Skip++
		case ActionOverwrite:
			s.Overwrite++
		}
	}
	s.Conflicts = len(p.Conflicts)
	s.Unmappable = len(p.Unmappable)
	s.Warnings = len(p.Warnings)
	return s
}

// Summarize 一行摘要（CLI 输出）。
func (p Plan) Summarize() string {
	s := p.Stats()
	return fmt.Sprintf("将新建 %d 条 / 跳过已存在 %d 条 / 覆盖 %d 条；冲突 %d 条；无法映射 %d 条；需确认 %d 条",
		s.Create, s.Skip, s.Overwrite, s.Conflicts, s.Unmappable, s.Warnings)
}

// Source 旧数据读取接口（窄接口，便于用内存替身做测试）。
type Source interface {
	ListDomains() ([]model.Domain, error)
	ListPortBindings() ([]model.PortBinding, error)
	ListFragments() ([]model.Fragment, error)
	ListServices() ([]model.Service, error)
}

// Builder 构造迁移计划。
type Builder struct {
	src  Source
	objs *objects.Service
	spec *typespec.Registry
	// seen 记录 (kind,id) → 首个来源，用于检出冲突。
	seen map[string]string
	// fragIndex 旧片段 id → 新中间件 id（片段主键来自 name，不是旧 id）。
	fragIndex map[string]string
	plan      Plan
}

// NewBuilder 构造计划构造器。objs 可为 nil（纯离线算计划，不查已存在）。
func NewBuilder(src Source, objs *objects.Service, spec *typespec.Registry) *Builder {
	return &Builder{
		src:       src,
		objs:      objs,
		spec:      spec,
		seen:      map[string]string{},
		fragIndex: map[string]string{},
		plan:      Plan{ReadCounts: map[string]int{}},
	}
}

// Build 读取旧表并算出完整计划。
func (b *Builder) Build() (Plan, error) {
	domains, err := b.src.ListDomains()
	if err != nil {
		return b.plan, fmt.Errorf("读 domains 失败: %w", err)
	}
	bindings, err := b.src.ListPortBindings()
	if err != nil {
		return b.plan, fmt.Errorf("读 port_bindings 失败: %w", err)
	}
	fragments, err := b.src.ListFragments()
	if err != nil {
		return b.plan, fmt.Errorf("读 fragments 失败: %w", err)
	}
	services, err := b.src.ListServices()
	if err != nil {
		return b.plan, fmt.Errorf("读 services 失败: %w", err)
	}
	b.plan.ReadCounts["domains"] = len(domains)
	b.plan.ReadCounts["port_bindings"] = len(bindings)
	b.plan.ReadCounts["fragments"] = len(fragments)
	b.plan.ReadCounts["services"] = len(services)

	// 依赖顺序：域名前置（route.roots 要引用它），再入口点、中间件、服务、路由。
	b.planDomains(domains)
	b.planEntrypoints(bindings)
	b.planFragments(fragments)
	b.planServices(services)
	return b.plan, nil
}

func (b *Builder) planDomains(list []model.Domain) {
	for _, d := range list {
		id := objects.Slug(d.Name)
		if id == "" {
			b.unmappable("domains/"+d.Name, "域名 slug 为空")
			continue
		}
		spec := map[string]any{"name": d.Name}
		if d.CredentialID != "" {
			// domain.credentialId 指向 dns-credential —— 该 kind 已在注册表里
			// （docs/v4.1/model/dns-credential.yaml），但不在首批界面批次：它的
			// fields 是随供应商变化的对象、provider 选项来自扩展注册表，
			// 需要 uiHint: dns-credential 定制渲染。
			// 照搬旧 id 必然悬空（旧 id 与 V4.1 的 dns-credential id 命名空间不同），
			// 所以不迁该字段，但明确报出来 —— 静默丢掉才是真的丢数据。
			// 该域名在 dns-credential 批次落地前只能走旧 DNS 路径（ADR-013 acme.sh）。
			b.unmappable("domains/"+d.Name,
				"credentialId="+d.CredentialID+" 未迁移（dns-credential 尚无界面，无法重选）："+
					"该域名需先走旧 DNS 路径（ADR-013 acme.sh）")
		}
		b.add(typespec.KindDomain, id, "domains/"+d.Name, spec)
	}
}

func (b *Builder) planEntrypoints(list []model.PortBinding) {
	for _, p := range list {
		id := objects.Slug(p.Protocol)
		if id == "" {
			b.unmappable("port_bindings/"+p.Protocol, "协议 slug 为空")
			continue
		}
		// 协议必须真在类型目录里：真实库可能残留测试协议（如 "test"），
		// 与其让 Apply 逐条失败，不如在计划阶段就报告并跳过。
		//
		// 只查「在不在目录」，**不查组件是否已安装**：installed=false 是运行期事实
		// （如 caddy-l4 没装），不是数据非法。旧库里这些协议本来就在工作，
		// 迁移不该丢数据——真到渲染时再报「依赖未安装」不迟。
		if !b.protocolExists(p.Protocol) {
			b.unmappable("port_bindings/"+p.Protocol, "协议不在类型目录中（可能残留测试数据）")
			continue
		}
		ports := make([]any, 0, len(p.Ports))
		for _, n := range p.Ports {
			ports = append(ports, float64(n))
		}
		spec := map[string]any{
			"protocol": p.Protocol,
			"ports":    ports,
			"network":  networkOf(p.Protocol),
			"enabled":  true,
		}
		if p.Description != "" {
			spec["description"] = p.Description
		}
		b.add(typespec.KindEntrypoint, id, "port_bindings/"+p.Protocol, spec)
	}
}

// planFragments 旧片段是裸 Caddy 文本 → type=code 逃生舱。
//
// 保留原样的理由：内置片段（frag-basic-auth 等）本来就有专门的结构化类型，
// 但旧数据里它们已经是「一段 Caddy 代码」。硬拆成结构化参数反而有信息丢失风险
// （片段可能含 <%VAR%>、条件块等）。code 类型逐字透传，渲染行为与迁移前一致。
func (b *Builder) planFragments(list []model.Fragment) {
	for _, f := range list {
		id := objects.Slug(f.Name)
		if id == "" {
			id = objects.Slug(f.ID)
		}
		if id == "" {
			b.unmappable("fragments/"+f.ID, "片段名 slug 为空")
			continue
		}
		spec := map[string]any{
			"name":           f.Name,
			"type":           "code",
			"code":           f.Code,
			"defaultEnabled": f.DefaultEnabled,
		}
		if f.Description != "" {
			spec["description"] = f.Description
		}
		b.fragIndex[f.ID] = id
		b.add(typespec.KindMiddleware, id, "fragments/"+f.ID, spec)
	}
}

func (b *Builder) planServices(list []model.Service) {
	for _, svc := range list {
		// Docker 派生服务不落库，这里读到的都是 manual；但仍防御一手。
		if svc.ContainerState != "" {
			b.unmappable("services/"+svc.ID, "docker 派生服务不迁（运行时对象）")
			continue
		}
		backend, ok := backendOf(svc)
		if !ok {
			b.unmappable("services/"+svc.ID, "无法解析上游地址（Upstream 为空或格式非法）")
			continue
		}
		if svc.Type == model.RouteTypeFileServer && svc.Root == "" {
			b.unmappable("services/"+svc.ID, "file_server 缺 root，无法映射")
			continue
		}
		if svc.Type != model.RouteTypeReverseProxy && svc.Type != model.RouteTypeFileServer {
			b.unmappable("services/"+svc.ID, "旧类型 "+svc.Type+" 在 V4.1 无对应服务类型")
			continue
		}

		// 旧 ServiceID 是稳定 id，优先沿用；冲突时加后缀避免覆盖。
		svcID := objects.Slug(svc.Name)
		if svcID == "" {
			svcID = objects.Slug(svc.ID)
		}
		if svcID == "" {
			b.unmappable("services/"+svc.ID, "服务名 slug 为空")
			continue
		}
		svcID = b.unique(typespec.KindService, svcID, "services/"+svc.ID)

		sSpec := map[string]any{
			"name":    svc.Name,
			"type":    svc.Type,
			"backend": backend,
		}
		if svc.Description != "" {
			sSpec["description"] = svc.Description
		}
		switch svc.Type {
		case model.RouteTypeFileServer:
			sSpec["root"] = svc.Root
			sSpec["browse"] = svc.Browse
		default:
			if svc.UpstreamProto != "" {
				sSpec["upstreamProto"] = svc.UpstreamProto
			}
			if svc.HealthURI != "" {
				sSpec["healthUri"] = svc.HealthURI
			}
		}
		b.add(typespec.KindService, svcID, "services/"+svc.ID, sSpec)
		if svc.Type == model.RouteTypeFileServer {
			// route.backend 是必填字段，但静态文件没有上游 → 只能填占位值。
			// 渲染器按服务类型走 file_server 指令、不读 backend，所以渲染是对的；
			// 但如果有人把路由的 service 引用清空，就会渲染成 reverse_proxy 到
			// 占位端口。故记一条告警，提醒确认引用没丢。
			b.warn("services/"+svc.ID,
				"file_server 路由的 backend 是占位 127.0.0.1:1（渲染按服务类型出 root 指令）；"+
					"请确认路由的「服务」引用未被清空")
		}

		// 旧 Domains 数组 → 每条一个 route（ADR-043 §3）。
		if len(svc.Domains) == 0 {
			b.unmappable("services/"+svc.ID, "没有域名/端口接入，拆不出路由（仍已迁出服务对象）")
			continue
		}
		for i, d := range svc.Domains {
			b.planRoute(svc, svcID, backend, d, i)
		}
	}
}

func (b *Builder) planRoute(svc model.Service, svcID string, backend map[string]any, d model.ProxyDomain, idx int) {
	// 路由名：优先 subdomain，其次 host，最后兜底 "<服务名>-<序号>"
	name := d.Subdomain
	if name == "" {
		name = d.Host()
	}
	if name == "" {
		name = fmt.Sprintf("%s-%d", svc.Name, idx+1)
	}
	routeID := objects.Slug(name)
	if routeID == "" {
		b.unmappable(fmt.Sprintf("services/%s#%d", svc.ID, idx), "路由名 slug 为空")
		return
	}
	routeID = b.unique(typespec.KindRoute, routeID, fmt.Sprintf("services/%s#%d", svc.ID, idx))

	proto := d.Protocol
	if proto == "" {
		proto = "https"
	}
	epID := objects.Slug(proto)
	// 旧路由引用的入口点必须一并产出，否则计划自身就有悬空引用
	//（port_bindings 里有就用它，没有就补一条 caddy 内置的）。
	if _, ok := b.seen[typespec.KindEntrypoint+"/"+epID]; !ok {
		b.ensureBuiltinEntrypoint(epID, proto)
	}

	spec := map[string]any{
		"name":       name,
		"service":    svcID,
		"backend":    backend,
		"subdomain":  d.Subdomain,
		"entrypoint": epID,
		"enabled":    svc.Enabled,
		"tls":        tlsOf(d.Protocol),
	}
	if d.RootDomain != "" {
		rootID := objects.Slug(d.RootDomain)
		if rootID == "" {
			b.unmappable(fmt.Sprintf("services/%s#%d", svc.ID, idx), "根域名 slug 为空")
			return
		}
		// 旧路由引用了 domains 表里没有的根域名：补一条，否则 route.roots 悬空。
		if _, ok := b.seen[typespec.KindDomain+"/"+rootID]; !ok {
			b.add(typespec.KindDomain, rootID, "route:"+d.RootDomain,
				map[string]any{"name": d.RootDomain})
		}
		spec["roots"] = []any{rootID}
	} else {
		spec["roots"] = []any{}
	}
	if d.CustomPort && d.Port > 0 {
		spec["port"] = float64(d.Port)
	}
	if mids := b.resolveFragments(svc.FragmentIDs); len(mids) > 0 {
		spec["middlewares"] = mids
	} else {
		spec["middlewares"] = []any{}
	}
	b.add(typespec.KindRoute, routeID, fmt.Sprintf("services/%s#%d", svc.ID, idx), spec)
}

// ensureBuiltinEntrypoint 补一个最小入口点对象（http/https 是 caddy 内置）。
func (b *Builder) ensureBuiltinEntrypoint(id, protocol string) {
	defPort := 443.0
	if protocol == "http" {
		defPort = 80.0
	}
	b.add(typespec.KindEntrypoint, id, "implicit:"+protocol, map[string]any{
		"protocol":    protocol,
		"ports":       []any{defPort},
		"network":     networkOf(protocol),
		"enabled":     true,
		"description": "迁移补齐（caddy 内置协议）",
	})
}

// resolveFragments 旧片段 id → 新中间件 id；缺失的记入无法映射。
//
// 必须查 fragIndex（planFragments 建好的 旧 id → 新 id 映射），不能直接 slug(旧 id)：
// 片段的 id 是 frag-gzip 之类的内部 id，而新对象主键来自 name（GZIP → gzip）。
func (b *Builder) resolveFragments(ids []string) []any {
	out := make([]any, 0, len(ids))
	for _, fid := range ids {
		newID, ok := b.fragIndex[fid]
		if !ok {
			b.unmappable("fragments/"+fid, "被服务引用但 fragments 表里不存在（片段已删）")
			continue
		}
		out = append(out, newID)
	}
	return out
}

// unique 同一 (kind,id) 已被占用时追加序号后缀，并把冲突记进报告。
//
// 只在**撞车**时登记后缀：正常路径不碰 seen（登记由 add 负责），
// 否则 add 会把它当成「重复写入」而丢弃这条对象。
func (b *Builder) unique(kind, id, source string) string {
	key := kind + "/" + id
	prev, taken := b.seen[key]
	if !taken {
		return id
	}
	for i := 2; ; i++ {
		cand := id + "-" + strconv.Itoa(i)
		if _, ok := b.seen[kind+"/"+cand]; ok {
			continue
		}
		b.plan.Conflicts = append(b.plan.Conflicts, Conflict{
			Kind: kind, ID: cand,
			Sources: []string{prev, source},
			Reason:  "两个旧记录映射到同一 id，已加序号后缀避免互相覆盖",
		})
		return cand
	}
}

// add 把一条待写对象加入计划，并检出 (kind,id) 重复。
func (b *Builder) add(kind, id, source string, spec map[string]any) {
	key := kind + "/" + id
	if prev, ok := b.seen[key]; ok {
		b.plan.Conflicts = append(b.plan.Conflicts, Conflict{
			Kind: kind, ID: id,
			Sources: []string{prev, source},
			Reason:  "同一 id 被两条旧记录占用，后写入者被丢弃",
		})
		return
	}
	b.seen[key] = source

	o := objects.Object{
		Kind:   kind,
		ID:     id,
		Key:    keyOf(kind, spec, id),
		Spec:   spec,
		Status: map[string]any{"state": objects.StatePending, "migratedFrom": source},
	}
	action := ActionCreate
	if b.objs != nil {
		if _, err := b.objs.Get(kind, id); err == nil {
			action = ActionSkip
		}
	}
	b.plan.Planned = append(b.plan.Planned, Planned{
		Kind: kind, ID: id, Source: source, Action: action, Object: o,
	})
}

// warn 记一条需人工确认项（按 (source,reason) 去重）。
func (b *Builder) warn(source, reason string) {
	for _, w := range b.plan.Warnings {
		if w.Source == source && w.Reason == reason {
			return
		}
	}
	b.plan.Warnings = append(b.plan.Warnings, Warning{Source: source, Reason: reason})
}

// unmappable 记一条无法映射。同一条旧记录常被多个服务引用（片段被删），
// 这里按 (source,reason) 去重，报告里只出现一行。
func (b *Builder) unmappable(source, reason string) {
	for _, u := range b.plan.Unmappable {
		if u.Source == source && u.Reason == reason {
			return
		}
	}
	b.plan.Unmappable = append(b.plan.Unmappable, Unmappable{Source: source, Reason: reason})
}

// keyOf 取对象显示主键（拿不到就用 id，保证 Key 非空）。
func keyOf(kind string, spec map[string]any, id string) string {
	o := objects.Object{Kind: kind, ID: id, Spec: spec}
	switch kind {
	case typespec.KindDomain:
		return o.String("name")
	case typespec.KindMiddleware:
		return o.String("name")
	case typespec.KindService:
		return o.String("name")
	case typespec.KindRoute:
		return o.String("name")
	}
	return id
}

// backendOf 从旧 Service 解析后端 {host, port}。
func backendOf(svc model.Service) (map[string]any, bool) {
	if svc.Type == model.RouteTypeFileServer {
		// 静态文件没有上游；但 V4.1 的 route.backend 必填。
		// 用占位 host 让文件服务路由也能落库，前端提示需改。
		return map[string]any{"host": "127.0.0.1", "port": float64(1)}, true
	}
	if len(svc.Upstream) == 0 {
		return nil, false
	}
	raw := strings.TrimSpace(svc.Upstream[0])
	if raw == "" {
		return nil, false
	}
	// 支持 "host:port" 与纯 "host"（取端口 80/443 无法判断 → 记为无法映射）
	if host, portStr, err := net.SplitHostPort(raw); err == nil {
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return nil, false
		}
		return map[string]any{"host": host, "port": float64(port)}, true
	}
	if strings.Contains(raw, "://") {
		u := strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://")
		host, portStr, err := net.SplitHostPort(u)
		if err == nil {
			if port, err := strconv.Atoi(portStr); err == nil && port > 0 && port <= 65535 {
				return map[string]any{"host": host, "port": float64(port)}, true
			}
		}
		return map[string]any{"host": u, "port": float64(443)}, true
	}
	// 无端口：https 给 443，http 给 80
	if strings.EqualFold(svc.UpstreamProto, "https") {
		return map[string]any{"host": raw, "port": float64(443)}, true
	}
	return map[string]any{"host": raw, "port": float64(80)}, true
}

// protocolExists 协议是否在类型目录的 entrypoint_protocols 里。
func (b *Builder) protocolExists(proto string) bool {
	for _, td := range b.spec.DynamicOptions(typespec.KindEntrypoint) {
		if td.ID == proto {
			return true
		}
	}
	return false
}

// networkOf 由协议推传输网络。
func networkOf(protocol string) string {
	switch strings.ToLower(protocol) {
	case "udp", "quic":
		return "udp"
	case "tcp", "http", "https", "grpc", "websocket", "":
		return "tcp"
	}
	return "both"
}

// tlsOf 由旧协议推 tls 取值。
func tlsOf(protocol string) string {
	if strings.EqualFold(protocol, "http") {
		return "off"
	}
	return "auto"
}

// Sort 稳定排序（报告可 diff）。
func (p *Plan) Sort() {
	sort.SliceStable(p.Planned, func(i, j int) bool {
		if p.Planned[i].Kind != p.Planned[j].Kind {
			return p.Planned[i].Kind < p.Planned[j].Kind
		}
		return p.Planned[i].ID < p.Planned[j].ID
	})
	sort.SliceStable(p.Conflicts, func(i, j int) bool {
		if p.Conflicts[i].Kind != p.Conflicts[j].Kind {
			return p.Conflicts[i].Kind < p.Conflicts[j].Kind
		}
		return p.Conflicts[i].ID < p.Conflicts[j].ID
	})
	sort.SliceStable(p.Unmappable, func(i, j int) bool { return p.Unmappable[i].Source < p.Unmappable[j].Source })
	sort.SliceStable(p.Warnings, func(i, j int) bool { return p.Warnings[i].Source < p.Warnings[j].Source })

	// JSON 里给空数组而不是 null，省得消费方到处判 null。
	if p.Conflicts == nil {
		p.Conflicts = []Conflict{}
	}
	if p.Unmappable == nil {
		p.Unmappable = []Unmappable{}
	}
	if p.Warnings == nil {
		p.Warnings = []Warning{}
	}
}
