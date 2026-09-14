package client

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// 本文件的测试对接真实的 Docker 守护进程,**全部为只读操作**:
// 不创建、不启动、不停止、不删除任何容器/镜像/网络/卷。
// socket 不可用时自动跳过,以便在无 Docker 的 CI 上照常执行单元测试。

func requireDocker(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("GATEBOX_SKIP_DOCKER_IT") != "" {
		t.Skip("显式跳过 Docker 集成测试")
	}
	if _, err := os.Stat(DefaultSocket); err != nil {
		t.Skipf("跳过:%s 不存在", DefaultSocket)
	}
	conn, err := net.DialTimeout("unix", DefaultSocket, 2*time.Second)
	if err != nil {
		t.Skipf("跳过:无法连接 %s(%v)", DefaultSocket, err)
	}
	conn.Close()
	return NewUnix("")
}

func TestIntegration_Ping(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	version, err := c.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if version == "" {
		t.Error("daemon 未返回 Api-Version 头")
	}
	t.Logf("daemon API 版本 = %s(客户端请求路径使用 %s)", version, APIVersion)
}

func TestIntegration_ListContainers(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	list, err := c.ListContainers(ctx, ListContainersOptions{All: true})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	t.Logf("发现 %d 个容器", len(list))

	for _, ct := range list {
		if ct.ID == "" {
			t.Error("容器 ID 为空")
		}
		if ct.Name() == "" {
			t.Errorf("容器 %s 名称为空", ct.ID[:12])
		}
		// 若为编排容器,project 与 service 必须成对出现(ADR-016 的稳定标识)
		if p := ct.ComposeProject(); p != "" && ct.ComposeService() == "" {
			t.Errorf("容器 %s 有 project=%q 却无 service", ct.Name(), p)
		}
		t.Logf("  %-24s %-10s project=%-10q service=%q",
			ct.Name(), ct.State, ct.ComposeProject(), ct.ComposeService())
	}
}

func TestIntegration_ListContainers_LabelFilter(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	all, err := c.ListContainers(ctx, ListContainersOptions{All: true})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}

	// 找一个真实存在的 compose 项目来验证 filters 编码确实生效
	var project string
	for _, ct := range all {
		if p := ct.ComposeProject(); p != "" {
			project = p
			break
		}
	}
	if project == "" {
		t.Skip("跳过:本机无 compose 项目")
	}

	filtered, err := c.ListContainers(ctx, ListContainersOptions{
		All:     true,
		Filters: Filters{"label": {LabelComposeProject + "=" + project}},
	})
	if err != nil {
		t.Fatalf("带 filters 的 ListContainers: %v", err)
	}
	if len(filtered) == 0 {
		t.Fatalf("按 project=%q 过滤返回 0 个容器,filters 编码可能有误", project)
	}
	for _, ct := range filtered {
		if ct.ComposeProject() != project {
			t.Errorf("过滤结果含无关容器 %s(project=%q)", ct.Name(), ct.ComposeProject())
		}
	}
	t.Logf("项目 %q 过滤出 %d/%d 个容器", project, len(filtered), len(all))
}

func TestIntegration_InspectContainer(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	list, err := c.ListContainers(ctx, ListContainersOptions{All: true})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(list) == 0 {
		t.Skip("跳过:本机无容器")
	}

	d, err := c.InspectContainer(ctx, list[0].ID)
	if err != nil {
		t.Fatalf("InspectContainer: %v", err)
	}
	if d.ID != list[0].ID {
		t.Errorf("ID 不一致: %s vs %s", d.ID, list[0].ID)
	}
	if d.TrimmedName() != list[0].Name() {
		t.Errorf("名称不一致: %q vs %q", d.TrimmedName(), list[0].Name())
	}
	if d.Created.IsZero() {
		t.Error("Created 解析失败(inspect 返回 RFC3339 字符串,与列表的 Unix 秒不同)")
	}
	if d.State.Running && d.State.StartedAt.IsZero() {
		t.Error("运行中容器的 StartedAt 为零值,运行时长将无法计算")
	}
	t.Logf("%s: tty=%v running=%v uptime=%v",
		d.TrimmedName(), d.Config.Tty, d.State.Running, d.State.Uptime().Truncate(time.Second))
}

