-- Run with: nvim --clean --headless -l tests/lua/work_test.lua (just lua-test)
-- Claude working a filed issue in a worktree (JGA-126), on a fake claude

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
-- the claude backend (D131): bud.ai.claude and Claude Code's TUI
dofile("tests/lua/lib/claude_backend.lua")
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
vim.env.CLAUDE_CONFIG_DIR = tmp .. "/claude-home"
require("bud.config").file = tmp .. "/config.json"
require("bud.ai.bridge").lock_dir = function()
  return tmp .. "/ide"
end

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

-- answers each message from the next reply file
local function reply(n, text)
  local line = vim.json.encode({ type = "result", subtype = "success", session_id = "s1", result = text })
  vim.fn.writefile({ line }, tmp .. "/reply" .. n)
end
reply(1, "Should the row fold or close?")
reply(2, 'Fixed.\n```bud-work\n{"status": "done", "commit": "abc123", "summary": "Esc folds rows now."}\n```')
local fake = tmp .. "/fake-claude"
vim.fn.writefile({
  "#!/bin/sh",
  "n=0",
  "while IFS= read -r line; do",
  [[  printf '%s\n' "$line" >> "$(dirname "$0")/sent.log"]],
  "  n=$((n + 1))",
  [[  cat "$(dirname "$0")/reply$n"]],
  "done",
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")
local function sent()
  return vim.tbl_map(vim.json.decode, vim.fn.readfile(tmp .. "/sent.log"))
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
local notified = {}
vim.notify = function(msg)
  table.insert(notified, msg)
end
local work = require("bud.ai.work")
-- cloud runs are saved here, not in the real state dir
work.runs_file = function()
  return tmp .. "/cloud-runs.json"
end
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
eq(ask("mcp__claude_ai_Linear__save_issue", {}).behavior, "allow", "moving the issue")
eq(ask("mcp__linear__save_issue", {}).behavior, "allow", "through the kit's linear server too")
eq(work.policy({ tool = "Edit", input = { file_path = repo .. "/README" }, root = repo }), nil, "another root")

eq(work.answer("JGA-7", "Fold it."), true, "answered")
wait(function()
  return run.state == "done"
end, "the fix")
eq(run.commit, "abc123", "commit")
eq(run.summary, "Esc folds rows now.", "summary")
eq(claude.get(run.key), nil, "session dropped")
eq(work.statusline(), "", "nothing in progress")
eq(work.policy({ tool = "Edit", input = { file_path = run.worktree .. "/README" }, root = run.worktree }), nil, "done")
local msgs = sent()
eq(#msgs, 2, "prompt and answer")
for _, want in ipairs({ "Linear issue JGA-7", "branch `jga-7-esc-drops-the-proposal` (from `main`)", "```bud-work" }) do
  assert(msgs[1].message.content:find(want, 1, true), ("prompt lacks %q"):format(want))
end
eq(msgs[2].message.content, "Fold it.", "answer sent")
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

-- cloud (JGA-237): ai.work.where = "cloud" sends the issue to `claude --cloud`
-- from a repo whose origin is on GitHub, and watches origin for the branch
local origin = tmp .. "/origin.git"
vim.fn.system({ "git", "init", "-q", "--bare", origin })
local crepo = tmp .. "/cloudrepo"
vim.fn.mkdir(crepo, "p")
local function cgit(args)
  local out = vim.fn.system(vim.list_extend({ "git", "-C", crepo, "-c", "commit.gpgsign=false" }, args))
  assert(vim.v.shell_error == 0, out)
  return vim.trim(out)
end
cgit({ "init", "-q", "-b", "main" })
vim.fn.writefile({ "hi" }, crepo .. "/README")
cgit({ "add", "README" })
cgit({ "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "chore: start" })
-- a GitHub-looking origin that really is the local bare repo
cgit({ "remote", "add", "origin", "https://github.com/me/proj.git" })
cgit({ "config", "url." .. origin .. ".insteadOf", "https://github.com/me/proj.git" })

eq(work.where(nil), "local", "local by default")
eq(work.where("cloud"), "cloud", "the call picks")
vim.fn.writefile({ '{"ai": {"work": {"where": "cloud"}}}' }, tmp .. "/config.json")
eq(work.where(nil), "cloud", "the setting picks")
eq(work.where("local"), "local", "the call beats the setting")

local launched
local function fake_launch(printed, code)
  work.launch = function(argv, cwd, on_line, on_exit)
    launched = { argv = argv, cwd = cwd }
    vim.schedule(function()
      for _, l in ipairs(printed) do
        on_line(l)
      end
      on_exit(code)
    end)
    return 1
  end
end

-- Linear is read and written through the local claude, stubbed here
local asked_linear = {}
work.linear = function(prompt, tools, cb)
  table.insert(asked_linear, { prompt = prompt, tools = tools })
  vim.schedule(function()
    if prompt:find("Move the Linear issue", 1, true) then
      return cb("done")
    end
    cb("# JGA-20 Cloud one\n\nStatus: Backlog\n\nMake the thing do the other thing.\n\nComment (Jason): use a table.")
  end)
end

-- base branch not on origin yet: refused, nothing launched
fake_launch({}, 0)
launched = nil
local brun = work.start("JGA-20", "Cloud one", crepo)
wait(function()
  return brun.state == "failed"
end, "the base check")
assert(brun.summary:find("isn't on origin yet", 1, true), brun.summary)
eq(launched, nil, "nothing launched without the base on origin")
work.runs["JGA-20"] = nil
cgit({ "push", "-q", "origin", "main" })

-- not a GitHub origin: refused
eq(work.start("JGA-20", "Cloud one", repo, "cloud"), nil, "no GitHub origin")

-- branches already on origin for the id (an earlier attempt), and ones whose
-- name only starts with the same digits, are not this run's
cgit({ "push", "-q", "origin", "main:refs/heads/jga-20-earlier-attempt" })
cgit({ "push", "-q", "origin", "main:refs/heads/jga-200-other-issue" })
local seen
work.on_origin("" .. crepo, "JGA-20", function(b)
  seen = b
end)
wait(function()
  return seen
end, "origin listing")
eq(seen, { "jga-20-earlier-attempt" }, "whole-word match only")

-- launched; the session link is read from what claude prints
fake_launch({
  "Created cloud session: Cloud one",
  "View: https://claude.ai/code/session_01Abc?from=cli&m=0",
  "Resume with: claude --teleport session_01Abc",
}, 0)
work.poll_ms = 30
local crun = work.start("JGA-20", "Cloud one", crepo)
wait(function()
  return crun.session
end, "the session link")
eq(crun.session, "session_01Abc", "session id")
eq(crun.url, "https://claude.ai/code/session_01Abc?from=cli&m=0", "url")
eq(launched.argv[1], "claude", "runs claude")
eq(launched.argv[2], "--cloud", "as a cloud session")
eq(launched.cwd, vim.uv.fs_realpath(crepo), "from the repo")
for _, want in ipairs({
  "Linear issue JGA-20",
  "Make the thing do the other thing.",
  "use a table.",
  "Linear isn't available in this session",
  "branch `jga-20-cloud-one` from `main`",
  "git push -u origin jga-20-cloud-one",
  "Don't open a pull request",
  "`claude/jga-20-cloud-one`",
}) do
  assert(launched.argv[3]:find(want, 1, true), "cloud prompt lacks: " .. want)
end
eq(crun.state, "working", "working until the branch is pushed")
vim.wait(150) -- a few polls: the earlier attempt's branch must not finish it
eq(crun.state, "working", "an earlier branch for the id doesn't count")
wait(function()
  return #asked_linear >= 2
end, "the status move")
assert(asked_linear[1].prompt:find("Print the Linear issue JGA-20", 1, true), "reads the issue first")
assert(vim.tbl_contains(asked_linear[1].tools, "mcp__claude_ai_Linear__get_issue"), "read tools only")
assert(not vim.tbl_contains(asked_linear[1].tools, "mcp__claude_ai_Linear__save_issue"), "no writes while reading")
assert(asked_linear[2].prompt:find('"In Progress"', 1, true), asked_linear[2].prompt)
eq(work.statusline() ~= "", true, "counted as in progress")

-- the branch appears on origin: done
cgit({ "push", "-q", "origin", "main:refs/heads/claude/jga-20-cloud-one-x1y2" })
wait(function()
  return crun.state == "done"
end, "the pushed branch")
eq(crun.branch, "claude/jga-20-cloud-one-x1y2", "the branch the session really pushed")
wait(function()
  return #asked_linear >= 3
end, "the In Review move")
assert(asked_linear[3].prompt:find('"In Review"', 1, true), asked_linear[3].prompt)
local clines = issue.list_lines({}, { ["JGA-20"] = crun })
assert(clines[3]:find("cloud  https://claude.ai/code/session_01Abc", 1, true), clines[3])

-- answering is for the web session, not bud
work.runs["JGA-21"] = nil
-- a folder Claude Code doesn't trust yet: says what to do
fake_launch({ "Quick safety check: Is this a project you trust?" }, 1)
local trun = work.start("JGA-21", "Untrusted", crepo, "cloud")
wait(function()
  return trun.state == "failed"
end, "the trust failure")
assert(trun.summary:find("run claude there once", 1, true), trun.summary)
-- a trust prompt that waits for a key never exits: it is stopped when seen
local stopped
local real_jobstop = vim.fn.jobstop
vim.fn.jobstop = function(job)
  stopped = job
  return 1
end
work.launch = function(_, _, on_line)
  vim.schedule(function()
    on_line("Quick safety check: Is this a project you trust?")
  end)
  return 7 -- and on_exit is never called
end
work.runs["JGA-22"] = nil
local hrun = work.start("JGA-22", "Hangs", crepo, "cloud")
wait(function()
  return hrun.state == "failed"
end, "the hanging trust prompt")
assert(hrun.summary:find("run claude there once", 1, true), hrun.summary)
eq(stopped, 7, "the waiting job is stopped")
vim.fn.jobstop = real_jobstop

-- Linear unreadable: nothing is launched blind
launched = nil
local real_linear = work.linear
work.linear = function(_, _, cb)
  vim.schedule(function()
    cb(nil, "not logged in")
  end)
end
work.runs["JGA-21"] = nil
trun = work.start("JGA-21", "Blind", crepo, "cloud")
wait(function()
  return trun.state == "failed"
end, "the unreadable issue")
assert(
  trun.summary:find("couldn't read the issue", 1, true) and trun.summary:find("not logged in", 1, true),
  trun.summary
)
eq(launched, nil, "no cloud session without the issue")
work.linear = real_linear
-- no session and no trust prompt: the output is shown
fake_launch({ "boom" }, 2)
work.runs["JGA-21"] = nil
trun = work.start("JGA-21", "Broken", crepo, "cloud")
wait(function()
  return trun.state == "failed"
end, "a failed launch")
assert(trun.summary:find("exited 2", 1, true) and trun.summary:find("boom", 1, true), trun.summary)
vim.fn.delete(tmp .. "/config.json")

-- cloud runs survive a restart (JGA-239): saved as they change, picked up by
-- load, and the ones still working are watched again
local saved_run = vim.json.decode(table.concat(vim.fn.readfile(tmp .. "/cloud-runs.json"), "\n"))
eq(saved_run["JGA-20"].state, "done", "a finished run was saved")
eq(saved_run["JGA-20"].url, "https://claude.ai/code/session_01Abc?from=cli&m=0", "with its link")
work.runs["JGA-30"] = {
  id = "JGA-30",
  cloud = true,
  state = "working",
  session = "session_x",
  repo = crepo,
  branch = "jga-30-x",
  base = "main",
  started = os.time(),
  before = {},
}
work.runs["JGA-31"] =
  { id = "JGA-31", cloud = true, state = "working", repo = crepo, branch = "jga-31-x", started = os.time() }
work.runs["JGA-32"] =
  { id = "JGA-32", cloud = true, state = "done", repo = crepo, branch = "jga-32-x", started = os.time() - 8 * 86400 }
work.save()
work.runs = {} -- bud restarts
local real_on_origin = work.on_origin
work.on_origin = function(_, _, cb)
  cb({ "claude/jga-30-x-ab12" })
end
work.load()
eq(work.runs["JGA-20"].state, "done", "a finished run is listed again")
eq(work.runs["JGA-32"], nil, "runs older than a week are dropped")
eq(work.runs["JGA-31"].state, "failed", "a run with no session can't be followed")
assert(work.runs["JGA-31"].summary:find("closed before", 1, true), work.runs["JGA-31"].summary)
wait(function()
  return work.runs["JGA-30"].state == "done"
end, "the resumed watch")
eq(work.runs["JGA-30"].branch, "claude/jga-30-x-ab12", "its branch")
work.on_origin = real_on_origin
local again = vim.json.decode(table.concat(vim.fn.readfile(tmp .. "/cloud-runs.json"), "\n"))
eq(again["JGA-30"].state, "done", "and saved again")

-- saving keeps a run another bud window saved, and this window's own win
local function disk()
  return vim.json.decode(table.concat(vim.fn.readfile(tmp .. "/cloud-runs.json"), "\n"))
end
local theirs = disk()
theirs["JGA-40"] = { id = "JGA-40", cloud = true, state = "done", started = os.time() }
vim.fn.writefile({ vim.json.encode(theirs) }, tmp .. "/cloud-runs.json")
work.save()
eq(disk()["JGA-40"].state, "done", "another window's run survives a save")
eq(disk()["JGA-30"].state, "done", "and this window's runs are still there")

-- a run restored after a restart counts the time it already waited, so it
-- gives up on schedule instead of starting over
work.runs["JGA-50"] = {
  id = "JGA-50",
  cloud = true,
  state = "working",
  session = "session_y",
  repo = crepo,
  branch = "jga-50-x",
  base = "main",
  started = os.time() - 7 * 3600,
  before = {},
}
work.on_origin = function(_, _, cb)
  cb({})
end
work.watch(work.runs["JGA-50"])
wait(function()
  return work.runs["JGA-50"].state == "failed"
end, "a restored run gives up")
work.on_origin = real_on_origin

-- a filed issue is worked in the repo it was filed for
issue.filed = { { id = "JGA-9", title = "Other", repo = repo } }
local started
work.start = function(id, title, r)
  started = { id, title, r }
end
issue.work("JGA-9")
eq(started, { "JGA-9", "Other", repo }, "work uses the filed repo")

claude.stop_all()
vim.fn.delete(tmp, "rf")
print("work_test: ok")
