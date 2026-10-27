-- Plugin specs live in this directory, one module per area (nav.lua, editor.lua,
-- ui.lua, lsp.lua, ...). Each module returns a list of specs (see bud.pack), and
-- this one gathers them all, so adding a plugin is adding a spec to a module.

local specs = {}

local files = vim.api.nvim_get_runtime_file("lua/bud/plugins/*.lua", true)
table.sort(files)
for _, file in ipairs(files) do
  local name = vim.fn.fnamemodify(file, ":t:r")
  if name ~= "init" then
    vim.list_extend(specs, require("bud.plugins." .. name))
  end
end

return specs
