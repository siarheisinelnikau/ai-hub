// Package auth mirrors app.py's token-file login and in-memory sessions.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// CookieName mirrors app.py's session cookie.
	CookieName = "aih_session"
	// cookieMaxAge = 30 days (app.py: max_age=30*24*60*60).
	CookieMaxAge = 30 * 24 * 60 * 60
)

// Store is the in-memory session set + token-file resolver (app.py `sessions`).
type Store struct {
	mu         sync.Mutex
	sessions   map[string]time.Time
	tokenFile  string
	configured string
}

// NewStore creates a session store backed by tokenFile (AIHUB/token).
func NewStore(tokenFile string) *Store {
	return &Store{sessions: map[string]time.Time{}, tokenFile: tokenFile}
}

func NewStoreWithToken(tokenFile, token string) *Store {
	s := NewStore(tokenFile)
	s.configured = token
	return s
}

// EnsureToken bootstraps a private random token before accepting connections.
// Existing real tokens are preserved. The old shared development token is retired.
func EnsureToken(path, configured string) (bool, error) {
	if configured != "" {
		if strings.TrimSpace(configured) == "" || configured == "dev-token-123" {
			return false, fmt.Errorf("AIHUB_TOKEN must be a nonempty private token")
		}
		return false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read token file: %w", err)
	}
	if token := strings.TrimSpace(string(b)); token != "" && token != "dev-token-123" {
		return false, nil
	}
	token := newSessionToken()
	if token == "" {
		return false, fmt.Errorf("cannot generate random token")
	}
	// O_EXCL prevents concurrent first starts from replacing each other's token.
	if errors.Is(err, os.ErrNotExist) {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return false, fmt.Errorf("create token file: %w", err)
		}
		_, writeErr := f.WriteString(token + "\n")
		closeErr := f.Close()
		if writeErr != nil {
			return false, writeErr
		}
		return true, closeErr
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// token resolves an explicit environment token before the legacy token file.
// Missing/unreadable/empty files fail closed; startup performs initialization.
func (s *Store) token() string {
	if s.configured != "" {
		return s.configured
	}
	b, err := os.ReadFile(s.tokenFile)
	if err != nil {
		return ""
	}
	token := strings.TrimSpace(string(b))
	if token == "dev-token-123" {
		return ""
	}
	return token
}

// Login verifies the provided token in constant time (app.py
// secrets.compare_digest). Returns a new session token, or "" on mismatch.
func (s *Store) Login(provided string) string {
	if !s.CheckToken(provided) {
		return ""
	}
	tok := newSessionToken()
	if tok == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, expires := range s.sessions {
		if !now.Before(expires) {
			delete(s.sessions, key)
		}
	}
	// Bound memory even when a client repeatedly authenticates successfully.
	if len(s.sessions) >= 4096 {
		return ""
	}
	s.sessions[tok] = now.Add(time.Duration(CookieMaxAge) * time.Second)
	return tok
}

// CheckToken authenticates API clients without allocating browser sessions.
func (s *Store) CheckToken(provided string) bool {
	real := s.token()
	return real != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(real)) == 1
}

// Logout discards the session (app.py sessions.discard).
func (s *Store) Logout(tok string) {
	if tok == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, tok)
	s.mu.Unlock()
}

// Check reports whether tok is an active session (app.py check()).
func (s *Store) Check(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expires, ok := s.sessions[tok]
	if ok && !time.Now().Before(expires) {
		delete(s.sessions, tok)
		return false
	}
	return ok
}

// SessionCookie mirrors app.py resp.set_cookie("aih_session", s, httponly=True,
// samesite="lax", max_age=30*24*60*60).
func SessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   CookieMaxAge,
	}
}

// newSessionToken mirrors secrets.token_urlsafe(24): 24 random bytes ->
// 32-char base64url, no padding.
func newSessionToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is not recoverable; fall back to empty
		// (will simply not authenticate) rather than panic in a handler.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
