package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// 本文件覆盖 network / volume / system 三组端点。

// --- 网络 ---

func TestListNetworks_Decode(t *testing.T) {
	const body = `[
	  {"Name":"bridge","Id":"n1","Driver":"bridge","Internal":false,
	   "IPAM":{"Driver":"default","Config":[{"Subnet":"172.17.0.0/16","Gateway":"172.17.0.1"}]}},
	  {"Name":"traefik-net","Id":"n2","Driver":"bridge","Internal":true,"Attachable":true,
	   "IPAM":{"Driver":"default","Config":[{"Subnet":"172.18.0.0/16","Gateway":"172.18.0.1"}]}}
	]`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})

	nets, err := c.ListNetworks(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("网络数 = %d", len(nets))
	}
	if got := nets[0].Subnet(); got != "172.17.0.0/16" {
		t.Errorf("Subnet() = %q", got)
	}
	if got := nets[0].Gateway(); got != "172.17.0.1" {
		t.Errorf("Gateway() = %q", got)
	}
	if !nets[1].Internal || !nets[1].Attachable {
		t.Errorf("第二个网络的开关未正确解析: %+v", nets[1])
	}
}

// TestNetworkSubnet_SkipsIPv6 列表页的 IPv4 列不应显示成 IPv6 地址。
func TestNetworkSubnet_SkipsIPv6(t *testing.T) {
	n := &Network{IPAM: IPAM{Config: []IPAMConfig{
		{Subnet: "fd00::/64", Gateway: "fd00::1"},
		{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1"},
	}}}
	if got := n.Subnet(); got != "10.0.0.0/24" {
		t.Errorf("Subnet() = %q, want IPv4 子网", got)
	}
	if got := n.Gateway(); got != "10.0.0.1" {
		t.Errorf("Gateway() = %q, want IPv4 网关", got)
	}

	// 纯 IPv6 网络应返回空串而非错误的地址
	only6 := &Network{IPAM: IPAM{Config: []IPAMConfig{{Subnet: "fd00::/64"}}}}
	if got := only6.Subnet(); got != "" {
		t.Errorf("纯 IPv6 网络 Subnet() = %q, want 空串", got)
	}
}

// TestCreateNetwork_Options 覆盖 docs §3.4 的两个开关。
func TestCreateNetwork_Options(t *testing.T) {
	var got CreateNetworkOptions
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"Id":"newnet","Warning":""}`))
	})

	id, err := c.CreateNetwork(context.Background(), CreateNetworkOptions{
		Name:       "gatebox-net",
		Driver:     "macvlan",
		Internal:   true,
		Attachable: true,
	})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if id != "newnet" {
		t.Errorf("网络 ID = %q", id)
	}
	if got.Name != "gatebox-net" || got.Driver != "macvlan" {
		t.Errorf("请求体 = %+v", got)
	}
	if !got.Internal || !got.Attachable {
		t.Error("隔离外部访问 / 允许手动附加容器 两个开关未传递")
	}
}

func TestRemoveNetwork_InUse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"network traefik-net has active endpoints"}`))
	})
	err := c.RemoveNetwork(context.Background(), "traefik-net")
	if err == nil {
		t.Fatal("有容器连接的网络应删除失败")
	}
	if !strings.Contains(err.Error(), "active endpoints") {
		t.Errorf("err = %v", err)
	}
}

func TestConnectNetwork(t *testing.T) {
	var got struct {
		Container      string `json:"Container"`
		EndpointConfig *struct {
			Aliases []string `json:"Aliases"`
		} `json:"EndpointConfig"`
	}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	})

	if err := c.ConnectNetwork(context.Background(), "net1", "c1", []string{"web"}); err != nil {
		t.Fatalf("ConnectNetwork: %v", err)
	}
	if got.Container != "c1" {
		t.Errorf("Container = %q", got.Container)
	}
	if got.EndpointConfig == nil || len(got.EndpointConfig.Aliases) != 1 {
		t.Errorf("别名未传递: %+v", got.EndpointConfig)
	}

	// 不传别名时不应发送 EndpointConfig。
	// 必须先清零:json.Decode 对 JSON 中缺失的字段不会重置结构体已有值,
	// 不清零的话这里断言的是上一次请求的残留数据。
	got.EndpointConfig = nil
	if err := c.ConnectNetwork(context.Background(), "net1", "c1", nil); err != nil {
		t.Fatal(err)
	}
	if got.EndpointConfig != nil {
		t.Error("无别名时不应携带 EndpointConfig")
	}
}

