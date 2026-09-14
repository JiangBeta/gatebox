package client

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// realStatsJSON 取自本机 traefik 容器的真实响应
// (Docker 29.6.2 / API 1.55 / cgroup v2,已裁剪无关字段)。
// 用真实数据做 fixture,可验证我们复现的正是 `docker stats` 显示的数字。
const realStatsJSON = `{
  "read": "2026-08-25T04:00:00.000000000Z",
  "cpu_stats": {
    "cpu_usage": { "total_usage": 4299804969000 },
    "system_cpu_usage": 5553225180000000,
    "online_cpus": 8
  },
  "precpu_stats": {
    "cpu_usage": { "total_usage": 4299713032000 },
    "system_cpu_usage": 5553217180000000,
    "online_cpus": 8
  },
  "memory_stats": {
    "usage": 32092160,
    "limit": 15579537408,
    "stats": {
      "active_file": 13500416,
      "inactive_file": 843776,
      "file": 14340096,
      "anon": 16000000
    }
  },
  "networks": {
    "eth0": { "rx_bytes": 1000, "tx_bytes": 2000 },
    "eth1": { "rx_bytes": 500,  "tx_bytes": 300 }
  },
  "blkio_stats": {
    "io_service_bytes_recursive": [
      { "op": "read",  "value": 4096 },
      { "op": "write", "value": 8192 },
      { "op": "sync",  "value": 999 }
    ]
  }
}`

func parseStats(t *testing.T, raw string) *Stats {
	t.Helper()
	var s Stats
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("解析 stats: %v", err)
	}
	return &s
}

// TestMemoryUsage_RealCgroupV2 验证内存口径与 `docker stats` 一致。
// 实测该容器 CLI 显示约 29.8MB;若错用 stats.file 会得到 17.7MB(差 1.7 倍)。
func TestMemoryUsage_RealCgroupV2(t *testing.T) {
	s := parseStats(t, realStatsJSON)

	got := s.MemoryUsage()
	want := uint64(32092160 - 843776) // usage - inactive_file
	if got != want {
		t.Errorf("MemoryUsage() = %d, want %d", got, want)
	}

	mb := float64(got) / (1 << 20)
	if math.Abs(mb-29.8) > 0.1 {
		t.Errorf("内存 = %.1f MB, want ≈29.8 MB(docker stats 口径)", mb)
	}
	// 反向确认:没有错用 file 字段
	if wrong := uint64(32092160 - 14340096); got == wrong {
		t.Error("错用了 stats.file 而非 inactive_file")
	}
}

