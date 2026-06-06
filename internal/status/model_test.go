package status

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"aihub/internal/registry"
)

func TestBuildPortToIDs(t *testing.T) {
	reg := &registry.File{
		Models: []registry.Model{
			{ID: "A", Port: 18099},
			{ID: "B", Port: 18099},
			{ID: "C", Port: 18098},
		},
		Ports: map[string]int{"a-x": 18097, "C": 18098},
	}
	r := BuildPortToIDs(reg)

	if !reflect.DeepEqual(r[18099], []string{"A", "B"}) {
		t.Errorf("r[18099] = %v, want [A B]", r[18099])
	}
	// Python double-appends: model C from the models loop + "C" from the ports map.
	if !reflect.DeepEqual(r[18098], []string{"C", "C"}) {
		t.Errorf("r[18098] = %v, want [C C]", r[18098])
	}
	if !reflect.DeepEqual(r[18097], []string{"a-x"}) {
		t.Errorf("r[18097] = %v, want [a-x]", r[18097])
	}
	if len(r) != 3 {
		t.Errorf("len(r) = %d, want 3 (ports 18099, 18098, 18097)", len(r))
	}
}

func TestModelStatusWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only: model_status windows mock branch")
	}
	reg := &registry.File{Models: []registry.Model{
		{ID: "m-a", Name: "A", Port: 18099, Sysd: "ai-m-a", Category: "LLM"},
		{ID: "m-b", Name: "B", Port: 18099, Sysd: "ai-m-b", Category: "LLM"},
		{ID: "m-c", Name: "C", Port: 18098, Sysd: "", Category: "LLM"},
	}}
	r := BuildPortToIDs(reg)

	st := ModelStatus(reg.Models[0], r)
	if !st.Enabled {
		t.Errorf("st.Enabled = false, want true (port defined on windows)")
	}
	if st.Running {
		t.Errorf("st.Running = true, want false (windows mock)")
	}
	if st.Healthy {
		t.Errorf("st.Healthy = true, want false (windows mock)")
	}
	if !st.PortConflict {
		t.Errorf("st.PortConflict = false, want true (shares 18099 with m-b)")
	}
	if !reflect.DeepEqual(st.GroupSiblings, []string{"m-b"}) {
		t.Errorf("st.GroupSiblings = %v, want [m-b]", st.GroupSiblings)
	}
	if st.Sysd != "ai-m-a" {
		t.Errorf("st.Sysd = %q, want ai-m-a", st.Sysd)
	}
	if st.Short != "A" {
		t.Errorf("st.Short = %q, want A (falls back to name)", st.Short)
	}

	st2 := ModelStatus(reg.Models[2], r)
	if st2.Sysd != "ai-m-c" {
		t.Errorf("st2.Sysd = %q, want ai-m-c (default unit)", st2.Sysd)
	}
	if st2.PortConflict {
		t.Errorf("st2.PortConflict = true, want false (18098 only has C itself)")
	}
	if len(st2.GroupSiblings) != 0 {
		t.Errorf("st2.GroupSiblings = %v, want empty", st2.GroupSiblings)
	}

	st3 := ModelStatus(registry.Model{ID: "no-port", Name: "N"}, r)
	if st3.Enabled {
		t.Errorf("st3.Enabled = true, want false (Port==0 on windows)")
	}
}

func TestModelStatusJSON(t *testing.T) {
	reg := &registry.File{Models: []registry.Model{
		{ID: "m-a", Name: "A", Port: 18099, Sysd: "ai-m-a", Category: "LLM"},
		{ID: "m-b", Name: "B", Port: 18099, Sysd: "ai-m-b", Category: "LLM"},
		{ID: "m-c", Name: "C", Port: 18098, Sysd: "", Category: "LLM"},
	}}
	st := ModelStatus(reg.Models[2], BuildPortToIDs(reg))

	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"group_siblings":[]`,
		`"in":[]`,
		`"out":[]`,
		`"sub":[]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON %s: missing %s (must be [], not null)", s, want)
		}
	}
}

