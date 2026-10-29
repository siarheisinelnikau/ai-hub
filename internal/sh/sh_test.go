package sh

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// shell runs a program the way this platform's shell would: `cmd /c` on
// Windows, `sh -c` elsewhere. Keeps the tests meaningful on both the dev
// box (Windows) and the production box (Linux) instead of silently
// requiring one of them.
func shell(script string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", script}
	}
	return []string{"sh", "-c", script}
}

func TestShNotFound(t *testing.T) {
	rc, out := Run([]string{"definitely-not-a-cmd-xyz-123"}, 5*time.Second)
	if rc != 127 {
		t.Errorf("rc = %d, want 127", rc)
	}
	if !strings.HasPrefix(out, "not found: ") {
		t.Errorf("out = %q, want prefix %q", out, "not found: ")
	}
}

func TestShTruncation(t *testing.T) {
	var script string
	if runtime.GOOS == "windows" {
		script = `for /L %i in (1,1,2000) do @echo xxxxxxxx`
	} else {
		script = `yes xxxxxxxx | head -n 2000`
	}
	rc, out := Run(shell(script), 30*time.Second)
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if len(out) != 8000 {
		t.Errorf("len(out) = %d, want 8000", len(out))
	}
}

func TestShTimeout(t *testing.T) {
	var script string
	if runtime.GOOS == "windows" {
		script = `ping -n 4 127.0.0.1`
	} else {
		script = `sleep 3`
	}
	rc, out := Run(shell(script), 800*time.Millisecond)
	if rc != 124 {
		t.Errorf("rc = %d, want 124", rc)
	}
	if out != "TIMEOUT" {
		t.Errorf("out = %q, want %q", out, "TIMEOUT")
	}
}

func TestShCombinedJoin(t *testing.T) {
	rc, out := Run(shell("echo hello"), 5*time.Second)
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.HasPrefix(out, "hello") {
		t.Errorf("out = %q, want prefix %q", out, "hello")
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("out = %q, want suffix %q", out, "\n")
	}
	if len(out) >= 10 {
		t.Errorf("len(out) = %d, want < 10", len(out))
	}
}
