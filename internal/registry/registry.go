// Package registry loads and queries registry.yaml, the single source of
// truth for all ai-hub model profiles.
package registry

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Parallel describes multi-instance (per-GPU) serving options for a model.
type Parallel struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	GPUPerInstance  int    `yaml:"gpu_per_instance" json:"gpu_per_instance"`
	MaxInstances    int    `yaml:"max_instances" json:"max_instances"`
	MaxGpus         int    `yaml:"max_gpus" json:"max_gpus"`
	GpuOptions      []int  `yaml:"gpu_options" json:"gpu_options"`
	DefaultGpus     int    `yaml:"default_gpus" json:"default_gpus"`
	CtxOptions      []int  `yaml:"ctx_options" json:"ctx_options"`
	DefaultCtx      int    `yaml:"default_ctx" json:"default_ctx"`
	VRAMPerInstance string `yaml:"vram_per_instance" json:"vram_per_instance"`
	// Launch-feasibility data, used by the panel UI to cap which GPU/ctx/agents
	// combinations physically fit (weights must fit on N×24GB minus overhead,
	// and the KV cache for ctx×agents tokens must fit in the rest). Tune per
	// model; the UI disables combinations that violate the budget.
	WeightsGB   float64 `yaml:"weights_gb" json:"weights_gb"`
	KVGBPer100k float64 `yaml:"kv_gb_per_100k" json:"kv_gb_per_100k"`
	MaxParallel int     `yaml:"max_parallel" json:"max_parallel"`
}

// SubModel is a model bundled under a parent (e.g. a checkpoint inside ComfyUI).
type SubModel struct {
	Name string `yaml:"name"`
	Tag  string `yaml:"tag"`
	VRAM string `yaml:"vram"`
	HF   string `yaml:"hf"`
}

// Model is one LLM/media profile entry in registry.yaml.
type Model struct {
	ID       string   `yaml:"id"`
	Name     string   `yaml:"name"`
	Short    string   `yaml:"short"`
	Category string   `yaml:"category"`
	Backend  string   `yaml:"backend"`
	Quant    string   `yaml:"quant"`
	Port     int      `yaml:"port"`
	Group    string   `yaml:"group"`
	VRAM     string   `yaml:"vram"`
	TPS      string   `yaml:"tps"`
	Ctx      string   `yaml:"ctx"`
	Sysd     string   `yaml:"sysd"`
	Native   bool     `yaml:"native"` // long-running systemd process rather than a Docker wrapper
	Desc     string   `yaml:"desc"`
	In       []string `yaml:"in"`
	Out      []string `yaml:"out"`
	Accel    string   `yaml:"accel"`
	HF       string   `yaml:"hf"`
	// Explicit provenance flag; uncensored status is never inferred from a name.
	Uncensored      bool       `yaml:"uncensored"`
	RequiresWeights bool       `yaml:"requires_weights"` // retain launch configuration after deleting weights
	Parallel        Parallel   `yaml:"parallel"`
	Sub             []SubModel `yaml:"sub"`
}

// File is the top-level structure of registry.yaml.
type File struct {
	Models   []Model        `yaml:"models"`
	Ports    map[string]int `yaml:"ports"`
	Profiles []Profile      `yaml:"profiles"`
}

// Profile is a named vLLM profile entry from the top-level "profiles" list in
// registry.yaml (app.py: PROFILES = {p["name"]: p for p in REG.get("profiles", [])}).
// Current registry has none -> nil.
type Profile struct {
	Name       string   `yaml:"name"`
	EnvFile    string   `yaml:"env_file"`
	StopBefore []string `yaml:"stop_before"`
	StartAfter []string `yaml:"start_after"`
}

// Load reads the first parseable registry from the fallback chain that
// mirrors load_registry() in app.py exactly:
//
//  1. root/registry.yaml      (AIHUB/registry.yaml)
//  2. devDir/registry.yaml    (Path(__file__).parent/registry.yaml)
//  3. devDir/tmp_registry.yaml
//  4. devDir/registry_hub_fix.yaml
//
// root is the resolved AIHUB root ("/srv/ai-hub" when it has a registry,
// otherwise devDir); devDir is the directory of the executable (the Go
// equivalent of Path(__file__).parent).
//
// Semantics match Python: missing or empty candidates are skipped, a
// candidate that fails to parse is skipped, and if nothing yields a usable
// registry the function returns an EMPTY File with a nil error (Python
// falls back to {"models": [], ...} so the hub still starts).
func Load(root, devDir string) (*File, error) {
	if path := os.Getenv("AIHUB_REGISTRY"); path != "" {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read configured registry: %w", err)
		}
		var f File
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&f); err != nil {
			return nil, fmt.Errorf("parse configured registry: %w", err)
		}
		seen := map[string]bool{}
		idPattern := regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,95}$`)
		unitPattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]*$`)
		for _, m := range f.Models {
			if !idPattern.MatchString(m.ID) || seen[m.ID] || m.Port < 1 || m.Port > 65535 || !unitPattern.MatchString(m.Sysd) {
				return nil, fmt.Errorf("invalid model id, port, service or duplicate id in configured registry: %q", m.ID)
			}
			seen[m.ID] = true
		}
		return &f, nil
	}
	seen := map[string]bool{}
	candidates := []string{}
	for _, p := range []string{
		filepath.Join(root, "registry.yaml"),
		filepath.Join(devDir, "registry.yaml"),
		filepath.Join(devDir, "tmp_registry.yaml"),
		filepath.Join(devDir, "registry_hub_fix.yaml"),
	} {
		if !seen[p] {
			seen[p] = true
			candidates = append(candidates, p)
		}
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		// Python: `data = yaml.safe_load(...); if data:` — skip empty/blank.
		var raw interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		if m, ok := raw.(map[string]interface{}); !ok || len(m) == 0 {
			continue
		}
		var f File
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		return &f, nil
	}
	return &File{}, nil // empty fallback: hub still starts (parity with app.py)
}

// AIHUBRoot mirrors the AIHUB resolution in app.py: "/srv/ai-hub" when a
// registry.yaml exists there, otherwise devDir (the executable's directory,
// the Go equivalent of Path(__file__).parent).
func AIHUBRoot(devDir string) string {
	if _, err := os.Stat("/srv/ai-hub/registry.yaml"); err == nil {
		return "/srv/ai-hub"
	}
	return devDir
}

// Get returns the model with the given id, or nil if not found.
func (f *File) Get(id string) *Model {
	for i := range f.Models {
		if f.Models[i].ID == id {
			return &f.Models[i]
		}
	}
	return nil
}

// GetProfile returns the profile with the given name, or nil.
func (f *File) GetProfile(name string) *Profile {
	for i := range f.Profiles {
		if f.Profiles[i].Name == name {
			return &f.Profiles[i]
		}
	}
	return nil
}

// IDs returns all model ids in file order.
func (f *File) IDs() []string {
	ids := make([]string, len(f.Models))
	for i := range f.Models {
		ids[i] = f.Models[i].ID
	}
	return ids
}
