-- Working a filed issue (JGA-126, D71): Claude fixes it in a git worktree of
-- its own, beside the repo (<repo>.worktrees/<id>, on branch <id>-<title>), so
-- the tree you're testing in never changes under you. Edits inside that
-- worktree are accepted without asking; shell commands still go through bud's
-- permission float (D69), and so does anything outside it. Claude moves the
-- issue to In Progress, runs the project's checks, commits with the issue id,
-- moves it to In Review, and answers with a bud-work block. Merging and
-- pushing stay yours.

local claude = require("bud.ai.claude")

local M = {}

-- runs by issue id: { id, title, repo, worktree, branch, base, key, state, summary, commit }
-- state: "working", "waiting" (Claude asked something), "done", "failed"
M.runs = {}

local edit_tools = { Edit = true, MultiEdit = true, Write = true, NotebookEdit = true }

-- git runs git in dir and returns its trimmed stdout, or nil and stderr
function M.git(dir, args)
  local res = vim.system(vim.list_extend({ "git", "-C", dir }, args), { text = true }):wait()
  if res.code ~= 0 then
    return nil, vim.trim(res.stderr or "")
  end
  return vim.trim(res.stdout or "")
end

-- branch_name is the issue's branch: its id, then the title as a slug
function M.branch_name(id, title)
  local slug = (title or ""):lower():gsub("[^%w]+", "-"):gsub("^%-+", ""):gsub("%-+$", "")
  slug = slug:sub(1, 40):gsub("%-+$", "")
  return id:lower() .. (slug ~= "" and ("-" .. slug) or "")
end

-- worktree_path is where id's worktree goes: next to the repo, never in
-- bud's own dirs, which bud uninstall deletes
function M.worktree_path(repo, id)
  return vim.fs.dirname(repo) .. "/" .. vim.fs.basename(repo) .. ".worktrees/" .. id:lower()
end

-- base is the branch new work starts from: the remote's default, else the
-- branch checked out (or its commit, detached). Never "HEAD", which in the
-- worktree would mean the work's own branch
function M.base(repo)
  local ref = M.git(repo, { "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD" })
  if ref then
    local local_branch = ref:gsub("^origin/", "")
    if M.git(repo, { "rev-parse", "--verify", "--quiet", local_branch }) then
      return local_branch
    end
    return ref
  end
  local current = M.git(repo, { "branch", "--show-current" })
  if current and current ~= "" then
    return current
  end
  return M.git(repo, { "rev-parse", "--short", "HEAD" }) or "HEAD"
end

-- prepare makes (or reuses) id's worktree, returning its path, branch and
-- base, or nil and why
function M.prepare(repo, id, title)
  local top, why = M.git(repo, { "rev-parse", "--show-toplevel" })
  if not top then
    -- bud's own issues are filed from the synced config when no checkout is linked
    local hint = repo == vim.fn.stdpath("config") and " (for bud itself, bud dev link a checkout first)" or ""
    return nil, "not a git repo: " .. (why or repo) .. hint
  end
  repo = vim.uv.fs_realpath(top) or top
  local path = M.worktree_path(repo, id)
  local branch = M.branch_name(id, title)
  local base = M.base(repo)
  if vim.fn.isdirectory(path) == 1 then
    return {
      path = vim.uv.fs_realpath(path) or path,
      branch = M.git(path, { "branch", "--show-current" }) or branch,
      base = base,
      repo = repo,
    }
  end
  vim.fn.mkdir(vim.fs.dirname(path), "p")
  local args = M.git(repo, { "rev-parse", "--verify", "--quiet", "refs/heads/" .. branch })
      and { "worktree", "add", path, branch }
    or { "worktree", "add", "-b", branch, path, base }
  local ok, err = M.git(repo, args)
  if not ok then
    return nil, "git worktree add failed: " .. (err or "")
  end
  return { path = vim.uv.fs_realpath(path) or path, branch = branch, base = base, repo = repo }
end

