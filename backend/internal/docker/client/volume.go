package client

import (
	"context"
	"net/url"
	"time"
)

// Volume 存储卷。
type Volume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	CreatedAt  time.Time         `json:"CreatedAt"`
	Scope      string            `json:"Scope"`
	Labels     map[string]string `json:"Labels"`
	Options    map[string]string `json:"Options"`

	// UsageData 仅在列表请求带 filters 或 daemon 支持时返回。
	// Size 与 RefCount 为 -1 表示「未统计」而非 0 —— 展示时须区分。
	UsageData *VolumeUsage `json:"UsageData,omitempty"`
}

// VolumeUsage 卷的占用情况。
type VolumeUsage struct {
	Size     int64 `json:"Size"`     // -1 = 未统计
	RefCount int64 `json:"RefCount"` // -1 = 未统计;0 = 未被任何容器使用
}

// InUse 报告该卷是否被容器占用。第二个返回值为 false 表示 daemon 未提供该信息。
func (v *Volume) InUse() (inUse, known bool) {
	if v.UsageData == nil || v.UsageData.RefCount < 0 {
		return false, false
	}
	return v.UsageData.RefCount > 0, true
}

// ComposeProject 返回创建该卷的 compose 项目名(若由 compose 创建)。
func (v *Volume) ComposeProject() string { return v.Labels[LabelComposeProject] }

// ListVolumes 列出卷。
//
// 注意响应是对象而非数组:{"Volumes": [...], "Warnings": [...]}。
func (c *Client) ListVolumes(ctx context.Context, filters Filters) ([]Volume, []string, error) {
	q := url.Values{}
	if err := filters.apply(q); err != nil {
		return nil, nil, err
	}
	var out struct {
		Volumes  []Volume `json:"Volumes"`
		Warnings []string `json:"Warnings"`
	}
	if err := c.getJSON(ctx, "/volumes", q, &out); err != nil {
		return nil, nil, err
	}
	return out.Volumes, out.Warnings, nil
}

// InspectVolume 获取卷详情。
func (c *Client) InspectVolume(ctx context.Context, name string) (*Volume, error) {
	var out Volume
	if err := c.getJSON(ctx, "/volumes/"+name, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateVolumeOptions 创建卷的参数。
type CreateVolumeOptions struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver,omitempty"`
	DriverOpts map[string]string `json:"DriverOpts,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
}

// CreateVolume 创建卷。
func (c *Client) CreateVolume(ctx context.Context, opts CreateVolumeOptions) (*Volume, error) {
	var out Volume
	if err := c.postJSON(ctx, "/volumes/create", nil, opts, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveVolume 删除卷。卷被容器占用时 daemon 返回 409(ErrConflict)。
func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", boolArg(true))
	}
	return c.deleteReq(ctx, "/volumes/"+name, q, nil)
}

// PruneVolumes 清理未使用的卷,返回被删除的卷名与释放的空间(字节)。
//
// 对应存储卷页的「一键清理未使用」(docs §3.5)——孤儿卷吃满磁盘是
// HomeLab 最常见的故障之一。
func (c *Client) PruneVolumes(ctx context.Context, filters Filters) (deleted []string, reclaimed uint64, err error) {
	q := url.Values{}
	if err := filters.apply(q); err != nil {
		return nil, 0, err
	}
	var out struct {
		VolumesDeleted []string `json:"VolumesDeleted"`
		SpaceReclaimed uint64   `json:"SpaceReclaimed"`
	}
	if err := c.postJSON(ctx, "/volumes/prune", q, nil, &out); err != nil {
		return nil, 0, err
	}
	return out.VolumesDeleted, out.SpaceReclaimed, nil
}
