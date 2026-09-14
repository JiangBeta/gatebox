package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/docker/client"
)

// 本文件承载镜像 / 网络 / 存储卷三个 Tab 的 REST 处理器(docs §3.3 ~ §3.5)。
// 这三个对象是 docker 的运行时对象,GateBox 只读不存;删除前须做「占用检查」
// 并列出占用者(docs §4.5),不能只报一句「正在使用」。

// --- 视图模型 ---

// imageView 镜像列表项。InUse 与 Containers 由 imageUsage 反查容器得出,
// 因为 /images/json 的 Containers 字段恒为 -1(实测不填充)。
type imageView struct {
	ID         string    `json:"id"`
	Names      []string  `json:"names"`   // RepoTags,悬空镜像为空
	Digests    []string  `json:"digests"` // RepoDigests
	Arch       string    `json:"arch"`    // 平台,如 linux/amd64(来自 inspect)
	Size       int64     `json:"size"`
	CreatedAt  time.Time `json:"createdAt"`
	InUse      bool      `json:"inUse"`
	Containers []string  `json:"containers,omitempty"`
}

// networkView 网络列表项。
type networkView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Driver     string    `json:"driver"`
	Scope      string    `json:"scope"`
	Internal   bool      `json:"internal"`
	Attachable bool      `json:"attachable"`
	Subnet     string    `json:"subnet,omitempty"`
	Gateway    string    `json:"gateway,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// networkContainerView 连接到某网络的容器(查看弹层用)。
type networkContainerView struct {
	Name    string `json:"name"`
	IPv4    string `json:"ipv4"`
	IPv6    string `json:"ipv6,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

