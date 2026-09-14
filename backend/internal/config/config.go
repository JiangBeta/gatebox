// Package config 提供运行配置的加载与默认值。
//
// P0 仅保留最小骨架：addr 与 data_dir；完整配置（三源加载 env > conf > 默认）在 P1 补齐。
package config

import "os"

// Config 运行配置。
type Config struct {
	Addr    string // HTTP 监听地址
	DataDir string // 运行时数据根目录
}

// Load 从环境变量加载配置（缺省值兜底）。
func Load() Config {
	return Config{
		Addr:    getenv("GATEBOX_ADDR", "0.0.0.0:8080"),
		DataDir: getenv("GATEBOX_DATA_DIR", "./data"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
