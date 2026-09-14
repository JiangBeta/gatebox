package caddy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBinaryVersion(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "caddy")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'v2.11.4 h1:XKxkMTgNSizE'\n"), 0o700); err != nil {
		t.Fatalf("写 fake caddy: %v", err)
	}
	v, err := BinaryVersion(context.Background(), bin)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != "v2.11.4" {
		t.Errorf("version = %q, want v2.11.4", v)
	}

	if _, err := BinaryVersion(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Fatal("二进制缺失应报错")
	}
}

// TestValidateUnavailable 二进制缺失:返回 ErrValidateUnavailable(可降级跳过)。
func TestValidateUnavailable(t *testing.T) {
	err := Validate(context.Background(), filepath.Join(t.TempDir(), "no-such-caddy"), "{}\n")
	if err == nil {
		t.Fatal("二进制缺失应返回错误")
	}
	if !errors.Is(err, ErrValidateUnavailable) {
		t.Fatalf("应包装 ErrValidateUnavailable, got %v", err)
	}
}

// fakeCaddy 生成一个模拟 caddy 的 shell 脚本,输出固定文本并按指定码退出。
func fakeCaddy(t *testing.T, exitCode int, out string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "caddy")
	script := "#!/bin/sh\ncat <<'EOF'\n" + out + "EOF\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatalf("写 fake caddy: %v", err)
	}
	return bin
}

func TestValidateOK(t *testing.T) {
	bin := fakeCaddy(t, 0, "Valid configuration\n")
	if err := Validate(context.Background(), bin, "example.com {\n respond ok\n}\n"); err != nil {
		t.Fatalf("合法配置不应失败: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	bin := fakeCaddy(t, 1, "adapt: unrecognized directive\n")
	err := Validate(context.Background(), bin, ":8443 {\n bogus_directive foo\n}\n")
	if err == nil {
		t.Fatal("非法配置应返回错误")
	}
	if errors.Is(err, ErrValidateUnavailable) {
		t.Fatalf("校验失败不应误判为不可用: %v", err)
	}
	if !strings.Contains(err.Error(), "unrecognized directive") {
		t.Errorf("错误应带 caddy 输出, got: %v", err)
	}
}
