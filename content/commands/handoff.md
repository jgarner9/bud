---
description: Summarize this session and write a handoff so a new session can pick up the work
---

# Handoff

Write a handoff that lets a fresh session resume this work without this
conversation. Keep it tight: the next session reads the handoff, not the history.

1. **Summarize**, in this order:
   - What this session did: PRs, commits, files written, and the checks run with their results.
   - Decisions made and why, with their D-numbers when this is a Bud checkout.
   - What is still to do, in order, naming the next task first.
   - Anything waiting on Jason, and open questions.

2. **Record it.** In a Bud checkout, update `handoff/state.md` and
   `handoff/revisit.md` in the working tree, reusing their format (don't
   restructure them). In any other repo, write the summary to `HANDOFF.md` at
   the root. Don't commit unless Jason asks.

3. **Write the resume prompt**: a short block that names the repo, points at the
   handoff file(s), and says in a few lines where things stand and the next task
   to start. Write it so a fresh session can act on it without more context.

4. **Queue it**, so the next session starts without Jason (JGA-394). Save the
   prompt with your file-writing tool as `.bud-resume-prompt.txt` at the
   project's root, overwriting any old one. Never put the prompt in a shell
   command: a prompt in a heredoc or quotes can end the quoting and run as
   commands. Then run this fixed command once, from the project's root, through
   the normal shell permission flow:

   `bud resume queue --dir "$PWD" .bud-resume-prompt.txt`

   It refuses when a prompt is already queued; then say so, since the queued one
   starts first. Don't run any other `bud resume` command: `off`, `on` and `run`
   are Jason's. If `bud resume` isn't there or the command is refused, skip this
   step.

5. **Print the resume prompt** for Jason to paste by hand. If step 4 queued it,
   say the next session starts in a few minutes in the tmux session
   `bud-orchestrator` and replaces this one, so do nothing more after this
   reply. Otherwise tell him it's safe to start a new session (`/new`). Either
   way, restate the next task.

Drop anything a file already records. Never include secret values.