func TestMemoryUsage_CgroupVariants(t *testing.T) {
	cases := []struct {
		name  string
		usage uint64
		stats map[string]uint64
		want  uint64
	}{
		{
			name:  "cgroup v2 扣 inactive_file",
			usage: 1000,
			stats: map[string]uint64{"inactive_file": 300},
			want:  700,
		},
		{
			name:  "cgroup v1 扣 total_inactive_file",
			usage: 1000,
			stats: map[string]uint64{"total_inactive_file": 200, "cache": 400},
			want:  800,
		},
		{
			name:  "v1 与 v2 字段并存时 v1 优先(与 docker CLI 一致)",
			usage: 1000,
			stats: map[string]uint64{"total_inactive_file": 200, "inactive_file": 300},
			want:  800,
		},
		{
			name:  "无缓存字段时返回原始 usage",
			usage: 1000,
			stats: map[string]uint64{"anon": 900},
			want:  1000,
		},
		{
			name:  "缓存值不小于 usage 时不下溢",
			usage: 1000,
			stats: map[string]uint64{"inactive_file": 1000},
			want:  1000,
		},
		{
			name:  "stats 为 nil 时返回原始 usage",
			usage: 1000,
			stats: nil,
			want:  1000,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Stats{MemoryStats: MemoryStats{Usage: tc.usage, Stats: tc.stats}}
			if got := s.MemoryUsage(); got != tc.want {
				t.Errorf("MemoryUsage() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCPUPercent(t *testing.T) {
	mk := func(total, preTotal, sys, preSys uint64, online uint32, percpu int) *Stats {
		s := &Stats{}
		s.CPUStats.CPUUsage.TotalUsage = total
		s.CPUStats.SystemUsage = sys
		s.CPUStats.OnlineCPUs = online
		s.PreCPUStats.CPUUsage.TotalUsage = preTotal
		s.PreCPUStats.SystemUsage = preSys
		if percpu > 0 {
			s.CPUStats.CPUUsage.PerCPUUsage = make([]uint64, percpu)
		}
		return s
	}

	cases := []struct {
		name string
		s    *Stats
		want float64
	}{
		{
			// 1 秒内用满 1 核,8 核机器 → 100%(docker stats 的口径可超 100)
			name: "占满一核",
			s:    mk(1e9, 0, 8e9, 0, 8, 0),
			want: 100,
		},
		{
			name: "占半核",
			s:    mk(5e8, 0, 8e9, 0, 8, 0),
			want: 50,
		},
		{
			name: "占满全部八核",
			s:    mk(8e9, 0, 8e9, 0, 8, 0),
			want: 800,
		},
		{
			// online_cpus 缺失时回退到 percpu_usage 长度(旧版 daemon / cgroup v1)
			name: "online_cpus 缺失回退 percpu 长度",
			s:    mk(1e9, 0, 4e9, 0, 0, 4),
			want: 100,
		},
		{
			name: "两者都缺失回退单核",
			s:    mk(1e9, 0, 4e9, 0, 0, 0),
			want: 25,
		},
		{
			// 首个采样点无前值可比,应返回 0 而非 NaN/负数
			name: "首个采样点",
			s:    mk(1e9, 1e9, 8e9, 8e9, 8, 0),
			want: 0,
		},
		{
			name: "计数器回绕导致负差值时返回 0",
			s:    mk(1e9, 2e9, 8e9, 4e9, 8, 0),
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.s.CPUPercent()
			if math.Abs(got-tc.want) > 0.01 {
				t.Errorf("CPUPercent() = %.2f, want %.2f", got, tc.want)
			}
		})
	}
}

func TestCPUPercent_RealSample(t *testing.T) {
	s := parseStats(t, realStatsJSON)
	// Δcpu=91937000, Δsys=8000000000, 8 核 → 0.0114938 × 8 × 100 ≈ 9.19%
	got := s.CPUPercent()
	want := 91937000.0 / 8000000000.0 * 8 * 100
	if math.Abs(got-want) > 0.001 {
		t.Errorf("CPUPercent() = %.4f, want %.4f", got, want)
	}
}

func TestMemoryPercent(t *testing.T) {
	s := parseStats(t, realStatsJSON)
	want := float64(32092160-843776) / float64(15579537408) * 100
	if got := s.MemoryPercent(); math.Abs(got-want) > 0.0001 {
		t.Errorf("MemoryPercent() = %f, want %f", got, want)
	}

	// limit 为 0 时不应除零
	zero := &Stats{MemoryStats: MemoryStats{Usage: 100, Limit: 0}}
	if got := zero.MemoryPercent(); got != 0 {
		t.Errorf("limit=0 时 MemoryPercent() = %f, want 0", got)
	}
}

func TestNetworkAndBlockIO(t *testing.T) {
	s := parseStats(t, realStatsJSON)

	rx, tx := s.NetworkIO()
	if rx != 1500 || tx != 2300 {
		t.Errorf("NetworkIO() = (%d, %d), want (1500, 2300)", rx, tx)
	}

	read, write := s.BlockIO()
	if read != 4096 || write != 8192 {
		t.Errorf("BlockIO() = (%d, %d), want (4096, 8192)", read, write)
	}
}

// --- 容器辅助方法 ---

func TestContainerName(t *testing.T) {
	cases := []struct {
		names []string
		want  string
	}{
		{[]string{"/jellyfin"}, "jellyfin"},
		{[]string{"/gbtest-web-1", "/other"}, "gbtest-web-1"},
		{[]string{"noslash"}, "noslash"},
		{nil, ""},
	}
	for _, tc := range cases {
		c := &Container{Names: tc.names}
		if got := c.Name(); got != tc.want {
			t.Errorf("Name(%v) = %q, want %q", tc.names, got, tc.want)
		}
	}
}

// TestComposeLabels 覆盖 ADR-016 的稳定标识:project + service。
func TestComposeLabels(t *testing.T) {
	c := &Container{Labels: map[string]string{
		LabelComposeProject:     "hosts",
		LabelComposeService:     "traefik",
		LabelComposeConfigFiles: "/home/beta/Projects/dockerSets/Hosts/niaoyun.yaml",
	}}
	if c.ComposeProject() != "hosts" {
		t.Errorf("ComposeProject() = %q", c.ComposeProject())
	}
	if c.ComposeService() != "traefik" {
		t.Errorf("ComposeService() = %q", c.ComposeService())
	}

	// 游离容器:无 compose label,应返回空串而非 panic
	loose := &Container{Labels: map[string]string{"foo": "bar"}}
	if loose.ComposeProject() != "" || loose.ComposeService() != "" {
		t.Error("游离容器应返回空的 project/service")
	}
	var nilLabels Container
	if nilLabels.ComposeProject() != "" {
		t.Error("Labels 为 nil 时应返回空串")
	}
}

func TestContainerStateUptime(t *testing.T) {
	running := &ContainerState{Running: true, StartedAt: time.Now().Add(-2 * time.Hour)}
	if d := running.Uptime(); d < 119*time.Minute || d > 121*time.Minute {
		t.Errorf("Uptime() = %v, want ≈2h", d)
	}

	stopped := &ContainerState{Running: false, StartedAt: time.Now().Add(-2 * time.Hour)}
	if d := stopped.Uptime(); d != 0 {
		t.Errorf("已停止容器 Uptime() = %v, want 0", d)
	}

	never := &ContainerState{Running: true}
	if d := never.Uptime(); d != 0 {
		t.Errorf("StartedAt 为零值时 Uptime() = %v, want 0", d)
	}
}

func TestContainerDetailTrimmedName(t *testing.T) {
	d := &ContainerDetail{Name: "/jellyfin"}
	if d.TrimmedName() != "jellyfin" {
		t.Errorf("TrimmedName() = %q", d.TrimmedName())
	}
}
