package actions

import (
	"encoding/json"
	"os"
	"runtime"
	"testing"

	"aihub/internal/registry"
)

// testDeps builds a Deps with one known model (m-a, sysd ai-m-a) and no
// services — the current registry shape.
func testDeps() *Deps {
	return &Deps{
		Reg: &registry.File{
			Models: []registry.Model{
				{ID: "m-a", Name: "A", Port: 18021, Sysd: "ai-m-a"},
			},
		},
	}
}

// requireWindows skips tests whose assertions depend on the os.name == "nt"
// mock branch (they are deterministic only on this Windows dev box).
func requireWindows(t *testing.T) {
	t.Helper()
	if !isWindows() {
		t.Skip("windows-only mock branch; skipping on " + runtime.GOOS)
	}
}

func TestModelActionWindowsMock(t *testing.T) {
	requireWindows(t)
	d := testDeps()
	body, status := d.ModelAction("m-a", "start")
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("body[ok] = %v, want true", body["ok"])
	}
	if out, _ := body["output"].(string); out != "mock start m-a on Windows dev" {
		t.Fatalf("body[output] = %q, want %q", out, "mock start m-a on Windows dev")
	}
}

func TestModelActionUnknown(t *testing.T) {
	d := testDeps()
	body, status := d.ModelAction("nope", "start")
	if status != 404 {
		t.Fatalf("status = %d, want 404", status)
	}
	if detail, _ := body["detail"].(string); detail != "unknown model: nope" {
		t.Fatalf("body[detail] = %q, want %q", detail, "unknown model: nope")
	}
}

func TestServiceActionWindowsModel(t *testing.T) {
	requireWindows(t)
	d := testDeps()
	body, status := d.ServiceAction("m-a", "restart")
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("body[ok] = %v, want true", body["ok"])
	}
	if out, _ := body["output"].(string); out != "mock restart m-a" {
		t.Fatalf("body[output] = %q, want %q", out, "mock restart m-a")
	}
}

func TestServiceActionUnknown(t *testing.T) {
	d := testDeps()
	body, status := d.ServiceAction("zzz", "start")
	if status != 404 {
		t.Fatalf("status = %d, want 404", status)
	}
	if detail, _ := body["detail"].(string); detail != "unknown service/model: zzz" {
		t.Fatalf("body[detail] = %q, want %q", detail, "unknown service/model: zzz")
	}
}

func TestServiceBuildWindowsMock(t *testing.T) {
	requireWindows(t)
	d := testDeps()
	body, status := d.ServiceBuild("m-a")
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("body[ok] = %v, want true", body["ok"])
	}
	if out, _ := body["output"].(string); out != "mock build on Windows" {
		t.Fatalf("body[output] = %q, want %q", out, "mock build on Windows")
	}
}

func TestServiceBuildUnknown(t *testing.T) {
	d := testDeps()
	body, status := d.ServiceBuild("zzz")
	if status != 404 {
		t.Fatalf("status = %d, want 404", status)
	}
	if detail, _ := body["detail"].(string); detail != "unknown service" {
		t.Fatalf("body[detail] = %q, want %q", detail, "unknown service")
	}
}

func TestApplyProfileWindowsMock(t *testing.T) {
	requireWindows(t)
	d := testDeps()
	body, status := d.ApplyProfile("whatever")
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("body[ok] = %v, want true", body["ok"])
	}
	if out, _ := body["output"].(string); out != "mock profile apply on Windows" {
		t.Fatalf("body[output] = %q, want %q", out, "mock profile apply on Windows")
	}
}

