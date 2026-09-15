package component

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestNewCoreRegistryAbsPaths 回归：spawn 会设置子进程工作目录，所有
// 二进制与参数路径必须是绝对路径，否则相对 workDir 解析导致启动失败。
func TestNewCoreRegistryAbsPaths(t *testing.T) {
	r := NewCoreRegistry("./relX", "./relX/tools/caddy/caddy", "http://localhost:2019", nil)
	for _, c := range r.cores {
		if c.bin != "" && !filepath.IsAbs(c.bin) {
			t.Errorf("组件 %s 的 bin 非绝对路径: %s", c.desc.ID, c.bin)
		}
		if c.workDir != "" && !filepath.IsAbs(c.workDir) {
			t.Errorf("组件 %s 的 workDir 非绝对路径: %s", c.desc.ID, c.workDir)
		}
		for _, a := range c.runArgs {
			if strings.Contains(a, string(filepath.Separator)) && !filepath.IsAbs(a) {
				t.Errorf("组件 %s 的 runArg 非绝对路径: %s", c.desc.ID, a)
			}
		}
	}
}

func TestApplyLatest(t *testing.T) {
	cases := []struct {
		name           string
		current        string
		latest         string
		wantLatest     string
		wantUpdateable bool
	}{
		{"源更新", "2.0.0", "2.1.0", "2.1.0", true},
		{"已是最新", "2.1.0", "2.1.0", "2.1.0", false},
		{"系统领先于源", "3.1.5", "3.1.4", "3.1.5", false},
		{"当前未知", "", "2.1.0", "2.1.0", false},
	}
	for _, tc := range cases {
		info := Info{Current: tc.current}
		applyLatest(&info, tc.latest)
		if info.Latest != tc.wantLatest || info.UpdateAvailable != tc.wantUpdateable {
			t.Errorf("%s: Latest=%q UpdateAvailable=%v, 期望 %q/%v",
				tc.name, info.Latest, info.UpdateAvailable, tc.wantLatest, tc.wantUpdateable)
		}
	}
}
