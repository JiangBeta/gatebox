package client

import (
	"context"
	"net/url"
	"time"
)

// Network 网络(GET /networks)。
type Network struct {
	Name       string            `json:"Name"`
	ID         string            `json:"Id"`
	Created    time.Time         `json:"Created"`
	Scope      string            `json:"Scope"`  // local / swarm / global
	Driver     string            `json:"Driver"` // bridge / macvlan / ipvlan / overlay / host / null
	EnableIPv6 bool              `json:"EnableIPv6"`
	Internal   bool              `json:"Internal"`   // 隔离外部访问
	Attachable bool              `json:"Attachable"` // 允许手动附加容器
	IPAM       IPAM              `json:"IPAM"`
	Labels     map[string]string `json:"Labels"`

	// Containers 仅 inspect 返回;删除前用它列出占用者(docs §4.5)。
	Containers map[string]NetworkContainer `json:"Containers,omitempty"`
}

// Subnet 返回第一个 IPv4 子网,供列表页的 IPv4 列展示。
func (n *Network) Subnet() string {
	for _, cfg := range n.IPAM.Config {
		// 跳过 IPv6(含冒号)
		if cfg.Subnet != "" && !containsColon(cfg.Subnet) {
			return cfg.Subnet
		}
	}
	return ""
}

// Gateway 返回第一个 IPv4 网关。
func (n *Network) Gateway() string {
	for _, cfg := range n.IPAM.Config {
		if cfg.Gateway != "" && !containsColon(cfg.Gateway) {
			return cfg.Gateway
		}
	}
	return ""
}

func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

// IPAM 地址管理配置。
type IPAM struct {
	Driver  string            `json:"Driver"`
	Options map[string]string `json:"Options,omitempty"`
	Config  []IPAMConfig      `json:"Config"`
}

// IPAMConfig 单个子网配置。
type IPAMConfig struct {
	Subnet     string            `json:"Subnet,omitempty"`
	IPRange    string            `json:"IPRange,omitempty"`
	Gateway    string            `json:"Gateway,omitempty"`
	AuxAddress map[string]string `json:"AuxiliaryAddresses,omitempty"`
}

// NetworkContainer 连接到网络的容器。
type NetworkContainer struct {
	Name        string `json:"Name"`
	EndpointID  string `json:"EndpointID"`
	MacAddress  string `json:"MacAddress"`
	IPv4Address string `json:"IPv4Address"`
	IPv6Address string `json:"IPv6Address"`
}

// ListNetworks 列出网络。
func (c *Client) ListNetworks(ctx context.Context, filters Filters) ([]Network, error) {
	q := url.Values{}
	if err := filters.apply(q); err != nil {
		return nil, err
	}
	var out []Network
	if err := c.getJSON(ctx, "/networks", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InspectNetwork 获取网络详情(含已连接的容器)。
func (c *Client) InspectNetwork(ctx context.Context, id string) (*Network, error) {
	var out Network
	if err := c.getJSON(ctx, "/networks/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateNetworkOptions 创建网络的参数,对应 docs §3.4 的表单字段。
type CreateNetworkOptions struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver,omitempty"` // 空则由 daemon 用 bridge
	Internal   bool              `json:"Internal,omitempty"`
	Attachable bool              `json:"Attachable,omitempty"`
	EnableIPv6 bool              `json:"EnableIPv6,omitempty"`
	IPAM       *IPAM             `json:"IPAM,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
	Options    map[string]string `json:"Options,omitempty"`
}

// CreateNetwork 创建网络,返回新网络 ID。
func (c *Client) CreateNetwork(ctx context.Context, opts CreateNetworkOptions) (string, error) {
	var out struct {
		ID      string `json:"Id"`
		Warning string `json:"Warning"`
	}
	if err := c.postJSON(ctx, "/networks/create", nil, opts, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// RemoveNetwork 删除网络。
//
// 仍有容器连接时 daemon 返回 403/409;GateBox 的策略是先 inspect 列出
// 占用它的容器给用户看,而不是直接报一句「网络正在使用」(docs §4.5)。
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.deleteReq(ctx, "/networks/"+id, nil, nil)
}

// ConnectNetwork 把容器接入网络。
//
// 编排表单的「加入 caddy 网络」开关在容器已运行时走这条路径(ADR-016)。
func (c *Client) ConnectNetwork(ctx context.Context, networkID, containerID string, aliases []string) error {
	body := struct {
		Container      string `json:"Container"`
		EndpointConfig *struct {
			Aliases []string `json:"Aliases,omitempty"`
		} `json:"EndpointConfig,omitempty"`
	}{Container: containerID}
	if len(aliases) > 0 {
		body.EndpointConfig = &struct {
			Aliases []string `json:"Aliases,omitempty"`
		}{Aliases: aliases}
	}
	return c.postJSON(ctx, "/networks/"+networkID+"/connect", nil, body, nil)
}

// DisconnectNetwork 把容器移出网络。
func (c *Client) DisconnectNetwork(ctx context.Context, networkID, containerID string, force bool) error {
	body := struct {
		Container string `json:"Container"`
		Force     bool   `json:"Force"`
	}{Container: containerID, Force: force}
	return c.postJSON(ctx, "/networks/"+networkID+"/disconnect", nil, body, nil)
}

// PruneNetworks 清理未使用的网络,返回被删除的网络名。
func (c *Client) PruneNetworks(ctx context.Context, filters Filters) ([]string, error) {
	q := url.Values{}
	if err := filters.apply(q); err != nil {
		return nil, err
	}
	var out struct {
		NetworksDeleted []string `json:"NetworksDeleted"`
	}
	if err := c.postJSON(ctx, "/networks/prune", q, nil, &out); err != nil {
		return nil, err
	}
	return out.NetworksDeleted, nil
}
