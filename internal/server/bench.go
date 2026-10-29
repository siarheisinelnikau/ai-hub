package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"aihub/internal/credentials"
	"aihub/internal/status"
)

// handleModelBench runs a quick, deterministic benchmark against a RUNNING
// model: a fixed counting prompt streamed with max_tokens=256, measuring
// time-to-first-token (TTFT) and decode throughput (tok/s). Go-only endpoint
// (no Python parity constraint). Returns:
//
//	{"ok":true,"ttft_s":...,"tps":...,"tokens":...,"total_s":...,"port":...}
func (s *Server) handleModelBench(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	mid := r.PathValue("mid")
	snap := s.d.Poller.Cache().Snapshot()
	port := 0
	running := false
	backend := ""
	if models, ok := snap["models"].([]status.ModelStatusEntry); ok {
		for _, m := range models {
			if m.ID == mid {
				port = m.Port
				running = m.Running
				backend = m.Backend
			}
		}
	}
	if !running {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "detail": "model is not running — start it first"})
		return
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)
	payload := map[string]interface{}{
		"model":       mid,
		"temperature": 0,
		"max_tokens":  256,
		"stream":      true,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "List the integers from 1 to 256, one per line."},
		},
	}
	if strings.EqualFold(backend, "Strata") || strings.EqualFold(backend, "Maya") {
		payload["reasoning_effort"] = "none"
		payload["stream_options"] = map[string]bool{"include_usage": true}
	}
	reqBody, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "detail": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.EqualFold(backend, "Strata") || strings.EqualFold(backend, "Maya") {
		if key := credentials.Key(mid); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}

	client := &http.Client{Timeout: 180 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "detail": "request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "detail": fmt.Sprintf("model returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))})
		return
	}

	var ttftS float64
	firstSet := false
	tokens := 0
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		gotContent := false
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				gotContent = true
			}
		}
		if gotContent {
			if !firstSet {
				firstSet = true
				ttftS = time.Since(start).Seconds()
			}
			tokens++
		}
		if chunk.Usage != nil && chunk.Usage.CompletionTokens > 0 {
			tokens = chunk.Usage.CompletionTokens
		}
	}
	total := time.Since(start).Seconds()
	decodeWindow := total - ttftS
	tps := 0.0
	if tokens > 1 && decodeWindow > 0 {
		tps = float64(tokens-1) / decodeWindow
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"port":    port,
		"ttft_s":  round1f(ttftS),
		"tps":     round1f(tps),
		"tokens":  tokens,
		"total_s": round1f(total),
	})
}

func round1f(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
