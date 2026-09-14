package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/acme"
	"github.com/JiangBeta/gatebox/internal/caddy"
	"github.com/JiangBeta/gatebox/internal/ddns"
	dockerclient "github.com/JiangBeta/gatebox/internal/docker/client"
	"github.com/JiangBeta/gatebox/internal/gateway"
	"github.com/JiangBeta/gatebox/internal/models"
	"github.com/JiangBeta/gatebox/internal/store"
)

// gatewayAPI 网关单位的 HTTP 处理器(docs/gateway.md §5)。
type gatewayAPI struct {
	s               *store.Store
	cli             *dockerclient.Client     // 读 docker 容器派生 service;nil 时无 docker 派生
	caddyCli        *caddy.Client            // /load 原子加载(ADR-002)
	health          *gateway.HealthCollector // 健康快照(ADR-020 §1)
	dataDir         string                   // 定位日志文件与 user 扩展目录
	caddyBin        string                   // caddy 二进制路径(前置 validate;空则 fallback dataDir/tools/caddy/caddy)
	httpPort        int                      // 生成器全局 http_port 覆盖(0=80,与 traefik 共存用)
	httpsPort       int                      // 生成器全局 https_port 覆盖(0=443)
	extraHTTPSPorts []int                    // 额外 https 监听端口(与主端口并存,ADR-026)
	acme            *acme.Issuer             // acme.sh 证书签发(DNS-01,ADR-013 修订)
	ddns            *ddns.Manager            // ddns-go 二级域名上报配置联动

	versionOnce sync.Once // caddy 版本 CLI 读取只做一次(运行期不变)
	version     string
}

// RegisterGateway 注册网关相关路由。返回 gatewayAPI 实例,供 docker 单位回调
// (SyncDockerLabels,ADR-026 §7)。
func RegisterGateway(mux *http.ServeMux, s *store.Store, cli *dockerclient.Client, caddyCli *caddy.Client, health *gateway.HealthCollector, dataDir, caddyBin string, httpPort, httpsPort int, extraHTTPSPorts []int, ac *acme.Issuer) *gatewayAPI {
	a := &gatewayAPI{
		s: s, cli: cli, caddyCli: caddyCli, health: health, dataDir: dataDir, caddyBin: caddyBin,
		httpPort: httpPort, httpsPort: httpsPort, extraHTTPSPorts: extraHTTPSPorts, acme: ac,
		ddns: ddns.NewManager(filepath.Join(dataDir, "tools", "ddnsgo", ".ddns_go_config.yaml")),
	}

	mux.HandleFunc("GET /api/v1/gateway/apps", a.listGroups)
	mux.HandleFunc("POST /api/v1/gateway/apps", a.createApp)
	mux.HandleFunc("GET /api/v1/gateway/apps/{id}", a.getApp)
	mux.HandleFunc("PUT /api/v1/gateway/apps/{id}", a.updateApp)
	mux.HandleFunc("DELETE /api/v1/gateway/apps/{id}", a.deleteApp)

	mux.HandleFunc("POST /api/v1/gateway/apps/{appId}/services", a.createService)
	mux.HandleFunc("POST /api/v1/gateway/services", a.createOrphanService)
	mux.HandleFunc("GET /api/v1/gateway/apps/{appId}/services", a.listAppServices)
	mux.HandleFunc("GET /api/v1/gateway/services/{id}", a.getService)
	mux.HandleFunc("PUT /api/v1/gateway/services/{id}", a.updateService)
	mux.HandleFunc("DELETE /api/v1/gateway/services/{id}", a.deleteService)
	mux.HandleFunc("POST /api/v1/gateway/services/{id}/stop", a.stopService)
	mux.HandleFunc("POST /api/v1/gateway/services/{id}/start", a.startService)
	mux.HandleFunc("GET /api/v1/gateway/services/{id}/logs", a.serviceLogs)

	mux.HandleFunc("GET /api/v1/gateway/fragments", a.listFragments)
	mux.HandleFunc("POST /api/v1/gateway/fragments", a.createFragment)
	mux.HandleFunc("PUT /api/v1/gateway/fragments/{id}", a.updateFragment)
	mux.HandleFunc("DELETE /api/v1/gateway/fragments/{id}", a.deleteFragment)

	mux.HandleFunc("GET /api/v1/gateway/variables", a.listVariables)
	mux.HandleFunc("POST /api/v1/gateway/variables", a.createVariable)
	mux.HandleFunc("PUT /api/v1/gateway/variables/{key}", a.updateVariable)
	mux.HandleFunc("DELETE /api/v1/gateway/variables/{key}", a.deleteVariable)

	mux.HandleFunc("GET /api/v1/gateway/templates", a.listTemplates)
	mux.HandleFunc("GET /api/v1/gateway/health", a.gatewayHealth)
	mux.HandleFunc("GET /api/v1/gateway/version", a.gatewayVersion)

	mux.HandleFunc("GET /api/v1/routes/health", a.gatewayHealth) // 兼容旧端点

	// 网关端口设置(协议端口记录 + 重启加载,ADR-026)
	mux.HandleFunc("GET /api/v1/gateway/ports", a.getGatewayPorts)
	mux.HandleFunc("POST /api/v1/gateway/ports", a.createGatewayPort)
	mux.HandleFunc("PUT /api/v1/gateway/ports/{protocol}", a.updateGatewayPort)
	mux.HandleFunc("DELETE /api/v1/gateway/ports/{protocol}", a.deleteGatewayPort)
	mux.HandleFunc("POST /api/v1/gateway/ports/restart", a.restartGatewayPorts)

	// 网关端口设置(conf 持久化,ADR-026 多 https 端口)
	mux.HandleFunc("GET /api/v1/settings/gateway", a.getGatewaySettings)
	mux.HandleFunc("PUT /api/v1/settings/gateway", a.putGatewaySettings)
	return a
}

