package handler

import (
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/plugin"
)

func TestHasAPIScope(t *testing.T) {
	perms := []plugin.Permission{
		{API: []string{"domain:read", "credentials:read"}},
	}
	if !hasAPIScope(perms, "domain:read") {
		t.Error("应命中 domain:read")
	}
	if hasAPIScope(perms, "gateway:write") {
		t.Error("不应命中 gateway:write")
	}
	if !hasAPIScope([]plugin.Permission{{API: []string{"*"}}}, "anything") {
		t.Error("通配 scope 应命中")
	}
}

func TestParseWait(t *testing.T) {
	if parseWait("") != 0 || parseWait("abc") != 0 || parseWait("-1") != 0 {
		t.Error("非法 wait 应为 0")
	}
	if parseWait("5") != 5*time.Second {
		t.Error("wait=5 应为 5s")
	}
	if parseWait("100") != 30*time.Second {
		t.Error("wait 应封顶 30s")
	}
}
