-- Run with: nvim --clean --headless -l tests/lua/projects_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local tmp = vim.uv.fs_realpath(vim.fn.tempname()) or vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
tmp = vim.uv.fs_realpath(tmp)
for _, d in ipairs({ "a", "b", "b/sub", "c" }) do
  vim.fn.mkdir(tmp .. "/" .. d, "p")
end

local projects = require("bud.projects")
projects.file = tmp .. "/projects.json"

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

eq(projects.list(), {}, "no file")

local function write(text)
  local f = assert(io.open(projects.file, "w"))
  f:write(text)
  f:close()
end

-- the shape Go writes, never-opened as the zero time
write(vim.json.encode({
  projects = {
    { name = "a", path = tmp .. "/a", pinned = false, last_opened = "2026-10-27T09:00:00Z" },
    { name = "b", path = tmp .. "/b", pinned = false, last_opened = "2026-10-27T11:00:00Z" },
    { name = "c", path = tmp .. "/c", pinned = true, last_opened = "0001-01-01T00:00:00Z" },
    { name = "gone", path = tmp .. "/gone", pinned = false, last_opened = "2026-10-26T09:00:00Z" },
  },
}))
local names = vim.tbl_map(function(p)
  return p.name
end, projects.list())
eq(names, { "c", "b", "a", "gone" }, "pinned, then most recent")
eq(projects.list()[1].last_opened, nil, "never opened")
eq(projects.list()[4].missing, true, "missing dir")

eq(projects.find(tmp .. "/b/sub").name, "b", "find from a subdir")
eq(projects.find(tmp), nil, "outside every project")

write("{nope")
eq(projects.list(), {}, "corrupt file")

print("projects_test: ok")
