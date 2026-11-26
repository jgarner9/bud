-- Run with: nvim --clean --headless -l tests/lua/actions_test.lua (just lua-test)
-- Inline actions and the commit message draft, on a fake opencode and ACP agent

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
vim.fn.mkdir(tmp .. "/bin", "p")
tmp = vim.uv.fs_realpath(tmp)
require("bud.config").file = tmp .. "/config.json"

-- OpenCode's TUI draws a prompt, logs each line typed in, and quits on q
vim.fn.writefile({
  "#!/bin/sh",
  "echo '> ready'",
  "while IFS= read -r line; do",
  '  [ "$line" = q ] && exit 0',
  [[  printf '%s\n' "$line" >> "$(dirname "$0")/tui.log"]],
  "done",
}, tmp .. "/bin/opencode")
vim.fn.setfperm(tmp .. "/bin/opencode", "rwxr-xr-x")
vim.env.PATH = tmp .. "/bin:" .. vim.env.PATH
local function typed()
  local f = tmp .. "/bin/tui.log"
  return vim.fn.filereadable(f) == 1 and vim.fn.readfile(f) or {}
end

-- the session runs on the fake ACP agent, which logs what it's sent and
-- answers every prompt with a commit subject
local repo_root = vim.fn.getcwd()
local acp_log = tmp .. "/acp.log"
local agent = require("bud.ai.agent")
agent.command = function()
  return {
    "env",
    "FAKE_ACP_LOG=" .. acp_log,
    "FAKE_ACP_REPLY=feat: add the greeting\n\nSays hi.",
    vim.v.progpath,
    "--clean",
    "--headless",
    "-l",
    repo_root .. "/tests/lua/lib/fake_acp.lua",
  }
end
agent.ids_file = function()
  return tmp .. "/agent-sessions.json"
end
-- sent is the prompts the agent has been given
local function sent()
  if vim.fn.filereadable(acp_log) ~= 1 then
    return {}
  end
  local prompts = {}
  for _, line in ipairs(vim.fn.readfile(acp_log)) do
    local msg = vim.json.decode(line)
    if msg.method == "session/prompt" then
      table.insert(prompts, msg.params.prompt[1].text)
    end
  end
  return prompts
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

-- the current line, or a visual selection, as a range
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
eq(typed()[1], actions.prompts.explain:format("main.go lines 3-4"), "asked the TUI about the selection")
eq(term.running(repo), true, "the TUI is open")
eq(vim.api.nvim_get_current_win(), code_win, "the cursor stays in the code")
-- a second one goes to the same TUI
vim.cmd("normal! \27")
actions.run("tests")
wait(function()
  return #typed() == 2
end, "the tests request")
eq(typed()[2], actions.prompts.tests:format("main.go line 4"), "the current line")
eq(#sent(), 0, "nothing went to the agent")
-- quitting the TUI
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
local asked = sent()[1]
eq(asked:find("chore: start", 1, true) ~= nil, true, "recent commits for the style")
eq(asked:find("+func hi() {}", 1, true) ~= nil, true, "the staged diff")

agent._reset()
-- the place goes in words
eq(actions.mention("main.go#L3-4"), "main.go lines 3-4", "a range in words")
eq(actions.mention("main.go#L4"), "main.go line 4", "a line in words")

vim.fn.delete(tmp, "rf")
print("actions_test: ok")
