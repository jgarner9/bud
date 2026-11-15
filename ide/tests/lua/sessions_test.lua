-- Run with: nvim --clean --headless -l tests/lua/sessions_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/proj/src", "p")
tmp = vim.uv.fs_realpath(tmp)
local project = tmp .. "/proj"
vim.fn.writefile({ "hello" }, project .. "/src/a.txt")

require("bud.config").file = tmp .. "/config.json"
require("bud.projects").file = tmp .. "/projects.json"
local sessions = require("bud.sessions")
sessions.dir = tmp .. "/sessions"

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end
local function write(path, data)
  local f = assert(io.open(path, "w"))
  f:write(data)
  f:close()
end

write(tmp .. "/projects.json", vim.json.encode({ projects = { { name = "proj", path = project } } }))
vim.cmd.cd(vim.fn.fnameescape(project))
vim.cmd.edit("src/a.txt")

-- off by default: nothing saved, nothing restored
sessions.save()
eq(vim.fn.filereadable(sessions.file(project)), 0, "nothing saved while off")
eq(sessions.restore(project), false, "nothing restored while off")

write(tmp .. "/config.json", '{"dashboard": {"sessions": true}}')
sessions.save()
eq(vim.fn.filereadable(sessions.file(project)), 1, "saved")
-- the path is resolved, so a symlink to the project finds the same session
vim.uv.fs_symlink(project, tmp .. "/link")
eq(sessions.file(tmp .. "/link"), sessions.file(project), "same file through a symlink")

vim.cmd("silent! %bwipeout!")
vim.cmd.cd(vim.fn.fnameescape(tmp))
eq(sessions.restore(project), true, "restored")
eq(vim.fs.basename(vim.api.nvim_buf_get_name(0)), "a.txt", "the file is back")
eq(vim.fn.getcwd(), project, "and the cwd")

-- outside a registered project nothing is saved
vim.cmd("silent! %bwipeout!")
vim.cmd.cd(vim.fn.fnameescape(tmp))
os.remove(sessions.file(project))
vim.cmd.edit(vim.fn.fnameescape(project .. "/src/a.txt"))
sessions.save()
eq(vim.fn.filereadable(sessions.file(project)), 0, "not saved outside a project")

print("sessions_test: ok")
