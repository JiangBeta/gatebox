package handler

import (
	"testing"

	"github.com/JiangBeta/gatebox/internal/adapter/docker/client"
)

func TestVolumeDisplayName(t *testing.T) {
	tests := []struct {
		name string
		vol  *client.Volume
		want string
	}{
		{
			"compose 卷剥离 project 前缀",
			&client.Volume{Name: "myapp_postgres_data", Labels: map[string]string{client.LabelComposeProject: "myapp"}},
			"postgres_data",
		},
		{
			"compose 卷前缀不匹配则原样",
			&client.Volume{Name: "other_pg", Labels: map[string]string{client.LabelComposeProject: "myapp"}},
			"other_pg",
		},
		{
			"自定义命名卷无 label 原样显示自定义名",
			&client.Volume{Name: "my-data", Labels: nil},
			"my-data",
		},
		{
			"匿名卷(64 位 hex)显示匿名卷",
			&client.Volume{Name: "8784becbde19c58ce3283e7f93fa53bf30f2f9e799437b9a82f392c83bffbc68", Labels: nil},
			"匿名卷",
		},
		{
			"非 64 位 hex 当作自定义名",
			&client.Volume{Name: "abc123", Labels: nil},
			"abc123",
		},
	}
	for _, tt := range tests {
		if got := volumeDisplayName(tt.vol); got != tt.want {
			t.Errorf("%s: volumeDisplayName = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestIsHexHash(t *testing.T) {
	if !isHexHash("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", 64) {
		t.Error("64 位 hex 应判定为 true")
	}
	if isHexHash("8784becbde19", 64) {
		t.Error("长度不足应为 false")
	}
	if isHexHash("zzzzbecbde19c58ce3283e7f93fa53bf30f2f9e799437b9a82f392c83bffbc68", 64) {
		t.Error("含非 hex 字符应为 false")
	}
}
