---
name: bootstrap-project
description: Set up OpenCode context for the current repository, a short project AGENTS.md (what it is, layout, exact commands, conventions, gotchas) plus optional path-scoped rules, so later sessions can jump straight to the right files and symbols. Use when Jason asks to set up or refresh a repo's agent instructions.
---

# Bootstrap a project for OpenCode

Jason drives; you propose. Gather facts, show him the plan and the drafts, and
write files only after he says go.

## 1. See what's already there

- Read any existing `AGENTS.md`, `.opencode/` (rules, settings, skills), and a
  design doc such as `BASE.md` with its decision log.
- If an `AGENTS.md` exists, this is a refresh: propose edits to it and never
  replace it wholesale.

## 2. Map the code

- Orient with OpenCode's code tools (the `explore` subagent, or Serena if
  connected); otherwise use `git ls-files` and `rg --files` to see the layout.
- Take commands from the source, never from guesses: `go.mod`, `Makefile`,
  `justfile`, `Taskfile.yml`, `package.json` scripts, `Cargo.toml`,
  `pyproject.toml`, and the CI workflows in `.github/workflows/`.
- Note the entry points (main packages, `cmd/`, servers), the main packages
  and how they depend on each other, generated code that shouldn't be edited,
  and how tests are laid out.
- If Serena is connected and the repo has no `.serena/project.yml`, offer to
  run Serena's onboarding so symbol lookups work in later sessions.

## 3. Draft

A project `AGENTS.md` of about 100 lines, never more than 200:

- **What this is**: one or two sentences.
- **Layout**: the 5 to 15 paths that matter, one line each.
- **Commands**: build, test (all, and a single test), lint/format and run,
  exact and copy-pasteable, each one verified in step 2.
- **Hard constraints**: the decisions everything else depends on, by number
  when there's a decision log (`Standard library only (D1)`).
- **Conventions**: only ones that differ from the language's defaults, from
  the `code-style` skill, or that a linter or CI check enforces.
- **Gotchas**: generated files, slow or flaky tests, required environment
  variables (names only, never values).

Guidance that applies to only part of the tree goes in a nested `AGENTS.md`
in that directory instead of the main file.

## 4. Review with Jason

Show the plan and the drafts. Ask whether the file should be committed for
everyone (`AGENTS.md`) or kept personal (the global
`~/.config/opencode/AGENTS.md`), then write it.

## 5. Verify

Run one command from the Commands section, such as the single-test command,
to prove it works. Fix the doc if it doesn't.
