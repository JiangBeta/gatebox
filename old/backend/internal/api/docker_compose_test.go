package api

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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

	d := &dockerAPI{dataDir: dir}
	got := d.scanManagedProjects()
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanManagedProjects = %v, want %v", got, want)
	}
}
