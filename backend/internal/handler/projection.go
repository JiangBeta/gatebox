package handler

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/plugin"
	"github.com/JiangBeta/gatebox/internal/repository"
)

// 插件投影 API（ADR-036 I2 / ADR-039 §2）：核心状态的只读视图，供消费型插件自收敛。
//
// 与旧实现相比：
//   - 以 plugin token 鉴权并解析插件身份（"me"）；
//   - 按 manifest permissions.api 做 scope 校验；
//   - 支持 `?since=<revision>&wait=<秒>` 长轮询（level-triggered，变更通知的兜底实现）；
//   - 新增 credentials 投影（仅 credentials:read scope；密钥不进 domains 投影）。
type projectionAPI struct {
	mgr   *plugin.Manager
	s     *repository.Store
	ext   *extension.Registry
	extra func(ctx context.Context) []model.Service
}

// RegisterProjection 注册投影路由。
func RegisterProjection(mux *http.ServeMux, mgr *plugin.Manager, s *repository.Store, ext *extension.Registry, extra func(ctx context.Context) []model.Service) {
	h := &projectionAPI{mgr: mgr, s: s, ext: ext, extra: extra}
	mux.HandleFunc("GET /api/v1/extensions/me/projection/domains", h.domains)
	mux.HandleFunc("GET /api/v1/extensions/me/projection/credentials", h.credentials)
}

// projectedDomain 投影中的一条域名记录（不含密钥）。
type projectedDomain struct {
	Host         string `json:"host"`
	Protocol     string `json:"protocol"`
	RootDomain   string `json:"rootDomain,omitempty"`
	Type         string `json:"type,omitempty"`
	CredentialID string `json:"credentialId,omitempty"`
}

type domainsProjection struct {
	Revision    uint64            `json:"revision"`
	GeneratedAt time.Time         `json:"generatedAt"`
	Domains     []projectedDomain `json:"domains"`
}

// auth 解析 plugin token 并校验 scope；返回插件 id。
func (h *projectionAPI) auth(w http.ResponseWriter, r *http.Request, scope string) (string, bool) {
	tok := r.Header.Get("X-Plugin-Token")
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	id, perms, ok := h.mgr.PermissionsOfToken(tok)
	if !ok {
		writeErrCode(w, http.StatusUnauthorized, "UNAUTHORIZED", "plugin token 无效")
		return "", false
	}
	if scope != "" && !hasAPIScope(perms, scope) {
		writeErrCode(w, http.StatusForbidden, "FORBIDDEN", "缺少 scope: "+scope)
		return "", false
	}
	return id, true
}

// buildDomains 汇总当前启用服务的域名投影（含根域对应的凭证 id，不含密钥）。
func (h *projectionAPI) buildDomains(ctx context.Context) ([]projectedDomain, error) {
	services, err := h.s.ListServices()
	if err != nil {
		return nil, err
	}
	if h.extra != nil {
		services = append(services, h.extra(ctx)...)
	}
	domains, err := h.s.ListDomains()
	if err != nil {
		return nil, err
	}
	credOfRoot := make(map[string]string, len(domains))
	for _, d := range domains {
		credOfRoot[d.Name] = d.CredentialID
	}
	out := make([]projectedDomain, 0)
	for _, svc := range services {
		if !svc.Enabled {
			continue
		}
		for _, d := range svc.Domains {
			out = append(out, projectedDomain{
				Host: d.Host(), Protocol: d.Protocol, RootDomain: d.RootDomain,
				Type: svc.Type, CredentialID: credOfRoot[d.RootDomain],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Protocol < out[j].Protocol
	})
	return out, nil
}

func (h *projectionAPI) domains(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth(w, r, "domain:read"); !ok {
		return
	}
	projected, err := h.buildDomains(r.Context())
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "PROJECTION_FAILED", err.Error())
		return
	}
	rev := projectionRevision(projected)

	// 长轮询：since 与当前一致时等待变更或超时（level-triggered 兜底）。
	if since := r.URL.Query().Get("since"); since != "" && since == strconv.FormatUint(rev, 10) {
		wait := parseWait(r.URL.Query().Get("wait"))
		if wait > 0 {
			deadline := time.Now().Add(wait)
			for time.Now().Before(deadline) {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
				p2, err := h.buildDomains(r.Context())
				if err != nil {
					break
				}
				if projectionRevision(p2) != rev {
					projected, rev = p2, projectionRevision(p2)
					break
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, domainsProjection{Revision: rev, GeneratedAt: time.Now(), Domains: projected})
}

// credentialProjection 凭证投影（含解密后的字段，仅 credentials:read 可见）。
//
// 附 `ddns` 映射（来自供应商元数据），使消费插件无需内置供应商知识。
type credentialProjection struct {
	ID       string                 `json:"id"`
	Provider string                 `json:"provider"`
	Fields   map[string]string      `json:"fields"`
	DDNS     *extension.DDNSMapping `json:"ddns,omitempty"`
}

func (h *projectionAPI) credentials(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth(w, r, "credentials:read"); !ok {
		return
	}
	creds, err := h.s.ListCredentials()
	if err != nil {
		writeErrCode(w, http.StatusInternalServerError, "PROJECTION_FAILED", err.Error())
		return
	}
	out := make([]credentialProjection, 0, len(creds))
	for _, c := range creds {
		cp := credentialProjection{ID: c.ID, Provider: c.Provider, Fields: c.Fields}
		if h.ext != nil {
			if spec, ok := h.ext.DNSProvider(c.Provider); ok {
				cp.DDNS = spec.DDNS
			}
		}
		out = append(out, cp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

// hasAPIScope 校验 permissions.api 是否含指定 scope。
func hasAPIScope(perms []plugin.Permission, scope string) bool {
	for _, p := range perms {
		for _, s := range p.API {
			if s == scope || s == "*" {
				return true
			}
		}
	}
	return false
}

func parseWait(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	if n > 30 {
		n = 30
	}
	return time.Duration(n) * time.Second
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
