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

-- no language server, nothing to show, and vim.lsp isn't loaded to find that out
eq(sl.lsp(), "", "no lsp")
eq(package.loaded["vim.lsp"], nil, "vim.lsp left unloaded")
local _ = vim.lsp.get_clients
eq(sl.lsp(), "", "no lsp once it's loaded")

-- diagnostics name their numbers; narrow windows shorten them
vim.cmd.enew()
eq(sl.diagnostics(), "", "no diagnostics")
local ns = vim.api.nvim_create_namespace("statusline_test")
vim.api.nvim_buf_set_lines(0, 0, -1, false, { "a", "b", "c" })
local sev = vim.diagnostic.severity
vim.diagnostic.set(ns, 0, {
  { lnum = 0, col = 0, message = "x", severity = sev.ERROR },
  { lnum = 1, col = 0, message = "x", severity = sev.ERROR },
  { lnum = 2, col = 0, message = "x", severity = sev.WARN },
  { lnum = 2, col = 0, message = "x", severity = sev.INFO },
})
eq(sl.diagnostics(), "2 errors 1 warning 1 info", "named counts")
eq(sl.diagnostics(true), "E2 W1 I1", "short counts")

-- the position is a statusline format string; evaluated it reads in words
vim.api.nvim_win_set_cursor(0, { 2, 0 })
eq(vim.api.nvim_eval_statusline(sl.position(), { winid = 0 }).str, "Ln 2, Col 1  66% of 3", "position")
eq(vim.api.nvim_eval_statusline(sl.position(true), { winid = 0 }).str, "2:1", "short position")

print("statusline_test: ok")