func TestIntegration_InspectContainer_NotFound(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.InspectContainer(ctx, "gatebox-definitely-no-such-container")
	if err == nil {
		t.Fatal("不存在的容器应报错")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestIntegration_StatsOnce 验证 CPU/内存公式在真实数据上跑得通且结果合理。
func TestIntegration_StatsOnce(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	list, err := c.ListContainers(ctx, ListContainersOptions{})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(list) == 0 {
		t.Skip("跳过:本机无运行中的容器")
	}

	target := list[0]
	s, err := c.ContainerStatsOnce(ctx, target.ID)
	if err != nil {
		t.Fatalf("ContainerStatsOnce: %v", err)
	}

	cpu := s.CPUPercent()
	mem := s.MemoryUsage()
	memPct := s.MemoryPercent()

	if cpu < 0 {
		t.Errorf("CPU 占用率为负: %f", cpu)
	}
	if mem == 0 {
		t.Error("内存用量为 0,字段映射可能有误")
	}
	if mem > s.MemoryStats.Usage {
		t.Errorf("扣除缓存后的内存 %d 大于原始 usage %d", mem, s.MemoryStats.Usage)
	}
	if memPct < 0 || memPct > 100 {
		t.Errorf("内存占用率越界: %f", memPct)
	}
	if s.CPUStats.OnlineCPUs == 0 && len(s.CPUStats.CPUUsage.PerCPUUsage) == 0 {
		t.Log("注意:daemon 未提供 online_cpus 也无 percpu_usage,CPU 计算已回退到单核")
	}

	t.Logf("%s: CPU=%.2f%% 内存=%.1fMiB/%.1fMiB (%.2f%%) online_cpus=%d",
		target.Name(), cpu,
		float64(mem)/(1<<20), float64(s.MemoryStats.Limit)/(1<<20), memPct,
		s.CPUStats.OnlineCPUs)

	// 确认走的是 cgroup v2 分支(本机实测),或明确记录 v1
	if _, v2 := s.MemoryStats.Stats["inactive_file"]; v2 {
		t.Log("内存口径:cgroup v2(扣 inactive_file)")
	} else if _, v1 := s.MemoryStats.Stats["total_inactive_file"]; v1 {
		t.Log("内存口径:cgroup v1(扣 total_inactive_file)")
	} else {
		t.Log("内存口径:无缓存字段,使用原始 usage")
	}
}

// TestIntegration_Logs 验证日志流按 TTY 与否正确解析。
//
// 必须遍历容器找到**确实有日志输出**的那个:若固定取第一个容器,碰上一个
// 无输出的容器(本机 traefik 即是)会读到 0 帧却依然 PASS——测试通过了,
// 帧解析路径却一行都没执行到,是典型的假阳性。
func TestIntegration_Logs(t *testing.T) {
	c := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	list, err := c.ListContainers(ctx, ListContainersOptions{})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(list) == 0 {
		t.Skip("跳过:本机无运行中的容器")
	}

	var (
		checked    int
		unreadable []string
	)
	for _, target := range list {
		detail, err := c.InspectContainer(ctx, target.ID)
		if err != nil {
			t.Fatalf("InspectContainer(%s): %v", target.Name(), err)
		}

		rc, err := c.ContainerLogs(ctx, target.ID, LogsOptions{
			Stdout: true, Stderr: true, Tail: "20", // 不 follow,读完即止
		})
		if err != nil {
			// 部分日志驱动(syslog / fluentd / gelf 等)不支持回读,
			// daemon 会直接报错。这不是客户端缺陷,记录后继续找下一个。
			unreadable = append(unreadable, target.Name()+": "+err.Error())
			continue
		}

		lf := NewLogFrames(rc, detail.Config.Tty)
		var frames, bytesRead int
		var sawStdout, sawStderr bool
		for frames < 200 {
			st, payload, err := lf.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				rc.Close()
				t.Fatalf("解析 %s 的日志帧失败(tty=%v): %v", target.Name(), detail.Config.Tty, err)
			}
			frames++
			bytesRead += len(payload)
			switch st {
			case StreamStdout:
				sawStdout = true
			case StreamStderr:
				sawStderr = true
			}
		}
		rc.Close()
		checked++

		t.Logf("%-16s tty=%-5v 帧=%-4d 字节=%-6d stdout=%v stderr=%v",
			target.Name(), detail.Config.Tty, frames, bytesRead, sawStdout, sawStderr)

		if bytesRead > 0 {
			// 找到真正有输出的容器,帧解析路径已被执行 —— 目的达成
			t.Logf("✓ 已在 %s 上验证日志帧解析(%d 字节)", target.Name(), bytesRead)
			return
		}
	}

	for _, u := range unreadable {
		t.Logf("日志不可读(驱动限制): %s", u)
	}
	t.Skipf("跳过:检查了 %d 个容器,均无日志输出,无法验证帧解析", checked)
}
