package repository

import (
	"testing"

	"github.com/JiangBeta/gatebox/internal/model"
)

func TestEncryptDecrypt(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	plain := []byte("hello secret")
	enc, err := s.encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if string(enc) == string(plain) {
		t.Fatal("密文不应等于明文")
	}
	dec, err := s.decrypt(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(dec) != string(plain) {
		t.Fatalf("解密结果不一致: %q != %q", dec, plain)
	}
}

func TestCredentialRoundTrip(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	c := &model.DNSCredential{
		ID:       "c1",
		Provider: model.ProviderCloudflare,
		Name:     "主账号",
		Fields:   map[string]string{"token": "abc123"},
	}
	if err := s.SaveCredential(c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.GetCredential("c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Fields["token"] != "abc123" {
		t.Fatalf("token 不一致: %q", got.Fields["token"])
	}
	list, _ := s.ListCredentials()
	if len(list) != 1 {
		t.Fatalf("凭证数 = %d, want 1", len(list))
	}
	if err := s.DeleteCredential("c1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetCredential("c1"); err != ErrNotFound {
		t.Fatalf("删除后应 NotFound, got %v", err)
	}
}

func TestDomainCRUD(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()

	d := &model.Domain{ID: "d1", Name: "neob.cn"}
	if err := s.SaveDomain(d); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.GetDomain("d1")
	if err != nil || got.Name != "neob.cn" {
		t.Fatalf("get: %v %v", got, err)
	}
	if err := s.DeleteDomain("d1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetDomain("d1"); err != ErrNotFound {
		t.Fatalf("删除后应 NotFound, got %v", err)
	}
}
