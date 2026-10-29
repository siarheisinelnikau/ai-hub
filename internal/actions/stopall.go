package actions

import (
	"fmt"

	"aihub/internal/registry"
)

// StopAll stops every running LLM model in one call (Go extension —
// app.py has no stop-all endpoint, so no parity constraint). It iterates
// the registry in file order, picks Category=="LLM" models that statusFn
// reports as running, and stops each through the exact same path a single
// model stop takes (ModelAction "stop" -> sudo -n systemctl stop, 300s).
// A per-model failure does NOT abort the loop: it is collected in the
// returned errors map and the remaining models are still stopped.
//
// Both return values are always non-nil, so the JSON response marshals
// them to [] and {} (never null) when empty:
//
//	{"ok":true,"stopped":["m-a","m-b"],"errors":{"m-c":"<message>"}}
func (d *Deps) StopAll(statusFn func(string) bool) (stopped []string, errs map[string]string) {
	stopped = []string{}
	errs = map[string]string{}
	for _, mid := range selectRunningLLMs(d.Reg, statusFn) {
		body, code := d.ModelAction(mid, "stop")
		if code == 200 {
			if ok, _ := body["ok"].(bool); ok {
				stopped = append(stopped, mid)
				continue
			}
		}
		if msg := bodyMessage(body); msg != "" {
			errs[mid] = msg
		} else {
			errs[mid] = fmt.Sprintf("stop failed (HTTP %d)", code)
		}
	}
	return stopped, errs
}

// selectRunningLLMs returns the ids of Category=="LLM" models that
// statusFn reports as running, in registry (file) order. Pure: no I/O,
// no exec — the running predicate is injected, which keeps the filter
// testable on any platform.
func selectRunningLLMs(reg *registry.File, statusFn func(string) bool) []string {
	out := []string{}
	if reg == nil || statusFn == nil {
		return out
	}
	for _, m := range reg.Models {
		if m.Category == "LLM" && statusFn(m.ID) {
			out = append(out, m.ID)
		}
	}
	return out
}

// bodyMessage extracts a human-readable failure reason from a ModelAction
// response body ("output" on 200, "detail" on 4xx), or "" if neither is
// set.
func bodyMessage(body map[string]interface{}) string {
	if s, ok := body["output"].(string); ok && s != "" {
		return s
	}
	if s, ok := body["detail"].(string); ok && s != "" {
		return s
	}
	return ""
}
