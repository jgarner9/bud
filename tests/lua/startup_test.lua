-- Run with: nvim --clean --headless -l tests/lua/startup_test.lua (just lua-test)
-- Timing the first screen: from bud's own start when it's believable, once

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local startup = require("bud.startup")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local clock = 100e6
startup.now = function()
  return clock
end

-- bud's start counts when it's from the last 10 seconds, else init.lua's
startup.start(tostring(clock - 5000))
eq(startup.began, clock - 5000, "BUD_LAUNCHED used")
startup.start(tostring(clock - 20e6))
eq(startup.began, clock, "a stale one is ignored")
startup.start(tostring(clock + 1))
eq(startup.began, clock, "one from the future too")
startup.start(nil)
eq(startup.began, clock, "none: from now")
startup.start("junk")
eq(startup.began, clock, "junk: from now")

-- headless has no first screen, so nothing is watched
startup.watch()
eq(vim.g.bud_startup_ms, nil, "nothing timed headless")

-- finish records the milliseconds once, then tells whoever shows them
local fired = 0
vim.api.nvim_create_autocmd("User", {
  pattern = "BudStartup",
  callback = function()
    fired = fired + 1
  end,
})
startup.start(tostring(clock - 30000))
clock = clock + 12500
startup.finish()
eq(vim.g.bud_startup_ms, 42, "30 ms before nvim plus 12.5 in it")
clock = clock + 1e6
startup.finish()
eq(vim.g.bud_startup_ms, 42, "later redraws don't change it")
vim.wait(1000, function()
  return fired > 0
end)
eq(fired, 1, "User BudStartup fired once")

print("startup_test: ok")
