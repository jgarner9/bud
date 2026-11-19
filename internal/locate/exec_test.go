package locate

import (
	"strings"
	"testing"
)

func TestExecRunnerCapturesStderrAndExit(t *testing.T) {
	out, err := ExecRunner("sh", "-c", "printf stdout; printf stderr >&2; exit 2")
	if err != nil || string(out.Stdout) != "stdout" || out.Stderr != "stderr" || out.Exit != 2 || out.Truncated {
		t.Fatalf("got %+v, %v", out, err)
	}
}

func TestExecRunnerCapsOutput(t *testing.T) {
	out, err := ExecRunner("sh", "-c", "dd if=/dev/zero bs=1048576 count=35")
	if err != nil || len(out.Stdout) != MaxOutput || !out.Truncated {
		t.Fatalf("got %d bytes, truncated = %v, error = %v", len(out.Stdout), out.Truncated, err)
	}
}

func TestExecRunnerReportsStartFailure(t *testing.T) {
	_, err := ExecRunner("/a/nonexistent/locate-helper")
	if err == nil || !strings.Contains(err.Error(), "locate-helper") {
		t.Fatalf("got %v", err)
	}
}
