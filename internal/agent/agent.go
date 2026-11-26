// Package agent knows how bud runs OpenCode, the one agent bud talks to over
// the Agent Client Protocol (Suite D139, BASE D154): how it starts, and
// whether it is installed and the pinned version. The editor's bud.ai.agent
// builds the same command line; this side checks it for bud ai status and bud
// doctor.
package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const openCodeVersion = "2.3.2"

// OpenCodePackage is the npm package and version used for the Suite harness.
const OpenCodePackage = "@opencode/cli@" + openCodeVersion

// InstallCommand is how to get OpenCode, for messages
const InstallCommand = "npm install -g " + OpenCodePackage

// Argv is the command line that starts OpenCode as an ACP agent
func Argv() []string {
	return []string{"opencode", "acp"}
}

// TUI is OpenCode's own TUI on the project (or chat) directory dir
func TUI(dir string) []string {
	return []string{"opencode", dir}
}

// Status is what Check found
type Status struct {
	Argv    []string
	Bin     string // the command's path, empty when it isn't installed
	Version string
	// what's wrong, each with what to do about it; empty when it's ready
	Problems []Problem
}

type Problem struct {
	What, Fix string
	// Hard means OpenCode can't work at all; otherwise it may still
	Hard bool
}

// Ready says whether nothing stops OpenCode working
func (s Status) Ready() bool {
	for _, p := range s.Problems {
		if p.Hard {
			return false
		}
	}
	return true
}

// swappable in tests
var (
	lookPath = exec.LookPath
	// version runs bin --version with a short limit; failures just leave it
	// blank
	versionOf = func(bin string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "--version").Output()
		if err != nil {
			return ""
		}
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		return line
	}
)

// Check looks at whether OpenCode is installed. quick skips running it for
// its version
func Check(quick bool) Status {
	argv := Argv()
	st := Status{Argv: argv}
	bin, err := lookPath(argv[0])
	if err != nil {
		st.Problems = append(st.Problems, Problem{What: argv[0] + " isn't installed", Fix: InstallCommand, Hard: true})
		return st
	}
	st.Bin = bin
	if !quick {
		st.Version = versionOf(bin)
	}
	return st
}

// CheckOpenCodeHarness checks the executable a device profile will use,
// including that it is the pinned version
func CheckOpenCodeHarness() Status {
	st := Check(false)
	if st.Version == "opencode v"+openCodeVersion {
		st.Version = openCodeVersion
	}
	if st.Bin == "" || st.Version == openCodeVersion {
		return st
	}
	found := st.Version
	if found == "" {
		found = "unknown"
	}
	st.Problems = append(st.Problems, Problem{
		What: fmt.Sprintf("OpenCode at %s reports version %s, want %s", st.Bin, found, openCodeVersion),
		Fix:  "put OpenCode " + openCodeVersion + " first on PATH (" + InstallCommand + ")",
		Hard: true,
	})
	return st
}
