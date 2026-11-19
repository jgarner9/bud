package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/locate"
)

func TestLocateCommand(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed")
	}
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"def.go": "package sample\nfunc FindMe() {}\n",
		"use.go": "package sample\nfunc use() { FindMe() }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	var stdout, stderr strings.Builder
	app := &App{
		Cmds: CommandRegistry, Args: []string{"locate", "--json", "--max", "1", "--max-chars", "5", "FindMe"},
		Stdout: &stdout, Stderr: &stderr,
	}
	if code := ExitCode(app.Run(), &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	var result locate.Result
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, &stdout)
	}
	if result.Total != 2 || result.Cut != 1 || len(result.Hits) != 1 || result.Hits[0].File != "def.go" ||
		result.Hits[0].Line != 2 || result.Hits[0].Kind != locate.KindDef {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestLocateCommandUsageErrors(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"locate"}, "no query"},
		{[]string{"locate", "--kind", "def", "a phrase"}, "needs a symbol"},
		{[]string{"locate", "--lang", "unknown", "symbol"}, "not supported"},
		{[]string{"locate", "--max", "-1", "symbol"}, "cannot be negative"},
	} {
		var stderr strings.Builder
		app := &App{Cmds: CommandRegistry, Args: tt.args, Stderr: &stderr}
		if code := ExitCode(app.Run(), &stderr); code != ExitUsage || !strings.Contains(stderr.String(), tt.want) {
			t.Errorf("%v: exit %d, stderr %q", tt.args, code, &stderr)
		}
	}
}
