package caddy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrValidateUnavailable 表示 caddy 二进制缺失/不可执行,前置校验被跳过(降级)。
// 调用方可 errors.Is 判断:校验不可用 ≠ 配置非法,不应作为硬失败。
var ErrValidateUnavailable = errors.New("caddy validate 不可用")

// BinaryVersion 通过 caddy 二进制 CLI 读取版本号(caddy admin API 无 /version 端点)。
// 返回首字段如 "v2.11.4";二进制缺失/不可执行返回包装 ErrValidateUnavailable 的错误。
func BinaryVersion(ctx context.Context, bin string) (string, error) {
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("%w: %v", ErrValidateUnavailable, err)
	}
	cmd := exec.CommandContext(ctx, bin, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("caddy version 失败: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", errors.New("caddy version 输出为空")
	}
	return fields[0], nil
}

// Validate 用 caddy 二进制前置校验 Caddyfile 文本(ADR-002「先校验、失败回退」)。
//
// 只读校验,不触碰运行时;通过后调用方再走 /load 原子应用。
// 二进制缺失/不可执行返回包装 ErrValidateUnavailable 的错误(降级跳过);
// 配置非法返回带 caddy 输出内容的普通错误(应硬失败拦截,不进入 /load)。
func Validate(ctx context.Context, bin, caddyfile string) error {
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("%w: %v", ErrValidateUnavailable, err)
	}
	// caddy validate 支持 --config <file>:写入临时文件避免命名/管道差异。
	dir, err := os.MkdirTemp("", "gatebox-caddy-validate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(path, []byte(caddyfile), 0o600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "validate", "--config", path, "--adapter", "caddyfile")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("caddy validate 失败: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
