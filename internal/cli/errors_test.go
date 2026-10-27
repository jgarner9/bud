package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantStderr string
	}{
		{"success", nil, 0, ""},
		{"help", flag.ErrHelp, 0, ""},
		{"failure", errors.New("boom"), ExitFailure, "bud: boom\n"},
		{"wrapped failure", fmt.Errorf("installing rg: %w", errors.New("exit status 1")), ExitFailure, "bud: installing rg: exit status 1\n"},
		{"usage", usageErrorf("bad"), ExitUsage, "bud: bad\nrun 'bud --help' for usage\n"},
		{"usage already printed", &UsageError{Err: errors.New("bad flag"), Printed: true}, ExitUsage, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := ExitCode(tt.err, &stderr); code != tt.wantCode {
				t.Errorf("code = %d, want %d", code, tt.wantCode)
			}
			if got := stderr.String(); got != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got, tt.wantStderr)
			}
		})
	}
}

func TestAppRunParseErrors(t *testing.T) {
	cmds := map[string]*Command{"open": (&recorder{}).cmd("open")}

	tests := []struct {
		name string
		args []string
	}{
		{"global flag", []string{"-nope"}},
		{"command flag", []string{"open", "-nope"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := (&App{Cmds: cmds, Args: tt.args, Stdout: &stdout, Stderr: &stderr}).Run()
			var usage *UsageError
			if !errors.As(err, &usage) || !usage.Printed {
				t.Fatalf("err = %#v, want printed UsageError", err)
			}
			if !strings.HasPrefix(stderr.String(), "flag provided but not defined: -nope\nUsage: bud ") {
				t.Errorf("stderr = %q, want the error then usage", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
		})
	}
}

func TestAppRunHelp(t *testing.T) {
	cmds := map[string]*Command{"open": (&recorder{}).cmd("open")}

	tests := []struct {
		name      string
		args      []string
		wantUsage string
	}{
		{"global", []string{"-h"}, "Usage: bud [global flags] <command> [command flags] [args]\n"},
		{"global long", []string{"--help"}, "Usage: bud [global flags] <command> [command flags] [args]\n"},
		{"command", []string{"open", "-h"}, "Usage: bud [global flags] open [flags] [args]\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := (&App{Cmds: cmds, Args: tt.args, Stdout: &stdout, Stderr: &stderr}).Run()
			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want flag.ErrHelp", err)
			}
			if !strings.HasPrefix(stdout.String(), tt.wantUsage) {
				t.Errorf("stdout = %q, want it to start with %q", stdout.String(), tt.wantUsage)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want nothing", stderr.String())
			}
		})
	}
}

func TestOpenUsageErrors(t *testing.T) {
	var usage *UsageError
	if err := open(&Globals{}, []string{"a", "b"}); !errors.As(err, &usage) {
		t.Errorf("two paths: err = %v, want UsageError", err)
	}
	if _, err := resolveWorkspace(filepath.Join(t.TempDir(), "nope")); !errors.As(err, &usage) {
		t.Errorf("missing path: err = %v, want UsageError", err)
	}
}
