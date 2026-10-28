-- Run with: nvim --clean --headless -l tests/lua/statusline_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local sl = require("bud.statusline")
local icons = require("bud.icons")

local function eq(got, want, what)
  if got ~= want then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/lua", "p")
vim.cmd.cd(tmp)

-- a file shows relative to the project, or just its tail when short
vim.cmd.edit(vim.fn.fnameescape("lua/a%b.lua"))
eq(sl.file(), "lua/a%%b.lua", "relative, % escaped")
eq(sl.file(true), "a%%b.lua", "short")
vim.api.nvim_buf_set_lines(0, 0, -1, false, { "x" })
eq(sl.file(true), "a%%b.lua " .. icons.modified, "modified")
vim.bo.modified = false
vim.bo.readonly = true
eq(sl.file(true), "a%%b.lua " .. icons.locked, "read-only")

-- an unnamed buffer, and special ones by what they are
vim.cmd.enew()
eq(sl.file(), "[No Name]", "no name")
vim.bo.buftype = "nofile"
vim.bo.filetype = "neo-tree"
eq(sl.file(), "neo-tree", "special buffer")

-- no language server, nothing to show
eq(sl.lsp(), "", "no lsp")

print("statusline_test: ok")
