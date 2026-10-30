-- Run with: nvim --clean --headless -l tests/lua/cli_test.lua (just lua-test)
-- bud.cli.terminal and :BudUpdate, against a fake bud that prints its args

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
vim.o.shell = "sh"

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
local fake = tmp .. "/bud"
vim.fn.writefile({ "#!/bin/sh", 'echo "ran: $*"' }, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")
vim.env.BUD_BIN = fake

require("bud.core.autocmds")

local function ran(want)
  if
    not vim.wait(5000, function()
      return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n"):find(want, 1, true) ~= nil
    end, 10)
  then
    error("no " .. want .. " in the terminal: " .. table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n"))
  end
end

-- :BudUpdate runs bud update in a terminal, passing its arguments on
vim.cmd("BudUpdate")
eq(vim.bo.buftype, "terminal", "a terminal buffer")
ran("ran: update")
vim.cmd("BudUpdate --check")
ran("ran: update --check")

-- no bud to run says so instead of opening anything
vim.env.BUD_BIN = tmp .. "/missing"
vim.env.PATH = tmp .. "/empty"
local notified
vim.notify = function(msg)
  notified = msg
end
local buf = vim.api.nvim_get_current_buf()
require("bud.cli").terminal({ "update" })
eq(notified, "bud: can't find the bud binary", "missing binary")
eq(vim.api.nvim_get_current_buf(), buf, "no new buffer")

vim.fn.delete(tmp, "rf")
print("cli_test: ok")
