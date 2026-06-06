package actions

import (
	"strings"
	"testing"
	"time"
)

// fakeCmd is a scripted sh.Run replacement for cross-platform lifecycle
// tests (A-03). It records every argv and answers from a queue of
// responders (FIFO); an exhausted queue answers (0, "").
type fakeCmd struct {
	calls     []string
	respond   []func(args []string) (int, string)
	responded int
}

func (f *fakeCmd) Run(args []string, _ time.Duration) (int, string) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.responded < len(f.respond) {
		r := f.respond[f.responded]
		f.responded++
		return r(args)
	}
	return 0, ""
}

func (f *fakeCmd) called(substr string) int {
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c, substr) {
			n++
		}
	}
	return n
}

func withFakeCmd(t *testing.T, f *fakeCmd) {
	t.Helper()
	old := runCmd
	runCmd = f.Run
	t.Cleanup(func() { runCmd = old })
}

func TestNativeStartIsIdempotent(t *testing.T) {
	f := &fakeCmd{respond: []func([]string) (int, string){unitState(true)}}
	withFakeCmd(t, f)
	body, code := nativeAction("ai-strata", "start")
	if code != 200 || body["state"] != "running" || f.called("systemctl start") != 0 {
		t.Fatalf("unexpected: %v calls=%v", body, f.calls)
	}
}

func TestNativeStopVerified(t *testing.T) {
	f := &fakeCmd{respond: []func([]string) (int, string){func([]string) (int, string) { return 0, "" }, unitState(true)}}
	withFakeCmd(t, f)
	body, _ := nativeAction("ai-strata", "stop")
	if body["ok"] != false {
		t.Fatal("stop falsely succeeded with active service")
	}
}

// unitState scripts the `systemctl is-active` answer.
func unitState(active bool) func(args []string) (int, string) {
	return func(args []string) (int, string) {
		if active {
			return 0, "active\n"
		}
		return 3, "inactive\n" // systemctl is-active: rc 3 = not active
	}
}

// A-03 matrix: start over the four (unit, container) states.
func TestStartModelMatrix(t *testing.T) {
	cases := []struct {
		name       string
		unitActive bool
		container  string // docker ps output for the model
		wantState  string
		wantOK     bool
		wantCtl    string // expected systemctl verb issued ("" = none)
	}{
		{
			name:       "clean: unit inactive, no container -> start issued",
			unitActive: false,
			container:  "",
			wantState:  "started",
			wantOK:     true,
			wantCtl:    "systemctl start ai-m-a.service",
		},
		{
			name:       "stale unit: active (exited), no container -> restart issued (bare start would be a silent no-op)",
			unitActive: true,
			container:  "",
			wantState:  "restarted",
			wantOK:     true,
			wantCtl:    "systemctl restart ai-m-a.service",
		},
		{
			name:       "outside-systemd: unit inactive, container running -> no systemctl call, reported as running",
			unitActive: false,
			container:  "m-a\n",
			wantState:  "running",
			wantOK:     true,
			wantCtl:    "",
		},
		{
			name:       "running: unit active, container running -> no systemctl call, reported as running",
			unitActive: true,
			container:  "m-a\n",
			wantState:  "running",
			wantOK:     true,
			wantCtl:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeCmd{respond: []func([]string) (int, string){
				func(args []string) (int, string) { // docker ps
					return 0, tc.container
				},
				unitState(tc.unitActive), // systemctl is-active
				func(args []string) (int, string) { // systemctl start/restart
					return 0, ""
				},
			}}
			withFakeCmd(t, f)

			state, ok, _ := startModel("m-a", "ai-m-a", "start")
			if state != tc.wantState || ok != tc.wantOK {
				t.Fatalf("got state=%q ok=%v, want state=%q ok=%v (calls=%v)",
					state, ok, tc.wantState, tc.wantOK, f.calls)
			}
			if tc.wantCtl == "" {
				if n := f.called("systemctl start") + f.called("systemctl restart"); n != 0 {
					t.Fatalf("expected NO systemctl call, got calls=%v", f.calls)
				}
			} else if f.called(tc.wantCtl) != 1 {
				t.Fatalf("expected exactly one %q, got calls=%v", tc.wantCtl, f.calls)
			}
		})
	}
}

