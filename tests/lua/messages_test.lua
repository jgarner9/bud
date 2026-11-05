-- Run with: nvim --clean --headless -l tests/lua/messages_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local config = require("bud.config")
local messages = require("bud.messages")
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
config.file = tmp .. "/config.json"

-- this Neovim has ui2 under one of its names
assert(messages.module(), "no ui2 module")

-- turned off: not enabled
local enabled = false
local orig = messages.module
messages.module = function()
  return {
    enable = function()
      enabled = true
    end,
  }
end
vim.fn.writefile({ '{"ui": {"messages": false}}' }, config.file)
assert(messages.setup() == false and not enabled, "enabled while off")

-- on by default
vim.fn.delete(config.file)
assert(messages.setup() == true and enabled, "not enabled by default")
-- ui.cmdline_float = false: ui2 on, the command line left at the bottom
vim.fn.writefile({ '{"ui": {"cmdline_float": false}}' }, config.file)
local floated = false
local float = messages.float
messages.float = function()
  floated = true
end
assert(messages.setup() == true and not floated, "floated while off")
vim.fn.delete(config.file)
assert(messages.setup() == true and floated, "not floated by default")
messages.float = float
messages.module = orig

-- float moves ui2's command-line window while the command line is open and
-- puts it back after; a fake ui2 whose cmd window is a float like ui2's
vim.o.columns, vim.o.lines = 120, 40
local buf = vim.api.nvim_create_buf(false, true)
local cmd = vim.api.nvim_open_win(buf, false, {
  relative = "laststatus",
  row = 1,
  col = 0,
  width = 10000,
  height = 1,
  style = "minimal",
  focusable = false,
})
local group = messages.float({ wins = { cmd = cmd } })
local function enter(type)
  vim.api.nvim_exec_autocmds("CmdlineEnter", { group = group, pattern = type })
end
local function leave()
  vim.api.nvim_exec_autocmds("CmdlineLeave", { group = group })
end
local function title()
  local t = vim.api.nvim_win_get_config(cmd).title
  return t and t[1] and t[1][1] or ""
end

enter(":")
local c = vim.api.nvim_win_get_config(cmd)
assert(c.relative == "editor" and c.width == 80 and c.row == 10 and c.col == 19, vim.inspect(c))
assert(title():find("Command", 1, true), title())
assert(vim.deep_equal(vim.g.ui_cmdline_pos, { 13, 20 }), vim.inspect(vim.g.ui_cmdline_pos))

-- a nested expression retitles it, and leaving goes back to the outer title
enter("=")
assert(title():find("Expression", 1, true), title())
leave()
assert(title():find("Command", 1, true) and vim.api.nvim_win_get_config(cmd).relative == "editor", title())

leave()
c = vim.api.nvim_win_get_config(cmd)
assert(
  c.relative == "laststatus" and (c.border == nil or c.border == "none") and vim.g.ui_cmdline_pos == nil,
  vim.inspect(c)
)

-- a type with no title (a debug prompt) stays at the bottom
enter(">")
assert(vim.api.nvim_win_get_config(cmd).relative == "laststatus", "floated a debug prompt")
leave()

-- leaving puts back what was there before, ui2's highlights or a user's
vim.wo[cmd].winhighlight = "Normal:MsgArea,Search:"
enter(":")
assert(vim.wo[cmd].winhighlight:find("NormalFloat", 1, true), vim.wo[cmd].winhighlight)
leave()
assert(vim.wo[cmd].winhighlight == "Normal:MsgArea,Search:", vim.wo[cmd].winhighlight)

-- a wrapped command takes more rows, so completion opens under all of them
assert(messages.rows("", 80) == 1 and messages.rows(("x"):rep(78), 80) == 1, "one row")
assert(messages.rows(("x"):rep(79), 80) == 2 and messages.rows(("x"):rep(200), 80) == 3, "wrapped rows")

-- ui2 taking the window back mid-command (an expanded message): completion
-- follows it to the bottom instead of opening where the float was
enter(":")
assert(vim.g.ui_cmdline_pos, "no menu position")
vim.api.nvim_win_set_config(cmd, { relative = "laststatus", row = 1, col = 0, width = 10000 })
vim.api.nvim_exec_autocmds("CmdlineChanged", { group = group, pattern = ":" })
assert(vim.g.ui_cmdline_pos == nil, vim.inspect(vim.g.ui_cmdline_pos))
leave()

-- a narrow editor: the float fits inside it
vim.o.columns = 50
enter("/")
c = vim.api.nvim_win_get_config(cmd)
assert(c.width == 46 and c.col == 1 and title():find("Search", 1, true), vim.inspect(c))
leave()

print("messages_test: ok")
