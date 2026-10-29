package metrics

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Scrapper keeps the previous counter snapshot per model and computes
// delta-based TPS. For llama.cpp b10644+, /metrics counters are frozen
// during generation — we read /slots n_decoded for real-time TPS.
// Multi-slot aware: llama-server started with --parallel N reports N slots;
// per-slot n_decoded is tracked so the active request is counted even when
// it runs on a slot other than #0.
type Scrapper struct {
	prev          map[string]map[string]float64
	prevSlots     map[string][]float64 // per-slot n_decoded snapshot per model
	prevSlotsProc map[string][]bool    // per-slot is_processing snapshot per model
	prevSlotsTs   map[string]float64
	nowFn         func() float64
}

func NewScrapper() *Scrapper {
	return &Scrapper{
		prev:          map[string]map[string]float64{},
		prevSlots:     map[string][]float64{},
		prevSlotsProc: map[string][]bool{},
		prevSlotsTs:   map[string]float64{},
		nowFn:         func() float64 { return float64(time.Now().UnixNano()) / 1e9 },
	}
}

var slotsHTTPClient = &http.Client{Timeout: 2 * time.Second}

// slotSnap is one slot's generation state from /slots.
type slotSnap struct {
	decoded      int64
	processing   bool
	promptTokens int64
}

// readSlots polls /slots for live per-slot n_decoded and ctx_used values.
// Returns one snapshot per slot (order as reported by llama-server),
// plus whether any slot is processing. Returns nil if /slots is unreachable
// or unparseable.
func readSlots(port int) ([]slotSnap, bool) {
	resp, err := slotsHTTPClient.Get(fmt.Sprintf("http://127.0.0.1:%d/slots", port))
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return nil, false
	}
	var slots []map[string]interface{}
	if err := json.Unmarshal(body, &slots); err != nil || len(slots) == 0 {
		return nil, false
	}
	out := make([]slotSnap, 0, len(slots))
	any := false
	for _, s := range slots {
		var snap slotSnap
		if ip, ok := s["is_processing"].(bool); ok && ip {
			snap.processing = true
			any = true
			if npt := i64of(s["n_prompt_tokens"]); npt > 0 {
				snap.promptTokens = npt
			}
		}
		// top-level n_decoded (standard llama.cpp), fallback to next_token[0].n_decoded
		if nd := i64of(s["n_decoded"]); nd > 0 {
			snap.decoded = nd
		} else if ntArr, ok := s["next_token"].([]interface{}); ok && len(ntArr) > 0 {
			if nt, ok := ntArr[0].(map[string]interface{}); ok {
				snap.decoded = i64of(nt["n_decoded"])
			}
		}
		out = append(out, snap)
	}
	return out, any
}

