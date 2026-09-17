package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/model"
)

func TestProviderForCompilesConfigSync(t *testing.T) {
	target := filepath.Join(t.TempDir(), "ddns.conf")
	man := Manifest{
		APIVersion: "gatebox/v2", Kind: "config-only", ID: "ddns-go", Name: "ddns-go",
		Contributions: Contributions{
			Backend: []BackendContribution{{
				Point:  extension.PointConfigSync,
				Target: target,
				Impl: map[string]string{
					"type":     "template",
					"template": "{{ range .Domains }}{{ .Name }}\n{{ end }}",
				},
			}},
		},
	}
	p, err := providerFor(man)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ConfigSyncs) != 1 {
		t.Fatalf("want 1 config-sync, got %d", len(p.ConfigSyncs))
	}
	in := extension.ProjectionInput{Domains: []model.Domain{{ID: "1", Name: "a.cn"}, {ID: "2", Name: "b.cn"}}}
	if err := p.ConfigSyncs[0].SyncProjection(in); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a.cn\nb.cn\n" {
		t.Fatalf("rendered=%q", string(got))
	}
}

func TestProviderForSkipsNonTemplateConfigSync(t *testing.T) {
	man := Manifest{
		APIVersion: "gatebox/v2", Kind: "config-only", ID: "x",
		Contributions: Contributions{
			Backend: []BackendContribution{{Point: extension.PointConfigSync, Impl: map[string]string{"type": "sidecar"}}},
		},
	}
	p, err := providerFor(man)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ConfigSyncs) != 0 {
		t.Fatalf("non-template config-sync should be skipped")
	}
}
