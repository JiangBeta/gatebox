package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JiangBeta/gatebox/internal/component"
)

func TestDescriptorsEndpoint(t *testing.T) {
	reg := component.NewCoreRegistry(t.TempDir(), "/tmp/caddy", "http://127.0.0.1:2019", nil)
	mux := http.NewServeMux()
	RegisterDescriptors(mux, reg, nil)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/descriptors", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var items []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 descriptors, got %d", len(items))
	}
	first := items[0]
	for _, k := range []string{"id", "name", "kind", "functions", "observable"} {
		if _, ok := first[k]; !ok {
			t.Errorf("descriptor missing key %q: %v", k, first)
		}
	}
}

func TestSchemaEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	RegisterSchema(mux, nil)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/schema/service", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var got struct {
		FactKind string           `json:"factKind"`
		UIHint   string           `json:"uiHint"`
		Fields   []map[string]any `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.FactKind != "service" || got.UIHint != "gateway-service" || len(got.Fields) == 0 {
		t.Fatalf("unexpected schema: %+v", got)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/schema/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown factKind should 404, got %d", rec.Code)
	}
}
