package handler

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/adapter/caddy"
	"github.com/JiangBeta/gatebox/internal/adapter/docker/client"
	"github.com/JiangBeta/gatebox/internal/reconcile"
)

// SystemInfo 顶栏「升级 / 重启」两个动作要展示的控制面自身信息。
//
// 刻意**不含** latestVersion / upgradeNotes：升级通道（ADR-043 §6）本轮不开放，
// 没有可信的版本源就不编一个数字出来——那会让「有新版本」的角标变成永远为假的
// 装饰。前端拿不到 latestVersion 就显示「升级通道尚未开放」。
type SystemInfo struct {
	// Version 控制面版本（构建期 -ldflags 注入）。
	Version string `json:"version"`
	// CaddyVersion 网关运行时版本（读二进制；缺失时为空串，前端显示「未安装」）。
	CaddyVersion string `json:"caddyVersion,omitempty"`
	// Online 控制面 API 可达（恒为 true——能返回这个 JSON 就说明可达）。
	Online bool `json:"online"`
	// Containers 本机容器数（daemon 不可达时为 null，前端显示「不可达」）。
	Containers *ContainerCount `json:"containers"`
	// Tasks 运行中调和任务数（顶栏任务角标的另一路数据源）。
	Tasks TaskCount `json:"tasks"`
	// Restart 重启行为说明（原型重启确认框的文案来源）。
	Restart RestartInfo `json:"restart"`
}

// ContainerCount 容器计数。
type ContainerCount struct {
	Total   int `json:"total"`
	Running int `json:"running"`
}

// TaskCount 调和任务计数。
type TaskCount struct {
	Running int `json:"running"`
}

// RestartInfo 重启语义。Graceful=true 表示新版先停插件 sidecar 再退出，
// 容器由各主机 Agent 继续运行，不随控制面停。
type RestartInfo struct {
	Graceful bool   `json:"graceful"`
	Estimate string `json:"estimate"`
}

// RegisterSystem 注册 GET /api/v1/system（顶栏控制面自身信息）。
//
// 顶栏三键（任务 / 升级 / 重启）里，任务走 /runs，这里补另两键需要的版本与
// 重启语义；容器与任务计数一并给出，好让确认框里的数字是真的而不是编的。
func RegisterSystem(mux *http.ServeMux, version, caddyBin string, dc *client.Client, runs reconcile.RunStore) {
	mux.HandleFunc("GET /api/v1/system", func(w http.ResponseWriter, r *http.Request) {
		info := SystemInfo{
			Version: version,
			Online:  true,
			Restart: RestartInfo{Graceful: true, Estimate: "约 3 秒"},
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if v, err := caddy.BinaryVersion(ctx, caddyBin); err == nil && v != "" {
			info.CaddyVersion = v
		}
		if dc != nil {
			if cs, err := dc.ListContainers(ctx, client.ListContainersOptions{All: true}); err == nil {
				cc := &ContainerCount{Total: len(cs)}
				for _, c := range cs {
					if strings.EqualFold(c.State, "running") {
						cc.Running++
					}
				}
				info.Containers = cc
			}
		}
		if runs != nil {
			info.Tasks = TaskCount{Running: countRunning(runs)}
		}
		writeJSON(w, http.StatusOK, info)
	})
}

// countRunning 数运行中的 Run（顶栏任务角标）。
func countRunning(runs reconcile.RunStore) int {
	list, err := runs.List(50)
	if err != nil {
		return 0
	}
	n := 0
	for _, run := range list {
		if run.State == "running" {
			n++
		}
	}
	return n
}

// LocalIP 取本机第一个非回环 IPv4。
//
// 给 host 对象的 edge 自举填地址用：model/host.yaml 要求「edge 由系统自动创建
// （默认填好本机 IP）」。取不到就留空——地址是必填字段但可以由用户补，
// 硬编 127.0.0.1 反而会让「按主机地址生成后端」的推导算错。
func LocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if v4 := n.IP.To4(); v4 != nil && !v4.IsLoopback() {
					return v4.String()
				}
			}
		}
	}
	return ""
}
