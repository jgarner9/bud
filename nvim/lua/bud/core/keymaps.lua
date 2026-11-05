local map = vim.keymap.set

-- <leader>w is <C-w>: window moves (h j k l, w, s, v, q, ...) from any
-- window, Normal mode in a terminal pane included (Esc Esc gets there)
map("n", "<leader>w", "<C-w>", { desc = "+window" })

map("n", "<leader>d", function()
  require("bud.dashboard").open()
end, { desc = "Dashboard" })

-- the plugin list, read-only, since versions only change with a release (JGA-166)
map("n", "<leader>P", function()
  require("bud.pack_view").open()
end, { desc = "Plugins" })
