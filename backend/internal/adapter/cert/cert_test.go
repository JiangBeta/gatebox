package cert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseAndListCerts(t *testing.T) {
	// 生成一张自签证书
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(12345),
		Subject:      pkix.Name{CommonName: "ddnsgo.neob.cn"},
		DNSNames:     []string{"ddnsgo.neob.cn"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	// 按 acme.sh 结构落盘:<certsDir>/<fqdn>/fullchain.pem
	certsDir := t.TempDir()
	certDir := filepath.Join(certsDir, "ddnsgo.neob.cn")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certDir, "fullchain.pem"), pemBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	cm := NewAcme(certsDir)
	certs, err := cm.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 {
		t.Fatalf("证书数 = %d, want 1", len(certs))
	}
	got := certs[0]
	if got.FQDN != "ddnsgo.neob.cn" {
		t.Errorf("FQDN = %q", got.FQDN)
	}
	if got.Serial != "12345" {
		t.Errorf("序列号 = %q", got.Serial)
	}
	if got.NotAfter.Before(got.NotBefore) {
		t.Errorf("NotAfter 应晚于 NotBefore")
	}
}
