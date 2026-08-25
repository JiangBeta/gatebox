// Package cert 提供证书管理器抽象:当前实现读 Caddy 的证书存储,未来可切换独立 acme 客户端。
package cert

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/JiangBeta/gatebox/internal/models"
)

// CertManager 证书管理器接口(ADR-013)。
type CertManager interface {
	List(ctx context.Context) ([]models.Cert, error)
}

// CaddyCertManager 从 Caddy 的证书存储目录读取已签发证书。
//
// Caddy 将签发的证书存于 <data>/caddy/certificates/<issuer>/<domain>/<domain>.crt,
// 每个 .crt 是标准 x509 PEM,含 SAN / 有效期 / 颁发机构 / 序列号。
type CaddyCertManager struct {
	DataDir string // caddy 数据目录(内含 caddy/certificates)
}

// NewCaddy 构造 Caddy 证书管理器。
func NewCaddy(dataDir string) *CaddyCertManager {
	return &CaddyCertManager{DataDir: dataDir}
}

// List 遍历证书存储目录,解析所有 .crt 文件。
func (c *CaddyCertManager) List(_ context.Context) ([]models.Cert, error) {
	certsDir := filepath.Join(c.DataDir, "caddy", "certificates")
	var certs []models.Cert

	err := filepath.WalkDir(certsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过不可读项
		}
		if d.IsDir() || filepath.Ext(path) != ".crt" {
			return nil
		}
		cert, err := parseCertFile(path)
		if err != nil {
			return nil // 跳过无法解析的证书
		}
		certs = append(certs, *cert)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if certs == nil {
		certs = []models.Cert{}
	}
	return certs, nil
}

// parseCertFile 解析单个证书 PEM 文件为 Cert。
func parseCertFile(path string) (*models.Cert, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("无效 PEM: %s", path)
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	fqdn := c.Subject.CommonName
	if len(c.DNSNames) > 0 {
		fqdn = c.DNSNames[0]
	}
	issuer := c.Issuer.CommonName
	if issuer == "" && len(c.Issuer.Organization) > 0 {
		issuer = strings.Join(c.Issuer.Organization, ", ")
	}

	return &models.Cert{
		FQDN:      fqdn,
		Issuer:    issuer,
		NotBefore: c.NotBefore,
		NotAfter:  c.NotAfter,
		Serial:    c.SerialNumber.String(),
		KeyAlgo:   c.PublicKeyAlgorithm.String(),
	}, nil
}
