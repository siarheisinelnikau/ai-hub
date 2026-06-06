package metrics

import "testing"

func TestParseSGLangRankAndStreamingLabels(t *testing.T) {
	data, err := ParseSGLang(`# ignored
sglang:generation_tokens_total{is_streaming="true"} 512
sglang:generation_tokens_total{is_streaming="false"} 8
sglang:prompt_tokens_total{is_streaming="true"} 20400
sglang:gen_throughput{tp_rank="0"} 76.25
sglang:gen_throughput{tp_rank="1"} 76.25
sglang:num_running_reqs{tp_rank="0"} 1
sglang:num_running_reqs{tp_rank="1"} 1
sglang:num_queue_reqs{tp_rank="0"} 2
`)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]interface{}{"tps": 76.3, "tokens": 520, "prompt_tokens": 20400, "running": 1, "waiting": 2, "backend": "SGLang"} {
		if data[key] != want {
			t.Errorf("%s = %v, want %v", key, data[key], want)
		}
	}
	for _, invalid := range []string{"", "llamacpp:tokens_predicted_total 10", "sglang:gen_throughput invalid"} {
		if _, err := ParseSGLang(invalid); err == nil {
			t.Fatalf("expected error for %q", invalid)
		}
	}
}
