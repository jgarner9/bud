-- Run with: nvim --clean --headless -l tests/lua/app_test.lua (just lua-test)
-- A cold start's dashboard makes way for a drop on Bud.app (JGA-186). This
-- nvim plays the drop; a second headless nvim plays the dashboard, and the
-- two talk over their servers as they do in Bud

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local app = require("bud.app")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local dir = vim.fn.tempname()
vim.fn.mkdir(dir, "p")
app.file = function(role)
  return dir .. "/app-" .. role
end
local began = 1000
app.began = function()
  return began
end

-- dashboard starts a dashboard nvim whose bud started at at; split makes it
-- one you've used. It quits by writing dir/quit
local peer = dir .. "/peer.lua"
vim.fn.writefile({
  'vim.opt.rtp:prepend(vim.env.T_CWD .. "/nvim")',
  'local app = require("bud.app")',
  "app.file = function(role) return vim.env.T_DIR .. '/app-' .. role end",
  "app.began = function() return tonumber(vim.env.T_BEGAN) end",
  "app.quit = function() vim.fn.writefile({}, vim.env.T_DIR .. '/quit'); vim.cmd('qall!') end",
  'vim.bo.filetype = "bud-dashboard"',
  -- a float like ui2's, with the cursor, which doesn't count as using the window
  "vim.api.nvim_open_win(vim.api.nvim_create_buf(false, true), true, { relative = 'editor', row = 0, col = 0, width = 5, height = 1 })",
  'if vim.env.T_SPLIT == "1" then vim.cmd.wincmd("p"); vim.cmd.vsplit() end',
  'vim.env.BUD_APP = "start"',
  "app.setup()",
  "vim.fn.writefile({}, vim.env.T_DIR .. '/ready')",
}, peer)
local function dashboard(at, split)
  vim.fn.delete(dir .. "/quit")
  vim.fn.delete(dir .. "/ready")
  local job = vim.fn.jobstart({ "nvim", "--clean", "--headless", "-c", "luafile " .. peer }, {
    env = { T_CWD = vim.fn.getcwd(), T_DIR = dir, T_BEGAN = tostring(at), T_SPLIT = split and "1" or "0" },
  })
  assert(
    vim.wait(5000, function()
      return vim.fn.filereadable(dir .. "/ready") == 1
    end),
    "the dashboard nvim started"
  )
  return job
end
local function quit()
  vim.wait(1000, function()
    return vim.fn.filereadable(dir .. "/quit") == 1
  end)
  return vim.fn.filereadable(dir .. "/quit") == 1
end
local function drop(at)
  began = at
  vim.env.BUD_APP = "drop"
  app.setup()
end

-- headless (a background update's nvim) only clears BUD_APP
vim.env.BUD_APP = "drop"
app.setup({ headless = true })
eq({ vim.fn.filereadable(app.file("drop")), vim.env.BUD_APP }, { 0, nil }, "nothing left headless")

-- the dashboard first, then the drop: the drop asks it to make way
local job = dashboard(1000)
drop(1000.4)
eq(vim.env.BUD_APP, nil, "BUD_APP cleared, so a bud in Bud's terminal doesn't inherit it")
eq(quit(), true, "the dashboard made way for a drop that came after it")
vim.fn.jobwait({ job }, 1000)

-- the drop first, then the dashboard: it finds the drop still open and makes way
drop(2000)
job = dashboard(2000.5)
eq(quit(), true, "the dashboard made way for a drop that came first")
vim.fn.jobwait({ job }, 1000)

-- a drop from an earlier launch, or one whose window has closed, is ignored,
-- or Bud would quit with no window left
job = dashboard(2010)
eq(quit(), false, "a drop from a launch 10 s before")
vim.fn.jobstop(job)
vim.fn.writefile({ "4000", dir .. "/gone.sock" }, app.file("drop"))
job = dashboard(4000)
eq(quit(), false, "a drop whose nvim is gone")
vim.fn.jobstop(job)

-- a dashboard you've started using is kept
job = dashboard(5000, true)
drop(5000)
eq(quit(), false, "a dashboard you've split is kept")
vim.fn.jobstop(job)

-- nothing at all without a BUD_APP bud.app knows: no role, no record
vim.fn.delete(app.file("drop"))
vim.fn.delete(app.file("start"))
app.role = nil
vim.env.BUD_APP = "other"
app.setup()
eq(
  { app.role, vim.fn.glob(dir .. "/app-*", false, true), vim.env.BUD_APP },
  { nil, {}, nil },
  "an unknown BUD_APP is ignored"
)

-- yield does nothing in an nvim that isn't a cold start's dashboard
app.role = "drop"
local quits = 0
app.quit = function()
  quits = quits + 1
end
vim.bo.filetype = "bud-dashboard"
app.yield()
vim.wait(100)
eq(quits, 0, "only a cold start's dashboard yields")

vim.fn.delete(dir, "rf")
print("app_test: ok")
