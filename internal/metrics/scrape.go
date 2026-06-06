package metrics

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ModelRef identifies a model backend to scrape (mirrors the poller's view
// of a running model).
type ModelRef struct {
	ID      string
	Port    int
	Backend string // "vllm" or "llamacpp"
}

var httpClient = &http.Client{Timeout: 3 * time.Second} // curl -m 3

// httpGet mirrors curl -sf (fail on HTTP >= 400). Returns body bytes or error.
func httpGet(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// ScrapeModelMetrics mirrors app.py _scrape_model_metrics. Returns the parsed
// stats dict, or nil on any failure (http error, empty/whitespace-only body,
// parse error, unknown backend). For llamacpp it also scrapes /slots for
// ctx_used and computes delta TPS via the Scrapper.
func ScrapeModelMetrics(port int, backend, mid string, s *Scrapper) map[string]interface{} {
	if strings.EqualFold(backend, "Strata") || strings.EqualFold(backend, "Maya") {
		data := scrapeStrata(port, mid)
		if data != nil {
			data["backend"] = backend
		}
		return data
	}
	metricsURL := fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
	body, err := httpGet(metricsURL)
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	text := string(body)
	// registry backends are mixed-case ("vLLM"); match case-insensitively so
	// vLLM models get the vLLM parser (app.py's `== "vllm"` has the same bug).
	switch strings.ToLower(backend) {
	case "sglang":
		data, err := ParseSGLang(text)
		if err != nil {
			return nil
		}
		return data
	case "vllm":
		m, err := ParseVLLM(text)
		if err != nil {
			return nil
		}
		return m
	default: // llamacpp
		data, err := ParseLlamaCPP(text)
		if err != nil {
			return nil
		}
		data["port"] = port
		return s.ComputeLlamaCPPTPS(mid, data)
	}
}
