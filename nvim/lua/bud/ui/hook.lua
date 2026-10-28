-- Points vim.ui.select and vim.ui.input at bud.ui (JGA-136) without loading
-- it: bud.ui is only read the first time a picker opens, which keeps it out of
-- the startup budget (D35).

local orig = vim.ui.input
vim.ui.select = function(items, opts, on_choice)
  require("bud.ui").select(items, opts, on_choice)
end
vim.ui.input = function(opts, on_confirm)
  require("bud.ui").ui_input(opts, on_confirm, orig)
end
