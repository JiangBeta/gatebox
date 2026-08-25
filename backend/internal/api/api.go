// Package api 提供 HTTP 路由与处理器。
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/cert"
	"github.com/JiangBeta/gatebox/internal/models"
	"github.com/JiangBeta/gatebox/internal/store"
)

// 证书「即将过期」阈值(天)。
const expiringSoonDays = 10

type api struct {
	s  *store.Store
	cm cert.CertManager
}

// Register 将 API 路由注册到 mux。
func Register(mux *http.ServeMux, s *store.Store, cm cert.CertManager) {
	a := &api{s: s, cm: cm}

	mux.HandleFunc("GET /api/v1/domains", a.listDomains)
	mux.HandleFunc("POST /api/v1/domains", a.createDomain)
	mux.HandleFunc("GET /api/v1/domains/overview", a.overview)
	mux.HandleFunc("GET /api/v1/domains/{id}", a.getDomain)
	mux.HandleFunc("PUT /api/v1/domains/{id}", a.updateDomain)
	mux.HandleFunc("DELETE /api/v1/domains/{id}", a.deleteDomain)

	mux.HandleFunc("GET /api/v1/credentials", a.listCredentials)
	mux.HandleFunc("POST /api/v1/credentials", a.createCredential)
	mux.HandleFunc("POST /api/v1/credentials/verify", a.verifyCredential)
	mux.HandleFunc("PUT /api/v1/credentials/{id}", a.updateCredential)
	mux.HandleFunc("DELETE /api/v1/credentials/{id}", a.deleteCredential)

	mux.HandleFunc("GET /api/v1/certificates", a.listCertificates)
}

// --- 通用响应 ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- 域名 ---

