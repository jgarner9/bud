-- Run with: nvim --clean --headless -l tests/lua/claude_plugin_test.lua (just lua-test)
-- plugin_dir (D142): the last line of `bud --no-update plugin path`, kept on
-- success, a failure retried only after plugin_retry_ms, and prefetched async

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
dofile("tests/lua/lib/claude_backend.lua")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.uv.fs_realpath(vim.fn.tempname()) or vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
tmp = vim.uv.fs_realpath(tmp)
local fake = tmp .. "/bud"
local mode = tmp .. "/mode"
vim.fn.writefile({
  "#!/bin/sh",
  'echo "$*" >> "' .. tmp .. '/calls.log"',
  'if [ -f "' .. mode .. '" ]; then exit 1; fi',
  "echo 'a stray line'",
  "echo '" .. tmp .. "/plugin'",
  "echo",
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")
local function calls()
  return vim.fn.filereadable(tmp .. "/calls.log") == 1 and vim.fn.readfile(tmp .. "/calls.log") or {}
end

require("bud.cli").bin = function()
  return fake
end
local notes = {}
local notify = vim.notify
vim.notify = function(msg)
  table.insert(notes, msg)
end
local claude = require("bud.ai.claude")

-- a failure isn't kept: nil with a warning, nothing run again inside the retry window
vim.fn.writefile({ "" }, mode)
eq(claude.plugin_dir(), nil, "bud fails")
eq(#notes, 1, "warned")
eq(claude.plugin_dir(), nil, "still inside the retry window")
eq(#calls(), 1, "bud not run again yet")

-- after the window it asks again, and the last non-empty line is the dir
vim.fn.delete(mode)
claude.plugin_retry_ms = 0
eq(claude.plugin_dir(), tmp .. "/plugin", "the last non-empty line, after a retry")
eq(calls()[2], "--no-update plugin path", "asked without the update check")
eq(claude.plugin_dir(), tmp .. "/plugin", "kept")
eq(#calls(), 2, "a success is cached")

-- prefetch fills the cache without blocking, and without a warning
claude._plugin_reset()
notes = {}
claude.prefetch_plugin()
eq(
  vim.wait(5000, function()
    return #calls() == 3
  end, 20),
  true,
  "bud asked in the background"
)
vim.wait(200) -- the result is handed back on the main loop
eq(claude.plugin_dir(), tmp .. "/plugin", "prefetched")
eq(#calls(), 3, "so no blocking call was needed")
eq(notes, {}, "no warning")

vim.notify = notify
vim.fn.delete(tmp, "rf")
print("claude_plugin_test: ok")
