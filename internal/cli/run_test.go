package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/run"
)

func TestRunCommandExitAndLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout, stderr strings.Builder
	app := &App{Cmds: CommandRegistry, Args: []string{"run", "--json", "--", "sh", "-c", "echo error: failed; exit 7"}, Stdout: &stdout, Stderr: &stderr}
	if code := ExitCode(app.Run(), &stderr); code != 7 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	var result run.Result
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Log)
	if err != nil || !strings.Contains(string(data), "error: failed") {
		t.Fatalf("log %q: %v", data, err)
	}
	info, err := os.Stat(result.Log)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("log permissions: %v, %v", info, err)
	}
}