// volumeView 存储卷列表项。InUse 由 volumeUsage 反查容器 Mounts 得出,
// 因为 /volumes 的 UsageData 恒为 null(实测 daemon 不填充)。
type volumeView struct {
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Driver      string    `json:"driver"`
	Mountpoint  string    `json:"mountpoint"`
	Scope       string    `json:"scope"`
	InUse       bool      `json:"inUse"`
	Containers  []string  `json:"containers,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// volumeDisplayName 计算卷的友好名称:
//   - compose 卷(label 带 com.docker.compose.project)剥离「project_」前缀;
//   - 匿名卷(纯 64 位 hex 哈希、无 compose label)显示「匿名卷」;
//   - 其余命名卷显示原名。
func volumeDisplayName(v *client.Volume) string {
	if proj := v.ComposeProject(); proj != "" {
		prefix := proj + "_"
		if strings.HasPrefix(v.Name, prefix) {
			return v.Name[len(prefix):]
		}
		return v.Name
	}
	if isHexHash(v.Name, 64) {
		return "匿名卷"
	}
	return v.Name
}

// isHexHash 判断 s 是否恰为 n 位小写十六进制。
func isHexHash(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// --- 镜像 ---

// listImages 列出镜像,并发补架构信息并反查是否被容器占用。
func (d *dockerAPI) listImages(w http.ResponseWriter, r *http.Request) {
	images, err := d.cli.ListImages(r.Context(), client.ListImagesOptions{All: false})
	if err != nil {
		writeDockerErr(w, err)
		return
	}

	archs := inspectImagePlatforms(r.Context(), d.cli, images)
	usage := d.imageUsage(r.Context())

	out := make([]imageView, 0, len(images))
	for _, img := range images {
		v := imageView{
			ID:        img.ID,
			Names:     img.RepoTags,
			Digests:   img.RepoDigests,
			Arch:      archs[img.ID],
			Size:      img.Size,
			CreatedAt: img.CreatedAt(),
		}
		if names := usage[img.ID]; len(names) > 0 {
			v.InUse = true
			v.Containers = names
		}
		out = append(out, v)
	}
	// 新的在前;悬空镜像(无标签)名称取空、自然排后
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})

	writeJSON(w, http.StatusOK, out)
}

// inspectImagePlatforms 并发 inspect 每个镜像取平台标识。
// 列表接口不返回架构,而「架构」列是镜像页的必要信息(docs §3.3)。
func inspectImagePlatforms(ctx context.Context, cli *client.Client, images []client.Image) map[string]string {
	const concurrency = 8
	out := make(map[string]string, len(images))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	sem := make(chan struct{}, concurrency)

	for _, img := range images {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			det, err := cli.InspectImage(ctx, id)
			if err != nil {
				return // 单个 inspect 失败不影响整体,该列留空
			}
			mu.Lock()
			out[id] = det.Platform()
			mu.Unlock()
		}(img.ID)
	}
	wg.Wait()
	return out
}

// imageUsage 反查「镜像 ID → 使用它的容器名」。容器列表自带 ImageID,
// 无需 inspect;与镜像列表的 Id 同格式(sha256:...),直接匹配。
func (d *dockerAPI) imageUsage(ctx context.Context) map[string][]string {
	list, err := d.cli.ListContainers(ctx, client.ListContainersOptions{All: true})
	if err != nil {
		return nil // 反查失败时视为无占用;删除路径会再查,daemon 也会兜底 409
	}
	out := make(map[string][]string)
	for _, ct := range list {
		out[ct.ImageID] = append(out[ct.ImageID], ct.Name())
	}
	return out
}

// removeImage 删除镜像。删除前先把 ref 解析成规范 ID 并做占用检查(docs §4.5)。
func (d *dockerAPI) removeImage(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		writeErr(w, http.StatusBadRequest, "缺少镜像引用")
		return
	}

	// 先解析成规范 ID:name:tag / name / digest / sha256:... 都能拿到同一个 ID
	det, err := d.cli.InspectImage(r.Context(), ref)
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	if names := d.imageUsage(r.Context())[det.ID]; len(names) > 0 {
		writeErr(w, http.StatusConflict, "镜像正被容器使用: "+strings.Join(names, ", "))
		return
	}

	removed, err := d.cli.RemoveImage(r.Context(), ref, client.RemoveImageOptions{})
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "removed": removed})
}

// loadImage 从 tar 归档导入镜像。请求体即 tar 内容(前端直接上传原始字节),
// 直接透传给 LoadImage 以支持流式、避免大文件落盘。
func (d *dockerAPI) loadImage(w http.ResponseWriter, r *http.Request) {
	rc, err := d.cli.LoadImage(r.Context(), r.Body, false)
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	defer rc.Close()

	type loadMsg struct {
		Stream string `json:"stream"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	var loaded []string
	dec := json.NewDecoder(rc)
	for {
		var m loadMsg
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			writeErr(w, http.StatusInternalServerError, "读取导入结果失败: "+err.Error())
			return
		}
		if m.Error != "" {
			writeErr(w, http.StatusInternalServerError, m.Error)
			return
		}
		if s := strings.TrimSpace(m.Stream); s != "" {
			loaded = append(loaded, s)
		} else if s := strings.TrimSpace(m.Status); s != "" {
			loaded = append(loaded, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "loaded": loaded})
}

// --- 网络 ---

