-- Run with: nvim --clean --headless -l tests/lua/claude_term_test.lua (just lua-test)
-- :BudClaude term hands the session to Claude Code's TUI and back (D48)

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

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/proj", "p")
require("bud.config").file = tmp .. "/config.json"
-- never the real ~/.claude/ide
require("bud.ai.bridge").lock_dir = function()
  return tmp .. "/ide"
end

-- headless (-p) it answers each message; as the TUI it waits for a q line
local fake = tmp .. "/claude"
vim.fn.writefile({
  "#!/bin/sh",
  'echo "$@" >> "$(dirname "$0")/argv.log"',
  'if [ "$1" != "-p" ]; then',
  '  while IFS= read -r line; do [ "$line" = q ] && exit 0; done',
  "  exit 0",
  "fi",
  "while IFS= read -r line; do",
  [[  printf '{"type":"result","subtype":"success","session_id":"sess-1","result":"ok"}\n']],
  "done",
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")

local function argv_log()
  return vim.fn.filereadable(tmp .. "/argv.log") == 1 and vim.fn.readfile(tmp .. "/argv.log") or {}
end

local claude = require("bud.ai.claude")
claude.bin = function()
  return fake
end
claude.secret_env = function(_, cb)
  cb({})
end
claude.check_login = function(_, cb)
  vim.schedule(function()
    cb(true)
  end)
end
local term = require("bud.ai.term")

local root = vim.uv.fs_realpath(tmp .. "/proj")
vim.fn.chdir(root)

-- a headless session with a conversation
claude.send("hi")
wait(function()
  return claude.status() == "ready" and claude.session().session_id == "sess-1"
end, "first reply")
local s = claude.session()

-- opening the TUI stops the headless job and resumes its conversation
local editor = vim.api.nvim_get_current_win()
local win = term.open({ focus = false })
eq(vim.api.nvim_get_current_win(), editor, "focus stays in the editor")
wait(function()
  return #argv_log() == 2
end, "the TUI to start")
eq(argv_log()[2], "--resume sess-1", "the TUI resumes the session")
wait(function()
  return s.job == nil
end, "the headless job to stop")
eq(claude.status(), "term", "the terminal has the session")
eq(claude.send("not now"), false, "nothing else sends while it does")
eq(vim.bo[vim.api.nvim_win_get_buf(win)].buftype, "terminal", "a terminal pane")

-- toggle hides it, still running, and brings the same one back
vim.api.nvim_set_current_win(editor)
term.toggle()
eq(vim.api.nvim_win_is_valid(win), false, "hidden")
eq(term.running(), true, "still running")
term.toggle()
eq(#argv_log(), 2, "the same TUI, not a new one")
vim.cmd.stopinsert()

-- quitting the TUI hands the session back, continuing where it left off
vim.fn.chansend(vim.bo[vim.api.nvim_get_current_buf()].channel, "q\r")
wait(function()
  return not term.running()
end, "the TUI to exit")
eq(claude.status(), "stopped", "handed back")
claude.send("back")
wait(function()
  return #argv_log() == 3 and claude.status() == "ready"
end, "the headless job again")
eq(argv_log()[3]:match("%-%-continue") ~= nil, true, "continues the most recent conversation")

-- a hidden TUI nobody uses is stopped (D49); one on screen isn't
vim.api.nvim_set_current_win(editor)
term.open({ focus = false })
wait(function()
  return #argv_log() == 4
end, "a second TUI")
term.reap(30, vim.uv.hrtime() / 1e6 + 31 * 60 * 1000)
eq(term.running(), true, "on screen, kept")
term.toggle()
term.reap(30, vim.uv.hrtime() / 1e6 + 31 * 60 * 1000)
wait(function()
  return not term.running()
end, "the idle TUI to stop")
eq(claude.status(), "stopped", "handed back after the reap")

-- the layout's chat pane is the TUI
require("bud.layout.panes")
eq(require("bud.layout").providers.chat.available(), true, "chat pane available with claude")

claude._reset()
require("bud.ai.bridge").stop()
vim.fn.delete(tmp, "rf")
print("claude_term_test: ok")
