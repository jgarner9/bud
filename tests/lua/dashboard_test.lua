-- Run with: nvim --clean --headless -l tests/lua/dashboard_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
vim.g.mapleader = " "

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/proj/a", "p")
vim.fn.mkdir(tmp .. "/proj/b", "p")
tmp = vim.uv.fs_realpath(tmp)

require("bud.config").file = tmp .. "/config.json"
require("bud.projects").file = tmp .. "/projects.json"
local dashboard = require("bud.dashboard")
local view = require("bud.dashboard.view")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function has(lines, text, what)
  for _, line in ipairs(lines) do
    if line:find(text, 1, true) then
      return
    end
  end
  error(("%s: no line with %q in\n%s"):format(what, text, table.concat(lines, "\n")), 2)
end

-- update banner from update.json
eq(dashboard.update_banner(nil), nil, "no update.json")
eq(dashboard.update_banner({ checked_at = "x" }), nil, "nothing available")
eq(dashboard.update_banner({ available = "0.6.0" }).text, "bud 0.6.0 is available, press u", "update waiting")

-- moods
eq(dashboard.mood({}, 2), "sleepy", "after midnight")
eq(dashboard.mood({ update = true }, 14), "update", "update waiting")
eq(dashboard.mood({ doctor = { clean = true } }, 14), "happy", "all green")
eq(dashboard.mood({ doctor = { clean = false } }, 14), "normal", "warnings")

-- doctor banners
local banners, clean = dashboard.doctor_banners({
  { name = "git", status = "ok" },
  { name = "go", status = "warn" },
  { name = "x", status = "warn" },
  { name = "nvim config", status = "fail" },
})
eq(clean, false, "not clean")
eq(banners, {
  { level = "error", text = "1 doctor problem, press d" },
  { level = "warn", text = "2 doctor warnings, press d" },
}, "banners")
banners, clean = dashboard.doctor_banners({ { name = "git", status = "ok" } })
eq({ banners, clean }, { {}, true }, "clean")
local claude
banners, clean, claude = dashboard.doctor_banners({
  { name = "git", status = "ok" },
  { name = "claude code", status = "warn", detail = "not installed" },
})
eq(banners, { { level = "warn", text = "Claude Code isn't set up, press c" } }, "claude banner, not a doctor warning")
eq({ clean, claude.detail }, { false, "not installed" }, "claude result comes back")

-- health line's Claude Code part
eq(dashboard.claude_health(nil, false), "no claude yet", "before doctor, not on PATH")
eq(dashboard.claude_health(nil, true), "claude ready", "before doctor, on PATH")
eq(dashboard.claude_health({ status = "warn" }, true), "claude not set up", "logged out")
eq(dashboard.claude_health({ status = "ok" }, true), "claude ready", "ready")

-- git
eq(
  dashboard.parse_git("# branch.oid abc\n# branch.head main\n# branch.ab +2 -1\n1 .M N... x\n? new.txt\n"),
  { branch = "main", changed = 2, ahead = 2, behind = 1 },
  "git status"
)
eq(dashboard.parse_git("# branch.head (detached)\n").branch, "detached", "detached")

-- tips come from leader mappings with a description
vim.keymap.set("n", "<leader>f", "<nop>", { desc = "Find file" })
vim.keymap.set("n", "<leader>z", "<nop>", { desc = "Zzz" })
vim.keymap.set("n", "<leader>q", "<nop>")
eq(dashboard.tip(0), { lhs = "<Space>f", desc = "Find file" }, "tip day 0")
eq(dashboard.tip(1), { lhs = "<Space>z", desc = "Zzz" }, "tip day 1")

-- layout
local data = {
  mood = "sleepy",
  tagline = "hjkl and chill",
  menu = {
    { key = "p", icon = "project", label = "Open project" },
    { key = "f", icon = "file", label = "Open file" },
    { key = "q", icon = "quit", label = "Quit" },
  },
  projects = {
    { key = "1", name = "bud", pinned = true, git = { branch = "main", changed = 3, ahead = 0, behind = 0 } },
    { key = "2", name = "dollarwise", pinned = false },
  },
  banners = { { level = "warn", text = "1 doctor warning, press d" } },
  health = " 30 ms · 23 plugins · claude ready",
  tip = { lhs = "<Space>f", desc = "Find file" },
}
local icons = require("bud.icons")
-- every icon is one cell wide, or the centering is off
for name, glyph in pairs(icons) do
  eq(vim.fn.strdisplaywidth(glyph), 1, "width of icon " .. name)
