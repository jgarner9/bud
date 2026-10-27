-- Run with: nvim --clean --headless -l tests/lua/open_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local tmp = vim.fn.tempname()
for _, d in ipairs({ "reg/src", "repo/.git", "repo/lib", "loose", "gone" }) do
  vim.fn.mkdir(tmp .. "/" .. d, "p")
end
tmp = vim.uv.fs_realpath(tmp)
local function touch(path)
  vim.fn.writefile({}, tmp .. "/" .. path)
  return tmp .. "/" .. path
end
local reg_file = touch("reg/src/a.go")
local repo_file = touch("repo/lib/b.go")
local loose_file = touch("loose/notes.txt")

require("bud.projects").file = tmp .. "/projects.json"
require("bud.config").file = tmp .. "/config.json"
local open = require("bud.open")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local f = assert(io.open(tmp .. "/projects.json", "w"))
f:write(vim.json.encode({
  projects = {
    { name = "reg", path = tmp .. "/reg", pinned = true },
    { name = "gone", path = tmp .. "/nope" },
  },
}))
f:close()

-- registered first (missing ones left out), then repos from recent files that
-- aren't registered (once each), then browse
local list = open.candidates({ reg_file, repo_file, repo_file, loose_file, tmp .. "/deleted.go" })
eq(
  vim.tbl_map(function(c)
    return { c.kind, c.name }
  end, list),
  {
    { "pinned", "reg" },
    { "recent", "repo" },
    { "browse", "browse for a directory…" },
  },
  "candidates"
)
eq(list[2].path, tmp .. "/repo", "recent repo path")

-- the project a buffer belongs to
vim.cmd.edit(vim.fn.fnameescape(reg_file))
eq(open.root(), tmp .. "/reg", "registered project")
vim.cmd.edit(vim.fn.fnameescape(repo_file))
eq(open.root(), tmp .. "/repo", "git repo")
vim.cmd.cd(vim.fn.fnameescape(tmp .. "/loose"))
vim.cmd.edit(vim.fn.fnameescape(loose_file))
eq(open.root(), tmp .. "/loose", "falls back to the cwd")

-- typed paths
eq(open.typed_path(repo_file), repo_file, "absolute path to a file")
eq(open.typed_path("  " .. repo_file), nil, "leading space isn't a path")
eq(open.typed_path(tmp .. "/repo"), nil, "a dir isn't a file to open")
eq(open.typed_path("lib/b.go"), nil, "relative is a fuzzy query")
eq(open.typed_path(nil), nil, "no query")
vim.env.HOME = tmp
eq(open.typed_path("~/loose/notes.txt"), loose_file, "~ expands")

print("open_test: ok")
