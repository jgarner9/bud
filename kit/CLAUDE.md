## When you're running inside bud

bud is a Neovim IDE that starts Claude Code for each project. When you're in one of its sessions:

- The editor is connected: the `bud-ide` MCP tools (or the IDE integration, in Claude Code's own TUI) show the open files, the current selection and diagnostics. Check them before asking which file or code is meant.
- Edits you propose open as diffs in Neovim for the user to accept, change or reject. Keep each edit self-contained, and if one is rejected, ask why rather than retrying the same change.
- Replies often show in a narrow side pane: lead with the answer, keep it short, and use code blocks for code, since the user can send a block straight into the editor.

## Use the tools you have

Your tools are part of the answer, not a fallback. Check what this session has (MCP servers, skills, subagents, connectors) and use them before guessing:

- **Finding code:** Serena for symbols (definitions, references, callers), cocoindex-code for "where is the code that does X", code-index for file and text search across a large tree; Grep only for literal text.
- **Library and API docs:** Context7, even for libraries you know well; versions move.
- **Anything current or outside the repo:** Exa (or built-in search), then fetch the page.
- **What happened before:** Basic Memory, and write back what's worth keeping: decisions, preferences, gotchas.
- **Planned work:** Linear, through the plan-to-linear and file-issue skills.
- **Documents:** Claude Docs, only when the user asks for a doc.
- **Compressed output:** when a result says it was compressed and gives a hash, retrieve it rather than working from the summary.
- **Skills:** when a skill's description fits the task, load it first and follow it.

If a tool you'd expect is missing or fails to connect, say so instead of quietly working around it.

## Answering questions

Use the `lookup` skill. In short, stop at the first rung that answers:

1. What you know, only if it can't have changed since your training.
2. Basic Memory, `cache/` first; a stale note is a lead, not an answer.
3. Context7.
4. Exa.

Save what rungs 3 and 4 return to `cache/` in Basic Memory, and end the answer with a `◇` source line.

## Doing work

- One task at a time. Ask before decisions that are genuinely the user's (design trade-offs, deletes, defaults they'll feel); give a short comparison and a recommendation.
- One commit per task, conventional style, with the issue id: `feat: ... (JGA-44)`. Commit once the project's checks pass.
- Move the issue to In Review when it's committed, and to Done once a release ships it.
- An idea that comes up mid-task becomes a new issue, not extra scope, unless the user says to do it now.

## Planning

Brainstorming works out what the user wants and saves a spec, writing-plans turns it into a plan, and plan-to-linear files the tasks. The spec and plan live in the repo as the source of truth for what and why; Linear is the source of truth for status.
