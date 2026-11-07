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

-- the dashboard, as bud shows it
vim.bo.filetype = "bud-dashboard"

-- a drop leaves its marker, making the state dir if it has to, and BUD_APP
-- goes, so a bud started in Bud's terminal doesn't inherit it
vim.env.BUD_APP = "drop"
app.setup()
eq(vim.fn.filereadable(marker), 1, "a drop leaves its marker")
eq(vim.env.BUD_APP, nil, "BUD_APP cleared")

-- a cold start's dashboard quits when a drop arrives in its first seconds
vim.env.BUD_APP = "start"
app.setup()
vim.fn.writefile({}, marker)
vim.wait(1000, function()
  return quits > 0
end)
eq(quits, 1, "the dashboard made way for the drop")

-- with no drop it stays, and stops watching
quits = 0
local old = os.time() - 3600
vim.uv.fs_utime(marker, old, old)
vim.env.BUD_APP = "start"
app.setup()
vim.wait(app.window + 200)
eq(quits, 0, "a marker from an old drop is ignored")
vim.fn.writefile({}, marker)
vim.wait(200)
eq(quits, 0, "a drop after the window is a drop while Bud runs")

-- nothing is closed once you've started using the window
vim.uv.fs_utime(marker, old, old)
vim.env.BUD_APP = "start"
app.setup()
vim.cmd.vsplit()
vim.fn.writefile({}, marker)
vim.wait(app.window + 200)
eq(quits, 0, "a dashboard you've split is kept")
vim.cmd.only()

-- outside Bud.app nothing happens at all
vim.env.BUD_APP = nil
app.setup()
vim.wait(app.window + 100)
eq(quits, 0, "no BUD_APP, nothing watched")

vim.fn.delete(dir, "rf")
print("app_test: ok")
