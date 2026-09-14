// Package hostprobe 提供宿主机资源瞬态探针(/proc,零外部依赖)。
//
// 服务容器表格 hover 面板:每核 CPU 使用率、CPU 频率、内存
// Total/Used/Avail/Cache。全部来自 Linux /proc 伪文件系统——
// 不用 dmidecode(需 root 且非标输出)、不用 gopsutil(彼可自行封装)。
package hostprobe

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// Host 一次宿主机资源快照。
type Host struct {
	NumCores     int       `json:"numCores"`     // 逻辑核数
	CorePercents []float64 `json:"corePercents"` // 每核使用率 0-100,下标=核序号(cpu0..)
	CPUFreqGHz   float64   `json:"cpuFreqGHz"`   // 当前频率(首核近似)
	MemTotalGiB  float64   `json:"memTotalGiB"`
	MemUsedGiB   float64   `json:"memUsedGiB"`
	MemAvailGiB  float64   `json:"memAvailGiB"`
	MemCacheGiB  float64   `json:"memCacheGiB"`
}

// SampleInterval 两次 /proc/stat 采样间隔,用于计算每核 delta 占用率。
const SampleInterval = 120 * time.Millisecond

// Probe 采样宿主机资源。任一环节读 /proc 失败返回 error(调用方降级为空 Host)。
func Probe() (Host, error) {
	h := Host{}

	// 每核使用率:两次 /proc/stat 差分
	t0, tIdle0, err := readProcStat()
	if err != nil {
		return h, err
	}
	time.Sleep(SampleInterval)
	t1, tIdle1, err := readProcStat()
	if err != nil {
		return h, err
	}
	h.NumCores = len(t0)
	if h.NumCores > 0 {
		h.CorePercents = make([]float64, h.NumCores)
		for i := 0; i < h.NumCores; i++ {
			dt := t1[i] - t0[i]
			di := tIdle1[i] - tIdle0[i]
			if dt > 0 {
				util := 1 - float64(di)/float64(dt)
				if util < 0 {
					util = 0
				}
				if util > 1 {
					util = 1
				}
				h.CorePercents[i] = util * 100
			}
		}
	}

	if mhz, err := readCPUFreqMHz(); err == nil && mhz > 0 {
		h.CPUFreqGHz = mhz / 1000
	}

	if mem, err := readMemInfo(); err == nil {
		h.MemTotalGiB = mem.total
		h.MemAvailGiB = mem.avail
		h.MemUsedGiB = mem.total - mem.avail
		h.MemCacheGiB = mem.cache
	}
	return h, nil
}

type memInfo struct{ total, avail, cache float64 } // GiB

// readProcStat 读 /proc/stat 汇总 cpu 总行之外的每核 (cpu0..) 时间累计。
// 返回每核 total 与 idle(idle+iowait) 序列;解析失败返回 error。
func readProcStat() (total, idle []uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu") || strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		// fields[0]="cpu7",其后依次 user nice system idle iowait irq softirq steal ...
		if len(fields) <= 4 {
			continue
		}
		var t, id uint64
		for _, fld := range fields[1:] {
			n, e := strconv.ParseUint(fld, 10, 64)
			if e == nil {
				t += n
			}
		}
		for _, idx := range []int{4, 5} { // idle, iowait
			if idx < len(fields) {
				if n, e := strconv.ParseUint(fields[idx], 10, 64); e == nil {
					id += n
				}
			}
		}
		total = append(total, t)
		idle = append(idle, id)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	if len(total) == 0 {
		return nil, nil, os.ErrInvalid
	}
	return total, idle, nil
}

// readCPUFreqMHz 读 /proc/cpuinfo 首个 "cpu MHz" 当前频率(兆赫)。
func readCPUFreqMHz() (float64, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return ParseCpuInfoHz(lines)
}

// readMemInfo 读 /proc/meminfo 的 Total/Available/Cache,单位转换为 GiB。
func readMemInfo() (memInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return memInfo{}, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return memInfo{}, err
	}
	m, err := parseMemInfoLines(lines)
	if err != nil {
		return memInfo{}, err
	}
	return memInfo{
		total: float64(m.Total) / 1048576, // kB → GiB
		avail: float64(m.Avail) / 1048576,
		cache: float64(m.Cache) / 1048576,
	}, nil
}

// ParseStatLines / ParseMemInfoLines / ParseCpuInfoHz 供测试解析纯文本。
// parseStatLines 读 /proc/stat 每核 (cpu0..) 的 total 与 idle(idle+iowait)。
func ParseStatLines(lines []string) (total, idle []uint64, err error) { return parseStatLines(lines) }

func parseStatLines(lines []string) (total, idle []uint64, err error) {
	for _, line := range lines {
		if !strings.HasPrefix(line, "cpu") || strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) <= 4 {
			continue
		}
		var t, id uint64
		for _, fld := range fields[1:] {
			if n, e := strconv.ParseUint(fld, 10, 64); e == nil {
				t += n
			}
		}
		for _, idx := range []int{4, 5} {
			if idx < len(fields) {
				if n, e := strconv.ParseUint(fields[idx], 10, 64); e == nil {
					id += n
				}
			}
		}
		total = append(total, t)
		idle = append(idle, id)
	}
	if len(total) == 0 {
		return nil, nil, os.ErrInvalid
	}
	return total, idle, nil
}

// memInfoTest 内存取值(kB,供测试验证)。
type memInfoTest struct{ Total, Avail, Cache uint64 }

func ParseMemInfoLines(lines []string) (memInfoTest, error) { return parseMemInfoLines(lines) }

func parseMemInfoLines(lines []string) (memInfoTest, error) {
	kb := map[string]uint64{}
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) == 0 {
			continue
		}
		if n, e := strconv.ParseUint(fields[0], 10, 64); e == nil {
			kb[strings.TrimSpace(parts[0])] = n
		}
	}
	var m memInfoTest
	if v, ok := kb["MemTotal"]; ok {
		m.Total = v
	}
	if v, ok := kb["MemAvailable"]; ok {
		m.Avail = v
	}
	m.Cache = kb["Buffers"] + kb["Cached"]
	return m, nil
}

func ParseCpuInfoHz(lines []string) (float64, error) {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "cpu MHz") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		return strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	}
	return 0, os.ErrInvalid
}
