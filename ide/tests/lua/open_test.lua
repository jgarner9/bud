-- Run with: nvim --clean --headless -l tests/lua/open_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
-- the claude backend (D131): bud.ai.claude and Claude Code's TUI
dofile("tests/lua/lib/claude_backend.lua")
dofile("tests/lua/lib/nui.lua")

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

-- f searches the cwd from the dashboard, never recent files (JGA-163), and
-- the project anywhere else
local searched
require("bud.pack").deferred["fzf-lua"] = function()
  return {
    actions = {},
    files = function(o)
      searched = o.cwd
    end,
    oldfiles = function()
      searched = "oldfiles"
    end,
  }
end
vim.cmd.cd(vim.fn.fnameescape(tmp .. "/loose"))
local board = vim.api.nvim_create_buf(false, true)
vim.bo[board].filetype = "bud-dashboard"
vim.api.nvim_win_set_buf(0, board)
open.file()
eq(searched, vim.fn.getcwd(), "dashboard f: files in the cwd")
vim.cmd.edit(vim.fn.fnameescape(repo_file))
open.file()
eq(searched, tmp .. "/repo", "in a file: its project")
open.file({ dir = tmp .. "/reg" })
eq(searched, tmp .. "/reg", "opts.dir wins")

-- n asks where the new file goes and :w makes its dirs (JGA-164)
local answer
local asked
vim.ui.input = function(o, cb)
  asked = o.prompt
  cb(answer)
end
vim.cmd.cd(vim.fn.fnameescape(tmp))
answer = "notes/deep/todo.md"
open.new()
eq(asked:find("New file in", 1, true) ~= nil, true, "asks where")
eq(vim.api.nvim_buf_get_name(0), tmp .. "/notes/deep/todo.md", "named from the cwd")
vim.api.nvim_buf_set_lines(0, 0, -1, false, { "- milk" })
vim.cmd.write()
eq(vim.fn.readfile(tmp .. "/notes/deep/todo.md"), { "- milk" }, ":w saves it, dirs made")
answer = nil
open.new()
eq(vim.api.nvim_buf_get_name(0), "", "no answer: an unnamed buffer")

-- go replaces the dashboard with an empty buffer and the layout, and no
-- picker (JGA-161)
local cli = require("bud.cli")
cli.bin = function()
  return nil
end
require("bud.ai.claude").opened = function() end
local laid
require("bud.layout").open_project = function(path)
  laid = path
  return true
end
local picked = false
open.file = function()
  picked = true
end
local dash = vim.api.nvim_create_buf(false, true)
vim.bo[dash].bufhidden = "wipe"
vim.bo[dash].filetype = "bud-dashboard"
vim.api.nvim_buf_set_lines(dash, 0, -1, false, { "hey bud." })
vim.api.nvim_win_set_buf(0, dash)
open.go(tmp .. "/repo")
eq(laid, tmp .. "/repo", "the layout for the project")
eq(vim.api.nvim_buf_is_valid(dash), false, "the dashboard is gone")
eq(vim.api.nvim_buf_get_name(0), "", "an empty buffer")
eq(picked, false, "no file picker")
eq(vim.fn.getcwd(), vim.uv.fs_realpath(tmp .. "/repo"), "cd to it")

-- the directory browser (JGA-160): only dirs, dot dirs too, ./ and ../ first
vim.fn.mkdir(tmp .. "/repo/.hidden", "p")
local names = {}
for _, d in ipairs(open.dirs(tmp .. "/repo")) do
  table.insert(names, d.name)
end
eq(names, { "./", "../", ".git/", ".hidden/", "lib/" }, "dirs only, dot dirs, sorted")
eq(open.dirs("/")[2].path, "/", "../ at the root stays there")

local function press(keys)
  vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes(keys, true, false, true), "xt", false)
end
local function shown()
  return vim.api.nvim_buf_get_lines(0, 0, -1, false)
end
local chosen
open.go = function(path)
  chosen = path
end
local h = open.browse(tmp)
eq(vim.api.nvim_get_current_win(), h.win, "the browser has the cursor")
eq(shown()[1], " ./", "./ on top")
eq(shown()[2], " ../", "then ../")
eq(vim.tbl_contains(shown(), " notes.txt"), false, "no files")
-- down to repo/, then in
vim.api.nvim_win_set_cursor(h.win, { vim.fn.index(shown(), " repo/") + 1, 0 })
press("<CR>")
eq(shown()[5], " lib/", "into repo/")
-- up again lands on repo/
vim.api.nvim_win_set_cursor(h.win, { 2, 0 })
press("<CR>")
eq(shown()[vim.api.nvim_win_get_cursor(h.win)[1]], " repo/", "up lands on the dir just left")
-- o opens the one under the cursor as a project
press("o")
eq(chosen, tmp .. "/repo", "o opens it")
eq(vim.api.nvim_win_is_valid(h.win), false, "and closes the browser")
-- <S-CR> on ./ opens the dir being shown
chosen = nil
h = open.browse(tmp .. "/repo")
press("<S-CR>")
eq(chosen, tmp .. "/repo", "<S-CR> on ./ opens this dir")
-- q cancels
chosen = nil
h = open.browse(tmp)
press("q")
eq(chosen, nil, "q opens nothing")

print("open_test: ok")
