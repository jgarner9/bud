-- Run with: nvim --clean --headless -l tests/lua/marks_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

-- the temp dir may sit behind a symlink (macOS /tmp), which marks has to see through
local tmp = vim.fn.tempname()
local project = vim.fs.normalize(tmp .. "/project")
vim.fn.mkdir(project .. "/src", "p")
local real_project = vim.uv.fs_realpath(project)
vim.cmd.cd(project)

local marks = require("bud.marks")
marks.file = tmp .. "/state/marks.json"

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

-- fills slots in order and stores paths relative to the project root
eq(marks.pin(project .. "/a.go"), 1, "first pin")
eq(marks.pin(project .. "/src/b.go"), 2, "second pin")
eq(marks.get(), { "a.go", "src/b.go" }, "slots")
eq(marks.path(2), real_project .. "/src/b.go", "absolute path")

-- refuses duplicates and files outside the project
local slot, err = marks.pin(project .. "/a.go")
eq({ slot, err }, { nil, "already pinned to 1" }, "duplicate")
slot, err = marks.pin(tmp .. "/elsewhere.go")
eq({ slot, err }, { nil, "not inside the project" }, "outside")

-- unpinning frees the slot, and the next pin takes it
marks.unpin(1)
eq(marks.path(1), nil, "unpinned slot")
eq(marks.pin(project .. "/c.go"), 1, "refills the free slot")

-- a full project refuses a pin without a slot, and takes one with a slot
marks.pin(project .. "/d.go")
marks.pin(project .. "/e.go")
slot, err = marks.pin(project .. "/f.go")
eq({ slot, err }, { nil, "all slots are taken" }, "full")
marks.unpin(3)
eq(marks.pin(project .. "/f.go", 3), 3, "explicit slot")

-- slots are per project
local other = vim.fs.normalize(tmp .. "/other")
vim.fn.mkdir(other, "p")
vim.cmd.cd(other)
eq(marks.get(), {}, "another project starts empty")
vim.cmd.cd(project)
eq(marks.get()[3], "f.go", "back in the first project")

-- an unreadable file is ignored, not fatal
local f = assert(io.open(marks.file, "w"))
f:write("not json")
f:close()
local notify = vim.notify
vim.notify = function() end
eq(marks.get(), {}, "corrupt file")
vim.notify = notify

print("marks_test: ok")
