# Notes

Long-term notes live in Basic Memory, a self-hosted notes connector. Its
tools are named `mcp__claude_ai_Basic_Memory__*` when they come from the
claude.ai account, or `mcp__basic-memory__*` when bud added the server to
Claude Code directly.

At the start of each session, before your first reply, call Basic Memory's
`read_note` with identifier `main/reference/claude-ai-memory/index` and follow
the profile and preferences in it. Do this once per session, not every turn.

- The index lists the other mirrored notes; read one only when it's relevant.
- `reference/claude-ai-memory/` is a generated, read-only mirror of claude.ai
  memory. Never write, edit or move anything there. Durable updates go in the
  normal notes, following the "Knowledge Base Conventions" note.
- If Basic Memory isn't connected or can't be reached, carry on without it and
  mention once that the memory mirror couldn't be loaded.
