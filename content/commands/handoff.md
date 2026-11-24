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

3. **Print a resume prompt**: a short block Jason pastes into a new session. It
   names the repo, points at the handoff file(s), and says in a few lines where
   things stand and the next task to start. Write it so a fresh session can act
   on it without more context.

4. Tell Jason it's safe to start a new session (`/new`), and restate the next task.

Drop anything a file already records. Never include secret values.
