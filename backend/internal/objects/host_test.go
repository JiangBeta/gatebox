package objects

import (
	"errors"
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/typespec"
)

// TestEnsureEdgeCreatesWithLocalIP 守住自举的核心约定：
// 首启没有 host 对象时要建一个 role=edge 的本机主机，且地址用探测到的 IP。
func TestEnsureEdgeCreatesWithLocalIP(t *testing.T) {
	svc, _ := newTestService(t)
	o, err := svc.EnsureEdge(EdgeOptions{
		Address: "192.168.1.10",
		Online:  true,
		Now:     time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("EnsureEdge: %v", err)
	}
	if o.ID != EdgeHostID {
		t.Errorf("id = %q, 想要 %q", o.ID, EdgeHostID)
	}
	if got := o.Spec["role"]; got != "edge" {
		t.Errorf("role = %v, 想要 edge", got)
	}
	if got := o.Spec["address"]; got != "192.168.1.10" {
		t.Errorf("address = %v, 想要 192.168.1.10", got)
	}
	if got := o.Spec["dockerEndpoint"]; got != EdgeDockerEndpoint {
		t.Errorf("dockerEndpoint = %v, 想要 %s", got, EdgeDockerEndpoint)
	}
	if o.Status["online"] != true {
		t.Errorf("status.online = %v, 想要 true", o.Status["online"])
	}
	if o.Status["lastSeen"] != "2026-09-22T10:00:00Z" {
		t.Errorf("status.lastSeen = %v", o.Status["lastSeen"])
	}
}

// TestEnsureEdgeIdempotent 幂等：重复调用不产生第二个 host，也不覆盖用户改过的地址。
func TestEnsureEdgeIdempotent(t *testing.T) {
	svc, _ := newTestService(t)
	first, err := svc.EnsureEdge(EdgeOptions{Address: "10.0.0.1", Online: true})
	if err != nil {
		t.Fatalf("首次 EnsureEdge: %v", err)
	}
	// 用户手动改地址（model/host.yaml 说 address 手填）。
	if _, err := svc.Update(typespec.KindHost, EdgeHostID, map[string]any{
		"role": "edge", "host": EdgeHostID, "address": "10.0.0.9",
	}); err != nil {
		t.Fatalf("用户改地址: %v", err)
	}
	// 心跳刷新：探测到的 IP 不该把用户改的值冲掉。
	second, err := svc.EnsureEdge(EdgeOptions{Address: "10.0.0.1", Online: false})
	if err != nil {
		t.Fatalf("二次 EnsureEdge: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("二次调用 id 变了: %q → %q", first.ID, second.ID)
	}
	if got := second.Spec["address"]; got != "10.0.0.9" {
		t.Errorf("address = %v, 心跳不应覆盖用户填的 10.0.0.9", got)
	}
	if second.Status["online"] != false {
		t.Errorf("status.online = %v, 离线时刷新应写 false", second.Status["online"])
	}
	list, err := svc.List(typespec.KindHost)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("host 对象数 = %d, 想要 1（全局唯一 edge）", len(list))
	}
}

// TestEnsureEdgeOfflineStillExists 离线也要有对象：
// 页面要显示「主机离线」横幅，对象不存在就没东西可显示。
func TestEnsureEdgeOfflineStillExists(t *testing.T) {
	svc, _ := newTestService(t)
	o, err := svc.EnsureEdge(EdgeOptions{Address: "127.0.0.1", Online: false})
	if err != nil {
		t.Fatalf("EnsureEdge: %v", err)
	}
	if o.Status["online"] != false {
		t.Errorf("status.online = %v, 想要 false", o.Status["online"])
	}
}

// TestEnsureEdgeFillsEndpointDefault 首次创建时补默认 dockerEndpoint，缺省地址回落 127.0.0.1。
func TestEnsureEdgeFillsEndpointDefault(t *testing.T) {
	svc, _ := newTestService(t)
	o, err := svc.EnsureEdge(EdgeOptions{})
	if err != nil {
		t.Fatalf("EnsureEdge: %v", err)
	}
	if o.Spec["dockerEndpoint"] != EdgeDockerEndpoint {
		t.Errorf("dockerEndpoint = %v, 想要默认 %s", o.Spec["dockerEndpoint"], EdgeDockerEndpoint)
	}
	if o.Spec["address"] != EdgeHostAddress {
		t.Errorf("address = %v, 想要回退 %s", o.Spec["address"], EdgeHostAddress)
	}
}

// TestEdgeCannotBeDeletedOrDemoted 守住「edge 全局唯一且由系统创建」：
// 删掉或降级它会让后端地址推导链断在最后一环。
func TestEdgeCannotBeDeletedOrDemoted(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.EnsureEdge(EdgeOptions{Address: "10.0.0.1", Online: true}); err != nil {
		t.Fatalf("EnsureEdge: %v", err)
	}
	if _, err := svc.Delete(typespec.KindHost, EdgeHostID, true); !errors.Is(err, ErrEdgeLocked) {
		t.Errorf("删除 edge 的错误 = %v, 想要 ErrEdgeLocked", err)
	}
	// 改地址 / 备注应该允许（用户填的字段）。
	if _, err := svc.Update(typespec.KindHost, EdgeHostID, map[string]any{
		"role": "edge", "host": EdgeHostID, "address": "10.0.0.5", "remark": "换了网卡",
	}); err != nil {
		t.Errorf("改 edge 地址应允许: %v", err)
	}
	// 降级成 worker 不行。
	_, err := svc.Update(typespec.KindHost, EdgeHostID, map[string]any{
		"role": "worker", "host": EdgeHostID, "address": "10.0.0.5",
	})
	if !errors.Is(err, ErrEdgeLocked) {
		t.Errorf("edge 改 worker 的错误 = %v, 想要 ErrEdgeLocked", err)
	}
}

func TestIsNotFoundMatchesSentinel(t *testing.T) {
	if !isNotFound(ErrNotFound) {
		t.Error("isNotFound(ErrNotFound) = false")
	}
	if isNotFound(nil) {
		t.Error("isNotFound(nil) = true")
	}
	if isNotFound(errors.New("boom")) {
		t.Error("isNotFound(其他错误) = true")
	}
}