end
local lines, hls = view.build(data, 100, 40)
has(lines, "[-.-]  z", "sleepy face")
has(lines, '"hjkl and chill"', "tagline")
has(lines, "p  " .. icons.project .. " Open project", "menu with icon")
has(lines, icons.pinned .. " bud", "pinned project")
has(lines, icons.branch .. " main " .. icons.changed .. " 3", "git status")
has(lines, icons.unpinned .. " dollarwise", "unpinned project")
has(lines, icons.warn .. " 1 doctor warning, press d", "banner")
has(lines, icons.tip .. " tip: <Space>f  Find file", "tip")
-- centered both ways
local first = 1
while lines[first] == "" do
  first = first + 1
end
eq(first > 5, true, "vertical padding")
eq(lines[first]:match("^ +") ~= nil and #lines[first]:match("^ +") > 20, true, "horizontal padding")
-- every highlight lands inside its line
for _, h in ipairs(hls) do
  local line = lines[h[1] + 1]
  if h[2] < 0 or h[3] > #line or h[2] >= h[3] then
    error(("highlight %s out of line %q"):format(vim.inspect(h), line))
  end
end
-- a narrow window never pads negative
lines = view.build(data, 10, 5)
has(lines, "hey bud.", "narrow window")

-- minimal: one-line title, short keys four to a row, projects and the worst
-- banner's icon on one bottom line; no tagline, health or tip
data.style = "minimal"
data.menu = {
  { key = "p", label = "Open project", short = "project" },
  { key = "f", label = "Open file", short = "file" },
  { key = "r", label = "Recent files", short = "recent" },
  { key = "n", label = "New file", short = "new" },
  { key = "q", label = "Quit", short = "quit" },
}
data.banners = { { level = "warn", text = "w" }, { level = "error", text = "e" } }
lines = view.build(data, 100, 40)
has(lines, "[-.-] bud", "minimal title")
has(lines, "p  project", "short labels")
has(lines, "q  quit", "second row")
has(lines, "1 bud  2 dollarwise", "projects on one line")
has(lines, icons.error, "the worst banner's icon")
for _, line in ipairs(lines) do
  for _, absent in ipairs({ "hjkl and chill", "tip:", "plugins", icons.warn }) do
    if line:find(absent, 1, true) then
      error("minimal shows " .. absent .. ": " .. line)
    end
  end
end
data.style = nil

-- open, offline: projects from the registry, missing ones hidden
local f = assert(io.open(tmp .. "/projects.json", "w"))
f:write(vim.json.encode({
  projects = {
    { name = "a", path = tmp .. "/proj/a", pinned = true },
    { name = "b", path = tmp .. "/proj/b", last_opened = "2026-10-27T09:00:00Z" },
    { name = "gone", path = tmp .. "/proj/gone" },
  },
}))
f:close()
f = assert(io.open(tmp .. "/config.json", "w"))
f:write('{"dashboard": {"scratchpad": true}}')
f:close()

local buf = dashboard.open({ offline = true })
local shown = dashboard.lines()
has(shown, icons.pinned .. " a", "pinned from the registry")
has(shown, icons.unpinned .. " b", "recent from the registry")
has(shown, "x  " .. icons.scratchpad .. " Scratchpad", "scratchpad when turned on")
has(shown, icons.claude .. " ", "claude in the health line")
for _, line in ipairs(shown) do
  if line:find("gone", 1, true) then
    error("a missing project was shown")
  end
end
eq(vim.bo[buf].modifiable, false, "read-only")
eq(vim.wo.number, false, "no line numbers on the dashboard")
eq(vim.fn.maparg("1", "n", false, true).buffer, 1, "project keys are buffer-local")

-- t switches style and back; config.json's dashboard.style is only the default
has(shown, "t  " .. icons.style .. " Minimal view", "t in the silly menu")
vim.api.nvim_feedkeys("t", "x", false)
has(dashboard.lines(), "t  silly", "t in the minimal menu")
vim.api.nvim_feedkeys("t", "x", false)
has(dashboard.lines(), "Minimal view", "back to silly")

-- each default extra can be turned off in config.json
f = assert(io.open(tmp .. "/config.json", "w"))
f:write('{"dashboard": {"health": false, "tip": false}}')
f:close()
dashboard.open({ offline = true })
for _, line in ipairs(dashboard.lines()) do
  if line:find("plugins ·", 1, true) or line:find("tip:", 1, true) then
    error("turned-off extra still shown: " .. line)
  end
end
f = assert(io.open(tmp .. "/config.json", "w"))
f:write('{"dashboard": {"scratchpad": true}}')
f:close()
buf = dashboard.open({ offline = true })

-- leaving it wipes it, and the next buffer gets its window options back
vim.o.number = true
vim.cmd.enew()
eq(vim.api.nvim_buf_is_valid(buf), false, "wiped on leave")
eq(dashboard.lines(), nil, "no dashboard state left")
eq(vim.wo.number, true, "line numbers come back")

print("dashboard_test: ok")