// --- 卷 ---

func TestListVolumes_Decode(t *testing.T) {
	const body = `{
	  "Volumes":[
	    {"Name":"appdata","Driver":"local","Mountpoint":"/var/lib/docker/volumes/appdata/_data",
	     "CreatedAt":"2026-08-20T10:00:00Z","UsageData":{"Size":1048576,"RefCount":2},
	     "Labels":{"com.docker.compose.project":"jellyfin"}},
	    {"Name":"orphan","Driver":"local","UsageData":{"Size":2048,"RefCount":0}},
	    {"Name":"unknown","Driver":"local","UsageData":{"Size":-1,"RefCount":-1}}
	  ],
	  "Warnings":["something"]
	}`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})

	vols, warns, err := c.ListVolumes(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(vols) != 3 {
		t.Fatalf("卷数 = %d", len(vols))
	}
	if len(warns) != 1 {
		t.Errorf("warnings = %v", warns)
	}
	if vols[0].ComposeProject() != "jellyfin" {
		t.Errorf("ComposeProject() = %q", vols[0].ComposeProject())
	}

	// InUse 的三态:使用中 / 未使用 / 未统计
	if inUse, known := vols[0].InUse(); !inUse || !known {
		t.Errorf("RefCount=2 应为使用中, got (%v, %v)", inUse, known)
	}
	if inUse, known := vols[1].InUse(); inUse || !known {
		t.Errorf("RefCount=0 应为未使用且已知, got (%v, %v)", inUse, known)
	}
	// -1 是「未统计」而非「未使用」—— 混淆会导致误报孤儿卷、误删数据
	if _, known := vols[2].InUse(); known {
		t.Error("RefCount=-1 应为未知状态,不能当作未使用")
	}
}

func TestVolumeInUse_NilUsageData(t *testing.T) {
	v := &Volume{Name: "x"}
	if _, known := v.InUse(); known {
		t.Error("无 UsageData 时应返回未知")
	}
}