// --- 分组列表(App + Docker 自动只读组) ---

// groupView 一个分组(App 或 Docker 只读组)及其下的服务。
type groupView struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Source      string        `json:"source"` // app | docker
	Editable    bool          `json:"editable"`
	Services    []serviceItem `json:"services"`
}

// serviceItem 服务视图 = Service + source + 健康状态 + 归属展示名。
type serviceItem struct {
	models.Service
	Source  string `json:"source"`  // manual | docker
	Health  string `json:"health"`  // healthy | unhealthy | unknown
	AppName string `json:"appName"` // 归属应用展示名("默认"/docker compose 项目)
}

// listGroups 返回服务视图:manual 按归属应用分组(AppID 空归「默认」),
// docker 按 compose 项目分组(组名=项目展示名或项目名),服务项带 appName(ADR-018 修订)。
func (a *gatewayAPI) listGroups(w http.ResponseWriter, r *http.Request) {
	apps, err := a.s.ListApps()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	services, err := a.s.ListServices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	docker := a.deriveDockerServices(r.Context())

	// compose 项目 → 展示名
	dockerNames := map[string]string{}
	if comps, err := a.s.ListComposeInstances(); err == nil {
		for _, c := range comps {
			dockerNames[c.ProjectName] = c.DisplayName
		}
	}
	dockerGroupName := func(project string) string {
		if n, ok := dockerNames[project]; ok && n != "" {
			return n
		}
		return project
	}

	appMap := map[string]*groupView{}
	for _, ap := range apps {
		appMap[ap.ID] = &groupView{ID: ap.ID, Name: ap.Name, Source: "app", Editable: true, Services: []serviceItem{}}
	}
	// 「默认」伪组:未选归属(空 AppID)的 manual 服务落这里。
	defaultGrp := &groupView{ID: "_default", Name: "默认", Source: "default", Editable: true, Services: []serviceItem{}}

	// 服务归入各自 App;AppID 空 → 默认;AppID 悬空(应用已删)兜底「未分组」。
	orphan := &groupView{ID: "_orphan", Name: "未分组", Source: "app", Editable: true, Services: []serviceItem{}}
	for _, svc := range services {
		item := serviceItem{Service: svc, Source: "manual", Health: a.serviceHealthStatus(svc)}
		var g *groupView
		if svc.AppID == "" {
			g = defaultGrp
		} else if gg, ok := appMap[svc.AppID]; ok {
			g = gg
		} else {
			g = orphan
		}
		item.AppName = g.Name
		g.Services = append(g.Services, item)
	}

	// Docker 自动服务按 compose 项目拆只读组;归属=项目展示名。
	dockerByName := map[string]*groupView{}
	for _, svc := range docker {
		project := svc.AppID
		g, ok := dockerByName[project]
		if !ok {
			g = &groupView{ID: "docker:" + project, Name: dockerGroupName(project), Source: "docker", Editable: false, Services: []serviceItem{}}
			dockerByName[project] = g
		}
		item := serviceItem{Service: svc, Source: "docker", Health: a.serviceHealthStatus(svc), AppName: g.Name}
		g.Services = append(g.Services, item)
	}

	var out []groupView = []groupView{}
	for _, g := range appMap {
		if g.ID != "_orphan" && g.ID != "_default" {
			out = append(out, *g)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	// 「默认」「未分组」仅在确实含有服务时展示。
	if len(defaultGrp.Services) > 0 {
		out = append(out, *defaultGrp)
	}
	if o, ok := appMap["_orphan"]; ok && len(o.Services) > 0 {
		out = append(out, *o)
	}
	var dgs []groupView
	for _, g := range dockerByName {
		dgs = append(dgs, *g)
	}
	sort.SliceStable(dgs, func(i, j int) bool { return dgs[i].Name < dgs[j].Name })
	out = append(out, dgs...)

	writeJSON(w, http.StatusOK, out)
}

// serviceHealthStatus 返回某服务的健康状态;仅反代且带 upstream 才有意义。
func (a *gatewayAPI) serviceHealthStatus(svc models.Service) string {
	if svc.Type != models.RouteTypeReverseProxy || len(svc.Upstream) == 0 || a.health == nil {
		return gateway.HealthUnknown
	}
	return a.health.Status(svc.Upstream[0])
}

// gatewayHealth 网关/上游健康总览(ADR-020 §1)。
func (a *gatewayAPI) gatewayHealth(w http.ResponseWriter, r *http.Request) {
	if a.health == nil {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "upstreams": map[string]string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reachable": a.health.Reachable(),
		"upstreams": a.health.Snapshot(),
	})
}

// gatewayVersion 返回 caddy 版本与可达性(供代理 Tab 统计行)。
//
// caddy admin API 无 /version 端点,改用受管二进制 CLI `caddy version`;
// 版本运行期不变,首次成功读取后缓存(sync.Once)。
func (a *gatewayAPI) gatewayVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": a.caddyVersion(r.Context()), "reachable": a.version != ""})
}

