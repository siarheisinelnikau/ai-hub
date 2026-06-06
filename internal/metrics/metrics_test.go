package metrics

import "testing"

// TestParseVLLM asserts EXACT Python parity: app.py's vllm regex key class is
// [a-z_]+ (no colon), so real vLLM metric lines (vllm:name{...} value) never
// match and the 'vllm:...' lookups always miss → all zeros (verified against
// app.py _parse_metrics_vllm on the same sample).
func TestParseVLLM(t *testing.T) {
	text := `# HELP vllm:num_generation_tokens_total Total tokens generated
# TYPE vllm:num_generation_tokens_total counter
vllm:num_generation_tokens_total{model_name="qwen"} 1234
vllm:num_prompt_tokens_total 567
vllm:num_requests_running{model_name="qwen"} 2
vllm:num_requests_waiting 1
vllm:avg_generation_throughput 55.567
vllm:gpu_cache_usage_pgf 0.5
`
	m, err := ParseVLLM(text)
	if err != nil {
		t.Fatalf("ParseVLLM: %v", err)
	}
	if got := m["tps"]; got != 0.0 {
		t.Errorf("tps = %v, want 0.0 (Python parity)", got)
	}
	for _, k := range []string{"tokens", "prompt_tokens", "running", "waiting"} {
		if got := m[k]; got != 0 {
			t.Errorf("%s = %v, want 0 (Python parity)", k, got)
		}
	}
	if got := m["backend"]; got != "vllm" {
		t.Errorf("backend = %v, want vllm", got)
	}
}

// TestParseVLLMUnprefixed covers the (rare) case where metric lines have no
// colon prefix: the regex matches, but lookups use 'vllm:'-prefixed keys, so
// the result is still all zeros — same as Python.
func TestParseVLLMUnprefixed(t *testing.T) {
	text := "num_requests_running 2\navg_generation_throughput 55.567\n"
	m, err := ParseVLLM(text)
	if err != nil {
		t.Fatalf("ParseVLLM: %v", err)
	}
	if got := m["running"]; got != 0 {
		t.Errorf("running = %v, want 0 (lookup key is vllm:num_requests_running)", got)
	}
}

func TestParseLlamaCPP(t *testing.T) {
	text := `# c
llamacpp:tokens_predicted_total 500
llamacpp:prompt_tokens_total 400
llamacpp:tokens_predicted_seconds_total 10.0
llamacpp:prompt_seconds_total 5.0
llamacpp:predicted_tokens_seconds 40.0
llamacpp:prompt_tokens_seconds 30.0
llamacpp:requests_processing 1
llamacpp:requests_deferred 2
llamacpp:n_tokens_max 8192
`
	m, err := ParseLlamaCPP(text)
	if err != nil {
		t.Fatalf("ParseLlamaCPP: %v", err)
	}
	if got := m["tokens"]; got != 500 {
		t.Errorf("tokens = %v, want 500", got)
	}
	if got := m["prompt_tokens"]; got != 400 {
		t.Errorf("prompt_tokens = %v, want 400", got)
	}
	if got := m["pred_seconds"]; got != 10.0 {
		t.Errorf("pred_seconds = %v, want 10.0", got)
	}
	if got := m["running"]; got != 1 {
		t.Errorf("running = %v, want 1", got)
	}
	if got := m["idle"]; got != 2 {
		t.Errorf("idle = %v, want 2", got)
	}
	if got := m["n_tokens_max"]; got != 8192 {
		t.Errorf("n_tokens_max = %v, want 8192", got)
	}
	if got := m["backend"]; got != "llamacpp" {
		t.Errorf("backend = %v, want llamacpp", got)
	}
}

func TestParseLlamaCPPFallbackKeys(t *testing.T) {
	text := `llamacpp_predicted_tokens_total 111
llamacpp_prompt_tokens_total 222
llamacpp_slots_processing 3
llamacpp_slots_idle 4
`
	m, err := ParseLlamaCPP(text)
	if err != nil {
		t.Fatalf("ParseLlamaCPP: %v", err)
	}
	if got := m["tokens"]; got != 111 {
		t.Errorf("tokens = %v, want 111", got)
	}
	if got := m["prompt_tokens"]; got != 222 {
		t.Errorf("prompt_tokens = %v, want 222", got)
	}
	if got := m["running"]; got != 3 {
		t.Errorf("running = %v, want 3", got)
	}
	if got := m["idle"]; got != 4 {
		t.Errorf("idle = %v, want 4", got)
	}
}

// TestParseVLLMBadValue: a line that MATCHES the regex but whose value fails
// float() → Python raises ValueError → scrape returns None; Go mirrors with
// error. "foo e+" matches ^([a-z_]+)\s+([\d.e+-]+) with value "e+".
func TestParseVLLMBadValue(t *testing.T) {
	_, err := ParseVLLM("foo e+\n")
	if err == nil {
		t.Error("expected error for unparseable matched value, got nil")
	}
}

// TestParseVLLMUnmatchedColonLine: "vllm:num_requests_running notanumber"
// never matches (colon after key) → no error, all zeros (Python parity).
func TestParseVLLMUnmatchedColonLine(t *testing.T) {
	m, err := ParseVLLM("vllm:num_requests_running notanumber\n")
	if err != nil {
		t.Fatalf("ParseVLLM: %v, want nil (line does not match regex)", err)
	}
	if got := m["running"]; got != 0 {
		t.Errorf("running = %v, want 0", got)
	}
}

func TestParseLlamaCPPEmpty(t *testing.T) {
	m, err := ParseLlamaCPP("")
	if err != nil {
		t.Fatalf("ParseLlamaCPP(\"\"): %v", err)
	}
	if got := m["tokens"]; got != 0 {
		t.Errorf("tokens = %v, want 0", got)
	}
	if got := m["backend"]; got != "llamacpp" {
		t.Errorf("backend = %v, want llamacpp", got)
	}
}
