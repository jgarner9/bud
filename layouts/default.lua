-- The default layout (BASE.md 5.6): file tree | editor | AI chat, with the
-- <leader>t terminal along the bottom. Chat is Claude Code's own TUI for now
-- (D48), skipped when Claude Code isn't installed.
return {
  description = "File tree, editor and chat; <leader>t opens the terminal",
  panes = {
    { kind = "tree", side = "left", size = 30 },
    { kind = "chat", side = "right", size = 60 },
  },
  terminal = { height = 0.35 },
}
