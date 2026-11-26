-- Run with: nvim --clean --headless -l tests/lua/work_test.lua (just lua-test)
-- The agent working a filed issue in a worktree (JGA-126), on the fake ACP agent

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
dofile("tests/lua/lib/nui.lua")

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

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/bud", "p")
tmp = vim.uv.fs_realpath(tmp)
require("bud.config").file = tmp .. "/config.json"

-- a repo with one commit on main
local repo = tmp .. "/bud"
local function git(args)
  local out = vim.fn.system(vim.list_extend({ "git", "-C", repo, "-c", "commit.gpgsign=false" }, args))
  assert(vim.v.shell_error == 0, out)
  return vim.trim(out)
end
git({ "init", "-q", "-b", "main" })
vim.fn.writefile({ "hi" }, repo .. "/README")
git({ "add", "README" })
git({ "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "chore: start" })

-- the fake agent answers each prompt from the next reply and logs what it's sent
local replies = {
  "Should the row fold or close?",
  'Fixed.\n```bud-work\n{"status": "done", "commit": "abc123", "summary": "Esc folds rows now."}\n```',
}
local repo_root = vim.fn.getcwd()
local acp_log = tmp .. "/acp.log"
local agent = require("bud.ai.agent")
agent.command = function()
  return {
    "env",
    "FAKE_ACP_LOG=" .. acp_log,
    "FAKE_ACP_REPLIES=" .. vim.json.encode(replies),
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
  local prompts = {}
  for _, line in ipairs(vim.fn.readfile(acp_log)) do
    local msg = vim.json.decode(line)
    if msg.method == "session/prompt" then
      table.insert(prompts, msg.params.prompt[1].text)
    end
  end
  return prompts
end

local notified = {}
vim.notify = function(msg)
  table.insert(notified, msg)
end
local work = require("bud.ai.work")
local issue = require("bud.ai.issue")
local permissions = require("bud.ai.permissions")
table.insert(permissions.policies, work.policy)

-- names: the id and a slug of the title; the worktree beside the repo
eq(work.branch_name("JGA-7", "Esc drops the proposal!"), "jga-7-esc-drops-the-proposal", "branch")
eq(work.branch_name("JGA-7", nil), "jga-7", "branch without a title")
eq(#work.branch_name("JGA-7", ("word "):rep(30)) <= 46, true, "branch kept short")
eq(work.worktree_path(repo, "JGA-7"), tmp .. "/bud.worktrees/jga-7", "worktree path")
eq(work.base(repo), "main", "no remote: the current branch")

-- prepare makes the worktree once, then reuses it
local wt = assert(work.prepare(repo, "JGA-7", "Esc drops the proposal"))
eq(wt.path, tmp .. "/bud.worktrees/jga-7", "worktree made")
eq(git({ "-C", wt.path, "branch", "--show-current" }), "jga-7-esc-drops-the-proposal", "on its branch")
eq(work.prepare(repo, "JGA-7", "another title").branch, "jga-7-esc-drops-the-proposal", "reused")
eq(select(2, work.prepare(tmp, "JGA-8")):match("^not a git repo") ~= nil, true, "outside a repo")

-- inside: the worktree's files, not its neighbours or a way out
eq(work.inside(wt.path, wt.path .. "/README"), true, "a file in it")
eq(work.inside(wt.path, "new/file.lua"), true, "relative, not there yet")
eq(work.inside(wt.path, repo .. "/README"), false, "the main checkout")
eq(work.inside(wt.path, wt.path .. "/../x"), false, "climbing out")
eq(work.inside(wt.path, wt.path .. "-other/x"), false, "a prefix isn't inside")
vim.uv.fs_symlink(repo, wt.path .. "/escape")
eq(work.inside(wt.path, wt.path .. "/escape/README"), false, "a symlink out")
eq(work.inside(wt.path, nil), false, "no path")
-- a root reached through a symlink still holds its files
local via = tmp .. "/via-link"
vim.uv.fs_symlink(vim.fs.dirname(wt.path), via)
local linked = via .. "/" .. vim.fs.basename(wt.path)
eq(work.inside(linked, wt.path .. "/README"), true, "a symlinked root")
eq(work.inside(linked, linked .. "/README"), true, "a path through the link")

-- the whole run: a question, an answer, then done
local run = assert(work.start("jga-7", "Esc drops the proposal", repo))
eq(run.id, "JGA-7", "id upper-cased")
wait(function()
  return run.state == "waiting"
end, "the question")
eq(run.question, "Should the row fold or close?", "question kept")
assert(work.statusline():find("1"), "statusline shows the waiting run")

-- while it runs: edits in its worktree go through, the rest is asked
local function ask(tool, input, root)
  local got
  permissions.request({
    tool = tool,
    input = input,
    root = root or run.worktree,
    respond = function(r)
      got = r
    end,
  })
  return got
end
eq(ask("Edit", { file_path = run.worktree .. "/README" }).behavior, "allow", "edit inside")
eq(work.policy({ tool = "Edit", input = { file_path = repo .. "/README" }, root = run.worktree }), nil, "edit outside")
eq(work.policy({ tool = "Bash", input = { command = "just check" }, root = run.worktree }), nil, "commands asked")
eq(ask("linear_save_issue", {}).behavior, "allow", "moving the issue (OpenCode names it <server>_<tool>)")
eq(work.policy({ tool = "mcp__linear__save_issue", root = run.worktree }), nil, "not an OpenCode tool name")
eq(work.policy({ tool = "calendar_create_event", root = run.worktree }), nil, "another server")
eq(work.policy({ tool = "Edit", input = { file_path = repo .. "/README" }, root = repo }), nil, "another root")

eq(work.answer("JGA-7", "Fold it."), true, "answered")
wait(function()
  return run.state == "done"
end, "the fix")
eq(run.commit, "abc123", "commit")
eq(run.summary, "Esc folds rows now.", "summary")
eq(agent.get(run.key), nil, "session dropped")
eq(work.statusline(), "", "nothing in progress")
eq(work.policy({ tool = "Edit", input = { file_path = run.worktree .. "/README" }, root = run.worktree }), nil, "done")
local msgs = sent()
eq(#msgs, 2, "prompt and answer")
for _, want in ipairs({ "Linear issue JGA-7", "branch `jga-7-esc-drops-the-proposal` (from `main`)", "```bud-work" }) do
  assert(msgs[1]:find(want, 1, true), ("prompt lacks %q"):format(want))
end
eq(msgs[2], "Fold it.", "answer sent")
assert(
  notified[#notified]:find("JGA-7 is ready to review on jga-7-esc-drops-the-proposal", 1, true),
  notified[#notified]
)

-- :BudIssues lists filed issues with their work
local lines, rows =
  issue.list_lines({ { id = "JGA-7", title = "Esc drops the proposal" }, { id = "JGA-9", title = "Other" } }, work.runs)
assert(lines[2]:find("JGA-7      done      Esc drops the proposal", 1, true), lines[2])
assert(lines[3]:find("jga-7-esc-drops-the-proposal", 1, true), lines[3])
eq(rows[3], "JGA-7", "detail rows point at the issue")
assert(lines[5]:find("JGA-9      filed", 1, true), lines[5])

-- a filed issue is worked in the repo it was filed for
issue.filed = { { id = "JGA-9", title = "Other", repo = repo } }
local started
work.start = function(id, title, r)
  started = { id, title, r }
end
issue.work("JGA-9")
eq(started, { "JGA-9", "Other", repo }, "work uses the filed repo")

agent.stop_all()
vim.fn.delete(tmp, "rf")
print("work_test: ok")
