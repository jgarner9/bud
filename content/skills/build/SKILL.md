---
name: build
description: Building mode. How a coding task is worked so it costs few tokens and stays correct; one task per context, a spec before code, the code written by the cheapest coder that can follow the spec, and checks instead of reading. Use when starting to write, fix or change code, working an issue, or picking the next task. Not for explaining code or planning what to build.
---

# Building

Every tool call resends the whole conversation, so the cost of a task is the size of this context times the number of calls. Keep both small.

## One task per context

One issue, one context. When a task is committed, stop and tell Jason to `/clear` (or `/compact` when the next task needs what was learned) before the next one. As the context grows, watch its size and recommend a `/handoff` and a new session before every turn gets expensive.

## Your model and the split

This session runs `glm-5.3`. It plans, decides and checks; it does not write the code itself. Work is plan-then-build: a planning session on `glm-5.3` writes the whole spec up front (the `planning` skill), then this build session works through that plan, handing each task to a sub-agent on a cheaper model from the table below. `minimax-m3` is the starting coder for features; `glm-5.3-flash` is for mechanical edits.

## The loop

1. **Read only what the task names.** The issue, then the files and lines it points at. A search across many files goes to the `explore` subagent, which returns conclusions, not file contents. Send independent tool calls in one turn.
2. **Decide, then write the spec.** Every decision and edge case is yours. The spec is what a coder needs and nothing else: the files to touch with line anchors, signatures, the behaviour as numbered rules, the exact error text, the tests, and the commands that must pass. Put it where the repo keeps such things, or in the brief when it is short.
3. **Rate what is left and hand it over.** Use the table. Reading and searching across many files goes to the `explore` subagent; writing code goes to the `general` subagent, whose model the rating picks. The brief is the spec's path, the files the coder may touch, and the checks. No history, no background.
4. **Check, don't read.** Run the repo's check yourself. Read the diff stat and the test names; read code only where no test can speak for it. Send corrections back to the same coder.
5. **Report it done and ready to commit**, then stop. Commit only when asked, and move the issue once it's committed.

| Rating | The coding that is left | Coder |
| --- | --- | --- |
| 0 | Under about 60 changed lines | Write it yourself: a subagent costs more than it saves |
| 1–2 | One file, nothing existing to understand | `general` subagent, flash model (`glm-5.3-flash` or `mimo-v2.6-flash`) |
| 3 | A few files, mechanical | `general` subagent, mid model (`minimax-m3` or `kimi-k2.7-code`) |
| 4–5 | Several files, has to fit existing code | `general` subagent, mid model (`minimax-m3` or `kimi-k2.7-code`) |
| 6–7 | State, error paths, many callers | `general` subagent, top model (`glm-5.3` or `kimi-k3.1-pro`) |
| 8+ | Concurrency, security, or the design is the work | Write it yourself |

Say the rating and the coder in one line before dispatching. A coder that fails the checks twice moves up one row.

## Load when the task needs it

- `code-style`: before writing or reviewing code yourself.
- `debugging`: a bug, a failing test or behaviour nobody expected.
- `verify`: before saying anything is done or passing.
- `tools`: finding code or looking up a library.
- `release`: cutting a release.