func TestLogsWindows(t *testing.T) {
	requireWindows(t)
	d := testDeps()
	body, status := d.Logs("m-a", 200)
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	want := "mock logs for m-a on Windows dev\nunit: ai-m-a\nno logs"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestLogsWindowsUnknown(t *testing.T) {
	requireWindows(t)
	d := testDeps()
	body, status := d.Logs("zzz", 200)
	// unit defaults to "ai-zzz"; the .service file does not exist, so the
	// generic branch 404s. The HTTP handler builds {"detail":"unknown
	// model/service: zzz"} from this status.
	if status != 404 {
		t.Fatalf("status = %d, want 404", status)
	}
	if body != "" {
		t.Fatalf("body = %q, want empty on 404", body)
	}
}

func TestDownloadsWindows(t *testing.T) {
	requireWindows(t)
	d := testDeps()
	body, status := d.Downloads()
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if body != "mock downloads on Windows" {
		t.Fatalf("body = %q, want %q", body, "mock downloads on Windows")
	}
}

func TestParseLaunchParams(t *testing.T) {
	cases := []struct {
		g, c, p  string
		wg, wc, wp int
	}{
		{"", "", "", 0, 0, 0},
		{"2", "131072", "4", 2, 131072, 4},
		{"1", "65536", "", 1, 65536, 0},
		{"-1", "-2", "-3", 0, 0, 0}, // negatives clamp to 0
		{"abc", "xyz", "zz", 0, 0, 0},
	}
	for _, tc := range cases {
		g, c, p := ParseLaunchParams(tc.g, tc.c, tc.p)
		if g != tc.wg || c != tc.wc || p != tc.wp {
			t.Errorf("ParseLaunchParams(%q,%q,%q) = %d %d %d, want %d %d %d",
				tc.g, tc.c, tc.p, g, c, p, tc.wg, tc.wc, tc.wp)
		}
	}
}

func TestWriteState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIHUB_STATE_DIR", dir)

	writeState("m-a", 2, 131072, 4)
	b, err := os.ReadFile(dir + "/m-a.json")
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]interface{}
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	if st["gpus"] != float64(2) || st["ctx"] != float64(131072) || st["parallel"] != float64(4) {
		t.Errorf("state = %v, want gpus=2 ctx=131072 parallel=4", st)
	}

	// Zero fields are not written (preserve existing values).
	writeState("m-a", 0, 0, 0)
	b, _ = os.ReadFile(dir + "/m-a.json")
	_ = json.Unmarshal(b, &st)
	if st["gpus"] != float64(2) || st["ctx"] != float64(131072) || st["parallel"] != float64(4) {
		t.Errorf("state after zero-write = %v, want unchanged", st)
	}

	// Partial update (only parallel) merges.
	writeState("m-a", 0, 0, 8)
	b, _ = os.ReadFile(dir + "/m-a.json")
	_ = json.Unmarshal(b, &st)
	if st["parallel"] != float64(8) || st["gpus"] != float64(2) {
		t.Errorf("state after partial = %v, want parallel=8 gpus=2", st)
	}
}

// stopAllDeps builds the StopAll fixture: 3 LLMs (llm-a, llm-b, llm-c)
// and one media model (media-x) in this file order.
func stopAllDeps() *Deps {
	return &Deps{
		Reg: &registry.File{
			Models: []registry.Model{
				{ID: "llm-a", Category: "LLM", Sysd: "ai-llm-a"},
				{ID: "media-x", Category: "media", Sysd: "ai-media-x"},
				{ID: "llm-b", Category: "LLM", Sysd: "ai-llm-b"},
				{ID: "llm-c", Category: "LLM", Sysd: "ai-llm-c"},
			},
		},
	}
}

