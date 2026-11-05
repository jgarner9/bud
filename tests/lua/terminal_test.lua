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

-- Esc is mapped in this terminal only, not in every terminal (fzf's pickers)
local esc = vim.fn.maparg("<Esc>", "t", false, true)
eq(esc.buffer, 1, "Esc is buffer-local")
vim.api.nvim_win_call(editor, function()
  eq(vim.fn.maparg("<Esc>", "t"), "", "no Esc mapping outside the terminal")
end)
-- headless nvim doesn't run terminal-mode keys from feedkeys, so call the
-- mapping (the keypress itself is checked with a UI attached): a lone Esc
-- reaches the shell once the window for a second one has passed
local termkeys = require("bud.termkeys")
termkeys.window = 50
local sent = {}
local chansend = vim.fn.chansend
vim.fn.chansend = function(chan, data)
  table.insert(sent, data)
  return chansend(chan, data)
end
esc.callback()
eq(
  vim.wait(1000, function()
    return #sent == 1
  end, 10),
  true,
  "one Esc is passed on"
)
eq(sent[1], "\27", "as Esc")
-- Esc Esc goes to Normal mode instead, and passes nothing on
esc.callback()
esc.callback()
vim.wait(150)
eq(#sent, 1, "Esc Esc reaches no program")
vim.fn.chansend = chansend
eq(terminal.is_open(), true, "and doesn't hide it: <leader>t does")
terminal.toggle()
terminal.toggle()

-- hiding keeps the shell; showing brings back the same one
terminal.toggle()
eq(terminal.is_open(), false, "hidden")
eq(vim.api.nvim_buf_is_valid(buf), true, "shell kept running")
terminal.toggle()
eq(terminal.buf_id(), buf, "same terminal back")

-- when the shell exits, the next toggle starts a new one
-- a newline first: the shell still has the Esc passed on above
vim.fn.chansend(vim.bo[buf].channel, "\nexit\n")
vim.wait(3000, function()
  return terminal.buf_id() == nil
end, 20)
eq(terminal.is_open(), false, "closed when the shell exits")
terminal.toggle()
eq(terminal.buf_id() ~= buf, true, "a fresh shell")
terminal.toggle()

-- arrange: the terminal goes under the editor and the side pane (Claude
-- Code's), right of the explorer, which keeps the full height
vim.o.columns, vim.o.lines = 200, 50
vim.cmd("only")
local main = vim.api.nvim_get_current_win()
local explorer = { win = function() end }
package.loaded["bud.explorer"] = explorer
vim.cmd("topleft vsplit")
local tree = vim.api.nvim_get_current_win()
vim.api.nvim_win_set_width(tree, 24)
vim.wo[tree].winfixwidth = true
explorer.win = function()
  return tree
end
local side =
  vim.api.nvim_open_win(vim.api.nvim_create_buf(false, true), false, { split = "right", win = -1, width = 60 })
vim.w[side].bud_side = true
vim.wo[side].winfixwidth = true
vim.api.nvim_set_current_win(main)
terminal.toggle()
local term_win = vim.api.nvim_get_current_win()
eq(vim.fn.winlayout(), {
  "row",
  { { "leaf", tree }, { "col", { { "row", { { "leaf", main }, { "leaf", side } } }, { "leaf", term_win } } } },
}, "explorer | (editor side) over the terminal")
eq(vim.api.nvim_win_get_width(tree), 24, "the explorer keeps its width")
eq(vim.api.nvim_win_get_width(side), 60, "the side pane keeps its width")
eq(vim.api.nvim_get_current_win(), term_win, "the cursor in the terminal")
terminal.toggle()
-- a side pane opened while the terminal is up goes above it too
terminal.toggle()
vim.api.nvim_win_close(side, true)
side = vim.api.nvim_open_win(vim.api.nvim_create_buf(false, true), false, { split = "right", win = -1, width = 60 })
vim.w[side].bud_side = true
vim.wo[side].winfixwidth = true
terminal.arrange()
term_win = vim.fn.win_getid(vim.fn.bufwinnr(terminal.buf_id()))
eq(vim.fn.winlayout(), {
  "row",
  { { "leaf", tree }, { "col", { { "row", { { "leaf", main }, { "leaf", side } } }, { "leaf", term_win } } } },
}, "rearranged")
eq({ vim.api.nvim_win_get_width(tree), vim.api.nvim_win_get_width(side) }, { 24, 60 }, "widths kept")
terminal.toggle()
package.loaded["bud.explorer"] = nil

-- <leader>w is <C-w>, for moving between windows
vim.g.mapleader = " "
require("bud.core.keymaps")
eq(vim.fn.maparg("<leader>w", "n"), "<C-W>", "<leader>w")

print("terminal_test: ok")
