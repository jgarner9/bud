-- Run with: nvim --clean --headless -l tests/lua/escape_test.lua (just lua-test)
-- jk leaves Insert mode with no lag: j goes in at once, a quick k takes it back out

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local escape = require("bud.escape")
local clock = 0
escape.now = function()
  return clock
end
escape.setup()

local function typed(keys)
  vim.api.nvim_feedkeys(vim.keycode(keys), "xt", false)
end

local function line_after(keys)
  vim.api.nvim_buf_set_lines(0, 0, -1, false, { "" })
  vim.api.nvim_win_set_cursor(0, { 1, 0 })
  typed(keys)
  return vim.api.nvim_get_current_line()
end

-- a quick jk leaves Insert mode with no j or k left: the x after it is
-- Normal mode's x, deleting the space under the cursor
eq(line_after("ihello jkx"), "hello", "jk then x")

-- a k on its own, or after another key, is a k
eq(line_after("ikey"), "key", "a k on its own")
eq(line_after("ijak"), "jak", "a k after another key")

-- a slow jk types both and stays in Insert mode
escape.window = -1 -- every j is too slow now
eq(line_after("ijkx"), "jkx", "a slow jk")
escape.window = 200

print("escape_test: ok")
