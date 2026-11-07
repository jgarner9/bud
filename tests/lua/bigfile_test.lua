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

-- the lockfiles named here are the ones Neovim matches by name, as it does
for name, ft in pairs(bigfile.named) do
  eq(vim.filetype.match({ filename = "/x/" .. name }), ft, "Neovim's filetype for " .. name)
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
-- a window that had expression folds keeps them as its default, but not for the big file
vim.o.foldmethod = "expr"
buf = open(write("big.go", lines))
eq(vim.bo[buf].filetype, "bigfile", "a big file")
eq(vim.wo.foldmethod, "manual", "no expression folds on it")
eq(vim.api.nvim_get_option_value("foldmethod", { scope = "global" }), "expr", "the window's default kept")
vim.o.foldmethod = "manual"
eq(vim.b[buf].completion, false, "no completion")
vim.wait(1000, function()
  return vim.bo[buf].syntax == "go"
end)
eq(vim.bo[buf].syntax, "go", "Go's regex syntax, after the first screen")
eq(#notes, 1, "said once")
eq(notes[1]:find("big.go", 1, true) ~= nil, true, "names the file")
eq(bigfile.filetype(buf), "go", "its real filetype, for the bridge")

-- reloading it (:e, or a reload after Claude rewrote it) keeps it a big file,
-- without saying so again
vim.cmd.edit()
eq(vim.bo[buf].filetype, "bigfile", "still big after :e")
eq(#notes, 1, "not said again")

-- a file that shrinks is itself again, and is announced again when it grows back
vim.fn.writefile({ "package main" }, dir .. "/big.go")
vim.cmd.edit()
eq(vim.bo[buf].filetype, "go", "small again")
vim.fn.writefile(lines, dir .. "/big.go")
vim.cmd.edit()
eq({ vim.bo[buf].filetype, #notes }, { "bigfile", 2 }, "big again, and said")

-- a shell script, which Neovim tells by content: still its real filetype
local script = { "#!/bin/sh" }
for i = 1, 200 do
  script[#script + 1] = ("echo line %d of a long script"):format(i)
end
buf = open(write("deploy", script))
eq({ vim.bo[buf].filetype, bigfile.filetype(buf) }, { "bigfile", "sh" }, "a big shell script")

-- a path Neovim's own patterns know (a log under /log/): big is still big
vim.fn.mkdir(dir .. "/log", "p")
buf = open(write("log/auth.log", lines))
eq(vim.bo[buf].filetype, "bigfile", "a big log")

-- a lockfile Neovim knows by name: big is big, small keeps its filetype
buf = open(write("go.sum", lines))
eq(vim.bo[buf].filetype, "bigfile", "a big go.sum")
eq(bigfile.filetype(buf), "gosum", "and what it really is")
vim.fn.mkdir(dir .. "/small", "p")
buf = open(write("small/go.sum", { "example.com/x v1.0.0 h1:abc=" }))
eq(vim.bo[buf].filetype, "gosum", "a small go.sum")

-- a buffer that isn't loaded has no line count to go on, and a name matched
-- on its own isn't a file
local unloaded = vim.fn.bufadd(write("unloaded.go", { "package x" }))
eq(vim.filetype.match({ buf = unloaded }), "go", "an unloaded buffer isn't big")
eq(vim.filetype.match({ filename = dir .. "/big.go" }), "go", "nor is a name alone")
eq(vim.filetype.match({ filename = dir .. "/go.sum" }), "gosum", "a lockfile's name alone")

-- Claude's edit review shows a big file's proposed side in its real syntax,
-- without making it a big file of its own
local tools = require("bud.ai.bridge.tools")
local said = #notes
tools.review({ path = dir .. "/big.go", contents = table.concat(lines, "\n") .. "\n", name = "big" }, function() end)
local proposed = vim.api.nvim_get_current_buf()
eq(vim.bo[proposed].filetype, "", "the proposed side has no filetype")
vim.wait(1000, function()
  return vim.bo[proposed].syntax == "go"
end)
eq(vim.bo[proposed].syntax, "go", "and Go's syntax, after the first screen")
eq(#notes, said, "and nothing said about it")
vim.api.nvim_feedkeys("q", "x", false)

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