func (a *gatewayAPI) caddyVersion(ctx context.Context) string {
	a.versionOnce.Do(func() {
		bin := a.caddyBin
		if bin == "" {
			bin = filepath.Join(a.dataDir, "tools", "caddy", "caddy")
		}
		if v, err := caddy.BinaryVersion(ctx, bin); err == nil {
			a.version = v
		}
	})
	return a.version
}

// --- App(应用) ---

// appInput 创建 / 编辑应用的入参(可附带批量创建服务)。
type appInput struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Services    []serviceInput `json:"services,omitempty"`
}

// serviceInput 创建 / 编辑服务的入参。
type serviceInput struct {
	AppID              *string          `json:"appId,omitempty"` // 归属应用;nil=不变/默认。编辑时指定=new 归属,忽略=保留
	Name               string           `json:"name"`
	Description        string           `json:"description"`
	Type               string           `json:"type"` // reverse_proxy | file_server
	Domains            []domainRowInput `json:"domains"`
	Upstream           []string         `json:"upstream,omitempty"`
	UpstreamProto      string           `json:"upstreamProto,omitempty"`
	Root               string           `json:"root,omitempty"`
	Browse             bool             `json:"browse,omitempty"`
	HealthURI          *string          `json:"healthUri,omitempty"` // nil=默认"/";空=关闭;非空=指定
	FragmentIDs        []string         `json:"fragmentIds,omitempty"`
	ExcludeFragmentIDs []string         `json:"excludeFragmentIds,omitempty"`
}

// domainRowInput 一行域名。
type domainRowInput struct {
	Protocol   string `json:"protocol"` // https | http
	Subdomain  string `json:"subdomain"`
	RootDomain string `json:"rootDomain"`
	CustomPort bool   `json:"customPort"`
	Port       int    `json:"port,omitempty"`
}

func (in *serviceInput) validate() (string, bool) {
	if strings.TrimSpace(in.Name) == "" {
		return "服务名不能为空", false
	}
	switch in.Type {
	case models.RouteTypeReverseProxy:
		// 单上游校验升级为多上游(负载均衡,ADR-018「字段数组化向后兼容」)。
		if len(in.Upstream) == 0 {
			return "反向代理需要后端地址", false
		}
		for _, u := range in.Upstream {
			if strings.TrimSpace(u) == "" {
				return "反向代理后端地址不能为空", false
			}
		}
	case models.RouteTypeFileServer:
		if strings.TrimSpace(in.Root) == "" {
			return "静态文件需要根目录", false
		}
	case "":
		// type 未填时给出默认值,交由 caller 填充
	default:
		return "未知服务类型: " + in.Type, false
	}
	if len(in.Domains) == 0 {
		return "至少需要一个域名", false
	}
	for _, d := range in.Domains {
		if strings.TrimSpace(d.RootDomain) == "" {
			return "域名行缺少 rootDomain", false
		}
		if d.Protocol == "" {
			return "域名行缺少协议", false
		}
		if d.CustomPort && (d.Port < 1 || d.Port > 65535) {
			return "自定义端口需在 1~65535", false
		}
	}
	return "", true
}

// healthURIValue 计算 healthUri:反代默认 "/";其余为空(关闭)。
func (in *serviceInput) healthURIValue() string {
	if in.Type != models.RouteTypeReverseProxy {
		return ""
	}
	if in.HealthURI == nil {
		return "/"
	}
	return *in.HealthURI
}

