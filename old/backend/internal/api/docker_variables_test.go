package api

import (
	"reflect"
	"testing"
)

func TestComposePublishedPorts(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want map[string]string
	}{
		{
			"多服务保序编号,只统计发布端口",
			`services:
  web:
    image: nginx
    ports:
      - "8080:80"
      - "8443:443/tcp"
  db:
    image: postgres
    ports:
      - "5432:5432"
  cron:
    image: cron
    ports:
      - "80"
  api:
    image: app
    ports:
      - target: 9000
        published: 9001
      - target: 9002
`,
			map[string]string{
				"GB_SER_1_PORT_1": "8080",
				"GB_SER_1_PORT_2": "8443",
				"GB_SER_2_PORT_1": "5432",
				"GB_SER_4_PORT_1": "9001",
			},
		},
		{
			"带 IP 的短形式取倒数第二段",
			"services:\n  a:\n    ports:\n      - \"127.0.0.1:8080:80\"\n",
			map[string]string{"GB_SER_1_PORT_1": "8080"},
		},
		{
			"无 services / 无端口不产生变量",
			"version: '3'\n",
			map[string]string{},
		},
		{
			"空字符串不崩溃",
			"",
			map[string]string{},
		},
	}
	for _, tt := range tests {
		if got := composePublishedPorts(tt.yaml); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: composePublishedPorts = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPublishedPortFromString(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"8080:80", "8080"},
		{"127.0.0.1:8080:80", "8080"},
		{"8080:80/tcp", "8080"},
		{"80", ""}, // 纯容器端口
		{"", ""},
	}
	for _, tt := range tests {
		if got := publishedPortFromString(tt.in); got != tt.want {
			t.Errorf("publishedPortFromString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
