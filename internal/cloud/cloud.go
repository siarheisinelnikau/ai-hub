// Package cloud manages "cloud" model providers that are exposed as hub
// cards, mirroring local LLM profiles. The first provider is OpenAI OAuth
// (EvanZhouDev/openai-oauth): an OpenAI-compatible proxy backed by a ChatGPT
// account (Codex models such as gpt-6-astra).
//
// The hub does not run the proxy itself here — it only stores the mapping
// (which upstream models to show as cards, with display metadata), probes the
// proxy for liveness, and merges the mapped models into /api/status so the UI
// renders them next to the local models. Start/stop controls the systemd unit
// that runs the proxy when it is installed locally.
package cloud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aihub/internal/sh"
	"aihub/internal/status"
)

const (
	// DefaultBaseURL is the OpenAI-compatible base URL of openai-oauth
	// (v2 CLI default port is 10531; the hub proxies /v1/models and chat).
	DefaultBaseURL = "http://127.0.0.1:10531/v1"
	// DefaultService is the systemd unit that runs openai-oauth on this box.
	DefaultService = "ai-openai-oauth"
	// DefaultPort is the port shown on the card (:10531).
	DefaultPort = 10531
	// DefaultCategory is the UI section cloud models render under.
	DefaultCategory = "Cloud"
	// DefaultBackend is the card's "Engine" value.
	DefaultBackend = "openai-oauth"
)

// Model is one mapped cloud model (== one hub card). ID is the upstream model
// id (e.g. "gpt-6-astra") so the card's API/Chat URLs carry the correct model
// name without any rewriting.
type Model struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Short    string   `json:"short"`
	Category string   `json:"category"`
	Quant    string   `json:"quant"`
	TPS      string   `json:"tps"`
	Ctx      string   `json:"ctx"`
	Desc     string   `json:"desc"`
	In       []string `json:"in"`
	Out      []string `json:"out"`
	Accel    string   `json:"accel"`
	HF       string   `json:"hf"`
	Enabled  bool     `json:"enabled"`
}

// Config is the persisted cloud-provider mapping (/srv/ai-hub/cloud.json).
type Config struct {
	Enabled bool    `json:"enabled"`
	BaseURL string  `json:"base_url"`
	Service string  `json:"service"`
	Port    int     `json:"port"`
	Models  []Model `json:"models"`
}

// RemoteModel is one entry from the provider's GET /v1/models.
type RemoteModel struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// Path returns the config file path. AIHUB_CLOUD_CONFIG overrides it (tests);
// otherwise it lives next to registry.yaml under the AIHUB root so the parity
// harness (which runs against a temp root) never picks up a production file.
func Path(root string) string {
	if v := os.Getenv("AIHUB_CLOUD_CONFIG"); v != "" {
		return v
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, "cloud.json")
}

// Default returns the empty (disabled) configuration.
func Default() *Config {
	return &Config{
		Enabled: false,
		BaseURL: DefaultBaseURL,
		Service: DefaultService,
		Port:    DefaultPort,
		Models:  []Model{},
	}
}

// Load reads the cloud config. A missing file yields the disabled default; a
// malformed file yields the default plus an error (the hub still starts).
func Load(root string) (*Config, error) {
	data, err := os.ReadFile(Path(root))
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Default(), err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	cfg.normalize()
	return &cfg, nil
}

// MustLoad is Load ignoring the error (handlers never fail on a bad config;
// they fall back to defaults).
func MustLoad(root string) *Config {
	cfg, _ := Load(root)
	return cfg
}

// normalize fills defaults and de-duplicates models by id.
func (c *Config) normalize() {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.Service == "" {
		c.Service = DefaultService
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	seen := map[string]bool{}
	out := make([]Model, 0, len(c.Models))
	for _, m := range c.Models {
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		if m.Category == "" {
			m.Category = DefaultCategory
		}
		if m.Name == "" {
			m.Name = m.ID
		}
		if m.Short == "" {
			m.Short = m.Name
		}
		if m.In == nil {
			m.In = []string{"text"}
		}
		if m.Out == nil {
			m.Out = []string{"text"}
		}
		out = append(out, m)
	}
	c.Models = out
}

// Save validates, normalizes and writes the config (0644).
func Save(root string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	cfg.normalize()
	p := Path(root)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(p, data, 0644)
}

// Get returns the mapped model with the given id, or nil.
func (c *Config) Get(id string) *Model {
	if c == nil {
		return nil
	}
	for i := range c.Models {
		if c.Models[i].ID == id {
			return &c.Models[i]
		}
	}
	return nil
}

// EnabledModels returns the mapped models that are switched on.
func (c *Config) EnabledModels() []Model {
	if c == nil {
		return nil
	}
	out := make([]Model, 0, len(c.Models))
	for _, m := range c.Models {
		if m.Enabled {
			out = append(out, m)
		}
	}
	return out
}

// RemoteModels fetches GET {base}/models from the provider. Errors are
// returned so the settings UI can show why the list is empty (proxy down,
// region block, …).
func (c *Config) RemoteModels() ([]RemoteModel, error) {
	if c == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("no base URL configured")
	}
	u := strings.TrimRight(c.BaseURL, "/") + "/models"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data []RemoteModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid /v1/models response: %w", err)
	}
	return body.Data, nil
}

