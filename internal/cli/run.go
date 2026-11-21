package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/run"
	"os"
)

func init() { RegisterCommand(RunCommand()) }

// RunCommand runs a command and retains its full output in a private log.
func RunCommand() *Command {
	var out toolOutput
	var shell bool
	var timeout time.Duration
	var tail, context, maxFailures int
	var logPath, lines string
	var flags *flag.FlagSet
	return &Command{
		Name:  "run",
		Usage: "Run a command and summarize its output: run [flags] -- <command> [args...]",
		Flags: func(fs *flag.FlagSet) {
			flags = fs
			out.Bind(fs, 6000)
			fs.BoolVar(&shell, "sh", false, "run arguments as a shell command")
			fs.DurationVar(&timeout, "timeout", 30*time.Minute, "timeout (0 disables)")
			fs.IntVar(&tail, "tail", 20, "number of tail lines")
			fs.IntVar(&context, "context", 2, "context lines around failures")
			fs.IntVar(&maxFailures, "max-failures", 10, "maximum failure lines")
			fs.StringVar(&logPath, "log", "", "read an existing log")
			fs.StringVar(&lines, "lines", "", "line range with --log")
		},
		Run: func(g *Globals, args []string) error {
			if tail < 0 || context < 0 || maxFailures < 0 || timeout < 0 || out.MaxChars < 0 {
				return usageErrorf("run: limits cannot be negative")
			}
			if tail > run.MaxTail || context > run.MaxContext || maxFailures > run.MaxFailures {
				return usageErrorf("run: limits exceed maximum")
			}
			set := map[string]bool{}
			flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
			w := g.stdout
			if w == nil {
				w = os.Stdout
			}
			if logPath != "" || lines != "" {
				if logPath == "" || len(args) != 0 {
					return usageErrorf("run: --log requires a path and no command")
				}
				if set["lines"] && set["tail"] {
					return usageErrorf("run: use --lines or --tail with --log, not both")
				}
				return readLog(w, out, logPath, lines, tail)
			}
			if len(args) == 0 {
				return usageErrorf("run: no command supplied")
			}
			argv := args
			if shell {
				argv = []string{"sh", "-c", strings.Join(args, " ")}
			}
			p, err := paths.Resolve()
			if err != nil {
				return err
			}
			res, err := run.Run(run.Options{Argv: argv, LogDir: p.RunLogs(), Timeout: timeout, Tail: tail, Context: context, MaxFailures: maxFailures})
			if err != nil {
				return err
			}
			if !res.Failed() && !set["tail"] {
				res.Tail = []run.Line{}
			}
			if err := out.Print(w, runText(res, shell), res); err != nil {
				return err
			}
			if res.Failed() {
				return &commandExitError{code: res.Exit}
			}
			return nil
		},
	}
}

func readLog(w io.Writer, out toolOutput, path, lines string, tail int) error {
	var r *run.Range
	if lines != "" {
		rg, err := run.ParseRange(lines)
		if err != nil {
			return usageErrorf("run: %v", err)
		}
		r = &rg
	}
	s, err := run.ReadLog(path, r, tail)
	if err != nil {
		return err
	}
	if s.Lines == nil {
		s.Lines = []run.Line{}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "log: %s (%d lines)", path, s.Total)
	if len(s.Lines) == 0 {
		b.WriteString("\nno lines in that range")
	}
	writeLines(&b, s.Lines, nil)
	if s.Capped && r == nil {
		fmt.Fprintf(&b, "\n[tail cut at %d lines; read earlier lines with --lines a:b]", run.MaxRead)
	} else if s.Capped {
		fmt.Fprintf(&b, "\n[range cut at %d lines; read on with --lines %d:]", run.MaxRead, s.Lines[len(s.Lines)-1].N+1)
	}
	return out.Print(w, b.String(), s)
}

// runText is the compact text result: one status line, the log path, the
// failure lines with context (`12:` a failure, `13-` context, `--` a gap) and
// the tail, minus tail lines already shown as failures.
func runText(r run.Result, shell bool) string {
	cmd := displayCommand(r.Argv, shell)
	took := (time.Duration(r.DurationMS) * time.Millisecond).Round(100 * time.Millisecond)
	var b strings.Builder
	switch {
	case r.TimedOut:
		fmt.Fprintf(&b, "run TIMED OUT: %s (exit %d, %s, %d lines)", cmd, r.Exit, took, r.Lines)
	case r.Signal != "":
		fmt.Fprintf(&b, "run KILLED by signal (%s): %s (exit %d, %s, %d lines)", r.Signal, cmd, r.Exit, took, r.Lines)
	case r.Failed():
		fmt.Fprintf(&b, "run FAILED: %s (exit %d, %s, %d lines)", cmd, r.Exit, took, r.Lines)
	default:
		fmt.Fprintf(&b, "run ok: %s (exit 0, %s, %d lines)", cmd, took, r.Lines)
	}
	fmt.Fprintf(&b, "\nlog: %s", r.Log)
	if r.LogError != "" {
		fmt.Fprintf(&b, "\nwarning: the log is incomplete: %s", r.LogError)
	}
	if r.Failed() {
		b.WriteString("\n")
		switch shown := countMatches(r.Failures); {
		case r.Matches == 0:
			b.WriteString("failures: no line matched the failure patterns (see the tail, or read the log)")
		case shown < r.Matches:
			fmt.Fprintf(&b, "failures: %d lines matched, first %d shown (read more: bud run --log %s --lines a:b)", r.Matches, shown, r.Log)
		default:
			fmt.Fprintf(&b, "failures: %d lines matched", r.Matches)
		}
		writeLines(&b, r.Failures, nil)
	}
	shownAbove := map[int]bool{}
	for _, l := range r.Failures {
		shownAbove[l.N] = true
	}
	var tail []run.Line
	for _, l := range r.Tail {
		if !shownAbove[l.N] {
			tail = append(tail, l)
		}
	}
	if len(tail) > 0 {
		fmt.Fprintf(&b, "\ntail (last %d lines, minus those shown above):", len(r.Tail))
		writeLines(&b, tail, nil)
	}
	return b.String()
}

func countMatches(ls []run.Line) int {
	n := 0
	for _, l := range ls {
		if l.Match {
			n++
		}
	}
	return n
}

// writeLines prints grep-style numbered lines, `--` where numbers skip.
func writeLines(b *strings.Builder, ls []run.Line, skip map[int]bool) {
	prev := 0
	for _, l := range ls {
		if skip[l.N] {
			continue
		}
		if prev != 0 && l.N != prev+1 {
			b.WriteString("\n--")
		}
		sep := "-"
		if l.Match {
			sep = ":"
		}
		fmt.Fprintf(b, "\n%d%s %s", l.N, sep, l.Text)
		prev = l.N
	}
}

// displayCommand shows the command as a shell would take it, cut at 200 chars.
func displayCommand(argv []string, shell bool) string {
	var s string
	if shell && len(argv) == 3 {
		s = "sh: " + argv[2]
	} else {
		parts := make([]string, len(argv))
		for i, a := range argv {
			parts[i] = runShellQuote(a)
		}
		s = strings.Join(parts, " ")
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

func runShellQuote(a string) string {
	if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./-_") == "" {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}
