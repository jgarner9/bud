-- Run with: nvim --clean --headless -l tests/lua/chat_test.lua (just lua-test)
-- Chat (JGA-165): Claude Code's own TUI as bud-ide's chat agent, in its own tab

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
vim.fn.mkdir(tmp, "p")
tmp = vim.uv.fs_realpath(tmp)
require("bud.config").file = tmp .. "/config.json"
vim.env.CLAUDE_CONFIG_DIR = tmp .. "/cc"
vim.env.BUD_BIN = "/usr/bin/bud-test"

-- the TUI logs its argv, dir and BUD_BIN, then waits for a q line
local fake = tmp .. "/claude"
vim.fn.writefile({
  "#!/bin/sh",
  'printf "%s|%s|%s\\n" "$*" "$(pwd -P)" "${BUD_BIN:-none}" >> "$(dirname "$0")/runs.log"',
  "echo '> chat'",
  'while IFS= read -r line; do [ "$line" = q ] && exit 0; done',
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")
local function runs()
  local f = tmp .. "/runs.log"
  return vim.fn.filereadable(f) == 1 and vim.fn.readfile(f) or {}
end

local claude = require("bud.ai.claude")
claude.bin = function()
  return fake
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

-- without bud-ide's chat agent it's plain Claude Code, saying how to get it
local home = vim.api.nvim_get_current_tabpage()
chat.open()
wait(function()
  return #runs() == 1
end, "the TUI")
eq(runs()[1], "|" .. tmp .. "/chat|none", "no agent, in the chat dir, without BUD_BIN")
eq(notes[1]:find("bud kit sync", 1, true) ~= nil, true, "says to sync the kit")
eq(vim.api.nvim_get_current_tabpage() ~= home, true, "in its own tab")
local chat_tab = vim.api.nvim_get_current_tabpage()

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

-- with the agent installed (from any marketplace) it runs as the agent
local plugin = tmp .. "/plugins-src/bud-ide"
vim.fn.mkdir(plugin .. "/agents", "p")
vim.fn.writefile({ "---", "name: chat", "---" }, plugin .. "/agents/chat.md")
vim.fn.mkdir(tmp .. "/cc/plugins", "p")
vim.fn.writefile({
  vim.json.encode({ version = 2, plugins = { ["bud-ide@bud-plugins"] = { { scope = "user", installPath = plugin } } } }),
}, tmp .. "/cc/plugins/installed_plugins.json")
eq(claude.plugin_file("bud-ide", "agents/chat.md"), plugin .. "/agents/chat.md", "finds the agent")
eq(claude.plugin_file("bud-ide", "agents/nope.md"), nil, "an older copy without it")
notes = {}
chat.open({ resume = true })
wait(function()
  return #runs() == 2
end, "the TUI as the agent")
eq(runs()[2], "--agent bud-ide:chat --resume|" .. tmp .. "/chat|none", "the agent, with the picker")
eq(notes, {}, "no warning")

vim.notify = notify
chat._reset()
eq(vim.api.nvim_list_tabpages(), { home }, "reset closes it")
vim.fn.delete(tmp, "rf")
print("chat_test: ok")
