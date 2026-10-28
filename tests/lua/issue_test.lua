-- Run with: nvim --clean --headless -l tests/lua/issue_test.lua (just lua-test)
-- Filing an issue from the editor (JGA-125), on a fake claude

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

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/repo", "p")
tmp = vim.uv.fs_realpath(tmp)
vim.env.CLAUDE_CONFIG_DIR = tmp .. "/claude-home"
require("bud.config").file = tmp .. "/config.json"
require("bud.ai.bridge").lock_dir = function()
  return tmp .. "/ide"
end

local proposal = {
  team = "JGA",
  project = "Bud IDE",
  issues = {
    { key = "a", title = "Dashboard drops a row", description = "It does.\nOn narrow terminals.", priority = 2 },
    { key = "b", title = "Same thing", description = "Me too", duplicate_of = "JGA-42" },
  },
  notes = "b looks like JGA-42",
}
local filed = { { key = "a", id = "JGA-200", url = "https://linear.app/x/JGA-200", title = "Dashboard drops a row" } }

-- answers each message from the next reply file, logging what it's sent
local function reply(n, text)
  local line = vim.json.encode({ type = "result", subtype = "success", session_id = "s1", result = text })
  vim.fn.writefile({ line }, tmp .. "/reply" .. n)
end
reply(1, "Found it.\n```bud-issues\n" .. vim.json.encode(proposal) .. "\n```")
reply(2, "Filed.\n```bud-filed\n" .. vim.json.encode(filed) .. "\n```")
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
  local f = tmp .. "/sent.log"
  return vim.fn.filereadable(f) == 1 and vim.tbl_map(vim.json.decode, vim.fn.readfile(f)) or {}
end

local claude = require("bud.ai.claude")
claude.bin = function()
  return fake
end
claude.secret_env = function(_, cb)
  cb({})
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
local issue = require("bud.ai.issue")
local permissions = require("bud.ai.permissions")
table.insert(permissions.policies, issue.policy)

-- parse: the tagged block, or why not
eq(issue.parse("x\n```bud-filed\n[1]\n```\n", "bud-filed"), { 1 }, "parse")
eq(select(2, issue.parse("no block", "bud-issues")), "no bud-issues block in the reply", "no block")
eq(
  select(2, issue.parse("```bud-issues\n{nope\n```", "bud-issues")),
  "the bud-issues block isn't valid JSON",
  "bad json"
)

-- pretty writes JSON that reads back the same, keys in a useful order
eq(vim.json.decode(issue.pretty(proposal)), proposal, "pretty round trip")
assert(issue.pretty(proposal):find('^{\n  "team": "JGA",\n  "project"'), issue.pretty(proposal))

-- context: the line, its diagnostics, versions
local repo = tmp .. "/repo"
vim.fn.chdir(repo)
vim.fn.writefile({ "local x = 1", "return y" }, repo .. "/init.lua")
vim.cmd.edit(repo .. "/init.lua")
vim.api.nvim_win_set_cursor(0, { 2, 0 })
local ns = vim.api.nvim_create_namespace("issue_test")
vim.diagnostic.set(ns, 0, {
  { lnum = 1, col = 7, message = "undefined global y", source = "lua_ls", severity = 2 },
  { lnum = 0, col = 0, message = "not this line", source = "lua_ls", severity = 2 },
})
local ctx = issue.context()
eq(ctx.where, "init.lua#L2", "where")
eq(ctx.diagnostics, { "L2 lua_ls: undefined global y" }, "diagnostics on the line only")
ctx.bud, ctx.messages = "0.11.0", { "E5108: boom" }
local text = issue.prompt("Dashboard drops a row\nonly when narrow", { team = "JGA", project = "Bud IDE" }, ctx)
for _, want in ipairs({
  "round 1: propose only",
  'Target: team "JGA", project "Bud IDE".',
  "> Dashboard drops a row\n> only when narrow",
  "Where: @init.lua#L2",
  "Diagnostic: L2 lua_ls: undefined global y",
  "Versions: bud 0.11.0, Neovim",
  "  E5108: boom",
}) do
  assert(text:find(want, 1, true), ("prompt lacks %q:\n%s"):format(want, text))
end

-- review_lines: one row per issue, details on demand
local lines, rows = issue.review_lines(proposal, {})
assert(lines[2]:find("2 issues for JGA / Bud IDE", 1, true), lines[2])
assert(lines[4]:find("a  High    Dashboard drops a row", 1, true), lines[4])
assert(lines[5]:find("b  comment on JGA-42: Same thing", 1, true), lines[5])
eq(rows[4], "a", "row a")
lines = issue.review_lines(proposal, { a = true })
eq(lines[5], "       It does.", "expanded")

