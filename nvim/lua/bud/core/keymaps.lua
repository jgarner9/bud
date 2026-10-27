local map = vim.keymap.set

-- vim.pack's plugin view, read-only: target the lockfile so it never offers updates
map("n", "<leader>d", function()
  require("bud.dashboard").open()
end, { desc = "Dashboard" })

map("n", "<leader>L", function()
  vim.pack.update(nil, { offline = true, target = "lockfile" })
end, { desc = "Plugins" })
