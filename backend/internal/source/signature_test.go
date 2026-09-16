package source

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyEd25519(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"schema":"gatebox.catalog/v1"}`)
	sig := ed25519.Sign(priv, data)
	sigB64 := base64.StdEncoding.EncodeToString(sig)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	if err := VerifyEd25519(data, sigB64, pubB64); err != nil {
		t.Fatalf("验签应通过: %v", err)
	}
	if err := VerifyEd25519([]byte("tampered"), sigB64, pubB64); err == nil {
		t.Error("篡改内容应验签失败")
	}
	if err := VerifyEd25519(data, sigB64, "not-base64!!"); err == nil {
		t.Error("非法公钥应报错")
	}
}

func TestFetchCatalogSigned(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	body := []byte(`{"schema":"gatebox.catalog/v1","plugins":[]}`)
	sig := ed25519.Sign(priv, body)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	mux.HandleFunc("/index.json.sig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(sig)))
	})

	c := New("")
	if _, err := c.FetchCatalogSigned(context.Background(), ts.URL+"/index.json", pubB64); err != nil {
		t.Fatalf("带签名索引应通过: %v", err)
	}
	// 错误公钥 → 失败
	badPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := c.FetchCatalogSigned(context.Background(), ts.URL+"/index.json", base64.StdEncoding.EncodeToString(badPub)); err == nil {
		t.Error("错误公钥应验签失败")
	}
}