func (in *serviceInput) toService(appID string, enabled bool, fragIDs []string, createdAt time.Time) models.Service {
	domains := make([]models.ProxyDomain, 0, len(in.Domains))
	for _, d := range in.Domains {
		domains = append(domains, models.ProxyDomain{
			Protocol: d.Protocol, Subdomain: strings.TrimSpace(d.Subdomain),
			RootDomain: strings.TrimSpace(d.RootDomain), CustomPort: d.CustomPort, Port: d.Port,
		})
	}
	proto := in.UpstreamProto
	if in.Type == models.RouteTypeReverseProxy && proto == "" {
		proto = "http"
	}
	// HTTPS 后端自动注入忽略证书校验片段(前端隐藏此片段,由后端保证)
	if in.Type == models.RouteTypeReverseProxy && proto == "https" {
		has := false
		for _, id := range fragIDs {
			if id == models.FragmentSkipVerify {
				has = true
				break
			}
		}
		if !has {
			fragIDs = append(fragIDs, models.FragmentSkipVerify)
		}
	}
	return models.Service{
		ID: newID(), AppID: appID, Name: strings.TrimSpace(in.Name), Description: in.Description,
		Type: in.Type, Domains: domains, Upstream: in.Upstream, UpstreamProto: proto,
		Root: in.Root, Browse: in.Browse, HealthURI: in.healthURIValue(),
		Enabled: enabled, FragmentIDs: fragIDs, ExcludeFragmentIDs: in.ExcludeFragmentIDs,
		CreatedAt: createdAt,
	}
}

// seedDefaultFragments 把「默认启用」的片段(含 hidden)seed 进 FragmentIDs,除非被显式排除。
// 保证未显式勾选的默认片段始终生效(ADR-018 修订 Q8)。强制隐藏的默认片段不可排除。
func (a *gatewayAPI) seedDefaultFragments(userIDs, exclude []string) []string {
	set := map[string]bool{}
	for _, id := range userIDs {
		set[id] = true
	}
	excluded := map[string]bool{}
	for _, id := range exclude {
		excluded[id] = true
	}

	userFrags, _ := a.s.ListFragments()
	toggles, _ := a.s.ListFragmentToggles()

	apply := func(id string, defaultEnabled, defaultHidden bool) {
		if !defaultEnabled {
			return
		}
		if excluded[id] && !defaultHidden {
			return
		}
		set[id] = true
	}
	for _, f := range userFrags {
		apply(f.ID, f.DefaultEnabled, f.DefaultHidden)
	}
	for _, b := range caddy.BuiltinFragmentCatalog() {
		enabled, hidden := b.DefaultEnabled, b.DefaultHidden
		if t, ok := toggles[b.ID]; ok {
			enabled, hidden = t.DefaultEnabled, t.DefaultHidden
		}
		apply(b.ID, enabled, hidden)
	}

	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (a *gatewayAPI) createApp(w http.ResponseWriter, r *http.Request) {
	var in appInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "应用名不能为空")
		return
	}
	app := &models.App{ID: newID(), Name: strings.TrimSpace(in.Name), Description: in.Description, CreatedAt: time.Now()}
	if err := a.s.SaveApp(app); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now()
	created := make([]models.Service, 0, len(in.Services))
	for _, si := range in.Services {
		if msg, ok := si.validate(); !ok {
			writeErr(w, http.StatusBadRequest, msg)
			_ = a.s.DeleteApp(app.ID) // 回滚应用,避免半成品
			return
		}
		svc := si.toService(app.ID, true, a.seedDefaultFragments(si.FragmentIDs, si.ExcludeFragmentIDs), now)
		if err := a.s.SaveService(&svc); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			_ = a.s.DeleteApp(app.ID)
			return
		}
		created = append(created, svc)
	}

	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "配置已保存但 caddy 加载失败: "+err.Error())
		return
	}
	resp := struct {
		models.App
		Services []models.Service `json:"services"`
	}{*app, created}
	writeJSON(w, http.StatusCreated, resp)
}

func (a *gatewayAPI) getApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "docker" {
		writeErr(w, http.StatusBadRequest, "docker 组为只读")
		return
	}
	app, err := a.s.GetApp(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "应用不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	svcs, err := a.s.ListServicesByApp(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := struct {
		models.App
		Services []models.Service `json:"services"`
	}{*app, svcs}
	writeJSON(w, http.StatusOK, resp)
}

func (a *gatewayAPI) updateApp(w http.ResponseWriter, r *http.Request) {
	app, err := a.s.GetApp(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "应用不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in appInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "应用名不能为空")
		return
	}
	app.Name = strings.TrimSpace(in.Name)
	app.Description = in.Description
	if err := a.s.SaveApp(app); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, app)
}

