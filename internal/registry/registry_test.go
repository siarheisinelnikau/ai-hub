package registry

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot walks up from the test's working directory (internal/registry)
// until it finds registry.yaml, returning that directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(d, "registry.yaml")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d { // reached filesystem root
			break
		}
		d = parent
	}
	t.Fatal("registry.yaml not found above test directory")
	return ""
}

func TestLoadRealFile(t *testing.T) {
	root := repoRoot(t)
	f, err := Load(root, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := len(f.Models); got != 18 {
		t.Fatalf("len(Models) = %d, want 18 (14 LLM profiles + 4 media)", got)
	}

	m := f.Models[0]
	if m.ID != "qwen-q4k-llama" {
		t.Errorf("Models[0].ID = %q, want %q", m.ID, "qwen-q4k-llama")
	}
	if m.Port != 18021 {
		t.Errorf("port = %d, want 18021", m.Port)
	}
	if m.Backend != "llama.cpp" {
		t.Errorf("backend = %q, want %q", m.Backend, "llama.cpp")
	}
	if !m.Parallel.Enabled {
		t.Error("parallel.Enabled = false, want true")
	}
	if m.Parallel.MaxGpus != 4 {
		t.Errorf("parallel.MaxGpus = %d, want 4", m.Parallel.MaxGpus)
	}
	if got := len(m.Parallel.CtxOptions); got != 3 {
		t.Errorf("len(parallel.CtxOptions) = %d, want 3 (q4k: 64k/128k/256k)", got)
	}
	if m.Parallel.DefaultCtx != 262144 {
		t.Errorf("parallel.DefaultCtx = %d, want 262144", m.Parallel.DefaultCtx)
	}
	if len(m.In) != 2 || m.In[0] != "text" || m.In[1] != "image" {
		t.Errorf("in = %v, want [text image] (q4k vision)", m.In)
	}
	if len(m.Out) != 1 || m.Out[0] != "text" {
		t.Errorf("out = %v, want [text]", m.Out)
	}

	for _, id := range []string{"gpt-oss-120b", "qwen38-flash-next-iq3-strata", "qwen35-122b-q4k-llama", "qwen38-35b-q8-llama", "nemotron35-super-q4k", "nemotron35-super-nvfp4"} {
		if m := f.Get(id); m == nil || !m.RequiresWeights {
			t.Errorf("Downloadable model %q has no retained launch profile", id)
		}
		if _, ok := f.Ports[id]; !ok {
			t.Errorf("Downloadable model %q has no port", id)
		}
	}

	q36 := f.Get("qwen38-flash-next-strata")
	if q36 == nil {
		t.Fatal("Get(qwen38-flash-next-strata) = nil, want model")
	}
	if q36.Port != 18029 || q36.Quant != "UD-IQ4_XS" {
		t.Errorf("Flash-Next IQ4 = port %d quant %q, want 18029/UD-IQ4_XS", q36.Port, q36.Quant)
	}
	uncensored := f.Get("qwen38-flash-next-uncensored-strata")
	if uncensored == nil || uncensored.Port != 18030 || uncensored.Quant != "UD-IQ4_XS" {
		t.Fatal("Uncensored Flash-Next IQ4 is missing or has incorrect port/quant")
	}

	c := f.Get("comfyui")
	if c == nil {
		t.Fatal("Get(comfyui) = nil, want model")
	}
	if got := len(c.Sub); got != 5 {
		t.Fatalf("len(comfyui.Sub) = %d, want 5", got)
	}
	if c.Sub[0].Name != "FLUX.1-dev" {
		t.Errorf("comfyui.Sub[0].Name = %q, want %q", c.Sub[0].Name, "FLUX.1-dev")
	}

	if got := len(f.Ports); got != 18 {
		t.Errorf("len(Ports) = %d, want 18 (14 LLM profiles + 4 media)", got)
	}
	if got := f.Ports["qwen38-flash-next-strata"]; got != 18029 {
		t.Errorf(`Ports["qwen38-flash-next-strata"] = %d, want 18029`, got)
	}
}

func TestGet(t *testing.T) {
	root := repoRoot(t)
	f, err := Load(root, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	m := f.Get("qwen-q4k-llama")
	if m == nil {
		t.Fatal("Get(qwen-q4k-llama) = nil, want model")
	}
	if m.Sysd != "ai-qwen-q4k-llama" {
		t.Errorf("sysd = %q, want %q", m.Sysd, "ai-qwen-q4k-llama")
	}
	if got := f.Get("nope"); got != nil {
		t.Errorf("Get(nope) = %v, want nil", got)
	}
}

func TestParallelLaunchFields(t *testing.T) {
	dir := t.TempDir()
	yaml := `
models:
  - id: m
    name: M
    port: 1
    parallel:
      enabled: true
      max_gpus: 4
      gpu_options: [1, 2, 3, 4]
      default_gpus: 4
      ctx_options: [65536, 131072, 262144]
      default_ctx: 262144
      weights_gb: 16.8
      kv_gb_per_100k: 14
      max_parallel: 8
ports:
  m: 1
`
	if err := os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := f.Get("m")
	if m == nil {
		t.Fatal("Get(m) = nil")
	}
	p := m.Parallel
	if !p.Enabled || p.MaxGpus != 4 || p.DefaultGpus != 4 {
		t.Errorf("parallel base = %+v, want enabled max_gpus=4 default_gpus=4", p)
	}
	if p.WeightsGB != 16.8 {
		t.Errorf("weights_gb = %v, want 16.8", p.WeightsGB)
	}
	if p.KVGBPer100k != 14 {
		t.Errorf("kv_gb_per_100k = %v, want 14", p.KVGBPer100k)
	}
	if p.MaxParallel != 8 {
		t.Errorf("max_parallel = %v, want 8", p.MaxParallel)
	}
}

func TestLoadMissing(t *testing.T) {
	// Parity with app.py: nothing found -> empty registry, nil error,
	// hub still starts.
	dir := t.TempDir()
	f, err := Load(dir, dir)
	if err != nil {
		t.Fatalf("Load: %v, want nil error (empty fallback)", err)
	}
	if len(f.Models) != 0 || len(f.Ports) != 0 {
		t.Fatalf("Load on empty dir = %+v, want empty File", f)
	}
}

func TestLoadSkipsUnparseable(t *testing.T) {
	dir := t.TempDir()
	// registry.yaml exists but is malformed -> skipped like in app.py.
	if err := os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte("models: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	// tmp_registry.yaml is the fallback candidate and must win.
	valid := "models:\n  - id: test-model\n    name: Test\n    port: 12345\nports:\n  test-model: 12345\n"
	if err := os.WriteFile(filepath.Join(dir, "tmp_registry.yaml"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m := f.Get("test-model"); m == nil || m.Port != 12345 {
		t.Fatalf("Load did not fall back to tmp_registry.yaml: %+v", f)
	}
}
