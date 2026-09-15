package component

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeRegistry 构造一个带真实长驻进程的测试注册表。
func fakeRegistry(t *testing.T) (*CoreRegistry, core) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "tools", "fakeproc", "fakeproc")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	// 用脚本避免与系统同名二进制（如 sleep）在卸载后发生 PATH 误判。
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatalf("写入脚本失败: %v", err)
	}
	c := core{
		desc: Descriptor{ID: "fakeproc", Name: "fakeproc", Tier: "optional", Removable: true,
			Capabilities: []string{"runnable"}},
		bin: bin, check: "pid", workDir: filepath.Dir(bin),
	}
	return &CoreRegistry{dataDir: dir, cores: []core{c}}, c
}

func TestProcessLifecycle(t *testing.T) {
	r, c := fakeRegistry(t)
	ctx := context.Background()

	info, err := r.Start(ctx, "fakeproc")
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if !info.Installed || info.Status.State != "running" {
		t.Fatalf("启动后状态异常: installed=%v state=%s", info.Installed, info.Status.State)
	}
	pid := readPid(pidPath(c))
	if pid <= 0 || !processAlive(pid) {
		t.Fatalf("pid 文件/进程异常: pid=%d", pid)
	}

	if _, err := r.Start(ctx, "fakeproc"); err == nil {
		t.Fatal("重复启动应报错")
	}

	if _, err := r.Stop(ctx, "fakeproc"); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	if processAlive(pid) {
		t.Fatal("停止后进程仍存活")
	}
	if _, err := os.Stat(pidPath(c)); !os.IsNotExist(err) {
		t.Fatal("停止后 pid 文件未清理")
	}

	if _, err := r.Restart(ctx, "fakeproc"); err != nil {
		t.Fatalf("重启失败: %v", err)
	}
	if _, err := r.Stop(ctx, "fakeproc"); err != nil {
		t.Fatalf("二次停止失败: %v", err)
	}
}

func TestUninstallRemovesAsDir(t *testing.T) {
	r, c := fakeRegistry(t)
	ctx := context.Background()
	if _, err := r.Start(ctx, "fakeproc"); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if _, err := r.Uninstall(ctx, "fakeproc"); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(c.bin)); !os.IsNotExist(err) {
		t.Fatal("卸载后组件目录未删除")
	}
	info, _ := r.Get(ctx, "fakeproc")
	if info.Installed {
		t.Fatal("卸载后仍标记为已安装")
	}
}

func TestStartNotRunnable(t *testing.T) {
	r, _ := fakeRegistry(t)
	r.cores[0].desc.Capabilities = []string{"health"}
	if _, err := r.Start(context.Background(), "fakeproc"); err == nil {
		t.Fatal("非 runnable 组件不应允许启动")
	}
}
