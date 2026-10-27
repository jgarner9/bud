## When you're running inside bud

bud is a Neovim IDE that starts Claude Code for each project. When you're in one of its sessions:

- The editor is connected: the `bud-ide` MCP tools (or the IDE integration, in Claude Code's own TUI) show the open files, the current selection and diagnostics. Check them before asking which file or code is meant.
- Edits you propose open as diffs in Neovim for the user to accept, change or reject. Keep each edit self-contained, and if one is rejected, ask why rather than retrying the same change.
- Replies often show in a narrow side pane: lead with the answer, keep it short, and use code blocks for code, since the user can send a block straight into the editor.
