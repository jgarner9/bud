---
name: bud-dev
description: How the bud repo (a Go CLI that installs, syncs and updates the Bud Suite, plus its Neovim IDE) is built and extended. Use when working in a bud checkout, or when asked to add a language spec, a tool, a layout, a command or content to bud.
---

# Working on bud

bud is one Go module and one binary (`cmd/bud`, `internal/`). It installs a device from its profile, syncs the signed content bundle and the overlay into each runtime's config (`bud sync`), checks for drift (`bud doctor`), updates itself, and launches the Neovim IDE, an isolated config (`NVIM_APPNAME=bud`) built from `ide/`. The binary is the release: `ide/nvim/`, `ide/languages/`, `ide/layouts/`, `ide/tools/`, `ide/migrations/`, `ide/bundles/` and `ide/plugins/` are embedded (`embed.go`), and the IDE parts are copied into `~/.config/bud`. `content/` is not embedded: CI packs it into a signed content release, and `bud sync` brings it to each device.

Read `AGENTS.md` at the repo root first: it holds the rules and points to `docs/internals/`, where each area of the code has a page; read the page for the area you are changing. `BASE.md` is the IDE's plan plus a numbered decision log (D1, D2, ...); when a decision changes, update it there too.

## Hard rules

- Go standard library only; commands use `flag.FlagSet`. Config and data files are JSON.
- Flags go before positionals: `bud [global flags] <command> [command flags] [args]`. macOS and Linux only.
- Commands return errors; only `cli.ExitCode` prints them. A command that isn't built yet fails saying so.
- Every bud path comes from `internal/paths.Resolve`; files bud rewrites go through `internal/atomicfile`.
- Nothing personal in the repo: no hostnames, tailnet names, home paths or real `op://` paths. Those live in the private overlay. `just leak-check` fails on them.
- The IDE's first screen has a 50 ms budget. Slow editor work goes on `vim.schedule` or lazy loading.

## Adding things

- **A command:** one file in `internal/cli/` calling `RegisterCommand` from `init()`, a row in `docs/commands.md`, and a row in `docs/config.md` for any new setting (a test fails without it).
- **A language:** copy `ide/languages/_template.lua` (unknown fields are errors), then add a case to `scripts/lang_check.lua` and run `just lang-check <name>`.
- **A tool:** `ide/tools/<name>/manifest.json` plus an `init.lua` returning `{ setup = function(manifest) end }`.
- **A layout:** `ide/layouts/<name>.lua` listing panes by kind (`tree`, `git`, `chat`), side and size.
- **Content:** `content/skills/<id>/SKILL.md`, `content/agents/<id>.md`, `content/commands/<id>.md` or an env fragment in `content/env/<id>.jsonc`. Keep it generic; anything personal goes in the overlay. A push to main that changes `content/` cuts a content release for the edge channel.

## Checking

`just check` is what CI runs: gofmt, stylua and selene, go vet, shellcheck, `go test ./...`, the Lua and script tests and the smoke test. Run the narrow test while you work (`just test -run TestName ./internal/<pkg>`) and `just check` once before you commit. Tests swap package-level vars and restore them with `t.Cleanup`. Commits are lowercase conventional (`feat:`, `fix:`, `chore:`, `docs:`) with the issue id.
