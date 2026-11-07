-- Run with: nvim --clean --headless -l tests/lua/highlight_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local highlight = require("bud.lang.highlight")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local started = {}
local function starts()
  highlight.start = function(buf)
    table.insert(started, buf)
  end
end
local function fails()
  highlight.start = function()
    error("no parser for html")
  end
end
local function html(setup)
  local buf = vim.api.nvim_create_buf(true, false)
  vim.api.nvim_set_current_buf(buf)
  if setup then
    setup(buf)
  end
  vim.bo[buf].filetype = "html"
  return buf
end
-- the scheduled start has run
local function tick()
  vim.wait(20)
end

starts()
highlight.setup({ "html" })
-- in bud the config runs before Neovim's `syntax on`, so syntaxset's FileType
-- autocmd comes after bud's: do the same here
vim.cmd("syntax off | syntax on")

-- a filetype tree-sitter covers: no regex syntax, tree-sitter on the next tick
local buf = html()
eq(vim.b[buf].current_syntax, nil, "no regex syntax before the first screen")
eq(started, {}, "tree-sitter waits a tick")
tick()
eq(started, { buf }, "tree-sitter started")
eq(vim.b[buf].current_syntax, nil, "still no regex syntax")

-- already highlighted: a repeated FileType (`:e`, `:set ft=html`) starts nothing new
vim.treesitter.highlighter.active[buf] = {}
vim.bo[buf].filetype = "html"
tick()
eq(started, { buf }, "no second start")
vim.treesitter.highlighter.active[buf] = nil

-- tree-sitter can't start (no parser): the regex syntax comes back
fails()
local failed = html()
tick()
eq(vim.b[failed].ts_highlight, nil, "the hold is let go")
eq(vim.b[failed].current_syntax, "html", "regex syntax back")
-- and the next html buffer has it at once, with no second try
starts()
started = {}
local after = html()
eq(vim.b[after].current_syntax, "html", "a failed parser isn't held for again")
tick()
eq(started, {}, "nor tried again")
highlight.failed = {}

-- a syntax picked before the tick (a modeline, say) is kept when it fails
fails()
local picked = html()
vim.bo[picked].syntax = "xml"
tick()
eq(vim.b[picked].current_syntax, "xml", "the picked syntax kept")
highlight.failed = {}

-- with :syntax manual, failing loads no regex syntax
vim.cmd("syntax manual")
local manual = html()
tick()
eq(vim.b[manual].current_syntax, nil, "manual: nothing loaded")
eq(vim.bo[manual].syntax, "", "manual: syntax left unset")
vim.cmd("syntax off | syntax on")
highlight.failed = {}

-- the filetype changes before the tick to one it isn't given: that one's regex syntax
starts()
started = {}
local changed = html()
vim.bo[changed].filetype = "css"
tick()
eq(started, {}, "no tree-sitter for a filetype it wasn't given")
eq(vim.b[changed].ts_highlight, nil, "the hold is let go")
eq(vim.b[changed].current_syntax, "css", "the new filetype's regex syntax")

-- a filetype it isn't given keeps its regex syntax
local other = vim.api.nvim_create_buf(true, false)
vim.api.nvim_set_current_buf(other)
vim.bo[other].filetype = "css"
eq(vim.b[other].current_syntax, "css", "other filetypes untouched")

-- a buffer wiped or unloaded before the tick is left alone
started = {}
local gone = html()
vim.api.nvim_set_current_buf(other)
vim.api.nvim_buf_delete(gone, { force = true })
local unloaded = html()
vim.api.nvim_set_current_buf(other)
vim.api.nvim_buf_delete(unloaded, { unload = true })
tick()
eq(started, {}, "no start on a wiped or unloaded buffer")

print("highlight_test: ok")
