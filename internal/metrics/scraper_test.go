package metrics

import (
	"testing"
)

func fOf(t *testing.T, out map[string]interface{}, key string) float64 {
	t.Helper()
	v, ok := out[key].(float64)
	if !ok {
		t.Fatalf("out[%q] = %T (%v), want float64", key, out[key], out[key])
	}
	return v
}

func i64Of(t *testing.T, out map[string]interface{}, key string) int64 {
	t.Helper()
	v, ok := out[key].(int64)
	if !ok {
		t.Fatalf("out[%q] = %T (%v), want int64", key, out[key], out[key])
	}
	return v
}

func sOf(t *testing.T, out map[string]interface{}, key string) string {
	t.Helper()
	v, ok := out[key].(string)
	if !ok {
		t.Fatalf("out[%q] = %T (%v), want string", key, out[key], out[key])
	}
	return v
}

func baseData() map[string]interface{} {
	return map[string]interface{}{
		"tokens": 100, "pred_seconds": 2.0, "prompt_tokens": 50,
		"prompt_seconds_total": 1.0, "running": 1, "idle": 0,
		"n_tokens_max": 8, "backend": "llamacpp",
	}
}

func TestFirstCallNoPrev(t *testing.T) {
	s := NewScrapper()
	now := 1000.0
	s.nowFn = func() float64 { return now }
	data := map[string]interface{}{
		"tokens": 100, "pred_seconds": 2.0, "prompt_tokens": 50,
		"prompt_seconds_total": 1.0, "running": 1, "idle": 0,
		"n_tokens_max": 8, "backend": "llamacpp",
	}
	out := s.ComputeLlamaCPPTPS("m1", data)
	// No delta on first call, no gauge -> cumulative fallback
	if got := fOf(t, out, "tps"); got != 50.0 {
		t.Errorf("tps = %v, want 50.0 (cumulative fallback on first call)", got)
	}
	if got := fOf(t, out, "prompt_tps"); got != 50.0 {
		t.Errorf("prompt_tps = %v, want 50.0 (cumulative fallback)", got)
	}
	if got := fOf(t, out, "tps_instant"); got != 0.0 {
		t.Errorf("tps_instant = %v, want 0.0", got)
	}
	if got := fOf(t, out, "tps_cumulative"); got != 50.0 {
		t.Errorf("tps_cumulative = %v, want 50.0", got)
	}
	if got := i64Of(t, out, "tokens"); got != int64(100) {
		t.Errorf("tokens = %v, want int64(100)", got)
	}
	if got := sOf(t, out, "backend"); got != "llamacpp" {
		t.Errorf("backend = %q, want \"llamacpp\"", got)
	}
}

func TestDeltaCall(t *testing.T) {
	s := NewScrapper()
	tf := 1000.0
	s.nowFn = func() float64 { return tf }
	first := baseData()
	s.ComputeLlamaCPPTPS("m1", first)
	// advance 10s, +100 tokens, +2 pred sec
	tf = 1010.0
	second := map[string]interface{}{
		"tokens": 200, "pred_seconds": 4.0, "prompt_tokens": 150,
		"prompt_seconds_total": 2.0, "running": 1, "idle": 0,
		"n_tokens_max": 8, "backend": "llamacpp",
	}
	out := s.ComputeLlamaCPPTPS("m1", second)
	// dTokens=100, dPredSec=2.0 -> tps=50.0; dPrompt=100, dPromptSec=1.0 -> prompt_tps=100.0
	if got := fOf(t, out, "tps"); got != 50.0 {
		t.Errorf("tps = %v, want 50.0", got)
	}
	if got := fOf(t, out, "prompt_tps"); got != 100.0 {
		t.Errorf("prompt_tps = %v, want 100.0", got)
	}
	// tps_instant = genTPS (partial during generation), 0 here because no running transition
	if got := fOf(t, out, "tps_instant"); got != 0.0 {
		t.Errorf("tps_instant = %v, want 0.0 (genTPS, not delta)", got)
	}
}

func TestDeltaNoPredSeconds(t *testing.T) {
	s := NewScrapper()
	tf := 1000.0
	s.nowFn = func() float64 { return tf }
	first := map[string]interface{}{
		"tokens": 100, "pred_seconds": 0.0, "prompt_tokens": 0,
		"prompt_seconds_total": 0.0, "running": 0, "idle": 0,
		"n_tokens_max": 0, "backend": "llamacpp",
	}
	s.ComputeLlamaCPPTPS("m2", first)
	tf = 1003.0
	second := map[string]interface{}{
		"tokens": 130, "pred_seconds": 0.0, "prompt_tokens": 0,
		"prompt_seconds_total": 0.0, "running": 0, "idle": 0,
		"n_tokens_max": 0, "backend": "llamacpp",
	}
	out := s.ComputeLlamaCPPTPS("m2", second)
	// dTokens=30>0, dPredSec=0 -> use dt=3 -> tps=10.0
	if got := fOf(t, out, "tps"); got != 10.0 {
		t.Errorf("tps = %v, want 10.0", got)
	}
	// tps_instant = genTPS (partial during generation), 0 here (no running state)
	if got := fOf(t, out, "tps_instant"); got != 0.0 {
		t.Errorf("tps_instant = %v, want 0.0 (genTPS, no running transition)", got)
	}
}

func TestCumulativeOnly(t *testing.T) {
	s := NewScrapper()
	tf := 1000.0
	s.nowFn = func() float64 { return tf }
	data := map[string]interface{}{
		"tokens": 0, "pred_seconds": 0.0, "prompt_tokens": 0,
		"prompt_seconds_total": 0.0, "running": 0, "idle": 0,
		"n_tokens_max": 0, "ctx_used": 42, "backend": "llamacpp",
	}
	out := s.ComputeLlamaCPPTPS("m3", data)
	if got := fOf(t, out, "tps"); got != 0.0 {
		t.Errorf("tps = %v, want 0.0", got)
	}
	if got := i64Of(t, out, "ctx_used"); got != int64(42) {
		t.Errorf("ctx_used = %v, want int64(42)", got)
	}
}

func TestIsolatedPerModel(t *testing.T) {
	s := NewScrapper()
	tf := 1000.0
	s.nowFn = func() float64 { return tf }
	a := map[string]interface{}{
		"tokens": 100, "pred_seconds": 2.0, "prompt_tokens": 0,
		"prompt_seconds_total": 0.0, "running": 0, "idle": 0,
		"n_tokens_max": 0, "backend": "llamacpp",
	}
	s.ComputeLlamaCPPTPS("mA", a)
	// model B first call (no prev for B) should still use cumulative
	bFirst := map[string]interface{}{
		"tokens": 50, "pred_seconds": 1.0, "prompt_tokens": 0,
		"prompt_seconds_total": 0.0, "running": 0, "idle": 0,
		"n_tokens_max": 0, "backend": "llamacpp",
	}
	outB := s.ComputeLlamaCPPTPS("mB", bFirst)
	if got := fOf(t, outB, "tps"); got != 50.0 {
		t.Errorf("mB tps = %v, want 50.0 (cumulative fallback, no prev for mB)", got)
	}
}
