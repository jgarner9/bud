# Orchestrator handoffs (`bud resume`)

A long-running OpenCode orchestrator session eventually fills its context. Before this, it ran `/handoff`, printed a resume prompt and stopped, and the work waited until Jason pasted that prompt into a new session. `bud resume` (JGA-394, Suite D131) starts that next session itself: the old session queues its prompt, a timer claims it once and starts OpenCode with it in a tmux session Jason can attach to.

## The pieces

| Piece | Where | What it does |
| --- | --- | --- |
| `/handoff` | `content/commands/handoff.md` | The OpenCode command a session runs when its context is nearly full: finish the step in progress, update the handoff notes, write the resume prompt to `.bud-resume-prompt.txt` with the file tool (never into a shell command, where a line of the prompt could end a heredoc and run as commands), queue it with `bud resume queue`, print it, stop. |
| `bud resume queue` | `internal/cli/resume.go` | Writes the prompt from a file (or `-` for stdin) to the queue, with the checkout it starts in and an optional agent. |
| `bud resume run` | `internal/resume/runner.go` | What the timer runs. Claims the queued prompt and starts the next orchestrator. |
| `bud resume launch` | `internal/cli/resume.go` | What runs inside tmux: reads the claimed entry and runs `opencode --prompt=` with it as a child. |
| `bud resume status`, `off`, `on` | `internal/cli/resume.go` | Read the queue, and the kill switch. |
| The timer | `server/resume/` | A systemd user oneshot, every two minutes, on the home server. |

## The queue

Everything lives in `paths.Resume()`, `~/.local/state/bud/resume/`, mode 0700:

- `pending.json` is the one queued prompt: `{"version": 1, "queued_at", "dir", "agent", "prompt"}`. `dir` is an absolute path to an existing directory, `prompt` is non-empty and at most 64 KiB, and `agent` can't start with `-`, so it can't smuggle a flag. `queue` writes it to a temp file and hard-links it into place, which fails when `pending.json` already exists: two sessions can't both hand off and leave the runner to pick one.
- `consumed/<UTC time>.json` is every prompt the runner claimed, with `claimed_at`, `launched_at` (written by `launch` inside tmux once opencode has started) and, on a failed start, `error`. A start counts only once `launched_at` is set with no error. The runner claims by renaming `pending.json` here, and only one rename can succeed, so a prompt starts at most once. A failed start is recorded and never retried, because the session may have half-started; `status` shows the error and the file still holds the prompt.
- `off` is the kill switch. While it exists, `run` does nothing and a queued prompt stays queued.
- `lock` is the runner's `flock`, taken without waiting, so two runs can't claim at once (the timer can't overlap itself, but a run by hand can). `bud resume off` and `on` take it too, waiting for a run in the middle of a start, so the kill switch can't land between a run's check and its start. The kernel drops it when the process exits, so a crash never leaves it held.

## One run

1. Take the lock, or stop when another run holds it.
2. If the kill switch is on, stop. It's checked under the lock, which `off` also takes.
3. Read `pending.json`; with nothing queued, stop. Every run but the one after a handoff ends here, so the timer costs nothing.
4. Wait until the prompt is `--min-age` old (two minutes), so the session that queued it can finish its last reply.
5. Count the claims in the last 24 hours, failed starts included, since each needs a look. At `--max-per-day` (eight) the run fails, so the journal shows it, and the prompt stays queued. This is what stops a session that hands off at once, over and over, from spending the Go allowance in a loop.
6. Claim the prompt (the rename), kill the tmux session `bud-orchestrator` if one is running, since it's the session that just handed off, and start a new one detached in the entry's `dir`, running `bud resume launch --opencode <bin> <entry>`. tmux runs that argv directly, without a shell, and `launch` reads the prompt from the entry file, so the prompt never goes through a shell or tmux's message size limit.
7. `launch` validates the entry again, finds opencode, changes to its `dir` and starts `opencode [--agent=<name>] --prompt=<prompt>` as its child on the pane's terminal, forwarding the hangup and stop signals and exiting when it does. The values are attached, so a prompt that starts with `-` stays the prompt instead of parsing as a flag such as `--auto`. It records `launched_at` only after the child has started, so a failed start can never read as a good one; any failure before that is recorded as the entry's `error`. OpenCode 2.3.2's TUI submits a `--prompt` as soon as the session and model are ready (`packages/tui/src/routes/session/index.tsx`).
8. The runner, still holding the lock, waits up to 10 seconds for `launched_at` or `error`, since tmux accepting the session only means the command was spawned. An error, a pane that ends before reporting (most often `opencode` missing from the unit's `PATH`) or no report in time fails the run, and `status` shows why. A pane still up after a failed start is stopped, so a late report can't leave an orchestrator running that `status` calls failed.

## Why tmux, and the approval gates

Starting a session is a local action, so it needs no approval, but the session it starts keeps every gate. `Argv` never adds `--auto`, `--yolo` or `--dangerously-skip-permissions` (a test checks), so the orchestrator's asks (`git commit`, `git push`, Linear writes and the rest of `content/env/base.jsonc`'s ask list) go to the TUI. Jason can answer them by attaching with `tmux attach -t bud-orchestrator`, over SSH from the laptop or the phone; the `bud-unattended-deny` plugin (Suite D111) refuses any ask left for 10 minutes, so an unattended session gets a denial and carries on instead of hanging. That's the reason for tmux over `opencode run`: `opencode run` rejects every ask at once (Suite D99), so a resumed session could never commit or push.

The `/handoff` command's `bud resume queue` goes through the normal shell permission flow too: the base rules ask for any command they don't list. In an unattended chain that ask times out and the session prints the prompt instead, so the first queue of a run needs Jason or an overlay rule that lets `bud resume queue *` run alone (revisit item 30).

## The unit

`bud-resume.service` runs with `KillMode=process`: a oneshot's leftover processes are killed when it exits by default, and the tmux server the run starts is one of them. It sets `PATH` so the orchestrator finds `opencode`, `git` and `gh`, and `NoNewPrivileges=yes`, which the orchestrator inherits, so it can't `sudo`. It has no `PrivateTmp`, since tmux's socket is in `/tmp` and Jason's shell has to find it to attach.
