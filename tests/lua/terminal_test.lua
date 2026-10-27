-- Run with: nvim --clean --headless -l tests/lua/terminal_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
vim.o.shell = "sh"

local terminal = require("bud.terminal")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local editor = vim.api.nvim_get_current_win()
terminal.toggle()
local win = vim.api.nvim_get_current_win()
eq(terminal.is_open(), true, "open")
eq(vim.bo.buftype, "terminal", "a terminal buffer")
eq(#vim.api.nvim_list_wins(), 2, "a split, not a replacement")
-- the bottom 35% of the screen
eq(vim.api.nvim_win_get_height(win), math.floor((vim.o.lines - vim.o.cmdheight) * 0.35), "35% tall")
eq(vim.fn.win_screenpos(win)[1] > vim.fn.win_screenpos(editor)[1], true, "below the editor")
local buf = terminal.buf_id()

-- Esc Esc is mapped in this terminal only, not in every terminal (fzf's pickers)
eq(vim.fn.maparg("<Esc><Esc>", "t", false, true).buffer, 1, "Esc Esc is buffer-local")
vim.api.nvim_win_call(editor, function()
  eq(vim.fn.maparg("<Esc><Esc>", "t"), "", "no Esc Esc outside the terminal")
end)
-- headless nvim doesn't run terminal-mode keys from feedkeys, so call the
-- mapping (the keypress itself is checked with a UI attached)
vim.fn.maparg("<Esc><Esc>", "t", false, true).callback()
eq(terminal.is_open(), false, "Esc Esc hides it")
terminal.toggle()

-- hiding keeps the shell; showing brings back the same one
terminal.toggle()
eq(terminal.is_open(), false, "hidden")
eq(vim.api.nvim_buf_is_valid(buf), true, "shell kept running")
terminal.toggle()
eq(terminal.buf_id(), buf, "same terminal back")

-- when the shell exits, the next toggle starts a new one
vim.fn.chansend(vim.bo[buf].channel, "exit\n")
vim.wait(3000, function()
  return terminal.buf_id() == nil
end, 20)
eq(terminal.is_open(), false, "closed when the shell exits")
terminal.toggle()
eq(terminal.buf_id() ~= buf, true, "a fresh shell")
terminal.toggle()

print("terminal_test: ok")
