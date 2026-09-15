package handler

// 扩展投影 API（ADR-036 I2）：核心状态的只读视图，供插件消费。
//
// 当前为单管理员、无鉴权中间件的形态；scoped token 与变更通知属 ADR-036 预留，
// 本阶段先提供只读投影 + revision（level-triggered 调和的锚点）。

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"sort"
	"time"
)

// projectedDomain 投影中的一条域名记录（不含凭证密钥）。
type projectedDomain struct {
	Host       string `json:"host"`
	Protocol   string `json:"protocol"`
	RootDomain string `json:"rootDomain,omitempty"`
	Type       string `json:"type,omitempty"`
}

// domainsProjection 域名投影 + revision（内容变化则 revision 变化）。
type domainsProjection struct {
	Revision    uint64            `json:"revision"`
	GeneratedAt time.Time         `json:"generatedAt"`
	Domains     []projectedDomain `json:"domains"`
}

// getDomainsProjection 返回启用服务的域名只读投影。
func (a *gatewayAPI) getDomainsProjection(w http.ResponseWriter, r *http.Request) {
	services, err := a.s.ListServices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	services = append(services, a.deriveDockerServices(r.Context())...)

	projected := make([]projectedDomain, 0)
	for _, svc := range services {
		if !svc.Enabled {
			continue
		}
		for _, d := range svc.Domains {
			projected = append(projected, projectedDomain{
				Host: d.Host(), Protocol: d.Protocol, RootDomain: d.RootDomain, Type: svc.Type,
			})
		}
	}
	sort.Slice(projected, func(i, j int) bool {
		if projected[i].Host != projected[j].Host {
			return projected[i].Host < projected[j].Host
		}
		return projected[i].Protocol < projected[j].Protocol
	})
	writeJSON(w, http.StatusOK, domainsProjection{
		Revision: projectionRevision(projected), GeneratedAt: time.Now(), Domains: projected,
	})
}

// projectionRevision 基于内容哈希生成稳定 revision（内容变化即变化）。
func projectionRevision(v any) uint64 {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write(raw)
	return h.Sum64()
}
