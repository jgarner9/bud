---
description: Hand this session off to a fresh one when its context is nearly full
---

Use this when your context is nearly full and there is still work to do. It ends this session cleanly and queues the prompt that starts the next one. Do these in order, and do nothing after the last step:

1. Finish or stop the step in progress, so nothing is half-written. Don't start new work.
2. Record where things stand in the project's own handoff notes (in the bud repo, `handoff/state.md` and `handoff/revisit.md`), through the normal commit and push flow, which asks for approval. If an ask is refused or not answered, say so in the resume prompt rather than retrying.
3. Write the resume prompt for the next session: a few paragraphs, in plain text, that tell a session with no memory of this one what to read first, what was just finished, what is in flight (branches, open PRs and what each waits on), the next task, and anything waiting on Jason. Name files and Linear issues, not chat history. Never put a secret, a token or a personal detail in it.
4. Queue it with this fixed command, run once from the project's root through the normal shell permission flow, with the prompt in place of the placeholder line:

   ```sh
   bud resume queue --dir "$PWD" - <<'BUD_RESUME_PROMPT'
   <the resume prompt>
   BUD_RESUME_PROMPT
   ```

   It refuses when a prompt is already queued; then say so and stop, since the queued one starts first. Don't run any other `bud resume` command: `off`, `on` and `run` are Jason's.
5. Reply with one line saying the handoff is queued, and print the resume prompt below it so it can be pasted by hand if the next session doesn't start. Then stop. The next session starts a few minutes later and replaces this one, so anything after this reply is lost.
