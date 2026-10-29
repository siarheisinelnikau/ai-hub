package webhooks

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// TestSaveLoadRoundTrip: Save creates the dir + file; Load reads it back
// (trimmed, blanks dropped). Uses a temp path via AIHUB_WEBHOOKS_FILE.
func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "nested", "webhooks.txt")
	t.Setenv("AIHUB_WEBHOOKS_FILE", f)
	t.Setenv("AIHUB_WEBHOOK_URLS", "")

	if err := Save([]string{"https://a", "  https://b  ", "", "   "}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load()
	if len(got) != 2 || got[0] != "https://a" || got[1] != "https://b" {
		t.Fatalf("Load = %v, want [https://a https://b]", got)
	}
}

// TestSource: env wins over file; with env unset the source is the file.
func TestSource(t *testing.T) {
	t.Setenv("AIHUB_WEBHOOK_URLS", "https://x")
	if Source() != "env" {
		t.Fatalf("Source = %q, want env", Source())
	}
	t.Setenv("AIHUB_WEBHOOK_URLS", "")
	if Source() != "file" {
		t.Fatalf("Source = %q, want file", Source())
	}
}

// TestFireText: the payload always carries a human-readable "text" field (used
// by Telegram/Slack) even when model/detail are empty.
func TestFireText(t *testing.T) {
	t.Setenv("AIHUB_WEBHOOK_URLS", "")
	t.Setenv("AIHUB_WEBHOOKS_FILE", filepath.Join(t.TempDir(), "wh.txt"))
	// No URLs configured -> Fire is a no-op (must not panic).
	Fire("settings.test", "ai-hub", "hello")
}

// TestSendPerBot: Send reports each bot's delivery individually (ok + HTTP
// status), distinguishing a 200 bot from a 404 bot.
func TestSendPerBot(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer okSrv.Close()
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer badSrv.Close()
	t.Setenv("AIHUB_WEBHOOK_URLS", "")

	results := Send("hi", []string{okSrv.URL, badSrv.URL})
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if !results[0].OK || results[0].Status != 200 {
		t.Fatalf("good bot = %+v, want OK 200", results[0])
	}
	if results[1].OK || results[1].Status != 404 {
		t.Fatalf("bad bot = %+v, want !OK 404", results[1])
	}
}
