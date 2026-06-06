package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"aihub/internal/auth"
)

func TestHTTPTokenAndOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	store := auth.NewStoreWithToken("", "test-mcp-token")
	handler := protectHTTP(next, store, []string{"https://hub.example"})
	for _, tc := range []struct {
		path, token, origin string
		code                int
	}{
		{"/health", "", "", 200},
		{"/mcp", "", "", 401},
		{"/mcp", "wrong", "", 401},
		{"/mcp", "test-mcp-token", "", 200},
		{"/mcp", "test-mcp-token", "https://hub.example", 200},
		{"/mcp", "test-mcp-token", "https://attacker.example", 403},
	} {
		r := httptest.NewRequest(http.MethodPost, tc.path, nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s origin=%s: %d, want %d", tc.path, tc.origin, w.Code, tc.code)
		}
	}
	if len(parseCorsOrigins("")) != 0 {
		t.Fatal("empty CORS policy must deny browser origins")
	}
}
