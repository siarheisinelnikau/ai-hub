package metrics

import "testing"

func TestParseStrata(t *testing.T) {
	data, err := ParseStrata([]byte(`{"engine":{"max_context":131072},"live":{"state":"generating","tok_s":93.5,"prompt_tokens":1200,"generated":200,"queued":1},"totals":{"output_tokens":1000,"prompt_tokens":8000,"decode_ms":20000,"prompt_ms":4000}}`))
	if err != nil {
		t.Fatal(err)
	}
	if data["tps"] != 93.5 || data["prompt_tps"] != float64(2000) || data["ctx_used"] != 1400 || data["running"] != 1 {
		t.Fatalf("wrong metrics: %#v", data)
	}
	idle, err := ParseStrata([]byte(`{"engine":{"max_context":131072},"live":{"state":"idle","tok_s":null},"totals":{"output_tokens":1000,"decode_ms":20000}}`))
	if err != nil || idle["tps"] != float64(50) || idle["running"] != 0 {
		t.Fatalf("wrong idle metrics: %#v %v", idle, err)
	}
	if _, err := ParseStrata([]byte(`{}`)); err == nil {
		t.Fatal("accepted non-Strata payload")
	}
}
