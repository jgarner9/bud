-- Run with: nvim --clean --headless -l tests/lua/actions_test.lua (just lua-test)
-- Inline actions and the commit message draft, on a fake claude

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(5000, cond, 10) then
    error("timed out waiting for " .. what, 2)
  end
end

local tmp = vim.uv.fs_realpath(vim.fn.tempname()) or vim.fn.tempname()
vim.fn.mkdir(tmp .. "/repo", "p")
tmp = vim.uv.fs_realpath(tmp)
vim.env.CLAUDE_CONFIG_DIR = tmp .. "/claude-home"
require("bud.config").file = tmp .. "/config.json"
require("bud.ai.bridge").lock_dir = function()
  return tmp .. "/ide"
end

-- logs what it's sent, answers with a commit subject
local fake = tmp .. "/fake-claude"
vim.fn.writefile({
  "#!/bin/sh",
  "while IFS= read -r line; do",
  [[  printf '%s\n' "$line" >> "$(dirname "$0")/sent.log"]],
  [[  printf '{"type":"result","subtype":"success","session_id":"s1","result":"feat: add the greeting\\n\\nSays hi."}\n']],
  "done",
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")
local function sent()
  local f = tmp .. "/sent.log"
  return vim.fn.filereadable(f) == 1 and vim.tbl_map(vim.json.decode, vim.fn.readfile(f)) or {}
end

local claude = require("bud.ai.claude")
claude.bin = function()
  return fake
end
claude.check_login = function(_, cb)
  vim.schedule(function()
    cb(true)
  end)
end
local actions = require("bud.ai.actions")

local repo = tmp .. "/repo"
vim.fn.chdir(repo)
vim.fn.system({ "git", "init", "-q" })
vim.fn.system({
  "git",
  "-c",
  "user.name=t",
  "-c",
  "user.email=t@t",
  "-c",
  "commit.gpgsign=false",
  "commit",
  "-q",
  "--allow-empty",
  "-m",
  "chore: start",
})
vim.fn.writefile({ "package main", "", "func hi() {}", "func bye() {}" }, repo .. "/main.go")
vim.cmd.edit(repo .. "/main.go")

-- the current line, or a visual selection, as an @-range
vim.api.nvim_win_set_cursor(0, { 3, 0 })
eq(actions.range(), "main.go#L3", "one line")
vim.cmd("normal! Vj")
eq(actions.range(), "main.go#L3-4", "a selection")

actions.run("explain")
wait(function()
  return #sent() == 1
end, "the explain request")
eq(sent()[1].message.content, actions.prompts.explain:format("@main.go#L3-4"), "asked about the selection")
local pane = require("bud.chat.pane")
eq(pane.view() ~= nil, true, "the answer shows in the pane")
wait(function()
  return claude.status() == "ready"
end, "the reply")

-- commit message: nothing staged says so; staged changes get a draft
vim.cmd.new()
vim.bo.filetype = "gitcommit"
local commit_buf = vim.api.nvim_get_current_buf()
vim.api.nvim_buf_set_lines(commit_buf, 0, -1, false, { "", "# Please enter the commit message" })
actions.commit()
eq(#sent(), 1, "nothing staged, nothing asked")
vim.fn.system({ "git", "add", "main.go" })
actions.commit()
wait(function()
  return vim.api.nvim_buf_get_lines(commit_buf, 0, 1, false)[1] == "feat: add the greeting"
end, "the draft")
eq(vim.api.nvim_buf_get_lines(commit_buf, 0, -1, false), {
  "feat: add the greeting",
  "",
  "Says hi.",
  "",
  "# Please enter the commit message",
}, "drafted above git's comments")
local asked = sent()[2].message.content
eq(asked:find("chore: start", 1, true) ~= nil, true, "recent commits for the style")
eq(asked:find("+func hi() {}", 1, true) ~= nil, true, "the staged diff")

pane._reset()
claude._reset()
require("bud.ai.bridge").stop()
vim.fn.delete(tmp, "rf")
print("actions_test: ok")
