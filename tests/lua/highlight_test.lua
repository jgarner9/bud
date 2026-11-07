-- Run with: nvim --clean --headless -l tests/lua/highlight_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local highlight = require("bud.lang.highlight")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local started = {}
highlight.start = function(buf)
  table.insert(started, buf)
end
highlight.setup({ "html" })
-- in bud the config runs before Neovim's `syntax on`, so syntaxset's FileType
-- autocmd comes after bud's: do the same here
vim.cmd("syntax off | syntax on")

-- a filetype tree-sitter covers: no regex syntax, tree-sitter on the next tick
local buf = vim.api.nvim_create_buf(true, false)
vim.api.nvim_set_current_buf(buf)
vim.bo[buf].filetype = "html"
eq(vim.b[buf].current_syntax, nil, "no regex syntax before the first screen")
eq(started, {}, "tree-sitter waits a tick")
vim.wait(100, function()
  return #started > 0
end)
eq(started, { buf }, "tree-sitter started")
eq(vim.b[buf].current_syntax, nil, "still no regex syntax")

-- tree-sitter can't start (no parser): the regex syntax comes back
highlight.start = function()
  error("no parser for html")
end
local failed = vim.api.nvim_create_buf(true, false)
vim.api.nvim_set_current_buf(failed)
vim.bo[failed].filetype = "html"
vim.wait(100, function()
  return vim.b[failed].current_syntax ~= nil
end)
eq(vim.b[failed].ts_highlight, nil, "the hold is let go")
eq(vim.b[failed].current_syntax, "html", "regex syntax back")

-- a filetype it isn't given keeps its regex syntax
local other = vim.api.nvim_create_buf(true, false)
vim.api.nvim_set_current_buf(other)
vim.bo[other].filetype = "css"
eq(vim.b[other].current_syntax, "css", "other filetypes untouched")

-- a buffer wiped before the tick is skipped
highlight.start = function()
  error("started on a wiped buffer")
end
local gone = vim.api.nvim_create_buf(true, false)
vim.bo[gone].filetype = "html"
vim.api.nvim_buf_delete(gone, { force = true })
vim.wait(50)

print("highlight_test: ok")
