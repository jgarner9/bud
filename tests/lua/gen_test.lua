-- Run with: nvim --clean --headless -l tests/lua/gen_test.lua (just lua-test)
-- :BudGen: Claude's files are found, load-tested in a headless Neovim, and
-- turned on or off; a fake claude stands in for the files it would write

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(8000, cond, 20) then
    error("timed out waiting for " .. what, 2)
  end
end

local tmp = vim.uv.fs_realpath(vim.fn.tempname()) or vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
tmp = vim.uv.fs_realpath(tmp)
vim.env.XDG_DATA_HOME = tmp .. "/data"
vim.env.CLAUDE_CONFIG_DIR = tmp .. "/claude-home"
require("bud.config").file = tmp .. "/config.json"
require("bud.ai.bridge").lock_dir = function()
  return tmp .. "/ide"
end

local gen = require("bud.gen")

-- the prompt carries the request and both contracts
local p = gen.prompt("open the test file", "return { name = 'x' }")
eq(p:find("open the test file", 1, true) ~= nil, true, "the request")
eq(p:find("manifest.json", 1, true) ~= nil and p:find("return { name = 'x' }", 1, true) ~= nil, true, "the contracts")

-- what changed between two snapshots
local tools, specs = gen.changed(
  { ["a/init.lua"] = "1", ["b/init.lua"] = "1" },
  { ["a/init.lua"] = "2", ["a/manifest.json"] = "1", ["b/init.lua"] = "1", ["languages/zig.lua"] = "1" }
)
eq({ tools, specs }, { { "a" }, { "zig" } }, "changed tools and specs")

-- the fake "writes" a good tool and a broken one, then answers
local fake = tmp .. "/fake-claude"
vim.fn.writefile({
  "#!/bin/sh",
  "while IFS= read -r line; do",
  "  mkdir -p hello bad",
  [[  printf '{"name":"hello","description":"says hi"}' > hello/manifest.json]],
  [[  printf 'return { setup = function() vim.api.nvim_create_user_command("BudHello", function() end, {}) end }' > hello/init.lua]],
  [[  printf '{"name":"bad"}' > bad/manifest.json]],
  [[  printf 'return { setup = function() error("nope") end }' > bad/init.lua]],
  [[  printf '{"type":"assistant","session_id":"g1","message":{"content":[{"type":"text","text":"Made hello: run :BudHello."}]}}\n']],
  [[  printf '{"type":"result","subtype":"success","session_id":"g1","result":"Made hello."}\n']],
  "done",
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")

local claude = require("bud.ai.claude")
claude.bin = function()
  return fake
end
claude.secret_env = function(cb)
  cb({})
end
claude.check_login = function(_, cb)
  vim.schedule(function()
    cb(true)
  end)
end
-- record what the CLI would be told
local cli_calls = {}
require("bud.cli").bin = function()
  return tmp .. "/fake-bud"
end
vim.fn.writefile({ "#!/bin/sh", 'echo "$@" >> "$(dirname "$0")/bud.log"' }, tmp .. "/fake-bud")
vim.fn.setfperm(tmp .. "/fake-bud", "rwxr-xr-x")

local notes = {}
local notify = vim.notify
vim.notify = function(msg)
  table.insert(notes, msg)
end
gen.run("a command that says hi")
local v = gen.view()
eq(claude.get("gen").root, vim.uv.fs_realpath(vim.fn.stdpath("data") .. "/user-tools"), "runs in user-tools")
wait(function()
  return #notes >= 2
end, "both load tests")
vim.notify = notify
wait(function()
  return vim.fn.filereadable(tmp .. "/bud.log") == 1 and #vim.fn.readfile(tmp .. "/bud.log") == 2
end, "the CLI calls")
for _, l in ipairs(vim.fn.readfile(tmp .. "/bud.log")) do
  table.insert(cli_calls, l)
end
table.sort(cli_calls)
eq(cli_calls[1]:match("^tools disable %-%-reason .*nope.* bad$") ~= nil, true, "the broken one is turned off, with why")
eq(cli_calls[2], "tools enable hello", "the good one is turned on")
eq(vim.fn.exists(":BudHello"), 2, "and loaded here, no restart")
eq(
  vim.tbl_contains(vim.api.nvim_buf_get_lines(v.transcript, 0, -1, false), "Made hello: run :BudHello."),
  true,
  "the reply shows"
)

gen._reset()
claude._reset()
require("bud.ai.bridge").stop()
vim.fn.delete(tmp, "rf")
print("gen_test: ok")