func TestSelectRunningLLMs(t *testing.T) {
	cases := []struct {
		name    string
		running map[string]bool
		want    []string
	}{
		{
			name:    "2 of 3 LLMs running; running media model skipped",
			running: map[string]bool{"llm-a": true, "llm-b": true, "media-x": true},
			want:    []string{"llm-a", "llm-b"},
		},
		{
			name:    "nothing running",
			running: map[string]bool{},
			want:    []string{},
		},
		{
			name:    "only non-LLM running",
			running: map[string]bool{"media-x": true},
			want:    []string{},
		},
		{
			name:    "all LLMs running, registry order kept",
			running: map[string]bool{"llm-c": true, "llm-a": true, "llm-b": true, "media-x": true},
			want:    []string{"llm-a", "llm-b", "llm-c"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectRunningLLMs(stopAllDeps().Reg, func(id string) bool { return tc.running[id] })
			if len(got) != len(tc.want) {
				t.Fatalf("selectRunningLLMs = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("selectRunningLLMs = %v, want %v", got, tc.want)
				}
			}
		})
	}

	// nil guards
	if got := selectRunningLLMs(nil, func(string) bool { return true }); len(got) != 0 {
		t.Fatalf("selectRunningLLMs(nil reg) = %v, want empty", got)
	}
	if got := selectRunningLLMs(stopAllDeps().Reg, nil); len(got) != 0 {
		t.Fatalf("selectRunningLLMs(nil statusFn) = %v, want empty", got)
	}
}

func TestStopAllWindows(t *testing.T) {
	requireWindows(t)
	d := stopAllDeps()
	// llm-a and llm-b are running; llm-c is idle; media-x is running but
	// not an LLM -> skipped. The Windows mock stop always succeeds.
	stopped, errs := d.StopAll(func(id string) bool {
		return id == "llm-a" || id == "llm-b" || id == "media-x"
	})
	if len(stopped) != 2 || stopped[0] != "llm-a" || stopped[1] != "llm-b" {
		t.Fatalf("stopped = %v, want [llm-a llm-b]", stopped)
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want empty", errs)
	}
}

func TestStopAllNothingRunning(t *testing.T) {
	// Cross-platform: empty selection -> empty, NON-NIL results (JSON []
	// and {}, never null).
	d := stopAllDeps()
	stopped, errs := d.StopAll(func(string) bool { return false })
	if stopped == nil || len(stopped) != 0 {
		t.Fatalf("stopped = %v (nil=%v), want non-nil empty", stopped, stopped == nil)
	}
	if errs == nil || len(errs) != 0 {
		t.Fatalf("errs = %v (nil=%v), want non-nil empty", errs, errs == nil)
	}
}

func TestStopAllNilGuards(t *testing.T) {
	d := &Deps{} // nil Reg
	stopped, errs := d.StopAll(func(string) bool { return true })
	if stopped == nil || len(stopped) != 0 {
		t.Fatalf("stopped = %v, want non-nil empty", stopped)
	}
	if errs == nil || len(errs) != 0 {
		t.Fatalf("errs = %v, want non-nil empty", errs)
	}
}

func TestBodyMessage(t *testing.T) {
	cases := []struct {
		body map[string]interface{}
		want string
	}{
		{map[string]interface{}{"output": "ai-x.service not installed"}, "ai-x.service not installed"},
		{map[string]interface{}{"detail": "unknown model: z"}, "unknown model: z"},
		{map[string]interface{}{"ok": true, "output": "out", "detail": "det"}, "out"},
		{map[string]interface{}{"ok": false}, ""},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := bodyMessage(tc.body); got != tc.want {
			t.Errorf("bodyMessage(%v) = %q, want %q", tc.body, got, tc.want)
		}
	}
}

func TestParseDockerNames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"\n", nil},
		{"comfyui", []string{"comfyui"}},
		{"voice\n", []string{"voice"}},
		{"voice\nmusic\n\n", []string{"voice", "music"}},
		{"  voice  \n music ", []string{"voice", "music"}},
	}
	for _, tc := range cases {
		got := parseDockerNames(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parseDockerNames(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("parseDockerNames(%q) = %v, want %v", tc.in, got, tc.want)
			}
		}
	}
}

func TestLastRunes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 3, "llo"},
		{"hi", 5, "hi"},
		{"", 3, ""},
		{"абвг", 2, "вг"}, // rune-based, not byte-based
	}
	for _, c := range cases {
		if got := lastRunes(c.in, c.n); got != c.want {
			t.Errorf("lastRunes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
