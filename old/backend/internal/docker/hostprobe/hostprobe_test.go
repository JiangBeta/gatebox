package hostprobe

import (
	"reflect"
	"testing"
)

func TestParseStatLines(t *testing.T) {
	lines := []string{
		"cpu  1000 0 500 9000 0 0 0 0 0 0",
		"cpu0 100 0 50 8000 0 0 0 0 0 0",
		"cpu1 200 0 100 9000 0 0 0 0 0 0",
		"intr 123",
	}
	total, idle, err := parseStatLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint64{8150, 9300}; !reflect.DeepEqual(total, want) {
		t.Errorf("total = %v, want %v", total, want)
	}
	if want := []uint64{8000, 9000}; !reflect.DeepEqual(idle, want) {
		t.Errorf("idle = %v, want %v", idle, want)
	}
}

func TestParseMemInfoLines(t *testing.T) {
	lines := []string{
		"MemTotal:       16253164 kB",
		"MemFree:         8000000 kB",
		"MemAvailable:    8287728 kB",
		"Buffers:          100000 kB",
		"Cached:           900000 kB",
	}
	m, err := parseMemInfoLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 16253164 || m.Avail != 8287728 || m.Cache != 1000000 {
		t.Errorf("meminfo = %+v", m)
	}
}

func TestParseCpuInfoHz(t *testing.T) {
	lines := []string{
		"processor\t: 0",
		"model name\t: Intel(R) ...",
		"cpu MHz\t\t: 2592.000",
	}
	f, err := ParseCpuInfoHz(lines)
	if err != nil {
		t.Fatal(err)
	}
	if f != 2592.000 {
		t.Errorf("hz = %v, want 2592", f)
	}
}
