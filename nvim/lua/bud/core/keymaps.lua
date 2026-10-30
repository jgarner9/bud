local map = vim.keymap.set

map("n", "<leader>d", function()
  require("bud.dashboard").open()
end, { desc = "Dashboard" })

-- the plugin list, read-only, since versions only change with a release (JGA-166)
map("n", "<leader>P", function()
  require("bud.pack_view").open()
end, { desc = "Plugins" })