-- policy: Linear reads pass, writes wait for the yes, other tools are left alone
local run = { root = repo, state = "proposing" }
issue.runs.t = run
local function ask(tool, root)
  local got
  permissions.request({
    tool = tool,
    input = {},
    root = root or repo,
    respond = function(r)
      got = r
    end,
  })
  return got
end
eq(ask("mcp__claude_ai_Linear__list_issues").behavior, "allow", "read while proposing")
eq(ask("mcp__claude_ai_Linear__save_issue").behavior, "deny", "write while proposing")
eq(issue.policy({ tool = "mcp__claude_ai_Linear__save_issue", root = "/elsewhere" }), nil, "another root")
eq(issue.policy({ tool = "Bash", root = repo }), nil, "not Linear")
run.state = "filing"
eq(ask("mcp__claude_ai_Linear__save_issue").behavior, "allow", "write once filing")
issue.runs.t = nil
eq(issue.policy({ tool = "mcp__claude_ai_Linear__save_issue", root = repo }), nil, "no report in flight")

-- target: what bud issue target says, and none for bud without a tracker
issue.bud = function(args)
  if vim.tbl_contains(args, "--bud") then
    return '{"team":"","project":"","source":"","root":""}'
  end
  return '{"team":"ENG","project":"","source":"config","root":"' .. repo .. '"}'
end
local got
issue.target("project", function(t)
  got = t
end)
eq(got.team, "ENG", "project target")
got = nil
issue.target("bud", function(t)
  got = t
end)
eq(got, nil, "no bud target")
assert(notified[#notified]:find("ai.issues.bud.team", 1, true), notified[#notified])

-- the whole flow: propose, review, y, filed
run = issue.start("Dashboard drops a row", { team = "JGA", project = "Bud IDE", root = repo }, repo, ctx)
wait(function()
  return run.state == "review"
end, "the proposal")
eq(run.proposal, proposal, "proposal read")
eq(vim.bo.buftype, "nofile", "review float focused")
vim.api.nvim_feedkeys("y", "x", false)
wait(function()
  return #issue.filed == 1
end, "the filing")
eq(issue.filed[1].id, "JGA-200", "filed id")
eq(issue.runs[run.key], nil, "run finished")
eq(claude.get(run.key), nil, "session dropped")
local msgs = sent()
eq(#msgs, 2, "two rounds")
assert(msgs[1].message.content:find("round 1", 1, true), "round 1 sent")
assert(msgs[2].message.content:find("^File these:\n```bud%-issues\n"), msgs[2].message.content)
eq(issue.parse(msgs[2].message.content, "bud-issues"), proposal, "round 2 carries the proposal")

-- n drops it: nothing more is sent
vim.fn.delete(tmp .. "/sent.log")
run = issue.start("Again", { team = "JGA", project = "Bud IDE", root = repo }, repo, ctx)
wait(function()
  return run.state == "review"
end, "the second proposal")
-- <Esc> folds an expanded row and keeps the proposal
vim.api.nvim_win_set_cursor(0, { 4, 0 })
vim.api.nvim_feedkeys(vim.keycode("<CR>"), "x", false)
eq(vim.api.nvim_buf_line_count(0) > #issue.review_lines(proposal, {}), true, "expanded")
vim.api.nvim_feedkeys(vim.keycode("<Esc>"), "x", false)
eq(vim.api.nvim_buf_line_count(0), #issue.review_lines(proposal, {}), "folded")
eq(issue.runs[run.key].state, "review", "still in review")
vim.api.nvim_feedkeys("n", "x", false)
eq(issue.runs[run.key], nil, "dropped")
eq(#sent(), 1, "only round 1 sent")

-- capture: q keeps the draft, <CR> sends it with the context
local real_start, started = issue.start, nil
issue.start = function(report, target, root, c)
  started = { report = report, target = target, root = root, ctx = c }
end
local target = { team = "JGA", project = "Bud IDE", root = repo }
issue.capture("project", target, repo, ctx)
-- <Esc> in normal mode cancels too, so leave insert mode without it
vim.cmd.stopinsert()
vim.api.nvim_buf_set_lines(0, 0, -1, false, { "Tree flickers", "when saving" })
vim.api.nvim_feedkeys("q", "x", false)
eq(issue.drafts.project, { "Tree flickers", "when saving" }, "draft kept")
issue.capture("project", target, repo, ctx)
vim.cmd.stopinsert()
eq(vim.api.nvim_buf_get_lines(0, 0, -1, false), { "Tree flickers", "when saving" }, "draft back")
vim.api.nvim_feedkeys(vim.keycode("<CR>"), "x", false)
eq(started.report, "Tree flickers\nwhen saving", "sent report")
eq(started.ctx, ctx, "sent context")
eq(issue.drafts.project, nil, "draft cleared")
issue.start = real_start

claude.stop_all()
vim.fn.delete(tmp, "rf")
print("issue_test: ok")
