package acme

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/model"
)

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// TestIssueArgs 签发参数含 --home 与 --dnssleep 300;force 追加 --force。
func TestIssueArgs(t *testing.T) {
	iss := &Issuer{HomeDir: "/data/tools/acme"}
	args := iss.issueArgs("dns_tencent", "a.example.com", false)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--home /data/tools/acme", "--issue", "--dns dns_tencent", "-d a.example.com", "--server letsencrypt", "--dnssleep 300"} {
		if !strings.Contains(joined, want) {
			t.Errorf("issueArgs 缺少 %q:\n%s", want, joined)
		}
	}
	if hasArg(args, "--force") {
		t.Errorf("非 force 不应含 --force:\n%s", joined)
	}
	if !hasArg(iss.issueArgs("dns_cf", "b.example.com", true), "--force") {
		t.Errorf("force=true 应含 --force")
	}
}

// TestRetryableIssueError 瞬态(DNS 传播)失败可重试,凭证类错误不重试。
func TestRetryableIssueError(t *testing.T) {
	retryable := []string{
		"gatebox.neob.cn: Invalid status. Verification error details: During secondary validation: No TXT record found at _acme-challenge.gatebox.neob.cn",
		"No TXT record found",
		"secondary validation",
		"Verification error",
		"read tcp: i/o timeout",
	}
	for _, s := range retryable {
		if !retryableIssueError(s) {
			t.Errorf("应可重试: %q", s)
		}
	}
	permanent := []string{
		"Error add txt for domain:_acme-challenge.x: 401 Unauthorized",
		"dnspod 凭证缺少腾讯云 SecretId/SecretKey",
		"not found zone",
	}
	for _, s := range permanent {
		if retryableIssueError(s) {
			t.Errorf("不应重试: %q", s)
		}
	}
}

// TestEnsureRetriesTransient 瞬态失败重试至多 issueAttempts 次后成功。
func TestEnsureRetriesTransient(t *testing.T) {
	dir := t.TempDir()
	iss := &Issuer{
		CertsDir:    filepath.Join(dir, "certs"),
		HomeDir:     filepath.Join(dir, "acme"),
		BinPath:     "acme.sh",
		failCooldow: map[string]time.Time{},
	}
	restore := runCmd
	defer func() { runCmd = restore }()

	issueCalls := 0
	runCmd = func(_ context.Context, _ string, _ []string, args ...string) (string, error) {
		if hasArg(args, "--install-cert") {
			return "installed", nil
		}
		issueCalls++
		if issueCalls < issueAttempts { // 前两次瞬态失败,第三次成功
			return "Invalid status. During secondary validation: No TXT record found", errors.New("exit 2")
		}
		return "ok", nil
	}

	cred := model.DNSCredential{Provider: model.ProviderCloudflare, Fields: map[string]string{"token": "x"}}
	if _, err := iss.Ensure(context.Background(), cred, "a.example.com"); err != nil {
		t.Fatalf("Ensure 应重试后成功: %v", err)
	}
	if issueCalls != issueAttempts {
		t.Errorf("签发调用 = %d, want %d", issueCalls, issueAttempts)
	}
}

// TestEnsureNoRetryOnPermanent 永久失败(凭证错误)只尝试一次。
func TestEnsureNoRetryOnPermanent(t *testing.T) {
	dir := t.TempDir()
	iss := &Issuer{
		CertsDir:    filepath.Join(dir, "certs"),
		HomeDir:     filepath.Join(dir, "acme"),
		BinPath:     "acme.sh",
		failCooldow: map[string]time.Time{},
	}
	restore := runCmd
	defer func() { runCmd = restore }()

	issueCalls := 0
	runCmd = func(_ context.Context, _ string, _ []string, _ ...string) (string, error) {
		issueCalls++
		return "Error add txt for domain:_acme-challenge.a: 401 Unauthorized", errors.New("exit 1")
	}

	cred := model.DNSCredential{Provider: model.ProviderCloudflare, Fields: map[string]string{"token": "x"}}
	if _, err := iss.Ensure(context.Background(), cred, "a.example.com"); err == nil {
		t.Fatal("永久失败应返回错误")
	}
	if issueCalls != 1 {
		t.Errorf("永久失败签发调用 = %d, want 1", issueCalls)
	}
}
