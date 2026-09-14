package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/docker/compose"
	"github.com/JiangBeta/gatebox/internal/models"
	"github.com/JiangBeta/gatebox/internal/store"
)

// 本文件承载编排 Tab 的 REST 处理器(docs §3.2)。
// 部署(up --progress json 流式)走 WebSocket,见 docker_ws.go 的 wsDeploy。

// --- 视图模型 ---

type composeView struct {
	ProjectName    string    `json:"projectName"`
	DisplayName    string    `json:"displayName"`
	Source         string    `json:"source"` // managed / external
	Deployed       bool      `json:"deployed"`
	Status         string    `json:"status"` // running / exited / ""(未部署)
	RunningCount   int       `json:"runningCount"`
	TotalCount     int       `json:"totalCount"`
	Editable       bool      `json:"editable"`
	ConfigFiles    string    `json:"configFiles"`
	LastDeployedAt time.Time `json:"lastDeployedAt,omitempty"`
	CreatedAt      time.Time `json:"createdAt,omitempty"`
}

// statusWord 从 "running(2)" / "exited(1)" / "running(2), exited(1)" 提取主状态词。
func statusWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '('); i >= 0 {
		return s[:i]
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// isManagedConfig 判断 compose 文件是否在 GateBox 的 appData 下(即托管项目)。
func (d *dockerAPI) isManagedConfig(configFiles string) bool {
	first := firstConfigFile(configFiles)
	return strings.HasPrefix(filepath.Clean(first), filepath.Clean(filepath.Join(d.dataDir, "appData"))+string(os.PathSeparator))
}

// firstConfigFile 取逗号分隔列表里的第一个文件路径。
func firstConfigFile(configFiles string) string {
	if i := strings.IndexByte(configFiles, ','); i >= 0 {
		return configFiles[:i]
	}
	return configFiles
}

// listCompose 编排列表:compose ls -a 发现 + DB 合并(docs §2.3 Q24)。
//
// 合并规则:discovered 项目写回 DB(更新 configFiles/status);DB 里有但
// discovered 没有的(compose down 后从 ls -a 消失)仍列出,标记「未部署」。
func (d *dockerAPI) listCompose(w http.ResponseWriter, r *http.Request) {
	projects, err := d.cmp.Discover(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	known, err := d.s.ListComposeInstances()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	knownMap := make(map[string]*models.ComposeInstance, len(known))
	for i := range known {
		knownMap[known[i].ProjectName] = &known[i]
	}

	discovered := make(map[string]compose.Project, len(projects))
	for _, p := range projects {
		discovered[p.Name] = p
	}

	out := make([]composeView, 0, len(projects)+len(known))

	// 先处理 docker 里实际存在的项目
	for _, p := range projects {
		inst, exists := knownMap[p.Name]
		if !exists {
			inst = &models.ComposeInstance{
				ProjectName: p.Name,
				DisplayName: p.Name,
				HostID:      "local",
				Managed:     d.isManagedConfig(p.ConfigFiles),
				ConfigFiles: p.ConfigFiles,
				CreatedAt:   time.Now(),
			}
		} else {
			inst.ConfigFiles = p.ConfigFiles
		}
		// 可编辑 = 托管且单文件。外部项目默认只读,编辑需先「接管」(docs §4.2);
		// 多文件项目连接管入口都不给。
		inst.Editable = inst.Managed && !strings.Contains(p.ConfigFiles, ",")
		_ = d.s.SaveComposeInstance(inst)
		out = append(out, composeViewFromProject(inst, p))
	}

	// DB 有但 docker 里没有的 → 未部署
	for _, inst := range known {
		if _, ok := discovered[inst.ProjectName]; ok {
			continue
		}
		// 托管项目(配置在 appData 下)保持托管且可编辑
		if inst.Managed {
			inst.Editable = true
			_ = d.s.SaveComposeInstance(&inst)
		}
		out = append(out, composeViewFromProject(&inst, compose.Project{}))
	}

	// appData 下存在 compose 文件、但 docker 与 DB 都没有的托管项目 → 补注册列出(未部署),
	// 避免手工放入的托管项目在编排 Tab「消失」。已有 records 由上方循环覆盖。
	for _, name := range d.scanManagedProjects() {
		if _, ok := discovered[name]; ok {
			continue // 已部署,docker 分支已处理并落库
		}
		if _, ok := knownMap[name]; ok {
			continue // DB 已有,上方未部署循环已列出
		}
		cfg := filepath.Join(d.dataDir, "appData", name, "docker-compose.yaml")
		if _, err := os.Stat(cfg); err != nil {
			cfg = filepath.Join(d.dataDir, "appData", name, "docker-compose.yml")
		}
		inst := &models.ComposeInstance{
			ProjectName: name,
			DisplayName: name,
			HostID:      "local",
			Managed:     true,
			Editable:    true,
			ConfigFiles: cfg,
			CreatedAt:   time.Now(),
		}
		_ = d.s.SaveComposeInstance(inst)
		out = append(out, composeViewFromProject(inst, compose.Project{}))
	}

	sort.SliceStable(out, func(i, j int) bool {
		// 已部署的在前,再按展示名
		if out[i].Deployed != out[j].Deployed {
			return out[i].Deployed
		}
		return out[i].DisplayName < out[j].DisplayName
	})

	writeJSON(w, http.StatusOK, out)
}

func composeViewFromProject(inst *models.ComposeInstance, p compose.Project) composeView {
	source := "external"
	if inst.Managed {
		source = "managed"
	}
	return composeView{
		ProjectName:    inst.ProjectName,
		DisplayName:    inst.DisplayName,
		Source:         source,
		Deployed:       p.Name != "",
		Status:         statusWord(p.Status),
		RunningCount:   p.RunningCount(),
		TotalCount:     p.TotalCount(),
		Editable:       inst.Editable,
		ConfigFiles:    inst.ConfigFiles,
		LastDeployedAt: inst.LastDeployedAt,
		CreatedAt:      inst.CreatedAt,
	}
}

// composeInput 创建/保存请求体。
type composeInput struct {
	DisplayName string `json:"displayName"`
	YAML        string `json:"yaml"`
}

// createCompose 创建托管应用:校验 projectName + 校验 YAML + 写盘 + 落库。
func (d *dockerAPI) createCompose(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if !models.ValidateProjectName(project) {
		writeErr(w, http.StatusBadRequest, "projectName 不合法:须为 [a-z0-9][a-z0-9_-]{1,62}")
		return
	}
	var in composeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if strings.TrimSpace(in.YAML) == "" {
		writeErr(w, http.StatusBadRequest, "YAML 不能为空")
		return
	}
	// 容器变量占位(${VAR})在写盘前替换(GB_PROJ_NAME/GB_PROJ_FILE/端口 + 用户变量)。
	resolved, err := d.interpolateContainerVars(project, strings.TrimSpace(in.DisplayName), in.YAML)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	dir := d.cmp.ManagedDir(project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := d.cmp.Validate(r.Context(), resolved, dir); err != nil {
		writeErr(w, http.StatusBadRequest, "校验失败: "+err.Error())
		return
	}
	if err := os.WriteFile(d.cmp.ManagedFile(project), []byte(resolved), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		displayName = project
	}
	inst := &models.ComposeInstance{
		ProjectName: project,
		DisplayName: displayName,
		HostID:      "local",
		Managed:     true,
		ConfigFiles: d.cmp.ManagedFile(project),
		Editable:    true,
		CreatedAt:   time.Now(),
	}
	if err := d.s.SaveComposeInstance(inst); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, composeViewFromProject(inst, compose.Project{}))
}

// getCompose 返回项目元数据 + 盘上 YAML(供编辑器读取)。
func (d *dockerAPI) getCompose(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	inst, err := d.s.GetComposeInstance(project)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "项目不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	file, _ := d.composePaths(inst)
	yamlStr, _ := os.ReadFile(file)

	writeJSON(w, http.StatusOK, map[string]any{
		"projectName":     inst.ProjectName,
		"displayName":     inst.DisplayName,
		"source":          map[bool]string{true: "managed", false: "external"}[inst.Managed],
		"managed":         inst.Managed,
		"editable":        inst.Editable,
		"configFiles":     inst.ConfigFiles,
		"yaml":            string(yamlStr),
		"hasDeployedYAML": inst.LastDeployedYAML != "",
		"lastDeployedAt":  inst.LastDeployedAt,
		// 项目目录(相对运行目录,无结尾 /),供前端把相对路径挂载归并到 ${GB_PROJ_FILE}
		"projectDir": d.cmp.ManagedDir(inst.ProjectName),
	})
}

// saveCompose 保存 YAML(校验 + 写盘 + 更新 DB)。托管项目专用。
func (d *dockerAPI) saveCompose(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	inst, err := d.s.GetComposeInstance(project)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "项目不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !inst.Editable {
		writeErr(w, http.StatusConflict, "该项目不可编辑")
		return
	}

	var in composeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if strings.TrimSpace(in.YAML) == "" {
		writeErr(w, http.StatusBadRequest, "YAML 不能为空")
		return
	}

	displayName := inst.DisplayName
	if dn := strings.TrimSpace(in.DisplayName); dn != "" {
		displayName = dn
	}
	resolved, err := d.interpolateContainerVars(inst.ProjectName, displayName, in.YAML)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	file, dir := d.composePaths(inst)
	if err := d.cmp.Validate(r.Context(), resolved, dir); err != nil {
		writeErr(w, http.StatusBadRequest, "校验失败: "+err.Error())
		return
	}
	if err := os.WriteFile(file, []byte(resolved), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if dn := strings.TrimSpace(in.DisplayName); dn != "" {
		inst.DisplayName = dn
	}
	if err := d.s.SaveComposeInstance(inst); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// validateCompose 独立校验端点:config -q + 三项本地校验,不写盘。
func (d *dockerAPI) validateCompose(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
		YAML    string `json:"yaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	resolved, err := d.interpolateContainerVars(in.Project, "", in.YAML)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	dir := d.cmp.ManagedDir(in.Project)
	_ = os.MkdirAll(dir, 0o755)
	if err := d.cmp.Validate(r.Context(), resolved, dir); err != nil {
		writeErr(w, http.StatusBadRequest, "校验失败: "+err.Error())
		return
	}
	warnings := d.localValidate(r.Context(), resolved, dir)
	warnings = append(warnings, d.detectUndefinedVars(resolved, dir)...)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "warnings": warnings})
}

// downCompose 停止并删除项目容器。
func (d *dockerAPI) downCompose(w http.ResponseWriter, r *http.Request) {
	inst, ok := d.loadCompose(w, r)
	if !ok {
		return
	}
	file, dir := d.composePaths(inst)
	if err := d.cmp.Down(r.Context(), inst.ProjectName, file, dir); err != nil {
		writeDockerErr(w, err)
		return
	}
	d.syncCaddyAsync(r) // 编排动作后自动同步到网关(ADR-026 §7)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// restartCompose 重启项目容器。
func (d *dockerAPI) restartCompose(w http.ResponseWriter, r *http.Request) {
	inst, ok := d.loadCompose(w, r)
	if !ok {
		return
	}
	file, dir := d.composePaths(inst)
	if err := d.cmp.Restart(r.Context(), inst.ProjectName, file, dir); err != nil {
		writeDockerErr(w, err)
		return
	}
	d.syncCaddyAsync(r) // 编排动作后自动同步到网关(ADR-026 §7)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// restoreCompose 恢复上次成功部署的 YAML(docs §4.1 Q16':纯数据,用户主动点击)。
func (d *dockerAPI) restoreCompose(w http.ResponseWriter, r *http.Request) {
	inst, ok := d.loadCompose(w, r)
	if !ok {
		return
	}
	if inst.LastDeployedYAML == "" {
		writeErr(w, http.StatusConflict, "没有可恢复的上次配置")
		return
	}
	file, _ := d.composePaths(inst)
	if err := os.WriteFile(file, []byte(inst.LastDeployedYAML), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteCompose 删除编排(docs §4.5)。
//
// 托管:down(必选)+ 可选删数据目录 / 命名卷;外部:只 down,永不删用户文件。
func (d *dockerAPI) deleteCompose(w http.ResponseWriter, r *http.Request) {
	inst, ok := d.loadCompose(w, r)
	if !ok {
		return
	}

	file, dir := d.composePaths(inst)
	if err := d.cmp.Down(r.Context(), inst.ProjectName, file, dir); err != nil {
		writeErr(w, http.StatusInternalServerError, "停止项目失败: "+err.Error())
		return
	}

	if inst.Managed {
		if r.URL.Query().Get("removeData") == "true" {
			_ = os.RemoveAll(d.cmp.ManagedDir(inst.ProjectName))
		}
		if r.URL.Query().Get("removeVolumes") == "true" {
			d.removeProjectVolumes(r, inst.ProjectName)
		}
	}

	if err := d.s.DeleteComposeInstance(inst.ProjectName); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// removeProjectVolumes 删除该项目的命名卷(前缀 <project>_)。
func (d *dockerAPI) removeProjectVolumes(r *http.Request, project string) {
	vols, _, err := d.cli.ListVolumes(r.Context(), nil)
	if err != nil {
		return
	}
	prefix := project + "_"
	for _, v := range vols {
		if strings.HasPrefix(v.Name, prefix) {
			_ = d.cli.RemoveVolume(r.Context(), v.Name, true)
		}
	}
}

// loadCompose 读取项目并写错误响应。返回 false 表示已写错误、调用方应直接 return。
func (d *dockerAPI) loadCompose(w http.ResponseWriter, r *http.Request) (*models.ComposeInstance, bool) {
	inst, err := d.s.GetComposeInstance(r.PathValue("project"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "项目不存在")
		return nil, false
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return inst, true
}

// composePaths 返回项目的 compose 文件路径与 --project-directory 目录。
func (d *dockerAPI) composePaths(inst *models.ComposeInstance) (file, dir string) {
	if inst.Managed {
		return d.cmp.ManagedFile(inst.ProjectName), d.cmp.ManagedDir(inst.ProjectName)
	}
	file = firstConfigFile(inst.ConfigFiles)
	return file, filepath.Dir(file)
}

// adoptCompose 接管外部项目(docs §4.2):原地标记可编辑。
//
// 拒绝多文件项目(逗号分隔)与用了 include:/extends: 的项目——这两类
// 「原地读写」的写要赌用户文件的复杂度,而赌注是别人 git 仓库里的文件。
func (d *dockerAPI) adoptCompose(w http.ResponseWriter, r *http.Request) {
	inst, ok := d.loadCompose(w, r)
	if !ok {
		return
	}
	if inst.Managed {
		writeErr(w, http.StatusConflict, "该项目已是托管状态")
		return
	}
	if strings.Contains(inst.ConfigFiles, ",") {
		writeErr(w, http.StatusConflict, "该项目由多文件组成,请在源文件中编辑")
		return
	}

	file, _ := d.composePaths(inst)
	content, err := os.ReadFile(file)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if usesIncludeOrExtends(string(content)) {
		writeErr(w, http.StatusConflict, "该项目使用了 include: / extends:,请在源文件中编辑")
		return
	}

	inst.Editable = true
	if err := d.s.SaveComposeInstance(inst); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// usesIncludeOrExtends 检测 compose 文件是否用了 include: / extends: 指令。
func usesIncludeOrExtends(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, kw := range []string{"include:", "extends:"} {
			if strings.HasPrefix(trimmed, kw) || strings.HasPrefix(trimmed, "- "+kw) {
				return true
			}
		}
	}
	return false
}

// listProxyable 跨单位接口:返回可被代理的容器(docs §5.6 / Task 13)。
//
// 稳定标识 = project + service,不含容器 ID 与 IP——从接口层面杜绝下游依赖易变值。
// 语义「可被代理」= running only,与网关侧的「异常保留」(includeStopped)区分。
func (d *dockerAPI) listProxyable(w http.ResponseWriter, r *http.Request) {
	out, err := proxyableContainers(r.Context(), d.cli, d.s, false)
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// proxyableLabels 只保留 caddy.* 与 gatebox.* 前缀的 label。
func proxyableLabels(labels map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range labels {
		if strings.HasPrefix(k, "caddy") || strings.HasPrefix(k, "gatebox") {
			out[k] = v
		}
	}
	return out
}

// scanManagedProjects 扫描 dataDir/appData 下含 compose 文件的托管项目目录名
// (docker-compose.yaml / .yml 任一存在)。空格/点前缀目录视为无关跳过。
func (d *dockerAPI) scanManagedProjects() []string {
	root := filepath.Join(d.dataDir, "appData")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.Contains(e.Name(), " ") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "docker-compose.yaml")); err != nil {
			if _, err := os.Stat(filepath.Join(root, e.Name(), "docker-compose.yml")); err != nil {
				continue
			}
		}
		out = append(out, e.Name())
	}
	return out
}
