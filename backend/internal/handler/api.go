// Package api 提供 HTTP 路由与处理器。
package handler

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/acme"
	"github.com/JiangBeta/gatebox/internal/adapter/cert"
	"github.com/JiangBeta/gatebox/internal/model"
	"github.com/JiangBeta/gatebox/internal/repository"
)

// 证书「即将过期」阈值(天)。
const expiringSoonDays = 10

type api struct {
	s  *repository.Store
	cm cert.CertManager
	ac *acme.Issuer // 证书签发/删除(nil 时证书管理只读)
}

// Register 将 API 路由注册到 mux。
func Register(mux *http.ServeMux, s *repository.Store, cm cert.CertManager, ac *acme.Issuer) {
	a := &api{s: s, cm: cm, ac: ac}

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
	mux.HandleFunc("GET /api/v1/certificates/logs", a.listCertLogs)
	mux.HandleFunc("GET /api/v1/certificates/logs/{id}", a.getCertLog)
	mux.HandleFunc("GET /api/v1/certificates/{fqdn}", a.getCertDetail)
	mux.HandleFunc("POST /api/v1/certificates/{fqdn}/renew", a.renewCert)
	mux.HandleFunc("DELETE /api/v1/certificates/{fqdn}", a.deleteCert)
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
	d := &model.Domain{
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
	if errors.Is(err, repository.ErrNotFound) {
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
	if errors.Is(err, repository.ErrNotFound) {
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
	id := r.PathValue("id")
	d, err := a.s.GetDomain(id)
	if errors.Is(err, repository.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "域名不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 校验其下无路由引用(ADR-012:删除 Domain 需校验其下无二级域名引用)。
	services, err := a.s.ListServices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if domainReferenced(services, d.Name) {
		writeErr(w, http.StatusConflict, "该域名下有服务引用,请先在「网关」删除对应服务")
		return
	}
	if err := a.s.DeleteDomain(id); err != nil {
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
	// 二级域名数聚合自网关 Service 的域名行(docs/domain.md §1:二级域名不建实体)。
	services, err := a.s.ListServices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	subCounts := subdomainCountByRoot(services)

	now := time.Now()
	total, expiring, expired := certStats(certs, now)

	resp := overviewResponse{
		DomainCount:      len(domains),
		CredentialCount:  len(creds),
		ProviderCount:    distinctProviders(creds),
		CertTotal:        total,
		CertExpiringSoon: expiring,
		CertExpired:      expired,
		Domains:          make([]domainOverview, 0, len(domains)),
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
			SubdomainCnt: subCounts[d.Name],
			CreatedAt:    d.CreatedAt.Format(time.RFC3339),
			LastIssuedAt: lastIssued,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// subdomainCountByRoot 按 rootDomain 统计二级域名条数(含 apex)。
// 只看落库的 manual 服务(docker 派生不落库、无 rootDomain 语义)。
func subdomainCountByRoot(services []model.Service) map[string]int {
	agg := map[string]int{}
	for _, svc := range services {
		for _, d := range svc.Domains {
			if d.RootDomain == "" {
				continue
			}
			agg[d.RootDomain]++
		}
	}
	return agg
}

// domainReferenced 判断 rootDomain 是否仍被服务的域名行引用(删除域名前置校验)。
func domainReferenced(services []model.Service, name string) bool {
	for _, svc := range services {
		for _, d := range svc.Domains {
			if d.RootDomain == name {
				return true
			}
		}
	}
	return false
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
	var in model.DNSCredential
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
	if errors.Is(err, repository.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "凭证不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in model.DNSCredential
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
	var in model.DNSCredential
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

// certDetail 单个证书详情(证书管理「查看」)。
type certDetail struct {
	model.Cert
	PublicKey  string `json:"publicKey"`  // fullchain.pem 文本
	PrivateKey string `json:"privateKey"` // key.pem 文本
}

// validateFQDN 校验路径参数为合法域名,防止路径穿越。
func validateFQDN(fqdn string) bool {
	if len(fqdn) == 0 || len(fqdn) > 253 {
		return false
	}
	for _, c := range fqdn {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// getCertDetail 返回某个 fqdn 的证书内容:公钥(fullchain)与私钥(key)分开展示。
func (a *api) getCertDetail(w http.ResponseWriter, r *http.Request) {
	if a.ac == nil {
		writeErr(w, http.StatusServiceUnavailable, "证书管理未启用")
		return
	}
	fqdn := r.PathValue("fqdn")
	if !validateFQDN(fqdn) {
		writeErr(w, http.StatusBadRequest, "非法域名")
		return
	}
	full, err := os.ReadFile(filepath.Join(a.ac.CertsDir, fqdn, "fullchain.pem"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "证书不存在: "+fqdn)
		return
	}
	key, err := os.ReadFile(filepath.Join(a.ac.CertsDir, fqdn, "key.pem"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "私钥不存在: "+fqdn)
		return
	}
	meta, _, err := parseCertMeta(full)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "解析证书失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, certDetail{Cert: *meta, PublicKey: string(full), PrivateKey: string(key)})
}

// findCredentialForFQDN 用最长后缀匹配把 fqdn 归到受管根域,取对应 DNS 凭证。
func (a *api) findCredentialForFQDN(fqdn string) (*model.DNSCredential, error) {
	domains, err := a.s.ListDomains()
	if err != nil {
		return nil, err
	}
	best := ""
	for _, d := range domains {
		if fqdn == d.Name || strings.HasSuffix(fqdn, "."+d.Name) {
			if len(d.Name) > len(best) {
				best = d.Name
			}
		}
	}
	if best == "" {
		return nil, nil
	}
	var raw *model.DNSCredential
	creds, err := a.s.ListCredentials()
	if err != nil {
		return nil, err
	}
	for i := range creds {
		if creds[i].ID == credentialIDFor(best, domains, creds) {
			raw = &creds[i]
			break
		}
	}
	if raw == nil {
		return nil, nil
	}
	return raw, nil
}

// credentialIDFor 取 rootDomain 的凭证 ID。
func credentialIDFor(root string, domains []model.Domain, _ []model.DNSCredential) string {
	for _, d := range domains {
		if d.Name == root {
			return d.CredentialID
		}
	}
	return ""
}

// renewCert 强制重新申请某个域的证书(DNS-01,DNS 凭证)。
func (a *api) renewCert(w http.ResponseWriter, r *http.Request) {
	if a.ac == nil {
		writeErr(w, http.StatusServiceUnavailable, "证书签发未启用")
		return
	}
	fqdn := r.PathValue("fqdn")
	if !validateFQDN(fqdn) {
		writeErr(w, http.StatusBadRequest, "非法域名")
		return
	}
	cred, err := a.findCredentialForFQDN(fqdn)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cred == nil {
		writeErr(w, http.StatusBadRequest, "该域名未登记 DNS 凭证(请先在域名管理配置)")
		return
	}
	logFile, err := a.ac.ForceRenew(r.Context(), *cred, fqdn)
	if err != nil {
		a.logCert("renew", fqdn, "fail", err.Error(), logFile)
		writeErr(w, http.StatusInternalServerError, "重新申请失败: "+err.Error())
		return
	}
	a.logCert("renew", fqdn, "success", "证书重新申请完成", logFile)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteCert 删除某个域的证书文件。
func (a *api) deleteCert(w http.ResponseWriter, r *http.Request) {
	if a.ac == nil {
		writeErr(w, http.StatusServiceUnavailable, "证书管理未启用")
		return
	}
	fqdn := r.PathValue("fqdn")
	if !validateFQDN(fqdn) {
		writeErr(w, http.StatusBadRequest, "非法域名")
		return
	}
	if err := a.ac.Remove(fqdn); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除证书失败: "+err.Error())
		return
	}
	a.logCert("delete", fqdn, "success", "证书已删除", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// logCert 追加一条证书操作日志(api 侧:renew/delete)。
func (a *api) logCert(action, fqdn, status, msg, logFile string) {
	_ = a.s.SaveCertLog(&model.CertLog{
		ID: newID(), FQDN: fqdn, Action: action, Status: status, Message: msg, LogFile: logFile, CreatedAt: time.Now(),
	})
}

// getCertLog 返回某条证书操作日志对应的 acme.sh 原始输出全文(debug 用)。
func (a *api) getCertLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := a.s.GetCertLog(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "日志不存在")
		return
	}
	if row.LogFile == "" {
		writeJSON(w, http.StatusOK, map[string]string{"log": ""})
		return
	}
	b, err := os.ReadFile(row.LogFile)
	if err != nil {
		writeErr(w, http.StatusNotFound, "日志文件不存在: "+row.LogFile)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"log": string(b)})
}

// parseCertMeta 解析 PEM 证书首个证书的元数据(与 cert 包 parseCertFile 对齐)。
func parseCertMeta(data []byte) (*model.Cert, time.Time, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, time.Time{}, errors.New("无效 PEM")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, time.Time{}, err
	}
	return &model.Cert{
		FQDN:      c.Subject.CommonName,
		Issuer:    c.Issuer.CommonName,
		NotBefore: c.NotBefore,
		NotAfter:  c.NotAfter,
		Serial:    c.SerialNumber.String(),
		KeyAlgo:   c.PublicKeyAlgorithm.String(),
	}, c.NotAfter, nil
}

// listCertLogs 返回证书操作日志(可带 ?fqdn= 过滤单个域名)。
func (a *api) listCertLogs(w http.ResponseWriter, r *http.Request) {
	rows, err := a.s.ListCertLogs(200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fqdn := strings.TrimSpace(r.URL.Query().Get("fqdn")); fqdn != "" && validateFQDN(fqdn) {
		filtered := rows[:0]
		for _, row := range rows {
			if row.FQDN == fqdn {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if rows == nil {
		rows = []model.CertLog{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// --- 纯函数(便于单测) ---

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validateCredentialFields 按供应商校验必要字段非空。
func validateCredentialFields(c *model.DNSCredential) (bool, string) {
	switch c.Provider {
	case model.ProviderCloudflare:
		if strings.TrimSpace(c.Fields["token"]) == "" {
			return false, "缺少 token"
		}
	case model.ProviderDNSPod:
		if strings.TrimSpace(c.Fields["id"]) == "" || strings.TrimSpace(c.Fields["token"]) == "" {
			return false, "缺少 id 或 token"
		}
	case model.ProviderAliyun:
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
func aggregateDomainCertStatus(certs []model.Cert, rootDomain string, now time.Time) string {
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
func certStats(certs []model.Cert, now time.Time) (total, expiring, expired int) {
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
func latestIssued(certs []model.Cert, rootDomain string) *time.Time {
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

func distinctProviders(creds []model.DNSCredential) int {
	seen := map[string]struct{}{}
	for _, c := range creds {
		seen[c.Provider] = struct{}{}
	}
	return len(seen)
}
