-- The default layout (BASE.md 5.6): the file tree, focused, beside the
-- editor, with the <leader>t terminal along the bottom. Chat stays closed
-- until <leader>ac (JGA-161).
return {
  description = "File tree and editor; <leader>ac opens chat, <leader>t the terminal",
  panes = {
    { kind = "tree", side = "left", size = 30, focus = true },
  },
  terminal = { height = 0.35 },
}
