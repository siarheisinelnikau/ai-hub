// Package webhooks fires HTTP notifications for hub events (model start/stop/
// unhealthy). Best-effort: a failing webhook never affects the action.
package webhooks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	mu   sync.Mutex
	last = map[string]time.Time{}
)

// Load returns the configured webhook URLs. Sources, in priority order:
//   - AIHUB_WEBHOOK_URLS env (comma-separated)
//   - /srv/ai-hub/webhooks.txt (one URL per line, # comments / blank ignored)
//
// Returns nil when no webhooks are configured (Fire is then a no-op).
func Load() []string {
	var urls []string
	if env := os.Getenv("AIHUB_WEBHOOK_URLS"); env != "" {
		for _, u := range strings.Split(env, ",") {
			if u = strings.TrimSpace(u); u != "" {
				urls = append(urls, u)
			}
		}
		return urls
	}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls
}

// ConfigPath is where the webhook URL list is read from / written to. Override
// with AIHUB_WEBHOOKS_FILE (used by tests / non-standard layouts); defaults to
// the server path.
func ConfigPath() string {
	if p := os.Getenv("AIHUB_WEBHOOKS_FILE"); p != "" {
		return p
	}
	return "/srv/ai-hub/webhooks.txt"
}

// Source reports where Load() would read from: "env" (AIHUB_WEBHOOK_URLS is
// set, so editing the file has no effect) or "file".
func Source() string {
	if os.Getenv("AIHUB_WEBHOOK_URLS") != "" {
		return "env"
	}
	return "file"
}

// Save writes the webhook URL list to ConfigPath (one URL per line, with a
// header comment). Blank lines are dropped. It creates the parent directory if
// missing. Returns an error if the path is not writable.
func Save(urls []string) error {
	var b strings.Builder
	b.WriteString("# ai-hub webhook URLs — one per line, # comments allowed\n")
	b.WriteString("# e.g. a Telegram bot: https://api.telegram.org/bot<TOKEN>/sendMessage?chat_id=<CHAT_ID>\n")
	for _, u := range urls {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		b.WriteString(u + "\n")
	}
	if dir := filepath.Dir(ConfigPath()); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(ConfigPath(), []byte(b.String()), 0o644)
}

// Fire POSTs a JSON event to all configured URLs asynchronously. Never blocks
// the caller and never returns an error (best-effort). Payload:
//
//	{"event": "...", "model": "...", "detail": "...", "text": "...", "ts": 123, "source": "ai-hub"}
//
// "text" is a human-readable one-liner so chat bots (Telegram/Slack) can use it
// directly as the message body.
func Fire(event, model, detail string) {
	urls := Load()
	if len(urls) == 0 {
		return
	}
	text := "ai-hub " + event
	if model != "" {
		text += " · " + model
	}
	if detail != "" {
		text += " — " + detail
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"event":  event,
		"model":  model,
		"detail": detail,
		"text":   text,
		"ts":     time.Now().Unix(),
		"source": "ai-hub",
	})
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		for _, u := range urls {
			resp, err := client.Post(u, "application/json", bytes.NewReader(payload))
			if err == nil && resp != nil {
				resp.Body.Close()
			}
		}
	}()
}

// Result is the outcome of delivering to a single webhook URL.
type Result struct {
	URL    string `json:"url"`
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Err    string `json:"err,omitempty"`
}

// Send posts a human-readable message SYNCHRONOUSLY to the given URLs (or to
// Load() when urls is nil) and returns a per-URL Result (HTTP status / error).
// This backs the settings "Send test" button so each bot's delivery is reported
// individually (ok / status / error) instead of a single count. For a Telegram
// bot URL (.../sendMessage?chat_id=..) the body's "text" is used directly.
func Send(message string, urls []string) []Result {
	if urls == nil {
		urls = Load()
	}
	client := &http.Client{Timeout: 8 * time.Second}
	var out []Result
	for _, u := range urls {
		payload, _ := json.Marshal(map[string]interface{}{
			"event":  "settings.test",
			"text":   message,
			"ts":     time.Now().Unix(),
			"source": "ai-hub",
		})
		r := Result{URL: u}
		resp, err := client.Post(u, "application/json", bytes.NewReader(payload))
		if err != nil {
			r.Err = err.Error()
		} else if resp != nil {
			r.Status = resp.StatusCode
			r.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
			resp.Body.Close()
		}
		out = append(out, r)
	}
	return out
}

// FireCoalesced is Fire with a per-(event,model) cooldown, so a condition that
// persists across polls (e.g. an unhealthy model) fires once instead of every
// poll cycle.
func FireCoalesced(event, model, detail string, cooldown time.Duration) {
	mu.Lock()
	key := event + "\x00" + model
	if t, ok := last[key]; ok && time.Since(t) < cooldown {
		mu.Unlock()
		return
	}
	last[key] = time.Now()
	mu.Unlock()
	Fire(event, model, detail)
}