-- inside says whether path (absolute, or relative to root) is in root, with
-- symlinks and .. resolved as far as the path exists
function M.inside(root, path)
  if type(path) ~= "string" or path == "" then
    return false
  end
  if path:sub(1, 1) ~= "/" then
    path = root .. "/" .. path
  end
  local rest = {}
  local dir = path
  while dir ~= "/" and not vim.uv.fs_stat(dir) do
    table.insert(rest, 1, vim.fs.basename(dir))
    dir = vim.fs.dirname(dir)
  end
  for _, part in ipairs(rest) do
    if part == ".." then
      return false
    end
  end
  local real = vim.uv.fs_realpath(dir) or dir
  local full = #rest > 0 and (real .. "/" .. table.concat(rest, "/")) or real
  return full == root or full:sub(1, #root + 1) == root .. "/"
end

local function run_at(root)
  for _, run in pairs(M.runs) do
    if run.worktree == root and (run.state == "working" or run.state == "waiting") then
      return run
    end
  end
end

-- policy: a working run's edits in its worktree, and its Linear updates, go
-- through; everything else is asked as usual
function M.policy(req)
  local run = run_at(req.root)
  if not run then
    return nil
  end
  if edit_tools[req.tool] then
    local input = req.input or {}
    if M.inside(run.worktree, input.file_path or input.notebook_path) then
      return "allow"
    end
    return nil
  end
  if (req.tool or ""):match("^mcp__.*[Ll]inear[^_]*__") then
    return "allow"
  end
  return nil
end

-- prompt is what Claude is asked to do with the issue
function M.prompt(run)
  return table.concat({
    ("Work on the Linear issue %s%s."):format(run.id, run.title and (' ("' .. run.title .. '")') or ""),
    "",
    ("You're in a git worktree made for it, on branch `%s` (from `%s`). Change files only inside it."):format(
      run.branch,
      run.base
    ),
    "",
    "1. Read the issue in Linear (description and comments) and move it to In Progress.",
    "2. Fix it the way the repo's CLAUDE.md, README and existing code say things are done here.",
    "3. Run the project's own checks (its CLAUDE.md, justfile, Makefile or package scripts say which).",
    "4. When they pass, commit on this branch in the repo's commit style, with the issue id in the subject,"
      .. " and move the issue to In Review. Don't push, merge or touch other branches.",
    "5. If you're stuck, or the checks fail and you can't fix them, stop and say what's left; commit nothing broken.",
    "",
    "End your last message with exactly one fenced block:",
    "```bud-work",
    '{"status": "done" | "failed", "commit": "<sha or null>", "summary": "<one or two sentences>"}',
    "```",
    "If you need an answer from the user before going on, ask in plain text and leave the block out.",
  }, "\n")
end

local function notify(msg, level)
  vim.notify("bud: " .. msg, level or vim.log.levels.INFO)
end

-- on_done reads a reply: finished (a bud-work block) or a question
local function on_done(run, d)
  if not d.ok then
    run.state, run.summary = "failed", d.text or "Claude Code stopped"
    notify(run.id .. " failed: " .. run.summary, vim.log.levels.WARN)
    return
  end
  local result = require("bud.ai.issue").parse(d.text, "bud-work")
  if not result then
    run.state, run.question = "waiting", d.text
    notify(run.id .. ": Claude has a question; answer it from :BudIssues (a)\n" .. (d.text or ""))
    return
  end
  run.state = result.status == "done" and "done" or "failed"
  run.summary, run.commit = result.summary, result.commit
  claude.drop(run.key)
  notify(
    ("%s %s on %s: %s"):format(
      run.id,
      run.state == "done" and "is ready to review" or "failed",
      run.branch,
      run.summary or ""
    ),
    run.state == "done" and vim.log.levels.INFO or vim.log.levels.WARN
  )
end

-- start hands issue id (title, if known) in repo to Claude
function M.start(id, title, repo)
  id = id:upper()
  local existing = M.runs[id]
  if existing and (existing.state == "working" or existing.state == "waiting") then
    notify(id .. " is already being worked on (:BudIssues)")
    return existing
  end
  local wt, why = M.prepare(repo, id, title)
  if not wt then
    notify("can't work " .. id .. ": " .. why, vim.log.levels.ERROR)
    return nil
  end
  local run = {
    id = id,
    title = title,
    repo = wt.repo,
    worktree = wt.path,
    branch = wt.branch,
    base = wt.base,
    key = "work:" .. id,
    state = "working",
  }
  M.runs[id] = run
  claude.session(run.worktree, { key = run.key, resume = "new", chat = true })
  local unsub
  unsub = claude.on("done", function(d, s)
    if s.key ~= run.key then
      return
    end
    on_done(run, d)
    if run.state ~= "waiting" then
      unsub()
    end
  end)
  if not claude.send(M.prompt(run), { key = run.key }) then
    unsub()
    run.state = "failed"
    return run
  end
  notify(("Claude is working on %s in %s"):format(id, vim.fn.fnamemodify(run.worktree, ":~")))
  return run
end

-- answer replies to a run that's waiting on a question
function M.answer(id, text)
  local run = M.runs[id]
  if not run or run.state ~= "waiting" then
    return false
  end
  run.state, run.question = "working", nil
  return claude.send(text, { key = run.key })
end

-- diff opens what the run changed against its base, in a tab of its own
function M.diff(id)
  local run = M.runs[id]
  if not run then
    return
  end
  vim.cmd("tabnew")
  vim.cmd.tcd(run.worktree)
  vim.cmd("DiffviewOpen " .. run.base .. "...HEAD")
end

-- statusline counts the runs in progress
function M.statusline()
  local working, waiting = 0, 0
  for _, run in pairs(M.runs) do
    if run.state == "working" then
      working = working + 1
    elseif run.state == "waiting" then
      waiting = waiting + 1
    end
  end
  if working + waiting == 0 then
    return ""
  end
  local icons = require("bud.icons")
  local text = icons.tool .. " " .. working
  if waiting > 0 then
    text = text .. " " .. icons.chat .. " " .. waiting
  end
  return text
end

function M.setup()
  table.insert(require("bud.ai.permissions").policies, M.policy)
  require("bud.statusline").add(M.statusline)
end

return M
