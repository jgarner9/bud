-- Run with: nvim --clean --headless -l tests/lua/buddy_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
vim.o.columns, vim.o.lines = 120, 40

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
local config = require("bud.config")
config.file = tmp .. "/config.json"

local buddy = require("bud.buddy")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function floats()
  local out = {}
  for _, win in ipairs(vim.api.nvim_list_wins()) do
    if vim.api.nvim_win_get_config(win).relative ~= "" then
      table.insert(out, win)
    end
  end
  return out
end

-- setup maps the key and loads no moves: the first screen pays nothing (D35)
buddy.setup()
eq(vim.fn.maparg("<leader>ub", "n") ~= "", true, "<leader>ub is mapped")
eq(package.loaded["bud.dashboard.moves"], nil, "setup loads no moves")
eq(buddy.on(), false, "off until asked")
eq(#floats(), 0, "no float while off")

-- ui.buddy off (the default): UIEnter doesn't start him
vim.api.nvim_exec_autocmds("UIEnter", {})
vim.wait(50)
eq(buddy.on(), false, "ui.buddy defaults to off")

-- on: one float in the bottom right corner that takes no focus
local moves = require("bud.dashboard.moves")
local player = require("bud.dashboard.player")
player.pause_ms, player.tick_ms = 20, 2
local editor = vim.api.nvim_get_current_win()
buddy.toggle()
eq(buddy.on(), true, "toggled on")
local wins = floats()
eq(#wins, 1, "one float")
local cfg = vim.api.nvim_win_get_config(wins[1])
eq({ cfg.width, cfg.height, cfg.focusable }, { moves.width, moves.height, false }, "the canvas, unfocusable")
eq({ cfg.anchor, cfg.col }, { "SE", vim.o.columns }, "in the bottom right corner")
eq(vim.api.nvim_get_current_win(), editor, "the editor keeps the focus")
local still = buddy.lines()
eq(#still, moves.height, "the canvas height")
eq(
  table.concat(still, "\n"):find("[o.o]", 1, true) ~= nil or table.concat(still, "\n"):find("[-.-]", 1, true) ~= nil,
  true,
  "he's standing there"
)

-- he moves, and settles back between moves
if not vim.wait(2000, function()
  return not vim.deep_equal(buddy.lines(), still)
end, 5) then
  error("the buddy never moved")
end
if not vim.wait(4000, function()
  return vim.deep_equal(buddy.lines(), still)
end, 5) then
  error("the buddy never settled")
end

-- he follows the editor's size
vim.o.columns = 100
vim.api.nvim_exec_autocmds("VimResized", {})
eq(vim.api.nvim_win_get_config(floats()[1]).col, 100, "back in the corner after a resize")

-- too small an editor has no corner to spare; he comes back when it grows
vim.o.columns = 30
vim.api.nvim_exec_autocmds("VimResized", {})
eq(#floats(), 0, "steps aside in a small editor")
eq(buddy.on(), true, "still on")
vim.o.columns = 120
vim.api.nvim_exec_autocmds("VimResized", {})
eq(#floats(), 1, "back when there's room")

-- the dashboard has its own buddy, so the corner one steps aside
local dash = vim.api.nvim_create_buf(false, true)
vim.bo[dash].filetype = "bud-dashboard"
local file = vim.api.nvim_get_current_buf()
vim.api.nvim_win_set_buf(editor, dash)
if not vim.wait(2000, function()
  return #floats() == 0
end, 5) then
  error("the buddy stayed up beside the dashboard")
end
vim.api.nvim_win_set_buf(editor, file)
if not vim.wait(2000, function()
  return #floats() == 1
end, 5) then
  error("the buddy never came back after the dashboard")
end

-- :only closes floats too; he's back by the next move
vim.cmd("only")
eq(#floats(), 0, ":only closed the float")
if not vim.wait(2000, function()
  return #floats() == 1
end, 5) then
  error("the buddy never came back after :only")
end

-- his buffer wiped from under him: a new one, and he still turns off and on
vim.cmd("%bwipeout!")
if
  not vim.wait(2000, function()
    return #floats() == 1 and buddy.lines() ~= nil and #buddy.lines() == moves.height
  end, 5)
then
  error("the buddy never came back after his buffer was wiped")
end
vim.cmd("%bwipeout!")
buddy.toggle()
eq(buddy.on(), false, "turns off with his buffer gone")
buddy.toggle()
eq(#floats(), 1, "and on again")
editor = vim.api.nvim_get_current_win()

-- off: the float and its buffer go
local buf = vim.api.nvim_win_get_buf(floats()[1])
buddy.toggle()
eq(buddy.on(), false, "toggled off")
eq(#floats(), 0, "the float is gone")
eq(vim.api.nvim_buf_is_valid(buf), false, "and its buffer")
eq(buddy.lines(), nil, "nothing to show")

-- ui.buddy = true starts him once the UI is up
local f = assert(io.open(config.file, "w"))
f:write('{"ui": {"buddy": true}}')
f:close()
buddy.setup()
vim.api.nvim_exec_autocmds("UIEnter", {})
if not vim.wait(1000, buddy.on, 5) then
  error("ui.buddy didn't start the buddy")
end
buddy.close()

print("buddy_test: ok")
