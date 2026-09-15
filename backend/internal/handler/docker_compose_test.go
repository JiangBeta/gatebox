package handler

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JiangBeta/gatebox/internal/adapter/docker/compose"
)

func TestScanManagedProjects(t *testing.T) {
	dir := t.TempDir()
	mk := func(p ...string) {
		full := filepath.Join(append([]string{dir}, p...)...)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("appData", "a", "docker-compose.yaml")
	mk("appData", "b", "docker-compose.yml")           // 兼容 .yml
	mk("appData", ".hidden", "docker-compose.yaml")    // 点前缀跳过
	mk("appData", "with space", "docker-compose.yaml") // 空格跳过
	mk("appData", "nodir-compose", "README.md")        // 无 compose 文件跳过

	d := &dockerAPI{dataDir: dir, cmp: compose.New(dir)}
	got := d.scanManagedProjects()
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanManagedProjects = %v, want %v", got, want)
	}
}

// TestComposeEditable 接管(adopt)后必须在列表重算中保留可编辑状态,且多文件项目始终只读。
func TestComposeEditable(t *testing.T) {
	cases := []struct {
		managed, adopted bool
		configFiles      string
		want             bool
	}{
		{false, false, "/x/docker-compose.yaml", false},              // 外部未接管
		{false, true, "/x/docker-compose.yaml", true},                // 外部已接管 ← 回归点
		{true, false, "/x/docker-compose.yaml", true},                // 托管
		{false, true, "/a/docker-compose.yaml,/b/other.yaml", false}, // 多文件不可编辑
		{true, true, "/a,/b", false},
	}
	for _, c := range cases {
		if got := composeEditable(c.managed, c.adopted, c.configFiles); got != c.want {
			t.Errorf("composeEditable(%v,%v,%q) = %v, want %v", c.managed, c.adopted, c.configFiles, got, c.want)
		}
	}
}
