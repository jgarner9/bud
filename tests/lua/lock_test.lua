-- Run with: nvim --clean --headless -l tests/lua/lock_test.lua (just lua-test)
-- Locked panes (D136): only their toggles close them, and buffers that land
-- in them go to the editor window

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(2000, cond, 10) then
    error("timed out waiting for " .. what, 2)
  end
end

local lock = require("bud.lock")
lock.setup()

local errors = {}
vim.notify = function(msg, level)
  if level == vim.log.levels.ERROR then
    table.insert(errors, msg)
  end
end

local editor = vim.api.nvim_get_current_win()
local opened = 0

-- pane opens a scratch side pane like the explorer does, locked
local function pane()
  opened = opened + 1
  local buf = vim.api.nvim_create_buf(false, true)
  vim.bo[buf].filetype = "fakepane"
  -- not an empty unnamed buffer, which :edit would take over in place
  vim.api.nvim_buf_set_name(buf, "fakepane://" .. opened)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, { "pane" })
  local win = vim.api.nvim_open_win(buf, false, { split = "left", win = -1, width = 20 })
  lock.lock(win, {
    name = "the explorer",
    key = "<leader>e",
    reopen = pane,
    allow = function(b)
      return vim.bo[b].filetype == "fakepane"
    end,
  })
  return win
end

local function panes()
  local found = {}
  for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if lock.spec(w) then
      table.insert(found, w)
    end
  end
  return found
end

-- :close in the pane opens it again, with an error naming the toggle
local win = pane()
vim.api.nvim_set_current_win(win)
vim.cmd.close()
wait(function()
  return #panes() == 1
end, "the pane to reopen")
eq(opened, 2, "reopened once")
eq(#errors, 1, "one error")
assert(errors[1]:find("the explorer is locked", 1, true), errors[1])
assert(errors[1]:find("<leader>e", 1, true), "names the toggle: " .. errors[1])
eq(vim.api.nvim_get_current_win(), editor, "focus back where it was")

-- :only from the editor closes it, and it comes back
errors = {}
vim.cmd.only()
wait(function()
  return #panes() == 1
end, "the pane after :only")
eq(#errors, 1, "an error after :only")

-- a file opened in the pane goes to the editor window instead
errors = {}
win = panes()[1]
local own = vim.api.nvim_win_get_buf(win)
local file = vim.fn.tempname() .. ".txt"
vim.fn.writefile({ "hello" }, file)
vim.api.nvim_set_current_win(win)
vim.cmd.edit(vim.fn.fnameescape(file))
wait(function()
  return vim.api.nvim_win_get_buf(win) == own
end, "the pane's own buffer back")
eq(vim.fn.bufname(vim.api.nvim_win_get_buf(editor)), file, "the file is in the editor")
eq(vim.api.nvim_get_current_win(), editor, "the cursor went with it")
eq(#errors, 1, "an error for the buffer change")
assert(errors[1]:find("opened in the editor", 1, true), errors[1])

-- a buffer the pane allows (its own kind) stays
errors = {}
local other = vim.api.nvim_create_buf(false, true)
vim.bo[other].filetype = "fakepane"
vim.api.nvim_win_set_buf(win, other)
vim.wait(100)
eq(vim.api.nvim_win_get_buf(win), other, "an allowed buffer stays")
eq(#errors, 0, "no error for an allowed buffer")

-- the toggle's own close is quiet and final
lock.close(win)
vim.wait(100)
eq(#panes(), 0, "closed by the toggle")
eq(#errors, 0, "no error for the toggle")
eq(vim.api.nvim_win_is_valid(win), false, "the window is gone")

-- hide works the same way
win = pane()
lock.hide(win)
vim.wait(100)
eq(#panes(), 0, "hidden by the toggle")

-- quietly lets the layout rearrange without reopening
win = pane()
lock.quietly(function()
  vim.api.nvim_win_close(win, false)
end)
vim.wait(100)
eq(#panes(), 0, "closed quietly")

-- restoring a session (:only first) is quiet too
local sessions = require("bud.sessions")
win = pane()
local session_file = vim.fn.tempname() .. ".vim"
vim.fn.writefile({ "silent only" }, session_file)
sessions.file = function()
  return session_file
end
sessions.enabled = function()
  return true
end
errors = {}
eq(sessions.restore("/x"), true, "restored")
vim.wait(100)
eq({ #panes(), #errors }, { 0, 0 }, "the session closed the pane without a fight")

-- editor() skips panes and side windows
win = pane()
vim.api.nvim_set_current_win(win)
eq(lock.editor(), editor, "editor from inside a pane")
lock.close(win)

-- the real <leader>t terminal: :close brings it back, the toggle puts it away
local terminal = require("bud.terminal")
errors = {}
vim.api.nvim_set_current_win(editor)
local tw = terminal.open({ focus = false })
eq(vim.api.nvim_get_current_win(), editor, "focus = false keeps the cursor")
vim.api.nvim_win_close(tw, false)
wait(function()
  return terminal.is_open()
end, "the terminal to reopen")
eq(#errors, 1, "an error for closing the terminal")
assert(errors[1]:find("<leader>t", 1, true), errors[1])
local job = terminal.job()
terminal.toggle()
vim.wait(100)
eq(terminal.is_open(), false, "the toggle hides it")
eq(terminal.job(), job, "the same shell keeps running")
eq(#errors, 1, "no new error for the toggle")

-- :q in the last window that isn't a pane takes the panes with it (here in
-- a second tab, so the test's Neovim stays)
vim.cmd.tabnew()
local tab_editor = vim.api.nvim_get_current_win()
pane()
pane()
eq(#vim.api.nvim_tabpage_list_wins(0), 3, "editor and two panes")
local tabs = #vim.api.nvim_list_tabpages()
vim.api.nvim_set_current_win(tab_editor)
vim.cmd.quit()
vim.wait(100)
eq(#vim.api.nvim_list_tabpages(), tabs - 1, "the tab closed, panes and all")
eq(#vim.tbl_filter(function(w)
  return lock.spec(w) ~= nil
end, vim.api.nvim_list_wins()), 0, "nothing reopened")

print("lock_test: ok")
