local map = vim.keymap.set

-- <leader>w is <C-w>: window moves (h j k l, w, s, v, q, ...) from any
-- window, Normal mode in a terminal pane included (jk gets there)
map("n", "<leader>w", "<C-w>", { desc = "+window" })

-- jk leaves Insert mode (and Terminal mode in bud's panes), with no lag
require("bud.escape").setup()

-- <leader>qq quit, <leader>qw save all and quit, <leader>qQ quit without saving
require("bud.quit").setup()

map("n", "<leader>d", function()
  require("bud.dashboard").open()
end, { desc = "Dashboard" })

-- the plugin list, read-only, since versions only change with a release (JGA-166)
map("n", "<leader>P", function()
  require("bud.pack_view").open()
end, { desc = "Plugins" })
