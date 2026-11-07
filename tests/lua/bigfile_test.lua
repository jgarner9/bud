-- Run with: nvim --clean --headless -l tests/lua/bigfile_test.lua (just lua-test)
-- Big files open as filetype "bigfile" with the real filetype's syntax

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
vim.cmd("filetype on")

local bigfile = require("bud.bigfile")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local notes = {}
vim.notify = function(msg)
  table.insert(notes, msg)
end

-- small limits, so the files can be small
bigfile.size = 2000
bigfile.line_length = 200
bigfile.setup()

local dir = vim.fn.tempname()
vim.fn.mkdir(dir, "p")
local function write(name, lines)
  local path = dir .. "/" .. name
  vim.fn.writefile(lines, path)
  return path
end
local function open(path)
  vim.cmd.edit(vim.fn.fnameescape(path))
  return vim.api.nvim_get_current_buf()
end

-- a small file keeps its filetype and everything that comes with it
local buf = open(write("small.go", { "package main", "", "func main() {}" }))
eq(vim.bo[buf].filetype, "go", "a small file is itself")
eq(vim.b[buf].completion, nil, "completion untouched")
eq(#notes, 0, "nothing said")

-- past the size: filetype bigfile, no completion, the real filetype's syntax
local lines = {}
for i = 1, 200 do
  lines[i] = ("var x%d = %d"):format(i, i)
end
buf = open(write("big.go", lines))
eq(vim.bo[buf].filetype, "bigfile", "a big file")
eq(vim.b[buf].completion, false, "no completion")
eq(vim.wo.foldmethod, "manual", "no expression folds")
vim.wait(1000, function()
  return vim.bo[buf].syntax == "go"
end)
eq(vim.bo[buf].syntax, "go", "Go's regex syntax, after the first screen")
eq(#notes, 1, "said once")
eq(notes[1]:find("big.go", 1, true) ~= nil, true, "names the file")

-- small but minified: one long line is a big file too
buf = open(write("app.min.js", { string.rep("a=1;", 400) }))
eq(vim.bo[buf].filetype, "bigfile", "long lines")
vim.wait(1000, function()
  return vim.bo[buf].syntax == "javascript"
end)
eq(vim.bo[buf].syntax, "javascript", "its own syntax")

-- a new file has no size yet
buf = open(dir .. "/new.go")
eq(vim.bo[buf].filetype, "go", "a new file is itself")

print("bigfile_test: ok")
