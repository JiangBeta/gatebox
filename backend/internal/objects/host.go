package objects

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/JiangBeta/gatebox/internal/typespec"
)

// 本机 edge 主机的固定标识与默认值（model/host.yaml「本机 edge 由系统自动创建，
// 全局有且只有一个」）。
const (
	// EdgeHostID 本机 edge 的 id。主键取 host 字段值，故恒为 "edge"。
	EdgeHostID = "edge"
	// EdgeHostAddress 取不到本机 IP 时的占位地址（仍可连本机 Docker）。
	EdgeHostAddress = "127.0.0.1"
	// EdgeDockerEndpoint 本机 Docker socket。
	EdgeDockerEndpoint = "unix:///var/run/docker.sock"
)

// ErrEdgeLocked 试图删改本机 edge 主机。
//
// 「本机 edge 由系统自动创建、全局有且只有一个」（model/host.yaml）是结构约束，
// 不是默认值：删掉它，后端地址推导链就断在最后一环，容器列表的 host 维度也没了。
// 所以锁住——用户想改主机用 worker 节点。
var ErrEdgeLocked = errors.New("objects: 本机 edge 主机由系统创建，不可删除或改动角色")

// assertEdgeDeletable 挡住删除本机 edge。
func (s *Service) assertEdgeDeletable(kind, id string) error {
	if kind == typespec.KindHost && id == EdgeHostID {
		return ErrEdgeLocked
	}
	return nil
}

// assertEdgeRole 挡住把本机 edge 的角色改掉（edge → worker）。
//
// 只挡角色，不管地址/备注：那些是用户填的字段，model/host.yaml 说 address
// 手填，拦掉就等于禁用编辑。角色不同——edge 身份是系统给的事实。
func (s *Service) assertEdgeRole(kind, id string, spec map[string]any) error {
	if kind != typespec.KindHost || id != EdgeHostID {
		return nil
	}
	role, _ := spec["role"].(string)
	if role != "" && role != "edge" {
		return ErrEdgeLocked
	}
	return nil
}

// EdgeOptions edge 自举的可覆盖项。
type EdgeOptions struct {
	// Address 本机 IP；留空则探测。
	Address string
	// DockerEndpoint 本机 Docker 接入点。
	DockerEndpoint string
	// Online 本机 Docker 是否可达（不可达也让对象存在，只是 status.online=false）。
	Online bool
	// Now 当前时间（测试可注入）。
	Now time.Time
}

// EnsureEdge 幂等确保本机 edge 主机对象存在，并刷新它的在线状态。
//
// 为什么要自举而不是让用户手填：本机 edge 是后端地址推导链的终点
// （容器端口 → 宿主端口 → 主机地址，ADR-042 §13）。链子少一环，所有
// 「按主机地址生成后端」的推导都算不出来，容器详情里也拿不到 host 维度。
//
// 幂等：已存在只更新 status 与本机 IP（用户改过 address 时不覆盖——
// address 是必填字段但用户可能就是想指向另一块网卡），不存在才创建。
func (s *Service) EnsureEdge(opts EdgeOptions) (Object, error) {
	if opts.DockerEndpoint == "" {
		opts.DockerEndpoint = EdgeDockerEndpoint
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	cur, err := s.Get(typespec.KindHost, EdgeHostID)
	if err != nil && !isNotFound(err) {
		return Object{}, err
	}
	if isNotFound(err) {
		addr := opts.Address
		if addr == "" {
			addr = EdgeHostAddress
		}
		o, cerr := s.Create(typespec.KindHost, map[string]any{
			"role":           "edge",
			"host":           EdgeHostID,
			"address":        addr,
			"dockerEndpoint": opts.DockerEndpoint,
			"remark":         "本机网关（系统自动创建）",
		})
		if cerr != nil {
			return Object{}, fmt.Errorf("创建本机 edge 主机失败: %w", cerr)
		}
		cur = o
		log.Printf("已创建本机 edge 主机对象: %s（%s）", o.ID, cur)
	}
	// 已存在时**不改** address：model/host.yaml 说它是手填字段，
	// 心跳不该把用户填的值冲回探测值。用户要改就在设置 → 主机里改。

	// 在线状态每次刷新都覆盖：它是观测值，不是用户填的。
	if serr := s.SetStatus(typespec.KindHost, EdgeHostID, map[string]any{
		"online":   opts.Online,
		"lastSeen": opts.Now.UTC().Format(time.RFC3339),
	}); serr != nil {
		return Object{}, serr
	}
	return s.Get(typespec.KindHost, EdgeHostID)
}

// cloneSpec 浅拷贝源字段（status 不在其中）。
func cloneSpec(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// isNotFound 判定仓储未找到（objects.ErrNotFound 即 repository.ErrNotFound 的别名）。
func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
