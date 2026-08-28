package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aihub/internal/buildinfo"
)

func TestHealthBuildMetadata(t *testing.T) {
	response := httptest.NewRecorder()
	New().Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	var body struct {
		OK      bool   `json:"ok"`
		Service string `json:"service"`
		buildinfo.Info
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !body.OK || body.Service != "ai-hub-mcp" || body.Version != buildinfo.Version || body.Revision == "" {
		t.Fatalf("unexpected health response: %s", response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("health metadata must not be cached across upgrades")
	}
}