// probeModels reports whether the provider's /v1/models answers.
func probeModels(baseURL string) bool {
	if baseURL == "" {
		return false
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/models")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 400
}

// serviceActive reports whether the local systemd unit is active. ok=false
// when the unit file is not installed on this box (external/remote provider).
func serviceActive(service string) (active, installed bool) {
	if service == "" {
		return false, false
	}
	if _, err := os.Stat("/etc/systemd/system/" + service + ".service"); err != nil {
		return false, false
	}
	rc, out := sh.Run([]string{"systemctl", "is-active", service + ".service"}, 5*time.Second)
	return rc == 0 && strings.Contains(out, "active"), true
}

// Probe returns the provider's card state: running (the proxy is up — either
// the local unit is active or the endpoint answers) and healthy (the endpoint
// actually serves /v1/models). installed reports whether a local unit exists.
func (c *Config) Probe() (running, healthy, installed bool) {
	if c == nil || !c.Enabled {
		return false, false, false
	}
	healthy = probeModels(c.BaseURL)
	active, hasUnit := serviceActive(c.Service)
	installed = hasUnit
	if hasUnit {
		running = active || healthy
		return
	}
	// No local unit: the provider is remote/external; its liveness is the
	// endpoint itself.
	running = healthy
	return
}

// Entries builds the /api/status model entries for the enabled cloud models.
// All cards share the provider's running/healthy state (one proxy serves them
// all) and the same port.
func (c *Config) Entries() []status.ModelStatusEntry {
	if c == nil || !c.Enabled {
		return nil
	}
	enabled := c.EnabledModels()
	if len(enabled) == 0 {
		return nil
	}
	running, healthy, _ := c.Probe()
	out := make([]status.ModelStatusEntry, 0, len(enabled))
	for _, m := range enabled {
		cat := m.Category
		if cat == "" {
			cat = DefaultCategory
		}
		short := m.Short
		if short == "" {
			short = m.Name
		}
		if short == "" {
			short = m.ID
		}
		in := m.In
		if in == nil {
			in = []string{"text"}
		}
		outs := m.Out
		if outs == nil {
			outs = []string{"text"}
		}
		out = append(out, status.ModelStatusEntry{
			ID:            m.ID,
			Name:          m.Name,
			Short:         short,
			Category:      cat,
			Backend:       DefaultBackend,
			Quant:         m.Quant,
			Port:          c.Port,
			VRAM:          "cloud",
			TPS:           m.TPS,
			Ctx:           m.Ctx,
			In:            in,
			Out:           outs,
			Desc:          m.Desc,
			Warn:          "",
			Sub:           []status.Sub{},
			Accel:         m.Accel,
			Group:         "cloud",
			Sysd:          c.Service,
			Running:       running,
			Healthy:       healthy,
			Enabled:       true,
			PortConflict:  false,
			GroupSiblings: []string{},
			HF:            m.HF,
		})
	}
	return out
}

// Action starts, stops or restarts the local provider unit. When no unit is
// installed (remote provider) it is a no-op reported as "external".
func (c *Config) Action(action string) (map[string]interface{}, int) {
	if action != "start" && action != "stop" && action != "restart" {
		return map[string]interface{}{"detail": "bad action"}, 400
	}
	if c == nil || c.Service == "" {
		return map[string]interface{}{"ok": false, "output": "no provider service configured"}, 200
	}
	if _, err := os.Stat("/etc/systemd/system/" + c.Service + ".service"); err != nil {
		return map[string]interface{}{
			"ok": false, "state": "external",
			"output": "provider has no local systemd unit — it runs externally, nothing to " + action + " here",
		}, 200
	}
	rc, out := sh.Run([]string{"sudo", "-n", "systemctl", action, c.Service + ".service"}, 120*time.Second)
	if rc != 0 {
		return map[string]interface{}{"ok": false, "state": "failed", "output": strings.TrimSpace(out)}, 200
	}
	return map[string]interface{}{"ok": true, "state": action + "ed", "output": strings.TrimSpace(out)}, 200
}
