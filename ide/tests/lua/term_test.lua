-- Run with: nvim --clean --headless -l tests/lua/term_test.lua (just lua-test)
-- The AI pane (D48, D137, D139): OpenCode's own TUI in a side pane of the
-- project, hidden and shown without stopping it, stopped when idle, full
-- screen on request

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

local tmp = vim.uv.fs_realpath(vim.fn.tempname() .. "/") or vim.fn.tempname()
vim.fn.mkdir(tmp .. "/bin", "p")
vim.fn.mkdir(tmp .. "/proj", "p")
vim.fn.mkdir(tmp .. "/other", "p")
tmp = vim.uv.fs_realpath(tmp)
local proj = vim.uv.fs_realpath(tmp .. "/proj")
local other = vim.uv.fs_realpath(tmp .. "/other")
require("bud.config").file = tmp .. "/config.json"

-- a fake opencode TUI: logs its argv and dir, then waits for a q line
vim.fn.writefile({
  "#!/bin/sh",
  'printf "%s|%s\\n" "$*" "$(pwd -P)" >> "$(dirname "$0")/runs.log"',
  "echo '> opencode'",
  'while IFS= read -r line; do [ "$line" = q ] && exit 0; done',
}, tmp .. "/bin/opencode")
vim.fn.setfperm(tmp .. "/bin/opencode", "rwxr-xr-x")
local function runs()
  local f = tmp .. "/bin/runs.log"
  return vim.fn.filereadable(f) == 1 and vim.fn.readfile(f) or {}
end

local term = require("bud.ai.term")

-- not installed: the layout's chat pane isn't available, and opening says why
local path = vim.env.PATH
vim.env.PATH = "/nonexistent"
eq(term.available(), false, "unavailable without opencode")
local notes = {}
local notify = vim.notify
vim.notify = function(msg)
  table.insert(notes, msg)
end
eq(term.open({ root = proj, focus = false }), nil, "nothing opens")
assert(notes[1]:find("opencode isn't installed: run bud ai setup", 1, true), notes[1])
vim.notify = notify

vim.env.PATH = tmp .. "/bin:" .. path
vim.cmd.cd(proj)
eq(term.available(), true, "available with opencode")

-- opening it runs opencode on the project, in a side pane that keeps focus
local editor = vim.api.nvim_get_current_win()
local win = term.open({ focus = false })
assert(win, "a pane")
eq(vim.w[win].bud_side, true, "marked as a side pane")
vim.api.nvim_buf_call(vim.api.nvim_win_get_buf(win), function()
  eq(vim.fn.maparg("k", "t", false, true).buffer, 1, "jk mapped in the pane")
end)
eq(vim.api.nvim_get_current_win(), editor, "focus stays in the editor")
wait(function()
  return #runs() == 1
end, "the TUI to start")
eq(runs()[1], proj .. "|" .. proj, "opencode on the project, run in it")
eq(vim.bo[vim.api.nvim_win_get_buf(win)].buftype, "terminal", "a terminal pane")

-- toggle hides it, still running, and brings the same one back
term.toggle()
eq(vim.api.nvim_win_is_valid(win), false, "hidden")
eq(term.running(), true, "still running")
term.toggle()
eq(#runs(), 1, "the same TUI, not a new one")
vim.cmd.stopinsert()

-- quitting the TUI closes the pane
vim.fn.chansend(vim.bo[vim.api.nvim_get_current_buf()].channel, "q\r")
wait(function()
  return not term.running()
end, "the TUI to exit")

-- a hidden TUI nobody uses is stopped (D49); one on screen isn't
vim.api.nvim_set_current_win(editor)
term.open({ focus = false })
wait(function()
  return #runs() == 2
end, "a second TUI")
term.reap(30, vim.uv.hrtime() / 1e6 + 31 * 60 * 1000)
eq(term.running(), true, "on screen, kept")
term.toggle()
term.reap(30, vim.uv.hrtime() / 1e6 + 31 * 60 * 1000)
wait(function()
  return not term.running()
end, "the idle TUI to stop")

-- full screen (D137): the same TUI in a tab of its own, and back to the pane
vim.api.nvim_set_current_win(editor)
local tabs = #vim.api.nvim_list_tabpages()
term.open({ root = other, focus = false })
local fbuf = vim.api.nvim_win_get_buf(term.open({ root = other, focus = false }))
term.full({ root = other })
eq(#vim.api.nvim_list_tabpages(), tabs + 1, "a tab of its own")
eq(vim.api.nvim_get_current_buf(), fbuf, "the same TUI, full screen")
eq(#vim.api.nvim_tabpage_list_wins(0), 1, "nothing else in that tab")
vim.cmd.stopinsert()
term.full({ root = other })
eq(#vim.api.nvim_list_tabpages(), tabs, "the tab goes")
eq(#vim.fn.win_findbuf(fbuf), 1, "back in its pane")
eq(term.running(other), true, "still running")
vim.cmd.stopinsert()

-- ai.agent.chat_layout = "full" makes the toggle open it full screen
local cfg = require("bud.config")
local get = cfg.get
cfg.get = function(key, default)
  if key == "ai.agent.chat_layout" then
    return "full"
  end
  return get(key, default)
end
vim.cmd.tcd(other)
term.toggle()
eq(#vim.api.nvim_list_tabpages(), tabs + 1, "the toggle opens it full screen")
vim.cmd.stopinsert()
term.toggle()
eq(#vim.api.nvim_list_tabpages(), tabs, "and the toggle goes back")
cfg.get = get
vim.cmd.stopinsert()

-- the layout's chat pane is the TUI
require("bud.layout.panes")
eq(require("bud.layout").providers.chat.available(), true, "chat pane available with opencode")

term.stop_all()
vim.fn.delete(tmp, "rf")
print("term_test: ok")
