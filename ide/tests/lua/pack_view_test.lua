-- Run with: nvim --clean --headless -l tests/lua/pack_view_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
dofile("tests/lua/lib/nui.lua")

local view = require("bud.pack_view")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

-- what a lazy spec waits for
eq(view.trigger({ event = "InsertEnter" }), "event InsertEnter", "event")
eq(view.trigger({ ft = { "go", "lua" }, cmd = "Foo" }), "ft go, lua; cmd Foo", "ft and cmd")
eq(view.trigger({ keys = { { "<leader>x", desc = "x" }, "gd" } }), "keys <leader>x gd", "keys")

-- rows from fake specs, sorted by name, loaded or waiting
local specs = {
  ["zeta.nvim"] = { src = "o/zeta.nvim" },
  ["Alpha.nvim"] = { src = "o/Alpha.nvim", version = vim.version.range("2"), event = "UIEnter" },
  ["mid.nvim"] = { src = "o/mid.nvim", cmd = "Mid" },
}
local lazy = { ["Alpha.nvim"] = { loaded = false }, ["mid.nvim"] = { loaded = true } }
local plugs = {
  { spec = { name = "zeta.nvim", src = "https://github.com/o/zeta.nvim" }, rev = "1234567890abcdef", path = "/p/zeta" },
  { spec = { name = "Alpha.nvim", src = "https://github.com/o/Alpha.nvim" }, rev = "abcdef1234567890", path = "/p/a" },
}
local entries = view.entries({ "zeta.nvim", "Alpha.nvim", "mid.nvim" }, specs, lazy, plugs)
eq(
  vim.tbl_map(function(e)
    return { e.name, e.loaded, e.trigger }
  end, entries),
  { { "Alpha.nvim", false, "event UIEnter" }, { "mid.nvim", true, nil }, { "zeta.nvim", true, nil } },
  "sorted, lazy ones waiting until loaded"
)
eq(entries[1].version, "2.0.0 - 3.0.0", "version range")
eq(entries[3].src, "https://github.com/o/zeta.nvim", "source from vim.pack")
eq(view.lines(entries), {
  " ○ Alpha.nvim  abcdef1  2.0.0 - 3.0.0  · on event UIEnter",
  " ● mid.nvim",
  " ● zeta.nvim   1234567",
}, "rows")

-- the float opens with a row per plugin; <CR> opens the details
local pack = require("bud.pack")
pack.names, pack.specs, pack.lazy = { "zeta.nvim", "Alpha.nvim", "mid.nvim" }, specs, lazy
vim.pack.get = function()
  return plugs
end
local h = view.open()
eq(vim.api.nvim_get_current_win(), h.win, "focused")
eq(#vim.api.nvim_buf_get_lines(h.buf, 0, -1, false), 3, "a row per plugin")
vim.api.nvim_feedkeys(vim.keycode("<CR>"), "xt", false)
local shown = vim.api.nvim_buf_get_lines(0, 0, -1, false)
eq(shown[1], " source   https://github.com/o/Alpha.nvim", "details of the plugin under the cursor")
eq(shown[5], " status   waiting for event UIEnter", "its status")

print("pack_view_test: ok")
