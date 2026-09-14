// Package adapter 提供外部系统适配：caddy / docker / acme / ddns / mosdns / systemd。
//
// 每个外部系统一个文件或子包，实现 component 的能力接口（ADR-028）。
package adapter