func (d *dockerAPI) listNetworks(w http.ResponseWriter, r *http.Request) {
	nets, err := d.cli.ListNetworks(r.Context(), nil)
	if err != nil {
		writeDockerErr(w, err)
		return
	}

	out := make([]networkView, 0, len(nets))
	for _, n := range nets {
		out = append(out, networkView{
			ID:         n.ID,
			Name:       n.Name,
			Driver:     n.Driver,
			Scope:      n.Scope,
			Internal:   n.Internal,
			Attachable: n.Attachable,
			Subnet:     n.Subnet(),
			Gateway:    n.Gateway(),
			CreatedAt:  n.Created,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	writeJSON(w, http.StatusOK, out)
}

// createNetwork 创建网络,对应 docs §3.4 的表单字段。
func (d *dockerAPI) createNetwork(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string `json:"name"`
		Driver     string `json:"driver"`
		Internal   bool   `json:"internal"`
		Attachable bool   `json:"attachable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "网络名不能为空")
		return
	}

	id, err := d.cli.CreateNetwork(r.Context(), client.CreateNetworkOptions{
		Name:       name,
		Driver:     in.Driver,
		Internal:   in.Internal,
		Attachable: in.Attachable,
	})
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// inspectNetwork 返回网络详情(含已连接的容器),供查看弹层使用。
func (d *dockerAPI) inspectNetwork(w http.ResponseWriter, r *http.Request) {
	n, err := d.cli.InspectNetwork(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDockerErr(w, err)
		return
	}

	containers := make([]networkContainerView, 0, len(n.Containers))
	for _, c := range n.Containers {
		containers = append(containers, networkContainerView{
			Name: c.Name, IPv4: c.IPv4Address, IPv6: c.IPv6Address,
		})
	}
	sort.SliceStable(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         n.ID,
		"name":       n.Name,
		"driver":     n.Driver,
		"scope":      n.Scope,
		"internal":   n.Internal,
		"attachable": n.Attachable,
		"subnet":     n.Subnet(),
		"gateway":    n.Gateway(),
		"createdAt":  n.Created,
		"containers": containers,
	})
}

// removeNetwork 删除网络。仍有容器连接时列出占用者而非直接删(docs §4.5)。
func (d *dockerAPI) removeNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	n, err := d.cli.InspectNetwork(r.Context(), id)
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	if len(n.Containers) > 0 {
		names := make([]string, 0, len(n.Containers))
		for _, c := range n.Containers {
			names = append(names, c.Name)
		}
		writeErr(w, http.StatusConflict, "网络仍被容器使用: "+strings.Join(names, ", "))
		return
	}

	if err := d.cli.RemoveNetwork(r.Context(), id); err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- 存储卷 ---

func (d *dockerAPI) listVolumes(w http.ResponseWriter, r *http.Request) {
	vols, warnings, err := d.cli.ListVolumes(r.Context(), nil)
	if err != nil {
		writeDockerErr(w, err)
		return
	}

	usage := d.volumeUsage(r.Context())
	out := make([]volumeView, 0, len(vols))
	for _, v := range vols {
		vv := volumeView{
			Name:        v.Name,
			DisplayName: volumeDisplayName(&v),
			Driver:      v.Driver,
			Mountpoint:  v.Mountpoint,
			Scope:       v.Scope,
			CreatedAt:   v.CreatedAt,
		}
		if names := usage[v.Name]; len(names) > 0 {
			vv.InUse = true
			vv.Containers = names
		}
		out = append(out, vv)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	writeJSON(w, http.StatusOK, map[string]any{"volumes": out, "warnings": warnings})
}

// volumeUsage 反查「卷名 → 使用它的容器名」。卷列表不返回占用信息(实测
// UsageData 恒为 null),只能遍历容器的 Mounts。复用 inspectAll 的并发 inspect。
func (d *dockerAPI) volumeUsage(ctx context.Context) map[string][]string {
	list, err := d.cli.ListContainers(ctx, client.ListContainersOptions{All: true})
	if err != nil {
		return nil
	}
	out := make(map[string][]string)
	for _, det := range d.inspectAll(ctx, list) {
		for _, m := range det.Mounts {
			if m.Type == "volume" {
				out[m.Name] = append(out[m.Name], det.TrimmedName())
			}
		}
	}
	return out
}

// createVolume 创建卷,对应存储卷页的「创建卷」表单。
func (d *dockerAPI) createVolume(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string `json:"name"`
		Driver string `json:"driver"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "卷名不能为空")
		return
	}

	_, err := d.cli.CreateVolume(r.Context(), client.CreateVolumeOptions{
		Name:   name,
		Driver: in.Driver,
	})
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

// removeVolume 删除卷。被容器占用时列出占用者而非直接删(docs §4.5)。
func (d *dockerAPI) removeVolume(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "缺少卷名")
		return
	}

	if names := d.volumeUsage(r.Context())[name]; len(names) > 0 {
		writeErr(w, http.StatusConflict, "卷仍被容器使用: "+strings.Join(names, ", "))
		return
	}
	if err := d.cli.RemoveVolume(r.Context(), name, false); err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// pruneVolumes 一键清理未使用的卷,返回删除的卷名与释放空间(docs §3.5)。
func (d *dockerAPI) pruneVolumes(w http.ResponseWriter, r *http.Request) {
	deleted, reclaimed, err := d.cli.PruneVolumes(r.Context(), nil)
	if err != nil {
		writeDockerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "reclaimed": reclaimed})
}
