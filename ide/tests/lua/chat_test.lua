-- Run with: nvim --clean --headless -l tests/lua/chat_test.lua (just lua-test)
-- Chat (JGA-165): OpenCode's own TUI in its own tab, in a dir that's no project's

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
vim.o.shell = "sh"

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(5000, cond, 10) then
    error("timed out waiting for " .. what, 2)
  end
end

local tmp = vim.uv.fs_realpath(vim.fn.tempname()) or vim.fn.tempname()
vim.fn.mkdir(tmp .. "/bin", "p")
tmp = vim.uv.fs_realpath(tmp)
require("bud.config").file = tmp .. "/config.json"

-- the TUI logs its argv and dir, then waits for a q line
vim.fn.writefile({
  "#!/bin/sh",
  'printf "%s|%s\\n" "$*" "$(pwd -P)" >> "$(dirname "$0")/runs.log"',
  "echo '> chat'",
  'while IFS= read -r line; do [ "$line" = q ] && exit 0; done',
}, tmp .. "/bin/opencode")
vim.fn.setfperm(tmp .. "/bin/opencode", "rwxr-xr-x")
local function runs()
  local f = tmp .. "/bin/runs.log"
  return vim.fn.filereadable(f) == 1 and vim.fn.readfile(f) or {}
end

local chat = require("bud.ai.chat")
chat.dir = function()
  return tmp .. "/chat"
end

local notes = {}
local notify = vim.notify
vim.notify = function(msg)
  table.insert(notes, msg)
end

-- without opencode, chat says how to get it and opens nothing
local path = vim.env.PATH
vim.env.PATH = "/nonexistent"
local home = vim.api.nvim_get_current_tabpage()
chat.open()
assert(notes[1] and notes[1]:find("opencode isn't installed: run bud ai setup", 1, true), tostring(notes[1]))
eq(vim.api.nvim_list_tabpages(), { home }, "no tab")
vim.env.PATH = tmp .. "/bin:" .. path

-- chat runs OpenCode on the chat dir, in a tab of its own
notes = {}
chat.open()
wait(function()
  return #runs() == 1
end, "the TUI")
eq(runs()[1], tmp .. "/chat|" .. tmp .. "/chat", "opencode in the chat dir")
eq(notes, {}, "no warning")
eq(vim.api.nvim_get_current_tabpage() ~= home, true, "in its own tab")
local chat_tab = vim.api.nvim_get_current_tabpage()
eq(vim.fn.maparg("k", "t", false, true).buffer, 1, "jk to Normal mode in chat")

-- toggle goes back and forth, and open again never starts a second one
chat.toggle()
eq(vim.api.nvim_get_current_tabpage(), home, "back where it came from")
chat.toggle()
eq(vim.api.nvim_get_current_tabpage(), chat_tab, "back to chat")
chat.open()
eq(#runs(), 1, "the same TUI")

-- quitting it closes the tab
vim.fn.chansend(vim.bo[vim.api.nvim_get_current_buf()].channel, "q\r")
wait(function()
  return not chat.running()
end, "the TUI to quit")
eq(vim.api.nvim_list_tabpages(), { home }, "its tab is gone")

vim.notify = notify
chat._reset()
eq(vim.api.nvim_list_tabpages(), { home }, "reset closes it")
vim.fn.delete(tmp, "rf")
print("chat_test: ok")
