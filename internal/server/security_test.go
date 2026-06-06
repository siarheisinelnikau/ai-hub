package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPICredentialsAndBrowserCSRF(t *testing.T) {
	s, secret := testServer(t)
	s.d.RequireAPIToken = true
	handler := s.Handler()
	for _, path := range []string{"/v1/models", "/v1/chat/completions", "/metrics"} {
		for _, credential := range []string{"", "Bearer wrong", "Basic " + secret} {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("Authorization", credential)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s accepted invalid credentials: %d", path, w.Code)
			}
		}
	}
	for _, session := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		if session {
			r.AddCookie(&http.Cookie{Name: "aih_session", Value: s.d.Store.Login(secret)})
		} else {
			r.Header.Set("Authorization", "Bearer "+secret)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("authenticated model list: %d", w.Code)
		}
	}
	for _, path := range []string{"/api/login", "/api/stop-all", "/v1/chat/completions"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"token":"secret"}`))
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Origin", "https://attacker.example")
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("cross-origin action %s: %d", path, w.Code)
		}
	}
}

func TestForwardedHeadersCannotBypassLoginLimit(t *testing.T) {
	s, _ := testServer(t)
	handler := s.Handler()
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"token":"wrong"}`))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("X-Forwarded-For", strings.Repeat("1", i+1))
		r.Header.Set("X-Real-IP", strings.Repeat("2", i+1))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if i == 5 && w.Code != http.StatusTooManyRequests {
			t.Fatalf("spoofed header bypassed limiter: %d", w.Code)
		}
	}
}

func TestSecureSessionCookieAndLogout(t *testing.T) {
	s, _ := testServer(t)
	s.d.SecureCookies = true
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"token":"secret"}`)))
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("missing secure session cookie: %v", cookies)
	}
	r := httptest.NewRequest(http.MethodDelete, "/api/login", nil)
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK || s.d.Store.Check(cookies[0].Value) || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not revoke and expire the session")
	}
}
