-- Run with: nvim --clean --headless -l tests/lua/app_test.lua (just lua-test)
-- A cold start's dashboard makes way for a drop on Bud.app (JGA-186)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local app = require("bud.app")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local dir = vim.fn.tempname()
local marker = dir .. "/state/app-drop"
app.file = function()
  return marker
end
app.window = 400
app.interval = 20
local quits = 0
app.quit = function()
  quits = quits + 1
end
local now = 1000
app.launched = function()
  return now
end

-- the dashboard, as bud shows it, with a float like ui2's message window beside it
vim.bo.filetype = "bud-dashboard"
vim.api.nvim_open_win(vim.api.nvim_create_buf(false, true), false, {
  relative = "editor",
  row = 0,
  col = 0,
  width = 10,
  height = 1,
  hide = true,
})

-- headless (a background update's nvim) only clears BUD_APP
vim.env.BUD_APP = "drop"
app.setup({ headless = true })
eq({ vim.fn.filereadable(marker), vim.env.BUD_APP }, { 0, nil }, "nothing left headless")

-- a drop leaves when its bud started and its pid, making the state dir if it
-- has to, and BUD_APP goes, so a bud started in Bud's terminal doesn't inherit it
vim.env.BUD_APP = "drop"
app.setup()
eq(app.drop(), { launched = 1000, pid = vim.fn.getpid() }, "a drop leaves its marker")
eq(vim.env.BUD_APP, nil, "BUD_APP cleared")

-- start runs the watch, as a cold start's dashboard
local function start(launched)
  now = launched
  vim.env.BUD_APP = "start"
  app.setup()
end
-- drop writes a marker as a drop that started at launched, in process pid
local function drop(launched, pid)
  vim.fn.writefile({ tostring(launched), tostring(pid) }, marker)
end
local function settle()
  vim.wait(app.window + 200, function()
    return quits > 0
  end)
end

-- a drop that started with it, already there: the dashboard makes way, ui2's float or not
start(1000.5)
settle()
eq(quits, 1, "the dashboard made way for the drop")

-- one that turns up later, while the dashboard is still watching
quits = 0
vim.fn.delete(marker)
start(2000)
vim.defer_fn(function()
  drop(2000.3, vim.fn.getpid())
end, 150)
settle()
eq(quits, 1, "a drop that arrives a moment later")

-- a marker from an earlier launch, or from a drop whose window is gone, is
-- ignored, or Bud would quit with no window left
quits = 0
drop(1000, vim.fn.getpid())
start(3000)
settle()
eq(quits, 0, "a drop from an earlier launch is ignored")
-- a pid past the largest macOS and Linux hand out by default
drop(4000, 4194305)
start(4000)
settle()
eq(quits, 0, "a drop whose window has closed is ignored")

-- nothing is closed once you've started using the window
drop(5000, vim.fn.getpid())
vim.cmd.vsplit()
start(5000)
settle()
eq(quits, 0, "a dashboard you've split is kept")
vim.cmd.only()

-- outside Bud.app nothing happens at all
vim.env.BUD_APP = nil
app.setup()
settle()
eq(quits, 0, "no BUD_APP, nothing watched")

vim.fn.delete(dir, "rf")
print("app_test: ok")
