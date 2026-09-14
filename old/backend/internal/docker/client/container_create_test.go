package client

import (
	"reflect"
	"testing"
)

func TestCreateBodyFromDetail(t *testing.T) {
	det := &ContainerDetail{
		ID:   "abc123",
		Name: "/myapp",
		Config: ContainerConfig{
			Hostname:   "myhost",
			WorkingDir: "/work",
			User:       "1000:1000",
			Env:        []string{"A=1", "B=2"},
			Cmd:        []string{"serve"},
			Image:      "nginx:1.25",
			Labels:     map[string]string{"x": "y"},
			Entrypoint: []string{"/entry.sh"},
			Tty:        true,
			OpenStdin:  true,
		},
		HostConfig: &HostConfig{
			NetworkMode: "bridge",
			RestartPolicy: struct {
				Name              string `json:"Name"`
				MaximumRetryCount int    `json:"MaximumRetryCount"`
			}{Name: "unless-stopped", MaximumRetryCount: 3},
			Binds: []string{"/data:/data"},
			PortBindings: map[string][]PortBinding{
				"80/tcp": {{HostIP: "0.0.0.0", HostPort: "8080"}},
			},
			Privileged: true,
			LogConfig: struct {
				Type   string            `json:"Type"`
				Config map[string]string `json:"Config"`
			}{Type: "json-file"},
		},
		NetworkSettings: &struct {
			Networks map[string]EndpointSettings `json:"Networks"`
		}{Networks: map[string]EndpointSettings{
			"caddy": {Aliases: []string{"myapp"}},
		}},
	}

	body := createBodyFromDetail(det, "nginx:1.26")

	if body.Image != "nginx:1.26" {
		t.Errorf("Image: got %q want nginx:1.26", body.Image)
	}
	if body.Hostname != "myhost" || body.WorkingDir != "/work" || body.User != "1000:1000" {
		t.Errorf("Config fields not copied: %+v", body)
	}
	if !reflect.DeepEqual(body.Env, []string{"A=1", "B=2"}) {
		t.Errorf("Env: got %v", body.Env)
	}
	if body.HostConfig == nil {
		t.Fatal("HostConfig is nil")
	}
	if body.HostConfig.NetworkMode != "bridge" || body.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Errorf("HostConfig mismatch: %+v", body.HostConfig)
	}
	if !body.HostConfig.Privileged {
		t.Error("Privileged not copied")
	}
	if body.HostConfig.LogConfig == nil || body.HostConfig.LogConfig.Type != "json-file" {
		t.Error("LogConfig not copied")
	}
	if body.NetworkingConfig == nil {
		t.Fatal("NetworkingConfig is nil")
	}
	ep, ok := body.NetworkingConfig.EndpointsConfig["caddy"]
	if !ok || !reflect.DeepEqual(ep.Aliases, []string{"myapp"}) {
		t.Errorf("network endpoint aliases mismatch: %+v", body.NetworkingConfig.EndpointsConfig)
	}
}
