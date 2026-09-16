package plugin

import (
	"os"
	"strings"
	"testing"
)

// TestSidecarEnv 最小化环境：不继承无关变量，但保留必需项。
func TestSidecarEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leaked")
	t.Setenv("GATEBOX_GITHUB_TOKEN", "secret-token")

	env := sidecarEnv("/data", "mosdns", 12345, "tok", "http://127.0.0.1:8099")
	joined := strings.Join(env, "\n")

	if strings.Contains(joined, "AWS_SECRET_ACCESS_KEY") {
		t.Error("不应继承无关密钥变量")
	}
	if strings.Contains(joined, "GATEBOX_GITHUB_TOKEN") {
		t.Error("不应继承控制面 GitHub 令牌")
	}
	for _, want := range []string{
		"GATEBOX_PLUGIN_ID=mosdns",
		"GATEBOX_PLUGIN_PORT=12345",
		"GATEBOX_PLUGIN_TOKEN=tok",
		"GATEBOX_CORE_URL=http://127.0.0.1:8099",
		"HOME=/data/tools/mosdns",
		"PATH=/usr/bin:/bin",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("缺少 %q", want)
		}
	}
	if os.Getenv("PATH") == "" {
		t.Skip("环境无 PATH")
	}
}
