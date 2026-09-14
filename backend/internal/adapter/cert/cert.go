// Package cert 提供证书管理器抽象:当前实现读 acme.sh 的证书存储。
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

	"github.com/JiangBeta/gatebox/internal/model"
)

// CertManager 证书管理器接口(ADR-013)。
type CertManager interface {
	List(ctx context.Context) ([]model.Cert, error)
}

// AcmeCertManager 从 acme.sh 的证书存储目录读取已签发证书。
//
// acme.sh 将签发的证书装于 <CertsDir>/<fqdn>/(fullchain.pem + key.pem),
// fullchain.pem 是标准 x509 PEM,含 SAN / 有效期 / 颁发机构 / 序列号。
type AcmeCertManager struct {
	CertsDir string // acme 证书根目录(如 data/tools/acme/certs)
}

// NewAcme 构造 acme.sh 证书管理器。
func NewAcme(certsDir string) *AcmeCertManager {
	return &AcmeCertManager{CertsDir: certsDir}
}

// List 遍历证书存储目录,解析所有 fullchain.pem 文件。
func (c *AcmeCertManager) List(_ context.Context) ([]model.Cert, error) {
	var certs []model.Cert

	err := filepath.WalkDir(c.CertsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过不可读项
		}
		if d.IsDir() || filepath.Base(path) != "fullchain.pem" {
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
		certs = []model.Cert{}
	}
	return certs, nil
}

// parseCertFile 解析单个证书 PEM 文件为 Cert。
func parseCertFile(path string) (*model.Cert, error) {
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

	return &model.Cert{
		FQDN:      fqdn,
		Issuer:    issuer,
		NotBefore: c.NotBefore,
		NotAfter:  c.NotAfter,
		Serial:    c.SerialNumber.String(),
		KeyAlgo:   c.PublicKeyAlgorithm.String(),
	}, nil
}