func TestPruneVolumes(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"VolumesDeleted":["a","b"],"SpaceReclaimed":4096}`))
	})
	deleted, reclaimed, err := c.PruneVolumes(context.Background(), nil)
	if err != nil {
		t.Fatalf("PruneVolumes: %v", err)
	}
	if len(deleted) != 2 || reclaimed != 4096 {
		t.Errorf("deleted=%v reclaimed=%d", deleted, reclaimed)
	}
}

func TestRemoveVolume_Force(t *testing.T) {
	var force string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		force = r.URL.Query().Get("force")
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.RemoveVolume(context.Background(), "v1", false); err != nil {
		t.Fatal(err)
	}
	if force != "" {
		t.Errorf("force=false 时不应传参数, got %q", force)
	}
	if err := c.RemoveVolume(context.Background(), "v1", true); err != nil {
		t.Fatal(err)
	}
	if force != "1" {
		t.Errorf("force = %q, want 1", force)
	}
}

// --- 系统 ---

// realInfoJSON 依据本机 docker info 的真实字段构造(NixOS / journald / cgroup v2)。
const realInfoJSON = `{
  "ID":"abc","ServerVersion":"29.6.2",
  "Containers":2,"ContainersRunning":2,"ContainersStopped":0,"Images":3,
  "Driver":"overlayfs","LoggingDriver":"journald",
  "CgroupDriver":"systemd","CgroupVersion":"2",
  "OperatingSystem":"NixOS 25.11","OSType":"linux","Architecture":"x86_64",
  "NCPU":8,"MemTotal":15579537408,
  "RegistryConfig":{"Mirrors":[],"InsecureRegistryCIDRs":["127.0.0.0/8"]},
  "LiveRestoreEnabled":false
}`

func TestSystemInfo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(realInfoJSON))
	})
	info, err := c.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("SystemInfo: %v", err)
	}

	if !info.CgroupV2() {
		t.Error("CgroupV2() 应为 true —— 内存口径会选错分支")
	}
	if !info.LogsReadable() {
		t.Error("journald 驱动应可回读日志")
	}
	if info.LiveRestoreEnabled {
		t.Error("LiveRestoreEnabled 解析错误")
	}
	if info.RegistryConfig == nil {
		t.Fatal("RegistryConfig 未解析 —— daemon 不可写时无法只读展示镜像加速器")
	}
	if len(info.RegistryConfig.InsecureRegistryCIDRs) != 1 {
		t.Errorf("InsecureRegistryCIDRs = %v", info.RegistryConfig.InsecureRegistryCIDRs)
	}
}

// TestInfoImagePlatform 覆盖架构名转换:docker info 用 uname 风格,
// 镜像 manifest 用 OCI 风格,直接拼接会拉不到镜像。
func TestInfoImagePlatform(t *testing.T) {
	info := &Info{OSType: "linux", Architecture: "x86_64"}
	if got := info.ImagePlatform(); got != "linux/amd64" {
		t.Errorf("ImagePlatform() = %q, want linux/amd64", got)
	}

	arm := &Info{OSType: "linux", Architecture: "aarch64"}
	if got := arm.ImagePlatform(); got != "linux/arm64" {
		t.Errorf("ImagePlatform() = %q, want linux/arm64", got)
	}
}

func TestNormalizeArch(t *testing.T) {
	cases := map[string]string{
		"x86_64":  "amd64",
		"amd64":   "amd64",
		"aarch64": "arm64",
		"arm64":   "arm64",
		"armv7l":  "arm",
		"armv6l":  "arm",
		"i686":    "386",
		"riscv64": "riscv64",
		"mips64":  "mips64", // 未知架构原样返回
	}
	for in, want := range cases {
		if got := NormalizeArch(in); got != want {
			t.Errorf("NormalizeArch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLogDriverReadable(t *testing.T) {
	readable := []string{"json-file", "local", "journald", ""}
	for _, d := range readable {
		if !LogDriverReadable(d) {
			t.Errorf("驱动 %q 应可回读", d)
		}
	}
	unreadable := []string{"syslog", "fluentd", "gelf", "awslogs", "splunk"}
	for _, d := range unreadable {
		if LogDriverReadable(d) {
			t.Errorf("驱动 %q 不支持回读,应返回 false 以便 UI 友好提示", d)
		}
	}
}

func TestSystemVersion(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Version":"29.6.2","ApiVersion":"1.55","MinAPIVersion":"1.40","Arch":"amd64","Os":"linux"}`))
	})
	v, err := c.SystemVersion(context.Background())
	if err != nil {
		t.Fatalf("SystemVersion: %v", err)
	}
	if v.APIVersion != "1.55" || v.MinAPIVersion != "1.40" {
		t.Errorf("版本 = %+v", v)
	}
	// 客户端锁定的版本必须不低于 daemon 的最低支持版本
	if v.MinAPIVersion > APIVersion[1:] {
		t.Errorf("客户端 API 版本 %s 低于 daemon 最低支持 %s", APIVersion, v.MinAPIVersion)
	}
}

func TestEventAt(t *testing.T) {
	e := &Event{Time: 1755000000, TimeNano: 1755000000123456789}
	if e.At().UnixNano() != 1755000000123456789 {
		t.Errorf("At() 应优先使用纳秒精度, got %v", e.At())
	}
	onlySec := &Event{Time: 1755000000}
	if onlySec.At().Unix() != 1755000000 {
		t.Errorf("At() = %v", onlySec.At())
	}
}