// deleteApp 删除应用及其级联删除所有服务(ADR-018 修订:App 删除级联)。
// 要求:应用下所有服务都已停用(enabled=false),否则 409。
func (a *gatewayAPI) deleteApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	app, err := a.s.GetApp(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "应用不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	svcs, err := a.s.ListServicesByApp(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, svc := range svcs {
		if svc.Enabled {
			writeErr(w, http.StatusConflict, "请先停止应用下的所有服务再删除")
			return
		}
	}
	if err := a.s.DeleteApp(app.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, svc := range svcs {
		if err := a.s.DeleteService(svc.ID); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "已删除但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// --- Service(服务) ---

func (a *gatewayAPI) listAppServices(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("appId")
	if _, err := a.s.GetApp(id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "应用不存在")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	svcs, err := a.s.ListServicesByApp(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]serviceItem, 0, len(svcs))
	for _, svc := range svcs {
		out = append(out, serviceItem{Service: svc, Source: "manual", Health: a.serviceHealthStatus(svc)})
	}
	writeJSON(w, http.StatusOK, out)
}

// createOrphanService 无应用上下文创建服务(ADR-018 修订:服务与应用弱绑定)。
// AppID 指定则归属该应用;为空则归属「默认」。
func (a *gatewayAPI) createOrphanService(w http.ResponseWriter, r *http.Request) {
	var in serviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if msg, ok := in.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	appID := ""
	if in.AppID != nil && *in.AppID != "" {
		appID = *in.AppID
		if _, err := a.s.GetApp(appID); errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "归属应用不存在")
			return
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	svc := in.toService(appID, true, a.seedDefaultFragments(in.FragmentIDs, in.ExcludeFragmentIDs), time.Now())
	if err := a.s.SaveService(&svc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "配置已保存但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, svc)
}

func (a *gatewayAPI) createService(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("appId")
	if _, err := a.s.GetApp(appID); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "应用不存在")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in serviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if msg, ok := in.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	svc := in.toService(appID, true, a.seedDefaultFragments(in.FragmentIDs, in.ExcludeFragmentIDs), time.Now())
	if err := a.s.SaveService(&svc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "配置已保存但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, svc)
}

func (a *gatewayAPI) getService(w http.ResponseWriter, r *http.Request) {
	svc, err := a.s.GetService(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "服务不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

func (a *gatewayAPI) updateService(w http.ResponseWriter, r *http.Request) {
	svc, err := a.s.GetService(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "服务不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in serviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if msg, ok := in.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	appID := svc.AppID
	if in.AppID != nil {
		appID = *in.AppID
	}
	newSvc := in.toService(appID, svc.Enabled, a.seedDefaultFragments(in.FragmentIDs, in.ExcludeFragmentIDs), svc.CreatedAt)
	newSvc.ID = svc.ID
	if err := a.s.SaveService(&newSvc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "配置已保存但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, newSvc)
}

func (a *gatewayAPI) deleteService(w http.ResponseWriter, r *http.Request) {
	svc, err := a.s.GetService(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "服务不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if svc.Enabled {
		writeErr(w, http.StatusConflict, "请先停止该服务再删除")
		return
	}
	if err := a.s.DeleteService(svc.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "已删除但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (a *gatewayAPI) stopService(w http.ResponseWriter, r *http.Request) {
	a.setServiceEnabled(w, r, false)
}

func (a *gatewayAPI) startService(w http.ResponseWriter, r *http.Request) {
	a.setServiceEnabled(w, r, true)
}

func (a *gatewayAPI) setServiceEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	svc, err := a.s.GetService(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "服务不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if svc.Enabled == enabled {
		writeJSON(w, http.StatusOK, svc)
		return
	}
	svc.Enabled = enabled
	if err := a.s.SaveService(svc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "状态已更新但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

// --- Fragment(Caddy 片段) ---

// fragmentView 下发的片段:用户片段 + 只读内置片段(builtin 标记)。
type fragmentView struct {
	models.Fragment
	Builtin bool `json:"builtin"`
}

// builtinFragmentByID 按 ID 在内置目录中查找。(不存在,false)
func builtinFragmentByID(id string) (models.FragmentTemplate, bool) {
	for _, b := range caddy.BuiltinFragmentCatalog() {
		if b.ID == id {
			return b, true
		}
	}
	return models.FragmentTemplate{}, false
}

// fragmentToView 构造片段下发视图(内置条目骨架)。
func fragmentToView(f models.Fragment, builtin bool) fragmentView {
	return fragmentView{Fragment: f, Builtin: builtin}
}

func (a *gatewayAPI) listTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, caddy.BuiltinFragmentCatalog())
}

func (a *gatewayAPI) listFragments(w http.ResponseWriter, r *http.Request) {
	frags, err := a.s.ListFragments()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	toggles, err := a.s.ListFragmentToggles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]fragmentView, 0, len(frags)+len(caddy.BuiltinFragmentCatalog()))
	for _, f := range frags {
		out = append(out, fragmentView{Fragment: f, Builtin: false})
	}
	for _, b := range caddy.BuiltinFragmentCatalog() {
		f := models.Fragment{
			ID: b.ID, Name: b.Name, Description: b.Description,
			DefaultEnabled: b.DefaultEnabled, DefaultHidden: b.DefaultHidden, Code: b.Code,
		}
		if t, ok := toggles[b.ID]; ok {
			f.DefaultEnabled = t.DefaultEnabled
			f.DefaultHidden = t.DefaultHidden
		}
		out = append(out, fragmentView{Fragment: f, Builtin: true})
	}
	writeJSON(w, http.StatusOK, out)
}

type fragmentInput struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	DefaultEnabled bool   `json:"defaultEnabled"`
	DefaultHidden  bool   `json:"defaultHidden"`
	Code           string `json:"code"`
}

func (a *gatewayAPI) createFragment(w http.ResponseWriter, r *http.Request) {
	var in fragmentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "片段名不能为空")
		return
	}
	f := &models.Fragment{
		ID: newID(), Name: in.Name, Description: in.Description,
		DefaultEnabled: in.DefaultEnabled, DefaultHidden: in.DefaultHidden, Code: in.Code,
		CreatedAt: time.Now(),
	}
	if err := a.s.SaveFragment(f); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 新片段不改现有服务配置;仅在保存后重新生成(片段本身可能被引用)。
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "配置已保存但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (a *gatewayAPI) updateFragment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in fragmentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	// 内置片段:仅允许更新默认开关并持久化,其余字段(名称/类型/code)不可改。
	if b, ok := builtinFragmentByID(id); ok {
		if err := a.s.SaveFragmentToggle(id, models.BuiltinFragmentToggle{
			DefaultEnabled: in.DefaultEnabled, DefaultHidden: in.DefaultHidden,
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		f := models.Fragment{
			ID: b.ID, Name: b.Name, Description: b.Description,
			DefaultEnabled: in.DefaultEnabled, DefaultHidden: in.DefaultHidden, Code: b.Code,
		}
		if err := a.reloadCaddy(r.Context()); err != nil {
			writeErr(w, http.StatusInternalServerError, "配置已保存但 caddy 加载失败: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, fragmentToView(f, true))
		return
	}
	f, err := a.s.GetFragment(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "片段不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "片段名不能为空")
		return
	}
	f.Name = in.Name
	f.Description = in.Description
	f.DefaultEnabled = in.DefaultEnabled
	f.DefaultHidden = in.DefaultHidden
	f.Code = in.Code
	if err := a.s.SaveFragment(f); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "配置已保存但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (a *gatewayAPI) deleteFragment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// 被服务引用时禁删(ADR-018 修订)。
	svcs, err := a.s.ListServices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, svc := range svcs {
		for _, fid := range svc.FragmentIDs {
			if fid == id {
				writeErr(w, http.StatusConflict, "该片段被服务引用,无法删除")
				return
			}
		}
	}
	if err := a.s.DeleteFragment(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "已删除但 caddy 加载失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// --- Variable(用户变量) ---

type variableInput struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

// validVariableKey 校验 key:非空、不得以 GB_ 开头(保留前缀,ADR-018 修订 §5.1)。
func validVariableKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if strings.HasPrefix(key, "GB_") {
		return false
	}
	for _, r := range key {
		if !(r == '_' || r == '-' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func (a *gatewayAPI) listVariables(w http.ResponseWriter, r *http.Request) {
	vs, err := a.s.ListVariables()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

func (a *gatewayAPI) createVariable(w http.ResponseWriter, r *http.Request) {
	var in variableInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if !validVariableKey(in.Key) {
		writeErr(w, http.StatusBadRequest, "变量 key 不合法(不得以 GB_ 开头)")
		return
	}
	key := strings.TrimSpace(in.Key)
	if _, err := a.s.GetVariable(key); err == nil {
		writeErr(w, http.StatusConflict, "变量已存在")
		return
	}
	v := &models.Variable{Key: key, Value: in.Value, Description: in.Description, CreatedAt: time.Now()}
	if err := a.s.SaveVariable(v); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		log.Printf("[gateway] variable create saved but caddy reload failed: %v", err)
	}
	writeJSON(w, http.StatusCreated, v)
}

func (a *gatewayAPI) updateVariable(w http.ResponseWriter, r *http.Request) {
	v, err := a.s.GetVariable(r.PathValue("key"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "变量不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in variableInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	v.Value = in.Value
	v.Description = in.Description
	if err := a.s.SaveVariable(v); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		log.Printf("[gateway] variable update saved but caddy reload failed: %v", err)
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *gatewayAPI) deleteVariable(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if _, err := a.s.GetVariable(key); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "变量不存在")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.s.DeleteVariable(key); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.reloadCaddy(r.Context()); err != nil {
		log.Printf("[gateway] variable delete saved but caddy reload failed: %v", err)
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// --- Caddyfile 联动 ---

// SyncDockerLabels 触发一次派生 + 重载(docker 页「同步到网关」/ 编排动作自动同步,
// ADR-026 §7)。语义与任何网关写操作后的 reloadCaddy 完全一致。
func (a *gatewayAPI) SyncDockerLabels(ctx context.Context) error {
	return a.reloadCaddy(ctx)
}

// reloadCaddy 重新生成 Caddyfile 并原子加载(manual 落库 + docker 派生 + 片段 + 变量)。
func (a *gatewayAPI) reloadCaddy(ctx context.Context) error {
	services, err := a.s.ListServices()
	if err != nil {
		return err
	}
	services = append(services, a.deriveDockerServices(ctx)...)
	apps, err := a.s.ListApps()
	if err != nil {
		return err
	}
	fragments, err := a.s.ListFragments()
	if err != nil {
		return err
	}
	variables, err := a.s.ListVariables()
	if err != nil {
		return err
	}
	dns, err := a.buildDNSMap()
	if err != nil {
		return err
	}
	// 证书 DNS-01:由独立 acme.sh 签发(ADR-013 修订)。caddy 仅按 tls 文件加载。
	// 签发较慢(DNS 验证),用独立超时 context,不随请求取消,并并行签发各域名。
	// 签发失败仅告警并继续:证书缺失由 caddy load 时对具体域名失败,不阻塞其余操作
	// (如停/删其他服务)。失败进入 issuer 冷却,短时间内不重复。
	if a.acme != nil {
		acmeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		type job struct {
			fqdn string
			cred models.DNSCredential
		}
		var jobs []job
		for _, svc := range services {
			for _, d := range svc.Domains {
				if d.Protocol != models.DomainProtoHTTPS {
					continue
				}
				cred, ok := dns[d.RootDomain]
				if !ok {
					continue // 未登记凭证的 HTTPS:不签发(保持 caddy 行为,留空 tls 由管理员处理)
				}
				jobs = append(jobs, job{fqdn: d.Host(), cred: cred})
			}
		}
		var wg sync.WaitGroup
		for _, j := range jobs {
			wg.Add(1)
			go func(fqdn string, cred models.DNSCredential) {
				defer wg.Done()
				logFile, err := a.acme.Ensure(acmeCtx, cred, fqdn)
				if err != nil {
					log.Printf("证书签发失败(继续): %s: %v", fqdn, err)
					a.logCert("ensure", fqdn, "fail", err.Error(), logFile)
				} else {
					a.logCert("ensure", fqdn, "success", "证书已签发/有效", logFile)
				}
			}(j.fqdn, j.cred)
		}
		wg.Wait()
	}
	opts, err := a.caddyPortOptions()
	if err != nil {
		return err
	}
	caddyfile, err := caddy.Generate(services, apps, fragments, variables, caddy.BuiltinFragmentCatalog(), a.dataDir, dns, opts)
	if err != nil {
		return err
	}
	// 前置校验(ADR-002「先校验、失败回退」):配置非法则硬失败拦截,不进入 /load。
	// caddy 二进制缺失/不可执行时降级跳过并告警(与证书/DDNS 失败同策略)。
	if err := a.validateCaddyfile(ctx, caddyfile); err != nil {
		return err
	}
	if err := a.caddyCli.Load(ctx, caddyfile); err != nil {
		return err
	}
	// 成功加载后写 $DATA_DIR/Caddyfile 备份(ADR-002「成功落盘」/ADR-022 上提)。
	a.persistCaddyfile(caddyfile)
	// DDNS 上报配置随每次配置变更重算(创建/删除/启停服务都会经过这里):
	// 只统计落库的启用服务,删除/停用自动从上报集合移除(ADR-021 §2)。
	a.syncDDNSWarn()
	return nil
}

// validateCaddyfile 生成后前置校验(软失败策略)。
func (a *gatewayAPI) validateCaddyfile(ctx context.Context, caddyfile string) error {
	bin := a.caddyBin
	if bin == "" {
		bin = filepath.Join(a.dataDir, "tools", "caddy", "caddy")
	}
	if err := caddy.Validate(ctx, bin, caddyfile); err != nil {
		if errors.Is(err, caddy.ErrValidateUnavailable) {
			log.Printf("caddy validate 不可用,跳过前置校验: %v", err)
			return nil
		}
		return err
	}
	return nil
}

// persistCaddyfile 把最近一次成功 /load 的 Caddyfile 原子备份到 $DATA_DIR/Caddyfile
// (ADR-002「成功落盘」/ADR-022 备份上提)。失败仅告警,不阻塞主流程——配置已生效,
// 备份只影响后续手动恢复能力。
func (a *gatewayAPI) persistCaddyfile(caddyfile string) {
	target := filepath.Join(a.dataDir, "Caddyfile")
	tmp, err := os.CreateTemp(a.dataDir, ".Caddyfile.tmp-*")
	if err != nil {
		log.Printf("Caddyfile 备份写盘失败(创建临时文件): %v", err)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(caddyfile); err != nil {
		tmp.Close()
		log.Printf("Caddyfile 备份写盘失败(写入): %v", err)
		return
	}
	if err := tmp.Close(); err != nil {
		log.Printf("Caddyfile 备份写盘失败(关闭): %v", err)
		return
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		log.Printf("Caddyfile 备份写盘失败(设置权限): %v", err)
		return
	}
	if err := os.Rename(tmpName, target); err != nil {
		log.Printf("Caddyfile 备份写盘失败(替换): %v", err)
		return
	}
}

// syncDDNSWarn 把当前启用服务的二级域名上报集合写入 ddns-go 配置。
// 失败仅告警不阻塞主流程(与证书签发失败同策略);ddns-go 周期重读配置,无需重启。
func (a *gatewayAPI) syncDDNSWarn() {
	if a.ddns == nil {
		return
	}
	services, err := a.s.ListServices()
	if err != nil {
		log.Printf("[gateway] ddns 同步读服务失败: %v", err)
		return
	}
	domains, err := a.s.ListDomains()
	if err != nil {
		log.Printf("[gateway] ddns 同步读域名失败: %v", err)
		return
	}
	creds, err := a.s.ListCredentials()
	if err != nil {
		log.Printf("[gateway] ddns 同步读凭证失败: %v", err)
		return
	}
	entries := ddns.BuildEntries(services, domains, creds)
	if err := a.ddns.Sync(entries); err != nil {
		log.Printf("[gateway] ddns 配置写盘失败(继续): %v", err)
	}
}

// buildDNSMap 构建 rootDomain → DNS 凭证映射,供 tls dns(DNS-01 证书签发)用。
func (a *gatewayAPI) buildDNSMap() (map[string]models.DNSCredential, error) {
	domains, err := a.s.ListDomains()
	if err != nil {
		return nil, err
	}
	creds, err := a.s.ListCredentials()
	if err != nil {
		return nil, err
	}
	credMap := make(map[string]models.DNSCredential, len(creds))
	for _, c := range creds {
		credMap[c.ID] = c
	}
	dns := make(map[string]models.DNSCredential, len(domains))
	for _, d := range domains {
		if c, ok := credMap[d.CredentialID]; ok {
			dns[d.Name] = c
		}
	}
	return dns, nil
}

// deriveDockerServices 派生 docker 自动 Service(docker 不可用时为空)。
func (a *gatewayAPI) deriveDockerServices(ctx context.Context) []models.Service {
	if a.cli == nil {
		return nil
	}
	containers, err := proxyableContainers(ctx, a.cli, a.s, true) // true:含异常容器(异常保留)
	if err != nil {
		return nil
	}
	domains, _ := a.s.ListDomains()
	fragments, _ := a.s.ListFragments()
	// gatebox.fragments 按名解析:用户片段 + 只读内置片段都可按名引用(ADR-026 §3)。
	for _, b := range caddy.BuiltinFragmentCatalog() {
		fragments = append(fragments, models.Fragment{ID: b.ID, Name: b.Name, Code: b.Code})
	}
	// 默认启用片段集(ADR-026 §3):与 manual Service 的 preselect 一致,派生自动带上。
	defaults := a.seedDefaultFragments(nil, nil)
	return gateway.DeriveRoutes(containers, domains, fragments, defaults)
}

// proxyableContainers 读容器并映射为网关单位消费的视图(ADR-016/019)。
// docker 单位与网关单位共用此实现,消除 DTO 重复。
// includeStopped=false 等价「可被代理」(仅 running,ADR-016,供 /docker/proxyable);
// true 时保留异常容器,供网关「异常保留」展示(gateway.md §9.3)。
func proxyableContainers(ctx context.Context, cli *dockerclient.Client, s *store.Store, includeStopped bool) ([]gateway.ProxyableContainer, error) {
	list, err := cli.ListContainers(ctx, dockerclient.ListContainersOptions{All: includeStopped})
	if err != nil {
		return nil, err
	}
	displayNames := map[string]string{}
	if comps, err := s.ListComposeInstances(); err == nil {
		for _, c := range comps {
			displayNames[c.ProjectName] = c.DisplayName
		}
	}
	out := make([]gateway.ProxyableContainer, 0, len(list))
	for _, ct := range list {
		if !includeStopped && ct.State != "running" {
			continue // 仅「可被代理」语义时丢弃非 running
		}
		pc := gateway.ProxyableContainer{
			Project:       ct.ComposeProject(),
			Service:       ct.ComposeService(),
			ContainerName: ct.Name(),
			DisplayName:   displayNames[ct.ComposeProject()],
			Labels:        proxyableLabels(ct.Labels),
			State:         ct.State,
		}
		for _, p := range ct.Ports {
			if p.Type != "" && p.Type != "tcp" {
				continue
			}
			pp := gateway.ProxyablePort{Internal: p.PrivatePort}
			if p.PublicPort > 0 {
				pp.Host = p.PublicPort
				if pc.HostPort == 0 {
					pc.HostPort = int(p.PublicPort)
				}
			}
			pc.Ports = append(pc.Ports, pp)
		}
		out = append(out, pc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Service < out[j].Service
	})
	return out, nil
}
