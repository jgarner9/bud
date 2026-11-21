-- Run with: nvim --clean --headless -l tests/lua/opencode_test.lua (just lua-test)
-- bud's opencode.nvim keys (D150, BASE D151): each runs its :Opencode
-- subcommand on the opencode backend, and says what's missing otherwise
-- instead of loading the plugin

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
require("bud.config").file = tmp .. "/config.json"
local function backend(name)
  vim.fn.writefile({ vim.json.encode({ ai = { backend = name } }) }, tmp .. "/config.json")
end

local ran = {}
vim.api.nvim_create_user_command("Opencode", function(o)
  local range = o.range == 2 and ("%d,%d "):format(o.line1, o.line2) or ""
  table.insert(ran, range .. o.args)
end, { nargs = "*", range = true })
local notes = {}
vim.notify = function(msg)
  table.insert(notes, msg)
end

local oc = require("bud.ai.opencode")
-- nvim stands in for opencode, since it is on PATH wherever this runs
oc.executable = vim.v.progpath
oc.map()

local function press(lhs)
  vim.fn.maparg(lhs, "n", false, true).callback()
end

backend("opencode")
for _, k in ipairs(oc.keys) do
  press(k.lhs)
end
eq(
  ran,
  { "diff open", "models", "session select", "permission accept", "permission deny" },
  "each key runs its subcommand"
)
eq(notes, {}, "no notice on the opencode backend")

vim.api.nvim_buf_set_lines(0, 0, -1, false, { "a", "b", "c" })
ran = {}
eq(oc.share(2, 3), true, "share runs")
eq(ran, { "2,3 add_visual_selection" }, "share passes the lines as a range")

ran, notes = {}, {}
eq(oc.run(), true, "a bare run toggles")
eq(ran, { "" }, "a bare run is :Opencode with no argument")

backend("claude")
ran, notes = {}, {}
press("<leader>ad")
eq(ran, {}, "another backend runs nothing")
assert(notes[1]:find("bud config set ai.backend opencode", 1, true), "the notice names the fix: " .. tostring(notes[1]))

backend("opencode")
oc.executable = tmp .. "/no-such-opencode"
ran, notes = {}, {}
eq(oc.share(1, 1), false, "share without opencode")
eq(ran, {}, "nothing runs without opencode")
assert(notes[1]:find("bud install --device", 1, true), "the notice names the install: " .. tostring(notes[1]))

eq(oc.setup_opts().default_global_keymaps, false, "opencode.nvim's own keys stay off")
print("ok")
