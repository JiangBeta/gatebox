// Package web 内嵌前端构建产物(单二进制交付)。
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
