# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Bud is a Neovim-based IDE driven by a small Go CLI (`bud`). The CLI installs, launches, updates and syncs an isolated Neovim config (`NVIM_APPNAME=bud`, so it never touches `~/.config/nvim`). `BASE.md` is the full plan plus a numbered decision log (D1, D2, ...). Most of it isn't built yet, so check the code before assuming a planned feature exists. When a decision changes, update `BASE.md` too.

AI scope is **Claude Code only** (D26): one Claude Code session per project, started and owned by Neovim (D25, BASE.md 4.2), with Neovim TUIs built on the `bud.ai.claude` Lua module. Don't add other harnesses, model providers or an API client unless asked.

## Commands

```sh
just                 # list recipes
just check           # what CI runs: gofmt check, go vet, shellcheck install.sh, go test ./...
just test            # go test ./...
just test -run TestOpen ./internal/cli   # single test (args pass through to go test)
just run <args>      # go run ./cmd/bud <args>, e.g. just run update
just build           # ./bud (gitignored)
just fmt             # gofmt -w .
just snapshot        # local GoReleaser build into dist/, needs goreleaser installed
```

If the Go build cache isn't writable (for example inside a sandbox), prefix commands with `GOCACHE=$TMPDIR/gocache`.

## Hard constraints

- **Standard library only (D1).** No third-party Go modules. Commands use `flag.FlagSet`, not cobra. Config and data files are JSON (D2).
- **Flags go before positionals (D4):** `bud [global flags] <command> [command flags] [args]`. That's plain `flag` behavior, so don't add interspersed parsing.
- **macOS and Linux only (D22).**

## Architecture

**Command registry.** Each command is one file in `internal/cli/` that calls `RegisterCommand` from `init()`. `App.Run` (`internal/cli/cli.go`) parses global flags. If the first argument names a registered command it runs that command; otherwise it falls back to `open` with the argument as a path, so `bud .` and `bud file.go` work. Each command gets its own `FlagSet` through `Command.Flags`. Global flags (`--verbose`, `--no-update`, `--profile`, `--layout`) are parsed into a `*Globals` that `App.Run` hands to every `Command.Run`; `Globals.Logf` is the `--verbose` logger. Usage text is written by `globalUsage`/`commandUsage`, and `flagOrderHint` explains D4 when a global flag lands after the command.

**Errors and output.** Commands return errors and never print errors or call `os.Exit`. `cli.ExitCode` (`internal/cli/errors.go`) is the only place that prints errors (one `bud: ...` line on stderr) and picks the exit code: 0 for success or `-h`, 2 for a `*UsageError` (build one with `usageErrorf`), 1 for anything else. `App.parse` silences the flag package and prints requested help to stdout, and the error plus usage to stderr. `App.Stdout`/`App.Stderr` let tests capture that output.

**Embedded config (D5).** The root package `bud` (`embed.go`) embeds `nvim/` with `//go:embed`. It must stay at the repo root, because embed can't reach parent directories. Later top-level dirs (`languages/`, `layouts/`) get embedded there too. The binary is the release: `internal/nvim.SyncConfig` copies the embedded files into `~/.config/bud` and records them in `.bud-manifest.json`. It overwrites managed files, deletes ones a release dropped, and never touches anything else. After a change it runs `nvim --headless "+Lazy! restore"`, so plugins match the committed `nvim/lazy-lock.json`. The target dir comes from `paths.Resolve().Config`, which is `~/.config/bud` on macOS too, because that's where Neovim looks.

**Paths and machine config.** `internal/paths.Resolve` is the only place that builds a bud path (XDG on macOS too, D13); don't hardcode one elsewhere. `internal/config` keeps `config.json` as a raw JSON tree so unknown keys survive `Save`, and `File.Settings()` decodes it over `Defaults()`.

**Doctor.** `internal/doctor.Run` returns one `Result` per check (`OK`/`Warn`/`Fail`). Only `Fail` makes `bud doctor` exit non-zero, which the updater will use for rollback. New checks are functions added to `Run`.

**Launching.** `open` resolves its args into a workspace: no args is the cwd, one dir is a project, and files open from the root found by walking up to `.git` (else the first file's dir). Then `nvim.Launch` `chdir`s and replaces the bud process with nvim via `syscall.Exec`. Nothing runs after that, which is why plans for in-editor processes (like the Claude Code session in BASE.md 4.2) live in Lua, not Go.

**Installer.** `internal/installer` installs missing system deps through the package manager (D16): Homebrew on macOS; pacman, then apt, then dnf on Linux. `NewPlan(profile)` works out what's missing and `Plan.Command()` the exact command line (with `sudo` unless running as root); `bud install` prints it, asks y/N (`--yes` skips), then `Plan.Run()` runs it on the terminal and re-checks. With no package manager, or no consent, it returns a `*MissingError` naming the deps and the command. It never upgrades anything, Neovim included (D19). Per-manager package names live in `Dep.Pkgs`, `Dep.Bins` lists alternative binary names (apt's `fdfind`), and `FullOnly` deps are skipped by the minimal profile. `bud install` also asks profile, terminal and languages (`internal/cli/install.go`, prompts in `prompt.go`) and only writes answers that differ from the defaults into `config.json`.

**Version.** `internal/version.Version` is set by GoReleaser via `-ldflags -X`. Without it, `String()` falls back to the module version Go stamps into the build.

**Neovim config.** `nvim/init.lua` sets the leader (`<Space>`), then loads `bud.core` (options, keymaps, autocmds) and `bud.lazy`. Plugin specs are files under `nvim/lua/bud/plugins/` that lazy.nvim imports. Lazy's own update checker is off: plugin versions change only by committing `lazy-lock.json` and shipping a release.

## Release and install

Pushing a `v*` tag runs `.github/workflows/release.yml` (GoReleaser): linux/darwin × amd64/arm64 archives plus `checksums.txt`. Archive names deliberately leave out the version (`bud_<os>_<arch>.tar.gz`) because `install.sh` downloads from `releases/latest/download/<name>`, so keep `.goreleaser.yaml` and `install.sh` in sync. `install.sh` must stay POSIX `sh` and shellcheck-clean. It verifies the checksum, installs to `~/.local/bin`, and asks on `/dev/tty` before running `bud install`, because stdin is the script under `curl | sh`.

## Tests

Tests swap package-level vars and restore them with `t.Cleanup`: `CommandRegistry`, `installer.Deps`, `installer.goos`, `version.Version`. Tests that need binaries on PATH use the `fakePath` helper in `internal/installer`. `App` tests build an `App` with a custom `Cmds` map and recorder commands instead of using the global registry.

## Commits

Lowercase conventional style: `feat:`, `fix:`, `chore:`, `docs:`, followed by a short summary.
