package metrics

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// StatsMax/StatsPoll mirror app.py constants.
const (
	StatsMax  = 360
	StatsPoll = 1
)

func round1(x float64) float64 { return math.Round(x*10) / 10 }

var (
	// EXACT Python regexes (app.py _parse_metrics_vllm / _parse_metrics_llamacpp).
	// NOTE the vllm key class is [a-z_]+ WITHOUT colon — so real vLLM metric
	// lines (vllm:name{...} value) never match, and r.get('vllm:...') lookups
	// always miss: Python returns all zeros for real vLLM /metrics. Parity
	// means we must reproduce that quirk, not "fix" it.
	reVLLMLabelled = regexp.MustCompile(`^([a-z_]+)\{[^}]*\}\s+([\d.e+-]+)`)
	reVLLMPlain    = regexp.MustCompile(`^([a-z_]+)\s+([\d.e+-]+)`)
	reLlama        = regexp.MustCompile(`^([a-z_:]+)\s+([\d.e+-]+)`)
)

// parsePromVLLM parses vLLM /metrics (Prometheus format) into a map
// (mirrors app.py _parse_metrics_vllm). A malformed value returns an error,
// mirroring Python float() raising → scrape returns None.
func parsePromVLLM(text string) (map[string]float64, error) {
	r := map[string]float64{}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		m := reVLLMLabelled.FindStringSubmatch(line)
		if m == nil {
			m = reVLLMPlain.FindStringSubmatch(line)
		}
		if m == nil {
			continue
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			return nil, err
		}
		r[m[1]] = v
	}
	return r, nil
}

// parsePromLlama parses llama.cpp /metrics (Prometheus format) into a map
// (mirrors app.py _parse_metrics_llamacpp).
func parsePromLlama(text string) (map[string]float64, error) {
	r := map[string]float64{}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		m := reLlama.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			return nil, err
		}
		r[m[1]] = v
	}
	return r, nil
}

func fget(r map[string]float64, key string) float64 {
	if v, ok := r[key]; ok {
		return v
	}
	return 0
}

func fgetAlt(r map[string]float64, a, b string) float64 {
	if v, ok := r[a]; ok {
		return v
	}
	if b != "" {
		if v2, ok2 := r[b]; ok2 {
			return v2
		}
	}
	return 0
}

// ParseVLLM parses vLLM /metrics text into the panel stats dict
// (mirrors app.py _parse_metrics_vllm return shape).
func ParseVLLM(text string) (map[string]interface{}, error) {
	r, err := parsePromVLLM(text)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"tps":           round1(fget(r, "vllm:avg_generation_throughput")),
		"tokens":        int(fget(r, "vllm:num_generation_tokens_total")),
		"prompt_tokens": int(fget(r, "vllm:num_prompt_tokens_total")),
		"running":       int(fget(r, "vllm:num_requests_running")),
		"waiting":       int(fget(r, "vllm:num_requests_waiting")),
		"backend":       "vllm",
	}, nil
}

// ParseLlamaCPP parses llama.cpp /metrics text into raw counters so a poller
// can compute delta-based TPS (mirrors app.py _parse_metrics_llamacpp).
func ParseLlamaCPP(text string) (map[string]interface{}, error) {
	r, err := parsePromLlama(text)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"tokens":               int(fgetAlt(r, "llamacpp:tokens_predicted_total", "llamacpp_predicted_tokens_total")),
		"prompt_tokens":        int(fgetAlt(r, "llamacpp:prompt_tokens_total", "llamacpp_prompt_tokens_total")),
		"pred_seconds":         fget(r, "llamacpp:tokens_predicted_seconds_total"),
		"prompt_seconds_total": fget(r, "llamacpp:prompt_seconds_total"),
		"gauge_tps":            fget(r, "llamacpp:predicted_tokens_seconds"),
		"gauge_prompt_tps":     fget(r, "llamacpp:prompt_tokens_seconds"),
		"running":              int(fgetAlt(r, "llamacpp:requests_processing", "llamacpp_slots_processing")),
		"idle":                 int(fgetAlt(r, "llamacpp:requests_deferred", "llamacpp_slots_idle")),
		"n_tokens_max":         int(fget(r, "llamacpp:n_tokens_max")),
		"backend":              "llamacpp",
	}, nil
}
