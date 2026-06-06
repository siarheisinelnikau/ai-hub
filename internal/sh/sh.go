// Package sh runs external commands mirroring app.py sh() semantics.
package sh

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// Run executes args with a timeout and returns (rc, out) where
// out = (stdout + "\n" + stderr) truncated to the LAST 8000 runes.
//
//   - context timeout      -> (124, "TIMEOUT")
//   - exec.ErrNotFound     -> (127, "not found: " + err.Error())
//   - other start error    -> (1, err.Error())
//   - success              -> (process exit code, combined)
func Run(args []string, timeout time.Duration) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return 124, "TIMEOUT"
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return 127, "not found: " + err.Error()
		}
		return 1, err.Error()
	}

	combined := out.String() + "\n" + errOut.String()
	rs := []rune(combined)
	if len(rs) > 8000 {
		rs = rs[len(rs)-8000:]
	}
	rc := 0
	if cmd.ProcessState != nil {
		rc = cmd.ProcessState.ExitCode()
	}
	return rc, string(rs)
}
