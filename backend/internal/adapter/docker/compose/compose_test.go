package compose

import "testing"

// TestManagedPaths 托管项目目录的唯一真源:ManagedRoot/Dir/File 必须一致派生。
func TestManagedPaths(t *testing.T) {
	c := New("/data/root")
	if got := c.ManagedRoot(); got != "/data/root/appData" {
		t.Errorf("ManagedRoot = %q, want /data/root/appData", got)
	}
	if got := c.ManagedDir("myproj"); got != "/data/root/appData/myproj" {
		t.Errorf("ManagedDir = %q, want /data/root/appData/myproj", got)
	}
	if got := c.ManagedFile("myproj"); got != "/data/root/appData/myproj/docker-compose.yaml" {
		t.Errorf("ManagedFile = %q, want /data/root/appData/myproj/docker-compose.yaml", got)
	}
}
