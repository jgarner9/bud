-- Run with: nvim --clean --headless -l tests/lua/actions_test.lua (just lua-test)
-- Inline actions and the commit message draft, on a fake claude

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
-- the claude backend (D131): bud.ai.claude and Claude Code's TUI
dofile("tests/lua/lib/claude_backend.lua")

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

-- headless (-p), it logs what it's sent and answers with a commit subject;
-- as the TUI it draws a prompt, logs each line typed in, and quits on q
local fake = tmp .. "/fake-claude"
vim.fn.writefile({
  "#!/bin/sh",
  'if [ "$1" != "-p" ]; then',
  "  echo '> ready'",
  "  while IFS= read -r line; do",
  '    [ "$line" = q ] && exit 0',
  [[    printf '%s\n' "$line" >> "$(dirname "$0")/tui.log"]],
  "  done",
  "  exit 0",
  "fi",
  "while IFS= read -r line; do",
  [[  printf '%s\n' "$line" >> "$(dirname "$0")/sent.log"]],
  [[  printf '{"type":"result","subtype":"success","session_id":"s1","result":"feat: add the greeting\\n\\nSays hi."}\n']],
  "done",
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")
local function typed()
  local f = tmp .. "/tui.log"
  return vim.fn.filereadable(f) == 1 and vim.fn.readfile(f) or {}
end
local function sent()
  local f = tmp .. "/sent.log"
  return vim.fn.filereadable(f) == 1 and vim.tbl_map(vim.json.decode, vim.fn.readfile(f)) or {}
end

local claude = require("bud.ai.claude")
-- bud's embedded plugin (D142) is unpacked by the bud binary, which these tests don't run
claude.plugin_dir = function()
  return nil
end
claude.bin = function()
  return fake
end
claude.check_login = function(_, cb)
  vim.schedule(function()
    cb(true)
  end)
end
local actions = require("bud.ai.actions")
local term = require("bud.ai.term")
term.settle = 10

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

-- an action goes to the project's TUI, opened beside the code
local code_win = vim.api.nvim_get_current_win()
actions.run("explain")
wait(function()
  return #typed() == 1
end, "the explain request")
eq(typed()[1], actions.prompts.explain:format("@main.go#L3-4"), "asked the TUI about the selection")
eq(term.running(repo), true, "the TUI is open")
eq(vim.api.nvim_get_current_win(), code_win, "the cursor stays in the code")
-- a second one goes to the same TUI
vim.cmd("normal! \27")
actions.run("tests")
wait(function()
  return #typed() == 2
end, "the tests request")
eq(typed()[2], actions.prompts.tests:format("@main.go#L4"), "the current line")
eq(#sent(), 0, "nothing went to a headless session")
-- quitting the TUI hands the session back for the commit draft
term.send("q")
wait(function()
  return not term.running(repo)
end, "the TUI to quit")

-- commit message: nothing staged says so; staged changes get a draft
vim.cmd.new()
vim.bo.filetype = "gitcommit"
local commit_buf = vim.api.nvim_get_current_buf()
vim.api.nvim_buf_set_lines(commit_buf, 0, -1, false, { "", "# Please enter the commit message" })
actions.commit()
eq(#sent(), 0, "nothing staged, nothing asked")
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
local asked = sent()[1].message.content
eq(asked:find("chore: start", 1, true) ~= nil, true, "recent commits for the style")
eq(asked:find("+func hi() {}", 1, true) ~= nil, true, "the staged diff")

claude._reset()
-- other backends (D131) get the place in words, not Claude Code's @-syntax
eq(actions.mention("main.go#L3-4", "claude"), "@main.go#L3-4", "claude's mention")
eq(actions.mention("main.go#L3-4", "openclaw"), "main.go lines 3-4", "a range in words")
eq(actions.mention("main.go#L4", "openclaw"), "main.go line 4", "a line in words")

require("bud.ai.bridge").stop()
vim.fn.delete(tmp, "rf")
print("actions_test: ok")
