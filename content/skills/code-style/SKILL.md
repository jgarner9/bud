---
name: code-style
description: How Jason writes, tests, reviews and commits code. Go first, plus shell and Lua; small standard-library tools with a spec and tests; errors that say what to do next; safe defaults for anything that touches files, git or the network. Use whenever writing, changing, reviewing or refactoring code, writing tests, scripts, CLIs or config, or writing a commit message.
---

# Code style

The repo wins: its `AGENTS.md`, formatter and linter config, and the code around the change come before anything here. These are the defaults when a repo doesn't say. Read the code before you assume a planned feature exists.

## Shape

- Small tools that each do one job. Standard library first: in Go, no third-party modules unless the repo already has them or Jason asks. CLIs use `flag` (one `flag.FlagSet` per subcommand), not a framework, and flags come before positionals.
- One source of truth per concern: build each path in one function, define each setting in one place, document each format in one file.
- Output is deterministic: the same input always gives the same output, sorted and stable.
- Config and data files are JSON. A tool that rewrites one keeps keys it doesn't know.
- Build what was asked. No speculative options, layers or abstractions.

## Errors and exit codes

- Functions return errors. Only the entry point prints them, as one `tool: message` line on stderr, and picks the exit code: 0 success, 1 failure, 2 bad usage.
- Wrap with what was being done, lowercase, no final period: `fmt.Errorf("read the report: %w", err)`.
- Say what to do next and what was kept: `can't find the default branch: set origin/HEAD with git remote set-head origin -a`, `nothing pushed (worktree kept at ...)`.
- A command that isn't built yet fails saying so. Never a silent exit 0.

## Per language

- **Go:** gofmt and `go vet` clean. `main` is `os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr))`, so the whole CLI runs in tests with buffers.
- **Shell:** shellcheck clean. Bash scripts start with `set -euo pipefail` (or `-uo` when they handle failures themselves) and indent with tabs. Anything piped to `sh`, like an installer, is POSIX `sh`. Scripts that run on macOS also work with its bash 3.2: no associative arrays, `mapfile` or `${x,,}`, and `shasum` where Linux has `sha256sum`.
- **Lua (Neovim):** stylua (2 spaces, double quotes, 120 columns) and selene. Modules build `local M = {}` and return it, with `---` doc comments.
- Target macOS and Linux unless the repo says otherwise.

## Commits

- Lowercase conventional commits with the issue id when there is one: `fix: skip dot entries in dev link (JGA-167)`. Types: `feat`, `fix`, `chore`, `docs`, `test`, `refactor`.
- One commit per task, made once the project's checks pass (the repo's `just check` or CI command).

## Load when the task needs it

- [state.md](state.md): anything that writes files, runs git, installs, reads secrets, uses the network or prints to a terminal.
- [tests.md](tests.md): writing or changing tests.
- [docs.md](docs.md): a `SPEC.md`, a README or comments.
- [reviewing.md](reviewing.md): reviewing a change.
