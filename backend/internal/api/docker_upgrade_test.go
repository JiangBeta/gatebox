package api

import (
	"testing"
)

func TestRegistryHost(t *testing.T) {
	cases := []struct {
		ref  string
		want string
	}{
		{"nginx:latest", ""},
		{"nginx", ""},
		{"library/nginx:1.25", ""},
		{"registry.example.com/team/app:1.0", "registry.example.com"},
		{"localhost:5000/app:1.0", "localhost:5000"},
		{"ghcr.io/org/repo:v2", "ghcr.io"},
	}
	for _, c := range cases {
		if got := registryHost(c.ref); got != c.want {
			t.Errorf("registryHost(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
}
