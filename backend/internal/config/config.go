// Package config 提供运行配置的加载与默认值。
package config

import "os"

// Config 运行配置
type Config struct {
	DataDir      string // 运行时数据根目录
	Addr         string // HTTP 监听地址
	CaddyAdmin   string // Caddy Admin API 地址
	DockerSocket string // Docker 守护进程 unix socket
}

// Load 从环境变量加载配置,未设置时取默认值。
func Load() Config {
	return Config{
		DataDir:      getenv("GATEBOX_DATA_DIR", "./data"),
		Addr:         getenv("GATEBOX_ADDR", "0.0.0.0:8080"),
		CaddyAdmin:   getenv("GATEBOX_CADDY_ADMIN", "http://localhost:2019"),
		DockerSocket: getenv("GATEBOX_DOCKER_SOCKET", "/var/run/docker.sock"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
