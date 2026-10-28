-- Run with: nvim --clean --headless -l tests/lua/index_test.lua (just lua-test)
-- cocoindex warming: background, once, only for projects set up with ccc init

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/ready/.cocoindex_code", "p")
vim.fn.mkdir(tmp .. "/fresh", "p")
-- the fake ccc takes a moment, logs where it ran, and leaves everything else alone
local fake = tmp .. "/ccc"
vim.fn.writefile({ "#!/bin/sh", "sleep 1", 'pwd >> "$(dirname "$0")/ran.log"' }, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")

local index = require("bud.ai.index")
index.bin = function()
  return fake
end

local start = vim.uv.hrtime()
eq(index.warm(tmp .. "/ready"), true, "started")
-- well under the fake's second: spawning alone can take 100 ms on a busy CI runner
eq((vim.uv.hrtime() - start) / 1e6 < 700, true, "without waiting for it")
eq(index.state(tmp .. "/ready"), "indexing", "indexing meanwhile")
eq(index.warm(tmp .. "/ready"), false, "once per project")
eq(index.warm(tmp .. "/fresh"), false, "not a project ccc init hasn't seen")
eq(vim.fn.isdirectory(tmp .. "/fresh/.cocoindex_code"), 0, "and nothing written into it")
assert(
  vim.wait(3000, function()
    return index.state(tmp .. "/ready") == "done"
  end, 20),
  "the index run to finish"
)
eq(#vim.fn.readfile(tmp .. "/ran.log"), 1, "ran once")

index.bin = function()
  return nil
end
index._reset()
eq(index.warm(tmp .. "/ready"), false, "no ccc, nothing to do")

vim.fn.delete(tmp, "rf")
print("index_test: ok")
