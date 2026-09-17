package component

import "testing"

func TestCoreDescriptorsHaveFunctionsAndContracts(t *testing.T) {
	reg := NewCoreRegistry(t.TempDir(), "/tmp/caddy", "http://127.0.0.1:2019", nil)
	descs := reg.Descriptors()
	if len(descs) != 3 {
		t.Fatalf("want 3 core descriptors, got %d", len(descs))
	}
	byID := map[string]Descriptor{}
	for _, d := range descs {
		byID[d.ID] = d
	}
	for _, id := range []string{"caddy", "acme", "docker"} {
		d, ok := byID[id]
		if !ok {
			t.Fatalf("missing descriptor %q", id)
		}
		if len(d.Functions) == 0 {
			t.Errorf("%s: functions empty", id)
		}
		if !d.Observable.State {
			t.Errorf("%s: observable.state should be true", id)
		}
	}
	if len(byID["acme"].Produces) == 0 || byID["acme"].Produces[0].Info != InfoCert {
		t.Errorf("acme should produce cert, got %+v", byID["acme"].Produces)
	}
	if len(byID["docker"].Produces) == 0 {
		t.Errorf("docker should produce label/service")
	}
}

func TestValidEffect(t *testing.T) {
	if !ValidEffect(EffectHotReload) || !ValidEffect(EffectRestart) {
		t.Fatal("known effect rejected")
	}
	if ValidEffect("nope") {
		t.Fatal("unknown effect accepted")
	}
}
