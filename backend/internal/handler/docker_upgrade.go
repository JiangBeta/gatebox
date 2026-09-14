package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/JiangBeta/gatebox/internal/adapter/docker/client"
)

// 本文件承载「容器升级」:比对镜像 digest,有新版则拉新镜像并重建容器。
//
// 流程(每容器):停止旧容器 → 以临时名创建新容器(配置照旧、镜像更新) →
// 删除旧容器 → 改回原名 → 原为运行状态则启动。中途失败旧容器仍保留,不丢数据。
//
// 编排容器(managed/external)同样可被该接口重建;重建会使其脱离 compose 的
// container_name 管理,因此升级前需在 UI 上充分提示(docs §…) 。

// upgradeRequest 一次「升级容器」请求,ids 为空表示全部容器。
type upgradeRequest struct {
	IDs []string `json:"ids"`
}

// upgradeResult 单容器的升级结果。
type upgradeResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"` // upgraded / up-to-date / skipped / error
	Image   string `json:"image"`
	Message string `json:"message,omitempty"`
}

// upgradeContainers 升级一个或多个容器。
func (d *dockerAPI) upgradeContainers(w http.ResponseWriter, r *http.Request) {
	var in upgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}

	ids := in.IDs
	if len(ids) == 0 {
		list, err := d.cli.ListContainers(r.Context(), client.ListContainersOptions{All: true})
		if err != nil {
			writeDockerErr(w, err)
			return
		}
		for _, ct := range list {
			ids = append(ids, ct.ID)
		}
	}

	results := make([]upgradeResult, 0, len(ids))
	for _, id := range ids {
		results = append(results, d.upgradeOne(r.Context(), id))
	}
	d.syncCaddyAsync(r) // 容器重建后 label 可能变化,自动同步到网关(ADR-026 §7)
	writeJSON(w, http.StatusOK, results)
}

// upgradeOne 处理单个容器的升级。
func (d *dockerAPI) upgradeOne(ctx context.Context, id string) upgradeResult {
	res := upgradeResult{ID: id, Image: "", Status: "error"}

	det, err := d.cli.InspectContainer(ctx, id)
	if err != nil {
		res.Message = errMessage(err)
		return res
	}
	res.Name = det.TrimmedName()
	res.Image = det.Config.Image

	ref := det.Config.Image
	if ref == "" {
		res.Status = "skipped"
		res.Message = "未记录镜像引用,无法升级"
		return res
	}
	// 按 digest 固定的镜像拉取到的仍是同一 digest,无从升级
	if strings.Contains(ref, "@") {
		res.Status = "skipped"
		res.Message = "按 digest 固定的镜像无法升级"
		return res
	}

	// 拉取最新镜像(私有仓库自动带认证),拉完即比对新旧 digest
	if err := d.cli.PullImageWait(ctx, ref, "", d.authForImage(ref)); err != nil {
		res.Message = "拉取镜像失败: " + errMessage(err)
		return res
	}

	img, err := d.cli.InspectImage(ctx, ref)
	if err != nil {
		res.Message = "读取镜像信息失败: " + errMessage(err)
		return res
	}
	if img.ID == det.Image {
		res.Status = "up-to-date"
		return res
	}

	if err := d.recreateWithImage(ctx, det, ref); err != nil {
		res.Message = "重建失败: " + errMessage(err)
		return res
	}
	res.Status = "upgraded"
	return res
}

// recreateWithImage 停止旧容器并重建为同配置、新镜像的容器。
//
// 用「临时名创建 → 删旧 → 改名」而非直接复用原名:创建失败时旧容器还在,
// 不会出现「旧删了新没建成」的真空。原运行中的容器重建后继续运行。
func (d *dockerAPI) recreateWithImage(ctx context.Context, det *client.ContainerDetail, newImage string) error {
	oldID := det.ID
	name := det.TrimmedName()

	// 1. 停止旧容器
	if det.State.Running {
		if err := d.cli.StopContainer(ctx, oldID, nil); err != nil && !errors.Is(err, client.ErrNotModified) {
			return err
		}
	}

	// 2. 以临时名创建新容器(旧容器仍在,原名被占用)
	tmpName := name + "-upgrade"
	newID, err := d.cli.RecreateContainer(ctx, det, newImage, tmpName)
	if err != nil {
		return fmt.Errorf("创建新容器失败: %w", err)
	}

	// 3. 删除旧容器(此时已停止)。失败则新容器保留临时名,不强行改名以免重名。
	var rmErr error
	if err := d.cli.RemoveContainer(ctx, oldID, client.RemoveContainerOptions{}); err != nil {
		rmErr = err
	}

	// 4. 删旧成功后改回原名,保持网络别名/引用一致
	if rmErr == nil {
		if err := d.cli.RenameContainer(ctx, newID, name); err != nil {
			// 改名失败不致命:新容器仍以临时名存在
		}
	}

	// 5. 原为运行状态则启动新容器
	if det.State.Running {
		if err := d.cli.StartContainer(ctx, newID); err != nil && !errors.Is(err, client.ErrNotModified) {
			return err
		}
	}

	if rmErr != nil {
		return fmt.Errorf("旧容器删除失败(%v);新容器保留为临时名 %s,请手动清理", rmErr, tmpName)
	}
	return nil
}

// authForImage 根据镜像 ref 的 registry 取拉取认证;Docker Hub 或无匹配时返回 nil(匿名)。
func (d *dockerAPI) authForImage(ref string) *client.RegistryAuth {
	host := registryHost(ref)
	if host == "" || d.s == nil {
		return nil
	}
	regs, err := d.s.ListRegistries()
	if err != nil {
		return nil
	}
	for _, reg := range regs {
		ru := strings.TrimPrefix(strings.TrimPrefix(reg.URL, "https://"), "http://")
		if strings.EqualFold(host, ru) {
			return &client.RegistryAuth{
				Username:      reg.Username,
				Password:      reg.Secret,
				ServerAddress: host,
			}
		}
	}
	return nil
}

// registryHost 提取镜像 ref 的 registry 主机;Docker Hub(无主机段或官方命名空间)返回空。
func registryHost(ref string) string {
	i := strings.Index(ref, "/")
	if i < 0 {
		return "" // 纯 "nginx:tag",Docker Hub
	}
	host := ref[:i]
	// 单段命名空间(如 "library/nginx")本质仍是 Docker Hub,无需认证
	if strings.Contains(host, ".") || strings.Contains(host, ":") {
		return host
	}
	return ""
}
