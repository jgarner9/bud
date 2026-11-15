-- Run with: nvim --clean --headless -l tests/lua/quit_test.lua (just lua-test)
-- Quitting with bud's terminals open: ExitPre ends Claude Code's panes and an
-- idle shell, never a busy one, and nothing while a :qa is about to fail

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
-- the claude backend (D131): bud.ai.claude and Claude Code's TUI
dofile("tests/lua/lib/claude_backend.lua")
vim.o.shell = "sh"

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(5000, cond, 20) then
    error("timed out waiting for " .. what, 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
require("bud.config").file = tmp .. "/config.json"
require("bud.ai.bridge").lock_dir = function()
  return tmp .. "/ide"
end
local fake = tmp .. "/claude"
vim.fn.writefile({ "#!/bin/sh", "echo '> ready'", "while IFS= read -r l; do :; done" }, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")
local claude = require("bud.ai.claude")
-- bud's embedded plugin (D142) is unpacked by the bud binary, which these tests don't run
claude.plugin_dir = function()
  return nil
end
claude.bin = function()
  return fake
end

local quit = require("bud.quit")
local term = require("bud.ai.term")
local terminal = require("bud.terminal")

-- which quits deal with unsaved files
for cmd, want in pairs({ wqa = true, xa = true, ["qa!"] = true, exit = true, qa = false, q = false }) do
  eq(quit.settles(cmd), want, ":" .. cmd)
end

local notes = {}
vim.notify = function(msg)
  table.insert(notes, msg)
end

-- an idle shell and Claude Code's pane both end
term.open({ focus = false })
terminal.toggle()
local shell = terminal.job()
wait(function()
  return vim.fn.jobpid(shell) > 0 and quit.busy(shell) == nil
end, "an idle shell")
local ended = quit.ending()
eq(#ended, 2, "the pane and the shell")
eq(vim.fn.jobwait({ shell }, 0)[1] ~= -1, true, "the shell is gone")
wait(function()
  return not term.running()
end, "the pane to close")

-- a busy shell stays, saying what it runs
terminal.toggle()
shell = terminal.job()
vim.fn.chansend(shell, "sleep 30\n")
wait(function()
  return quit.busy(shell) ~= nil
end, "the shell to run sleep")
eq(quit.busy(shell), "sleep", "names the program")
eq(quit.ending(), {}, "nothing to end")
eq(notes[#notes]:find("running sleep", 1, true) ~= nil, true, "says what's running")
eq(vim.fn.jobwait({ shell }, 0)[1], -1, "still running")
vim.fn.jobstop(shell)
wait(function()
  return terminal.job() == nil
end, "the shell to close")

-- with an unsaved file, a :qa that will fail ends nothing
term.open({ focus = false })
vim.cmd("enew")
vim.api.nvim_buf_set_lines(0, 0, -1, false, { "draft" })
eq(#quit.unsaved(), 1, "one unsaved file")
vim.fn.histadd(":", "qa")
eq(quit.ending(), {}, ":qa with unsaved changes ends nothing")
eq(term.running(), true, "Claude Code's pane stays")
vim.fn.histadd(":", "wqa")
eq(#quit.ending(), 1, ":wqa ends it")

vim.cmd("bwipeout!")
vim.fn.delete(tmp, "rf")
print("quit_test: ok")