// An explicit restart of a RUNNING model must still restart (user intent),
// unlike a start no-op.
func TestRestartWhileRunningStillRestarts(t *testing.T) {
	f := &fakeCmd{respond: []func([]string) (int, string){
		func(args []string) (int, string) { return 0, "m-a\n" }, // docker ps: running
		func(args []string) (int, string) { return 0, "" },      // systemctl restart
	}}
	withFakeCmd(t, f)

	state, ok, _ := startModel("m-a", "ai-m-a", "restart")
	if state != "restarted" || !ok {
		t.Fatalf("got state=%q ok=%v, want restarted/true", state, ok)
	}
	if f.called("systemctl restart ai-m-a.service") != 1 {
		t.Fatalf("expected one restart call, got calls=%v", f.calls)
	}
}

// A failed systemctl call reports state=failed, ok=false.
func TestStartModelFailed(t *testing.T) {
	f := &fakeCmd{respond: []func([]string) (int, string){
		func(args []string) (int, string) { return 0, "" }, // docker ps: none
		unitState(false), // inactive
		func(args []string) (int, string) { return 1, "Job failed" },
	}}
	withFakeCmd(t, f)
	state, ok, _ := startModel("m-a", "ai-m-a", "start")
	if state != "failed" || ok {
		t.Fatalf("got state=%q ok=%v, want failed/false", state, ok)
	}
}

// A-03: stop is idempotent — nothing running, unit inactive -> ok=true and
// NO docker calls at all (no false work, no false no-op).
func TestStopModelIdempotent(t *testing.T) {
	f := &fakeCmd{respond: []func([]string) (int, string){
		func(args []string) (int, string) { return 0, "" }, // systemctl stop
		func(args []string) (int, string) { return 0, "" }, // docker ps (verify)
	}}
	withFakeCmd(t, f)

	ok, _ := stopModel("m-a", "ai-m-a")
	if !ok {
		t.Fatalf("ok=false, want true (idempotent stop)")
	}
	if n := f.called("docker stop") + f.called("docker rm"); n != 0 {
		t.Fatalf("docker calls made on empty stop: %v", f.calls)
	}
}

// A-03: container started outside systemd (docker compose) -> the
// systemctl stop is a no-op, the docker fallback actually stops+removes it,
// and the final verification passes.
func TestStopModelDockerFallback(t *testing.T) {
	f := &fakeCmd{respond: []func([]string) (int, string){
		func(args []string) (int, string) { return 0, "" },      // systemctl stop (no-op)
		func(args []string) (int, string) { return 0, "m-a\n" }, // docker ps: found
		func(args []string) (int, string) { return 0, "" },      // docker stop
		func(args []string) (int, string) { return 0, "" },      // docker rm
		func(args []string) (int, string) { return 0, "" },      // docker ps: gone
	}}
	withFakeCmd(t, f)

	ok, out := stopModel("m-a", "ai-m-a")
	if !ok {
		t.Fatalf("ok=false, want true (out=%q)", out)
	}
	if f.called("docker stop --time 20 m-a") != 1 || f.called("docker rm -f m-a") != 1 {
		t.Fatalf("expected docker stop+rm, got calls=%v", f.calls)
	}
}

// A-03: if the container survives docker stop (wedge), stop reports failure
// with the still-running name — never a silent ok.
func TestStopModelStillRunning(t *testing.T) {
	f := &fakeCmd{respond: []func([]string) (int, string){
		func(args []string) (int, string) { return 0, "" },      // systemctl stop
		func(args []string) (int, string) { return 0, "m-a\n" }, // docker ps
		func(args []string) (int, string) { return 137, "" },    // docker stop fails
		func(args []string) (int, string) { return 0, "" },      // docker rm
		func(args []string) (int, string) { return 0, "m-a\n" }, // still there
	}}
	withFakeCmd(t, f)

	ok, out := stopModel("m-a", "ai-m-a")
	if ok {
		t.Fatalf("ok=true, want false")
	}
	if !strings.Contains(out, "still running: m-a") {
		t.Fatalf("out=%q, want 'still running: m-a'", out)
	}
}