// ComputeLlamaCPPTPS computes TPS for a llama.cpp model.
// Priority: /slots n_decoded (real-time during generation) > gauge > delta > cumulative.
func (s *Scrapper) ComputeLlamaCPPTPS(mid string, data map[string]interface{}) map[string]interface{} {
	tokens := i64of(data["tokens"])
	predSeconds := f64of(data["pred_seconds"])
	promptTokens := i64of(data["prompt_tokens"])
	promptSecondsTotal := f64of(data["prompt_seconds_total"])
	gaugeTPS := f64of(data["gauge_tps"])
	gaugePromptTPS := f64of(data["gauge_prompt_tps"])
	running := i64of(data["running"])
	idle := i64of(data["idle"])
	nTokensMax := i64of(data["n_tokens_max"])
	ctxUsed := i64of(data["ctx_used"])
	backend := srof(data["backend"])
	port := i64of(data["port"])

	now := s.nowFn()
	prev, hasPrev := s.prev[mid]

	// Single /slots read per poll: used both for the TPS delta (vs the
	// snapshot stored at the previous poll) and stored for the next one.
	var slotsTPS float64
	var slotsCtxUsed int64
	var curDecoded []float64
	var curProc []bool
	slotsRead := false
	if running > 0 && port > 0 {
		snaps, _ := readSlots(int(port))
		if snaps != nil {
			slotsRead = true
			curDecoded = make([]float64, len(snaps))
			curProc = make([]bool, len(snaps))
			for i, sn := range snaps {
				curDecoded[i] = float64(sn.decoded)
				curProc[i] = sn.processing
				if sn.promptTokens > slotsCtxUsed {
					slotsCtxUsed = sn.promptTokens
				}
			}
			prevDecoded := s.prevSlots[mid]
			prevProc := s.prevSlotsProc[mid]
			prevTs := s.prevSlotsTs[mid]
			if anyProc(curProc) && len(prevDecoded) > 0 && prevTs > 0 {
				// Tokens decoded since the previous poll, summed across ALL
				// slots. Per-slot rules:
				//   slot was processing at prev poll -> delta = nd - prev_nd
				//     (if nd < prev_nd the slot restarted mid-interval; count
				//     what the new request decoded so far)
				//   slot was idle at prev poll -> any tokens it has now were
				//     decoded in this interval -> delta = nd
				dd := 0.0
				for i, nd := range curDecoded {
					var d float64
					if i < len(prevDecoded) && i < len(prevProc) && prevProc[i] {
						d = nd - prevDecoded[i]
						if d < 0 {
							d = nd
						}
					} else {
						d = nd
					}
					if d > 0 {
						dd += d
					}
				}
				dt := now - prevTs
				if dd > 0 && dt > 0.1 { // at least 100ms to avoid noise
					slotsTPS = dd / dt
				}
			}
		}
	}

	deltaTPS := 0.0
	deltaPromptTPS := 0.0
	if hasPrev {
		dt := now - prev["ts"]
		if dt > 0 {
			dTokens := float64(tokens) - prev["tokens"]
			dPredSec := predSeconds - prev["pred_seconds"]
			dPrompt := float64(promptTokens) - prev["prompt_tokens"]
			dPromptSec := promptSecondsTotal - prev["prompt_seconds_total"]
			if dTokens > 0 && dPredSec > 0 {
				deltaTPS = dTokens / dPredSec
			} else if dTokens > 0 {
				deltaTPS = dTokens / dt
			}
			if dPrompt > 0 && dPromptSec > 0 {
				deltaPromptTPS = dPrompt / dPromptSec
			} else if dPrompt > 0 {
				deltaPromptTPS = dPrompt / dt
			}
		}
	}

	// Store slots state for next poll (only on a successful read, so a
	// transient /slots failure does not corrupt the next delta).
	if slotsRead {
		s.prevSlots[mid] = curDecoded
		s.prevSlotsProc[mid] = curProc
		s.prevSlotsTs[mid] = now
	} else {
		delete(s.prevSlots, mid)
		delete(s.prevSlotsProc, mid)
		delete(s.prevSlotsTs, mid)
	}
	// Use slots ctx_used if available (more accurate than metrics)
	if slotsCtxUsed > 0 {
		ctxUsed = slotsCtxUsed
	}

	s.prev[mid] = map[string]float64{
		"ts":                   now,
		"tokens":               float64(tokens),
		"prompt_tokens":        float64(promptTokens),
		"pred_seconds":         predSeconds,
		"prompt_seconds_total": promptSecondsTotal,
		"running":              float64(running),
	}
	cumTPS := 0.0
	if predSeconds > 0 {
		cumTPS = float64(tokens) / predSeconds
	}
	cumPromptTPS := 0.0
	if promptSecondsTotal > 0 {
		cumPromptTPS = float64(promptTokens) / promptSecondsTotal
	}

	// Priority: slots (real-time) > gauge > delta > cumulative
	finalTPS := 0.0
	if slotsTPS > 0 {
		finalTPS = round1(slotsTPS)
	} else if gaugeTPS > 0 {
		finalTPS = round1(gaugeTPS)
	} else if deltaTPS > 0 {
		finalTPS = round1(deltaTPS)
	} else if cumTPS > 0 {
		finalTPS = round1(cumTPS)
	}
	finalPromptTPS := 0.0
	if gaugePromptTPS > 0 {
		finalPromptTPS = round1(gaugePromptTPS)
	} else if deltaPromptTPS > 0 {
		finalPromptTPS = round1(deltaPromptTPS)
	} else if cumPromptTPS > 0 {
		finalPromptTPS = round1(cumPromptTPS)
	}

	return map[string]interface{}{
		"tps":            finalTPS,
		"prompt_tps":     finalPromptTPS,
		"tps_instant":    round1(slotsTPS),
		"tps_cumulative": round1(cumTPS),
		"tokens":         tokens,
		"prompt_tokens":  promptTokens,
		"running":        running,
		"idle":           idle,
		"n_tokens_max":   nTokensMax,
		"ctx_used":       ctxUsed,
		"backend":        backend,
	}
}

func i64of(v interface{}) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

func anyProc(flags []bool) bool {
	for _, f := range flags {
		if f {
			return true
		}
	}
	return false
}

func f64of(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

// F64 is the exported form of f64of: convert a JSON-decoded interface{} to
// float64 (0 when it is not a number). Used to read live scrape fields.
func F64(v interface{}) float64 { return f64of(v) }

// I64 is the exported form of i64of: convert a JSON-decoded interface{} to
// int64 (0 when it is not a number). Used to read live scrape fields.
func I64(v interface{}) int64 { return i64of(v) }

func srof(v interface{}) string {
	if x, ok := v.(string); ok {
		return x
	}
	return ""
}
