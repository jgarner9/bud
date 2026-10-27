-- Run with: nvim --clean --headless -l tests/lua/layout_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
vim.o.columns, vim.o.lines = 200, 50

local layout = require("bud.layout")
local terminal = require("bud.terminal")
local root = vim.fn.getcwd()

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

-- the shipped layouts all load
layout.dir = root .. "/layouts"
eq(layout.names(), { "debug", "default", "focus", "review" }, "shipped layouts")
for _, name in ipairs(layout.names()) do
  local spec, err = layout.load(name)
  eq(err, nil, name .. " loads")
  eq(type(spec.description), "string", name .. " has a description")
end
local _, missing = layout.load("nope")
eq(missing:match("no layout") ~= nil, true, "unknown layout")

-- validation
for spec, want in pairs({
  [{ description = "x", nope = 1 }] = 'unknown field "nope"',
  [{ panes = {} }] = "needs a description",
  [{ description = "x", panes = { { kind = "tree", side = "up", size = 3 } } }] = "side must be",
  [{ description = "x", panes = { { side = "left", size = 3 } } }] = "needs a kind",
  [{ description = "x", panes = { { kind = "tree", side = "left" } } }] = "needs a size",
  [{ description = "x", panes = { { kind = "tree", side = "left", size = 3, extra = true } } }] = "unknown field",
  [{ description = "x", terminal = { height = 0 } }] = "terminal.height",
}) do
  local got = layout.validate(spec) or ""
  eq(got:find(want, 1, true) ~= nil, true, "validate: " .. want .. " (got " .. got .. ")")
end
eq(layout.validate({ description = "x", panes = {}, terminal = { height = 12 }, diff = true }), nil, "a valid one")

-- fake providers instead of neo-tree: a scratch split on the asked side
local opened = {}
local function fake(kind)
  return {
    open = function(pane)
      table.insert(opened, kind)
      local buf = vim.api.nvim_create_buf(false, true)
      local split = pane.side == "bottom" and "below" or pane.side
      return vim.api.nvim_open_win(buf, false, { split = split, win = -1 })
    end,
  }
end
-- load panes.lua first, so these replace its providers
require("bud.layout.panes")
layout.register("tree", fake("tree"))
layout.register("git", {
  available = function()
    return false
  end,
  open = fake("git").open,
})
layout.providers.chat = nil

local dir = vim.fn.tempname()
vim.fn.mkdir(dir, "p")
layout.dir = dir
local function write(name, src)
  vim.fn.writefile(vim.split(src, "\n"), dir .. "/" .. name .. ".lua")
end
write(
  "wide",
  [[return {
  description = "tree, chat and logs",
  panes = {
    { kind = "tree", side = "left", size = 30 },
    { kind = "chat", side = "right", size = 60 },
    { kind = "tree", side = "bottom", size = 0.2 },
  },
  terminal = { height = 12 },
}]]
)
write("gitty", [[return { description = "git", panes = { { kind = "git", side = "left", size = 40 } } }]])
write("plain", [[return { description = "just the editor", panes = {} }]])
write("broken", [[return { description = 1 }]])

local editor = vim.api.nvim_get_current_win()
eq(layout.apply("wide"), true, "apply")
eq(opened, { "tree", "tree" }, "chat has no provider, so it's skipped")
eq(#vim.api.nvim_list_wins(), 3, "editor plus two panes")
eq(vim.api.nvim_get_current_win(), editor, "focus goes back to the editor")
local left, bottom = layout.wins_open()[1], layout.wins_open()[2]
eq(vim.api.nvim_win_get_width(left), 30, "30 columns")
eq(vim.wo[left].winfixwidth, true, "fixed width")
eq(vim.api.nvim_win_get_height(bottom), math.floor((vim.o.lines - vim.o.cmdheight) * 0.2), "20% tall")
eq(terminal.height, 12, "the layout sets the terminal's height")
eq(layout.current, "wide", "current")

-- applying again replaces the panes rather than stacking them
opened = {}
layout.reload()
eq(#vim.api.nvim_list_wins(), 3, "reload doesn't stack panes")

-- an unavailable kind is skipped; a layout with no terminal resets it
opened = {}
layout.apply("gitty")
eq(opened, {}, "git isn't available")
eq(#vim.api.nvim_list_wins(), 1, "only the editor")
eq(terminal.height, terminal.default_height, "terminal back to the default")

-- a broken layout leaves things as they were and reports why
local notes = {}
vim.notify = function(msg)
  table.insert(notes, msg)
end
layout.apply("wide")
eq(layout.apply("broken"), false, "broken layout")
eq(#vim.api.nvim_list_wins(), 3, "broken layout leaves the panes alone")
eq(notes[1]:match("description should be a string") ~= nil, true, "says why")

-- the default: BUD_LAYOUT, then config.json, then "default"
local config = require("bud.config")
config.file = dir .. "/config.json"
eq(layout.default(), "default", "default")
vim.fn.writefile({ '{"layout": "plain"}' }, config.file)
eq(layout.default(), "plain", "config.json")
local projects = require("bud.projects")
projects.file = dir .. "/projects.json"
local proj = vim.fs.normalize(vim.uv.fs_realpath(dir))
vim.fn.writefile({ vim.json.encode({ projects = { { name = "p", path = proj, layout = "wide" } } }) }, projects.file)
eq(layout.default(proj), "wide", "the project's own")
eq(layout.default(proj .. "/sub"), "wide", "anywhere inside the project")
eq(layout.default("/"), "plain", "outside it: config.json")
vim.env.BUD_LAYOUT = "gitty"
eq(layout.default(proj), "gitty", "--layout wins")
vim.env.BUD_LAYOUT = nil
vim.fn.writefile({ vim.json.encode({ projects = {} }) }, projects.file)

-- open_project: a saved session wins over the layout, unless --layout was given
local sessions = require("bud.sessions")
local restored = false
sessions.restore = function()
  return restored
end
layout.apply("plain")
config.file = dir .. "/none.json"
vim.fn.writefile({ '{"layout": "wide"}' }, dir .. "/none.json")
eq(layout.open_project(dir), true, "no session: layout")
eq(#vim.api.nvim_list_wins(), 3, "the layout's panes")
layout.apply("plain")
restored = true
eq(layout.open_project(dir), false, "session")
eq(#vim.api.nvim_list_wins(), 1, "session: no layout")
vim.env.BUD_LAYOUT = "wide"
eq(layout.open_project(dir), true, "--layout over a session")
eq(#vim.api.nvim_list_wins(), 3, "--layout's panes")
layout.apply("plain")
vim.env.BUD_LAYOUT = nil
eq(layout.open_project(dir, { files = true }), true, "files named: no session")

print("layout_test: ok")
