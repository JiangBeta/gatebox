package client

import (
	"context"
	"net/url"
)

// 本文件承载「容器升级重建」所需的客户端原语:
// POST /containers/create、POST /containers/{id}/rename,以及同步拉到完成的
// PullImageWait、按 inspect 详情反推 create 体的 RecreateContainer。
//
// 升级路径(docs 待补):拉新镜像 → 以临时名创建新容器 → 删旧容器 →
// 改名回原名 → 启动。全部失败时旧容器仍完好,不丢数据。

// ContainerHostConfig 创建容器的宿主机侧配置(create 请求体的 HostConfig 子集)。
type ContainerHostConfig struct {
	NetworkMode   string                   `json:"NetworkMode,omitempty"`
	RestartPolicy RestartPolicy            `json:"RestartPolicy,omitempty"`
	Binds         []string                 `json:"Binds,omitempty"`
	PortBindings  map[string][]PortBinding `json:"PortBindings,omitempty"`
	Privileged    bool                     `json:"Privileged,omitempty"`
	LogConfig     *LogConfig               `json:"LogConfig,omitempty"`
}

// RestartPolicy 重启策略。
type RestartPolicy struct {
	Name              string `json:"Name,omitempty"`
	MaximumRetryCount int    `json:"MaximumRetryCount,omitempty"`
}

// LogConfig 日志驱动配置。
type LogConfig struct {
	Type   string            `json:"Type,omitempty"`
	Config map[string]string `json:"Config,omitempty"`
}

// NetworkingConfig 创建容器时预接的网络。
type NetworkingConfig struct {
	EndpointsConfig map[string]EndpointSettings `json:"EndpointsConfig,omitempty"`
}

// ContainerCreateBody POST /containers/create 的请求体。
type ContainerCreateBody struct {
	Hostname         string               `json:"Hostname,omitempty"`
	User             string               `json:"User,omitempty"`
	WorkingDir       string               `json:"WorkingDir,omitempty"`
	Env              []string             `json:"Env,omitempty"`
	Cmd              []string             `json:"Cmd,omitempty"`
	Image            string               `json:"Image"`
	Labels           map[string]string    `json:"Labels,omitempty"`
	Entrypoint       []string             `json:"Entrypoint,omitempty"`
	Tty              bool                 `json:"Tty,omitempty"`
	OpenStdin        bool                 `json:"OpenStdin,omitempty"`
	HostConfig       *ContainerHostConfig `json:"HostConfig,omitempty"`
	NetworkingConfig *NetworkingConfig    `json:"NetworkingConfig,omitempty"`
}

// CreateContainer 创建一个容器并返回其 ID。name 为空时由 daemon 随机命名。
func (c *Client) CreateContainer(ctx context.Context, name string, body ContainerCreateBody) (string, error) {
	q := url.Values{}
	if name != "" {
		q.Set("name", name)
	}
	var out struct {
		ID      string `json:"Id"`
		Warning string `json:"Warning"`
	}
	if err := c.postJSON(ctx, "/containers/create", q, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// RenameContainer 重命名容器。旧名被占用时 daemon 返回 409(ErrConflict)。
func (c *Client) RenameContainer(ctx context.Context, id, newName string) error {
	q := url.Values{}
	q.Set("name", newName)
	return c.postJSON(ctx, "/containers/"+id+"/rename", q, nil, nil)
}

// PullImageWait 同步拉取镜像到完成。用于升级路径——需要拉完后立刻比对 digest。
//
// 匿名/私有认证在 auth 中给出。返回时若 daemon 报错(如镜像不存在)返回该错误。
func (c *Client) PullImageWait(ctx context.Context, ref, platform string, auth *RegistryAuth) error {
	rc, err := c.PullImage(ctx, ref, platform, auth)
	if err != nil {
		return err
	}
	defer rc.Close()

	var pullErr error
	_ = DecodePullStream(ctx, rc, func(p PullProgress) error {
		if p.Err != nil {
			pullErr = p.Err
		}
		return nil
	})
	if pullErr != nil {
		return pullErr
	}
	// 事件流未携带错误,但被 ctx 取消(如超时)
	return ctx.Err()
}

// RecreateContainer 按现有容器详情用新镜像 ref 创建一个配置一致的新容器。
// 返回新容器 ID。**不触碰原容器**——删除/改名由上层编排以保数据安全。
func (c *Client) RecreateContainer(ctx context.Context, det *ContainerDetail, newImage, name string) (string, error) {
	body := createBodyFromDetail(det, newImage)
	return c.CreateContainer(ctx, name, body)
}

// createBodyFromDetail 把 ContainerDetail 反推为 create 请求体(尽力完整还原)。
func createBodyFromDetail(det *ContainerDetail, newImage string) ContainerCreateBody {
	body := ContainerCreateBody{
		Hostname:   det.Config.Hostname,
		WorkingDir: det.Config.WorkingDir,
		User:       det.Config.User,
		Env:        det.Config.Env,
		Cmd:        det.Config.Cmd,
		Image:      newImage,
		Labels:     det.Config.Labels,
		Entrypoint: det.Config.Entrypoint,
		Tty:        det.Config.Tty,
		OpenStdin:  det.Config.OpenStdin,
	}

	if det.HostConfig != nil {
		hc := &ContainerHostConfig{
			NetworkMode:  det.HostConfig.NetworkMode,
			PortBindings: det.HostConfig.PortBindings,
			Privileged:   det.HostConfig.Privileged,
			Binds:        det.HostConfig.Binds,
			RestartPolicy: RestartPolicy{
				Name:              det.HostConfig.RestartPolicy.Name,
				MaximumRetryCount: det.HostConfig.RestartPolicy.MaximumRetryCount,
			},
		}
		if lc := det.HostConfig.LogConfig.Type; lc != "" {
			hc.LogConfig = &LogConfig{
				Type:   det.HostConfig.LogConfig.Type,
				Config: det.HostConfig.LogConfig.Config,
			}
		}
		body.HostConfig = hc
	}

	// 预接原容器所连的网络及其别名,避免重建后丢失网络归属。
	if det.NetworkSettings != nil && len(det.NetworkSettings.Networks) > 0 {
		eps := make(map[string]EndpointSettings, len(det.NetworkSettings.Networks))
		for n, cfg := range det.NetworkSettings.Networks {
			// 只保留归属信息,网络级字段(ID/网关等)重建时由 daemon 重新分配
			eps[n] = EndpointSettings{Aliases: cfg.Aliases}
		}
		body.NetworkingConfig = &NetworkingConfig{EndpointsConfig: eps}
	}

	return body
}
