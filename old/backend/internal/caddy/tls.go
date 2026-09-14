package caddy

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JiangBeta/gatebox/internal/models"
)

// rootDomainOf 返回 host 对应的 rootDomain 凭证。
//
// 匹配规则:host 等于 rootDomain,或以 "."+rootDomain 结尾。
// map 遍历顺序不确定,若多个 rootDomain 同时匹配(如同时登记了 neob.cn 与 a.neob.cn)
// 结果不确定——MVP 约定一个 host 只匹配一个已登记 rootDomain。
func rootDomainOf(host string, dns map[string]models.DNSCredential) (models.DNSCredential, bool) {
	for root, cred := range dns {
		if host == root || strings.HasSuffix(host, "."+root) {
			return cred, true
		}
	}
	return models.DNSCredential{}, false
}

// renderTLSDNS 生成 site block 的 tls 指令,引用 acme.sh 签发的证书文件(ADR-013 修订)。
//
// 证书由独立 acme.sh(DNS-01)签发,落盘于 <dataDir>/tools/acme/certs/<fqdn>/:
//
//	fullchain.pem / key.pem
//
// caddy 仅以文件方式加载证书,不再自行 ACME DNS 签发。
func renderTLSDNS(dataDir, fqdn string) string {
	dir := filepath.Join(dataDir, "tools", "acme", "certs", fqdn)
	crt := filepath.Join(dir, "fullchain.pem")
	key := filepath.Join(dir, "key.pem")
	return "\ttls " + crt + " " + key + "\n"
}

// tlsFilesExist 返回 acme.sh 证书产物是否已落盘。
// 缺失时 site block 不写 tls 文件引用(避免 caddy load 因文件缺失失败),
// 该域名交由 caddy 默认行为(公网 ACME 或证书报错,不影响其余域名)。
func tlsFilesExist(dataDir, fqdn string) bool {
	dir := filepath.Join(dataDir, "tools", "acme", "certs", fqdn)
	crt, cerr := os.Stat(filepath.Join(dir, "fullchain.pem"))
	key, kerr := os.Stat(filepath.Join(dir, "key.pem"))
	return cerr == nil && kerr == nil && !crt.IsDir() && !key.IsDir()
}
