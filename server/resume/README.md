# server/resume/

Starts the next OpenCode orchestrator session after one hands off (JGA-394, Suite D132). A session whose context is nearly full runs `/handoff`, which queues its resume prompt with `bud resume queue`; this timer runs `bud resume run` every two minutes, which claims the prompt once and starts OpenCode with it in the detached tmux session `bud-orchestrator`. How it works is in [docs/internals/resume.md](https://github.com/jgarner9/bud/blob/main/docs/internals/resume.md).

| File | What it is |
| --- | --- |
| `bud-resume.service` | The oneshot unit that runs `bud resume run`. |
| `bud-resume.timer` | Starts it two minutes after boot and two minutes after each run. |

## What you need first

On the home server, as the user who will run the orchestrator:

- `tmux`, and `opencode` at the pinned version, on the unit's `PATH` (edit `Environment=PATH=` in the unit if `opencode` lives somewhere else).
- A bud checkout the orchestrator works in, with `git` able to push to it and `gh` signed in.
- `bud sync` done, so OpenCode has the profile's config, the `/handoff` command and the unattended-deny plugin.
- Lingering on (`loginctl enable-linger`), so user units run without a login.

## Using it

- Start a loop: queue the first prompt by hand, `bud resume queue --dir <checkout> prompt.txt`, and the timer starts it within a few minutes.
- Watch it or answer its asks: `tmux attach -t bud-orchestrator`, and detach with `Ctrl-b d`. An ask nobody answers is refused after 10 minutes.
- See where it stands: `bud resume status`; `journalctl --user -u bud-resume` shows each start.
- Stop everything: `bud resume off` stops the running orchestrator and starts no more; `bud resume on` lets the timer start again.
- A failed start isn't retried. `bud resume status` shows the error, and the entry in `~/.local/state/bud/resume/consumed/` still holds the prompt.
