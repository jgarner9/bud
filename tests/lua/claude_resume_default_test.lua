-- Run with: nvim --clean --headless -l tests/lua/claude_resume_default_test.lua (just lua-test)
-- ai.claude.resume defaults to "new": a reopened project starts a fresh
-- conversation, and "continue" resumes the saved one (D106)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
require("bud.config").file = tmp .. "/config.json"
-- bud's own Claude Code dir: a shared ~/.claude never continues the newest
vim.env.CLAUDE_CONFIG_DIR = tmp .. "/cc"

local claude = require("bud.ai.claude")
claude.ids_file = function()
  return tmp .. "/claude-sessions.json"
end
claude.bin = function()
  return "claude"
end

-- a project with a conversation bud saved from an earlier session
vim.fn.mkdir(tmp .. "/proj", "p")
local root = vim.uv.fs_realpath(tmp .. "/proj")
local store = claude.project_dir(root)
vim.fn.mkdir(store, "p")
vim.fn.writefile({ "{}" }, store .. "/old.jsonl")
vim.fn.writefile({ vim.json.encode({ [root] = "old" }) }, tmp .. "/claude-sessions.json")

local function set_resume(value)
  vim.fn.writefile({ vim.json.encode({ ai = { claude = { resume = value } } }) }, tmp .. "/config.json")
end

eq(claude.handover(root), { "claude" }, "no config.json: a reopened project starts fresh")
set_resume("continue")
eq(claude.handover(root), { "claude", "--resume", "old" }, "continue resumes the saved conversation")
set_resume("new")
eq(claude.handover(root), { "claude" }, "new starts fresh")

claude._reset()
vim.fn.delete(tmp, "rf")
print("claude_resume_default_test: ok")