func (a *api) listDomains(w http.ResponseWriter, r *http.Request) {
	ds, err := a.s.ListDomains()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

func (a *api) createDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string `json:"name"`
		CredentialID string `json:"credentialId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "域名不能为空")
		return
	}
	d := &models.Domain{
		ID:           newID(),
		Name:         name,
		CredentialID: in.CredentialID,
		CreatedAt:    time.Now(),
	}
	if err := a.s.SaveDomain(d); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (a *api) getDomain(w http.ResponseWriter, r *http.Request) {
	d, err := a.s.GetDomain(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "域名不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *api) updateDomain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := a.s.GetDomain(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "域名不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in struct {
		Name         string `json:"name"`
		CredentialID string `json:"credentialId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		d.Name = name
	}
	d.CredentialID = in.CredentialID
	if err := a.s.SaveDomain(d); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *api) deleteDomain(w http.ResponseWriter, r *http.Request) {
	if err := a.s.DeleteDomain(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// --- 概览 ---

type domainOverview struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CertStatus   string `json:"certStatus"` // success | expiring | expired | unissued
	SubdomainCnt int    `json:"subdomainCount"`
	CreatedAt    string `json:"createdAt"`
	LastIssuedAt string `json:"lastIssuedAt"`
}

type overviewResponse struct {
	DomainCount      int              `json:"domainCount"`
	CredentialCount  int              `json:"credentialCount"`
	ProviderCount    int              `json:"providerCount"`
	CertTotal        int              `json:"certTotal"`
	CertExpiringSoon int              `json:"certExpiringSoon"`
	CertExpired      int              `json:"certExpired"`
	Domains          []domainOverview `json:"domains"`
}

func (a *api) overview(w http.ResponseWriter, r *http.Request) {
	domains, err := a.s.ListDomains()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	creds, err := a.s.ListCredentials()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	certs, err := a.cm.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now()
	total, expiring, expired := certStats(certs, now)

	resp := overviewResponse{
		DomainCount:     len(domains),
		CredentialCount: len(creds),
		ProviderCount:   distinctProviders(creds),
		CertTotal:       total,
		CertExpiringSoon: expiring,
		CertExpired:     expired,
		Domains:         make([]domainOverview, 0, len(domains)),
	}

	for _, d := range domains {
		lastIssued := ""
		if t := latestIssued(certs, d.Name); t != nil {
			lastIssued = t.Format(time.RFC3339)
		}
		resp.Domains = append(resp.Domains, domainOverview{
			ID:           d.ID,
			Name:         d.Name,
			CertStatus:   aggregateDomainCertStatus(certs, d.Name, now),
			SubdomainCnt: 0, // TODO(网关):从 ProxyRoute 聚合该 rootDomain 下的二级域名数
			CreatedAt:    d.CreatedAt.Format(time.RFC3339),
			LastIssuedAt: lastIssued,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// --- 凭证 ---

func (a *api) listCredentials(w http.ResponseWriter, r *http.Request) {
	cs, err := a.s.ListCredentials()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (a *api) createCredential(w http.ResponseWriter, r *http.Request) {
	var in models.DNSCredential
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if ok, msg := validateCredentialFields(&in); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	in.ID = newID()
	in.CreatedAt = time.Now()
	if err := a.s.SaveCredential(&in); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, in)
}

func (a *api) updateCredential(w http.ResponseWriter, r *http.Request) {
	c, err := a.s.GetCredential(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "凭证不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in models.DNSCredential
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	c.Name = in.Name
	c.Provider = in.Provider
	c.Fields = in.Fields
	if ok, msg := validateCredentialFields(c); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := a.s.SaveCredential(c); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *api) deleteCredential(w http.ResponseWriter, r *http.Request) {
	if err := a.s.DeleteCredential(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// verifyCredential 结构校验(真实供应商 API 验证待实现)。
func (a *api) verifyCredential(w http.ResponseWriter, r *http.Request) {
	var in models.DNSCredential
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	ok, msg := validateCredentialFields(&in)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": msg})
}

// --- 证书 ---

func (a *api) listCertificates(w http.ResponseWriter, r *http.Request) {
	certs, err := a.cm.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, certs)
}

// --- 纯函数(便于单测) ---

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validateCredentialFields 按供应商校验必要字段非空。
func validateCredentialFields(c *models.DNSCredential) (bool, string) {
	switch c.Provider {
	case models.ProviderCloudflare:
		if strings.TrimSpace(c.Fields["token"]) == "" {
			return false, "缺少 token"
		}
	case models.ProviderDNSPod:
		if strings.TrimSpace(c.Fields["id"]) == "" || strings.TrimSpace(c.Fields["token"]) == "" {
			return false, "缺少 id 或 token"
		}
	case models.ProviderAliyun:
		if strings.TrimSpace(c.Fields["accessKeyId"]) == "" || strings.TrimSpace(c.Fields["accessKeySecret"]) == "" {
			return false, "缺少 AccessKey ID 或 Secret"
		}
	default:
		return false, "未知供应商: " + c.Provider
	}
	return true, "结构校验通过"
}

// certStatus 单个证书状态。
func certStatus(notAfter, now time.Time) string {
	if now.After(notAfter) {
		return "expired"
	}
	if notAfter.Sub(now) < expiringSoonDays*24*time.Hour {
		return "expiring"
	}
	return "success"
}

// aggregateDomainCertStatus 聚合某 rootDomain 下所有证书的最坏状态。
func aggregateDomainCertStatus(certs []models.Cert, rootDomain string, now time.Time) string {
	worst := "success"
	hasAny := false
	for _, c := range certs {
		if c.FQDN != rootDomain && !strings.HasSuffix(c.FQDN, "."+rootDomain) {
			continue
		}
		hasAny = true
		worst = worseStatus(worst, certStatus(c.NotAfter, now))
	}
	if !hasAny {
		return "unissued"
	}
	return worst
}

// certStats 统计当前有效证书:总数 / 即将过期 / 已过期。
func certStats(certs []models.Cert, now time.Time) (total, expiring, expired int) {
	for _, c := range certs {
		switch certStatus(c.NotAfter, now) {
		case "expiring":
			expiring++
		case "expired":
			expired++
		}
		total++
	}
	return total, expiring, expired
}

// latestIssued 某 rootDomain 下证书的最近签发时间(NotBefore 最大)。
func latestIssued(certs []models.Cert, rootDomain string) *time.Time {
	var latest *time.Time
	for _, c := range certs {
		if c.FQDN != rootDomain && !strings.HasSuffix(c.FQDN, "."+rootDomain) {
			continue
		}
		t := c.NotBefore
		if latest == nil || t.After(*latest) {
			latest = &t
		}
	}
	return latest
}

func worseStatus(a, b string) string {
	rank := map[string]int{"success": 0, "expiring": 1, "expired": 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func distinctProviders(creds []models.DNSCredential) int {
	seen := map[string]struct{}{}
	for _, c := range creds {
		seen[c.Provider] = struct{}{}
	}
	return len(seen)
}
