package metrics

import (
	"aihub/internal/credentials"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func scrapeStrata(port int, mid string) map[string]interface{} {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/metrics", port), nil)
	if err != nil {
		return nil
	}
	if key := credentials.Key(mid); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil
	}
	data, err := ParseStrata(b)
	if err != nil {
		return nil
	}
	return data
}

func ParseStrata(b []byte) (map[string]interface{}, error) {
	var m struct {
		Engine struct {
			MaxContext int `json:"max_context"`
		} `json:"engine"`
		Live struct {
			State     string  `json:"state"`
			TPS       float64 `json:"tok_s"`
			PromptTPS float64 `json:"prefill_tok_s_mean"`
			Prompt    int     `json:"prompt_tokens"`
			Generated int     `json:"generated"`
			Running   int     `json:"running"`
			Queued    int     `json:"queued"`
		} `json:"live"`
		Totals struct {
			Output   int     `json:"output_tokens"`
			Prompt   int     `json:"prompt_tokens"`
			DecodeMS float64 `json:"decode_ms"`
			PromptMS float64 `json:"prompt_ms"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Engine.MaxContext <= 0 {
		return nil, fmt.Errorf("missing Strata engine context")
	}
	tps, promptTPS := m.Live.TPS, m.Live.PromptTPS
	if tps <= 0 && m.Totals.DecodeMS > 0 {
		tps = float64(m.Totals.Output) * 1000 / m.Totals.DecodeMS
	}
	if promptTPS <= 0 && m.Totals.PromptMS > 0 {
		promptTPS = float64(m.Totals.Prompt) * 1000 / m.Totals.PromptMS
	}
	running := m.Live.Running
	if running == 0 && (m.Live.State == "reading" || m.Live.State == "generating") {
		running = 1
	}
	return map[string]interface{}{"backend": "Strata", "tps": round1(tps), "prompt_tps": round1(promptTPS), "tokens": m.Totals.Output, "prompt_tokens": m.Totals.Prompt, "running": running, "waiting": m.Live.Queued, "n_ctx": m.Engine.MaxContext, "ctx_used": m.Live.Prompt + m.Live.Generated}, nil
}
