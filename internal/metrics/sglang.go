package metrics

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSGLang reads the native generation gauge rather than dividing token
// totals by uptime. Streamed/non-streamed counters are disjoint and summed;
// TP/EP worker gauges use max so replicated ranks do not multiply the rate.
func ParseSGLang(text string) (map[string]interface{}, error) {
	values := map[string]float64{}
	found := false
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "sglang:") {
			continue
		}
		name := strings.SplitN(fields[0], "{", 2)[0]
		switch name {
		case "sglang:generation_tokens_total", "sglang:prompt_tokens_total", "sglang:gen_throughput", "sglang:num_running_reqs", "sglang:num_queue_reqs":
		default:
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, err
		}
		found = true
		if strings.HasSuffix(name, "_total") {
			values[name] += value
		} else if value > values[name] {
			values[name] = value
		}
	}
	if !found {
		return nil, fmt.Errorf("missing SGLang metrics")
	}
	return map[string]interface{}{
		"backend": "SGLang", "tps": round1(values["sglang:gen_throughput"]),
		"tokens":        int(values["sglang:generation_tokens_total"]),
		"prompt_tokens": int(values["sglang:prompt_tokens_total"]),
		"running":       int(values["sglang:num_running_reqs"]),
		"waiting":       int(values["sglang:num_queue_reqs"]),
	}, nil
}
