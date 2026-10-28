-- Run with: nvim --clean --headless -l tests/lua/settings_test.lua (just lua-test)
-- The settings screen draws what the CLI reports and changes things through it

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local settings = require("bud.settings")
local config = {
  profile = "full",
  terminal = "kitty",
  layout = "default",
  format_on_save = true,
  update = { channel = "stable", auto = true },
  dashboard = { style = "silly", git = true },
  ai = { claude = { autostart = true, resume = "continue", idle_minutes = 30, args = {} }, chat = { pane = "native" } },
  tools = { disabled = { "off-tool" } },
}
local ran = {}
settings.run = function(args, cb)
  table.insert(ran, table.concat(args, " "))
  if args[1] == "config" and args[2] == "set" then
    local key, value = args[3], vim.json.decode(args[4])
    local cur = config
    local parts = vim.split(key, ".", { plain = true })
    for i = 1, #parts - 1 do
      cur = cur[parts[i]]
    end
    cur[parts[#parts]] = value
  end
  vim.schedule(function()
    cb(true, vim.json.encode(config))
  end)
end
require("bud.tools").list = function()
  return {
    { name = "on-tool", manifest = { description = "does a thing" } },
    { name = "off-tool", manifest = {} },
  }
end
package.loaded["bud.lang"] = {
  specs = function()
    return { go = {}, lua = {} }
  end,
  enabled = function()
    return { { name = "go" } }
  end,
}

-- pure layout
local lines, rows = settings.lines(config, { { name = "go", on = true } }, { { name = "t", on = false } })
local text = table.concat(lines, "\n")
eq(text:find("Format on save%s+on") ~= nil, true, "a bool")
eq(text:find("Channel%s+stable") ~= nil, true, "a choice")
eq(text:find("Extra flags%s+%(none%)") ~= nil, true, "an empty list")
eq(text:find("[x] go", 1, true) ~= nil and text:find("[ ] t", 1, true) ~= nil, true, "languages and tools")
local n = 0
for _ in pairs(rows) do
  n = n + 1
end
eq(n > 20, true, "every setting is a row")

-- the screen: Enter flips a bool through bud config set
settings.open()
assert(vim.wait(2000, function()
  return #ran >= 1
end))
vim.wait(50)
local function go_to(pattern)
  for i, l in ipairs(vim.api.nvim_buf_get_lines(settings.buf(), 0, -1, false)) do
    if l:find(pattern) then
      vim.api.nvim_win_set_cursor(0, { i, 4 })
      return
    end
  end
  error("no line " .. pattern)
end
go_to("Format on save")
ran = {}
settings.change()
assert(vim.wait(2000, function()
  return vim.tbl_contains(ran, "config ls")
end))
eq(ran[1], "config set format_on_save false", "flipped through the CLI")
vim.wait(50)
eq(
  table.concat(vim.api.nvim_buf_get_lines(settings.buf(), 0, -1, false), "\n"):find("Format on save%s+off") ~= nil,
  true,
  "redrawn"
)

-- a choice goes through vim.ui.select
vim.ui.select = function(items, _, cb)
  cb(items[2])
end
go_to("Channel")
ran = {}
settings.change()
assert(vim.wait(2000, function()
  return #ran >= 2
end))
eq(ran[1], 'config set update.channel "nightly"', "picked")

-- a tool
go_to("off%-tool")
ran = {}
settings.change()
assert(vim.wait(2000, function()
  return #ran >= 1
end))
eq(ran[1], "tools enable off-tool", "a tool turned on")

-- unset keys come back as JSON null, and the screen still redraws
config.tools.disabled = vim.NIL
config.ai.claude.command = vim.NIL
settings.refresh()
assert(
  vim.wait(2000, function()
    local shown = table.concat(vim.api.nvim_buf_get_lines(settings.buf(), 0, -1, false), "\n")
    return shown:find("%[x%] off%-tool") ~= nil
  end),
  "redraws with nulls"
)

print("settings_test: ok")
