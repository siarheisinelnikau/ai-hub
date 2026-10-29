package auth

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestLoginValid(t *testing.T) {
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokFile, []byte("secret-123\n"), 0o644); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	s := NewStore(tokFile)
	tok := s.Login("secret-123")
	if tok == "" {
		t.Fatal("Login returned empty token")
	}
	if len(tok) != 32 {
		t.Fatalf("token length = %d, want 32", len(tok))
	}
	if !s.Check(tok) {
		t.Fatal("Check(tok) = false, want true")
	}
}

func TestLoginInvalid(t *testing.T) {
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokFile, []byte("secret-123\n"), 0o644); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	s := NewStore(tokFile)
	if got := s.Login("wrong"); got != "" {
		t.Fatalf("Login(wrong) = %q, want empty", got)
	}
	if s.Check("wrong") {
		t.Fatal("Check(wrong) = true, want false")
	}
}

func TestLoginMissingFileFailsClosed(t *testing.T) {
	tokFile := filepath.Join(t.TempDir(), "token")
	s := NewStore(tokFile)
	for _, token := range []string{"", "dev-token-123", "anything"} {
		if s.Login(token) != "" {
			t.Fatal("missing token file accepted login")
		}
	}
	if _, err := os.Stat(tokFile); !os.IsNotExist(err) {
		t.Fatal("login created a token file")
	}
}

func TestEnsureToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	created, err := EnsureToken(path, "")
	if err != nil || !created {
		t.Fatalf("initialize: %v, %v", created, err)
	}
	b, _ := os.ReadFile(path)
	token := strings.TrimSpace(string(b))
	if len(token) != 32 || token == "dev-token-123" {
		t.Fatal("expected private random token")
	}
	if NewStore(path).Login(token) == "" {
		t.Fatal("new token cannot sign in")
	}
	if created, err = EnsureToken(path, ""); err != nil || created {
		t.Fatal("token was not preserved")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(b) {
		t.Fatal("token changed on restart")
	}
	if err := os.WriteFile(path, []byte("dev-token-123"), 0o600); err != nil {
		t.Fatal(err)
	}
	if NewStore(path).Login("dev-token-123") != "" {
		t.Fatal("legacy dev token still works")
	}
	if created, err = EnsureToken(path, ""); err != nil || !created {
		t.Fatal("legacy dev token was not rotated")
	}
}

func TestConfiguredTokenOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("old-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureToken(path, "dev-token-123"); err == nil {
		t.Fatal("accepted shared dev token")
	}
	s := NewStoreWithToken(path, "new-secret")
	if s.Login("new-secret") == "" || s.Login("old-secret") != "" {
		t.Fatal("configured token not preferred")
	}
	if _, err := EnsureToken(filepath.Join(path, "child"), ""); err == nil {
		t.Fatal("unreadable token file accepted")
	}
}

func TestLogout(t *testing.T) {
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokFile, []byte("secret-123"), 0o644); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	s := NewStore(tokFile)
	tok := s.Login("secret-123")
	if tok == "" {
		t.Fatal("Login returned empty token")
	}
	s.Logout(tok)
	if s.Check(tok) {
		t.Fatal("Check(tok) after Logout = true, want false")
	}
}

func TestSessionTokenFormat(t *testing.T) {
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokFile, []byte("secret-123"), 0o644); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	s := NewStore(tokFile)
	tok := s.Login("secret-123")
	if tok == "" {
		t.Fatal("Login returned empty token")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`).MatchString(tok) {
		t.Fatalf("token %q does not match ^[A-Za-z0-9_-]{32}$", tok)
	}
}

func TestSessionCookie(t *testing.T) {
	c := SessionCookie("abc")
	if c.Name != "aih_session" {
		t.Errorf("Name = %q, want %q", c.Name, "aih_session")
	}
	if c.Value != "abc" {
		t.Errorf("Value = %q, want %q", c.Value, "abc")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want %v", c.SameSite, http.SameSiteLaxMode)
	}
	if c.MaxAge != 2592000 {
		t.Errorf("MaxAge = %d, want 2592000", c.MaxAge)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q", c.Path, "/")
	}
}

func TestCheckEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "token"))
	if s.Check("") {
		t.Fatal("Check(\"\") = true, want false")
	}
}

func TestExpiredSessionsAreRejectedAndPruned(t *testing.T) {
	s := NewStoreWithToken("", "private-test-token")
	tok := s.Login("private-test-token")
	s.sessions[tok] = time.Now().Add(-time.Second)
	if s.Check(tok) || len(s.sessions) != 0 {
		t.Fatal("expired session remains usable")
	}
	s.sessions["old"] = time.Now().Add(-time.Second)
	if s.Login("private-test-token") == "" || len(s.sessions) != 1 {
		t.Fatal("login did not remove expired sessions")
	}
}