func TestModelStatusSubJSON(t *testing.T) {
	r := BuildPortToIDs(&registry.File{})

	// Voice-style sub with no HF: the hf key must be absent.
	m := registry.Model{
		ID: "voice", Name: "Voice", Port: 18110, Category: "media",
		Sub: []registry.SubModel{{Name: "Kokoro TTS", Tag: "tts", VRAM: "~1 GB"}},
	}
	st := ModelStatus(m, r)
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	const wantSub = `"sub":[{"name":"Kokoro TTS","tag":"tts","vram":"~1 GB"}]`
	if !strings.Contains(s, wantSub) {
		t.Errorf("JSON %s: missing %s", s, wantSub)
	}
	if strings.Contains(s, `"hf":`) {
		t.Errorf("JSON %s: unexpected hf key for sub without HF", s)
	}

	// Sub with HF set: the hf key must be present with the value.
	m2 := registry.Model{
		ID: "comfy", Name: "Comfy", Port: 18100, Category: "media",
		Sub: []registry.SubModel{{Name: "FLUX", Tag: "img", VRAM: "16 GB", HF: "black-forest-labs/FLUX"}},
	}
	st2 := ModelStatus(m2, r)
	b2, err := json.Marshal(st2)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(b2), `"hf":"black-forest-labs/FLUX"`) {
		t.Errorf("JSON %s: missing hf value", string(b2))
	}
}

func TestReadLaunchState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIHUB_STATE_DIR", dir)

	// Missing file -> all zeros.
	if g, c, p := readLaunchState("nope"); g != 0 || c != 0 || p != 0 {
		t.Errorf("readLaunchState(nope) = %d %d %d, want 0 0 0", g, c, p)
	}

	// Full state file.
	if err := os.WriteFile(dir+"/m-a.json", []byte(`{"gpus": 2, "ctx": 131072, "parallel": 4}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, c, p := readLaunchState("m-a"); g != 2 || c != 131072 || p != 4 {
		t.Errorf("readLaunchState(m-a) = %d %d %d, want 2 131072 4", g, c, p)
	}

	// Partial (legacy gpus+ctx only).
	if err := os.WriteFile(dir+"/m-b.json", []byte(`{"gpus": 4, "ctx": 262144}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, c, p := readLaunchState("m-b"); g != 4 || c != 262144 || p != 0 {
		t.Errorf("readLaunchState(m-b) = %d %d %d, want 4 262144 0", g, c, p)
	}

	// Bad JSON -> zeros, no panic.
	if err := os.WriteFile(dir+"/m-c.json", []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, c, p := readLaunchState("m-c"); g != 0 || c != 0 || p != 0 {
		t.Errorf("readLaunchState(m-c) = %d %d %d, want 0 0 0", g, c, p)
	}

	// String numbers (hand-edited state) are tolerated.
	if err := os.WriteFile(dir+"/m-d.json", []byte(`{"gpus": "3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, _, _ := readLaunchState("m-d"); g != 3 {
		t.Errorf("readLaunchState(m-d) gpus = %d, want 3", g)
	}
}

func TestHealthCheck(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", ts.URL, err)
	}
	portStr := u.Port()
	if portStr == "" {
		t.Fatalf("no port in test server URL %q", ts.URL)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("strconv.Atoi(%q): %v", portStr, err)
	}
	if !HealthCheck(port) {
		t.Errorf("HealthCheck(%d) = false, want true (live test server)", port)
	}

	// Closed port: every probe fails -> false.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port2 := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if HealthCheck(port2) {
		t.Errorf("HealthCheck(%d) = true, want false (closed port)", port2)
	}
}

func TestReadGPUIds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIHUB_STATE_DIR", dir)
	for _, payload := range []string{`{"gpu_ids":[1,2,3]}`, `{"gpu_ids":[1,1,3]}`, `{"gpu_ids":[1,2]}`} {
		if err := os.WriteFile(dir+"/strata.json", []byte(payload), 0o644); err != nil {
			t.Fatal(err)
		}
		got := readGPUIds("strata", 3)
		if payload == `{"gpu_ids":[1,2,3]}` {
			if got[0] != 1 || got[2] != 3 {
				t.Fatalf("wrong ids: %v", got)
			}
		} else if got[0] != 0 || got[2] != 2 {
			t.Fatalf("invalid ids not rejected: %v", got)
		}
	}
}

func TestHealthCheckUnloaded(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Write([]byte(`{"status":"ok","loaded":false}`))
			return
		}
		w.Write([]byte(`{"object":"list"}`))
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	if HealthCheck(port) {
		t.Fatal("unloaded Strata reported healthy through root/models fallback")
	}
}
