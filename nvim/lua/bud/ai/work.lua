-- Working a filed issue (JGA-126, D71): Claude fixes it in a git worktree of
-- its own, beside the repo (<repo>.worktrees/<id>, on branch <id>-<title>), so
-- the tree you're testing in never changes under you. Edits inside that
-- worktree are accepted without asking; shell commands still go through bud's
-- permission float (D69), and so does anything outside it. Claude moves the
-- issue to In Progress, runs the project's checks, commits with the issue id,
-- moves it to In Review, and answers with a bud-work block. Merging and
-- pushing stay yours.
--
-- With ai.work.where = "cloud" (JGA-237) the work goes to a Claude Code cloud
-- session instead: `claude --cloud` clones the repo from GitHub, works the
-- issue and pushes a branch named after it. bud only starts it, remembers the
-- session's link, and notices when the branch shows up on origin.

-- the configured backend's session (D131)
local ai = require("bud.ai.session")

local M = {}

local function notify(msg, level)
  vim.notify("bud: " .. msg, level or vim.log.levels.INFO)
end

-- runs by issue id: { id, title, repo, worktree, branch, base, key, state, summary, commit }
-- state: "working", "waiting" (Claude asked something), "done", "failed"
M.runs = {}

-- Cloud runs outlive the editor (JGA-239): they are saved as they change and
-- picked up again by setup, which resumes watching the ones still working
function M.runs_file()
  return vim.fn.stdpath("state") .. "/cloud-runs.json"
end

local keep =
  { "id", "title", "repo", "branch", "base", "cloud", "state", "summary", "session", "url", "before", "started" }
local week = 7 * 24 * 60 * 60

-- recent says whether a saved run is young enough to keep
local function recent(run)
  return type(run) == "table" and run.cloud and os.time() - (run.started or 0) < week
end

-- saved reads the runs on disk; nothing if the file is missing or broken
local function saved()
  local f = io.open(M.runs_file())
  if not f then
    return {}
  end
  local ok, runs = pcall(vim.json.decode, f:read("*a"))
  f:close()
  return ok and type(runs) == "table" and runs or {}
end

local warned_save = false

-- save writes the cloud runs. It starts from what's on disk, so a run another
-- bud window saved isn't lost; this window's own runs win over the file's
function M.save()
  local out = {}
  for id, run in pairs(saved()) do
    if recent(run) then
      out[id] = run
    end
  end
  for id, run in pairs(M.runs) do
    if run.cloud and recent(run) then
      local r = {}
      for _, k in ipairs(keep) do
        r[k] = run[k]
      end
      out[id] = r
    else
      out[id] = nil
    end
  end
  local function fail(why)
    -- once per session: the editor keeps working, but runs won't survive a restart
    if not warned_save then
      warned_save = true
      notify("couldn't save cloud runs: " .. tostring(why), vim.log.levels.WARN)
    end
  end
  local file = M.runs_file()
  vim.fn.mkdir(vim.fs.dirname(file), "p")
  -- a temp file renamed over it, so a crash never leaves half a file
  local tmp = file .. ".tmp"
  local f, err = io.open(tmp, "w")
  if not f then
    return fail(err)
  end
  local wrote, werr = f:write(vim.json.encode(out))
  f:close()
  if not wrote then
    vim.uv.fs_unlink(tmp)
    return fail(werr)
  end
  local renamed, rerr = vim.uv.fs_rename(tmp, file)
  if not renamed then
    vim.uv.fs_unlink(tmp)
    return fail(rerr)
  end
end

-- load brings back the saved cloud runs from the last week, and watches the
-- ones still working. One that never got a session (bud closed while it was
-- starting) can't be followed, so it is marked failed. The file is only
-- rewritten when load changed something
function M.load()
  local changed = false
  for id, run in pairs(saved()) do
    if not recent(run) then
      changed = true
    elseif not M.runs[id] then
      M.runs[id] = run
      if run.state == "working" then
        if run.session then
          M.watch(run)
        else
          run.state, run.summary = "failed", "bud closed before the cloud session started"
          changed = true
        end
      end
    end
  end
  if changed then
    M.save()
  end
end

-- the swappable edges of the cloud path (tests replace them)
-- launch runs argv in cwd on a pty, calling on_line for each line it prints and
-- on_exit(code) at the end; returns a job id, or nil and why
function M.launch(argv, cwd, on_line, on_exit)
  local id = vim.fn.jobstart(argv, {
    cwd = cwd,
    pty = true,
    on_stdout = function(_, data)
      for _, line in ipairs(data) do
        on_line((line:gsub("\27%[[%d;?]*[%a]", ""):gsub("\r", "")))
      end
    end,
    on_exit = function(_, code)
      on_exit(code)
    end,
  })
  if id <= 0 then
    return nil, "can't run " .. argv[1]
  end
  return id
end

-- on_origin calls cb(branches): the branches on repo's origin whose name has the
-- issue id as a whole word (jga-2-x, claude/jga-2-x, jga-2; not jga-20-x).
-- Cloud sessions can be limited to a claude/ prefix, so a run's own branch name
-- is only a guess (JGA-237)
function M.on_origin(repo, id, cb)
  local want = id:lower()
  vim.system({ "git", "-C", repo, "ls-remote", "--heads", "origin", "*" .. want .. "*" }, { text = true }, function(r)
    vim.schedule(function()
      local found = {}
      if r.code == 0 then
        for name in (r.stdout or ""):gmatch("%x+%s+refs/heads/(%S+)") do
          local lower = name:lower()
          if
            lower:match("^" .. vim.pesc(want) .. "$")
            or lower:match("^" .. vim.pesc(want) .. "%-")
            or lower:match("/" .. vim.pesc(want) .. "$")
            or lower:match("/" .. vim.pesc(want) .. "%-")
          then
            table.insert(found, name)
          end
        end
      end
      cb(found)
    end)
  end)
end

-- has_branch calls cb(true/false) for whether branch is on repo's origin
function M.has_branch(repo, branch, cb)
  vim.system(
    { "git", "-C", repo, "ls-remote", "--exit-code", "--heads", "origin", branch },
    { text = true },
    function(r)
      vim.schedule(function()
        cb(r.code == 0)
      end)
    end
  )
end

-- linear asks the claude on this machine, which has Linear, to do one thing
-- there, with only the tools given allowed, and calls cb(text) or cb(nil, why).
-- Sessions started by `claude --cloud` have no Linear connector of their own
-- (JGA-237), so bud reads the issue and moves its status for them
local linear_tools = {
  read = {
    "mcp__claude_ai_Linear__get_issue",
    "mcp__claude_ai_Linear__list_comments",
    "mcp__linear__get_issue",
    "mcp__linear__list_comments",
  },
  write = { "mcp__claude_ai_Linear__save_issue", "mcp__linear__save_issue" },
}
function M.linear(prompt, tools, cb)
  vim.system(
    { "claude", "-p", prompt, "--allowedTools", table.concat(tools, ",") },
    { text = true, timeout = 180000 },
    function(r)
      vim.schedule(function()
        if r.code ~= 0 then
          return cb(nil, vim.trim((r.stderr or "") .. (r.stdout or "")):sub(-300))
        end
        cb(vim.trim(r.stdout or ""))
      end)
    end
  )
end

-- set_status moves a Linear issue, and says if it couldn't
function M.set_status(id, status)
  M.linear(
    ("Move the Linear issue %s to the status %q. Change nothing else. Reply with one word: done."):format(id, status),
    linear_tools.write,
    function(_, why)
      if why then
        notify(("couldn't move %s to %s: %s"):format(id, status, why), vim.log.levels.WARN)
      end
    end
  )
end

-- how often a cloud run checks for its branch, and how long it waits
M.poll_ms = 30000
M.give_up_ms = 6 * 60 * 60 * 1000

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
  -- the root resolved too, or a worktree under a symlinked dir never matches
  root = vim.uv.fs_realpath(root) or root
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

-- where says where work runs: the call's choice, else ai.work.where
function M.where(choice)
  if choice == "cloud" or choice == "local" then
    return choice
  end
  return require("bud.config").get("ai.work.where", "local") == "cloud" and "cloud" or "local"
end

-- cloud_prompt is what the cloud session is asked to do: it has no worktree
-- of bud's and nobody to answer, so it pushes the branch itself
function M.cloud_prompt(run)
  return table.concat({
    ("Work on the Linear issue %s%s."):format(run.id, run.title and (' ("' .. run.title .. '")') or ""),
    "",
    "The issue, as it is in Linear (Linear isn't available in this session; bud moves its status, so don't try):",
    "",
    run.issue or "(bud couldn't read it; ask what it says before you change anything)",
    "",
    ("You're in a fresh cloud checkout of the repo. Make the branch `%s` from `%s` and work only on it."):format(
      run.branch,
      run.base
    ),
    "",
    "1. Fix it the way the repo's CLAUDE.md, README and existing code say things are done here.",
    "2. Run the project's own checks (its CLAUDE.md, justfile, Makefile or package scripts say which).",
    "3. When they pass, commit in the repo's commit style with the issue id in the subject and push the branch"
      .. (" (`git push -u origin %s`). "):format(run.branch)
      .. "Don't open a pull request, merge, or touch other branches."
      .. (" If your environment only lets you push a `claude/` branch, use `claude/%s` and keep the issue id in the name."):format(
        run.branch
      ),
    "4. If you're stuck, or the checks fail and you can't fix them, push nothing broken and say what's left.",
  }, "\n")
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
  ai.drop(run.key)
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

-- start_cloud hands id to a Claude Code cloud session (JGA-237)
function M.start_cloud(id, title, repo)
  local top = M.git(repo, { "rev-parse", "--show-toplevel" })
  if not top then
    notify("can't work " .. id .. " in the cloud: " .. repo .. " isn't a git repo", vim.log.levels.ERROR)
    return nil
  end
  repo = vim.uv.fs_realpath(top) or top
  local origin = M.git(repo, { "config", "--get", "remote.origin.url" })
  if not origin or not origin:find("github.com", 1, true) then
    notify(
      "can't work "
        .. id
        .. " in the cloud: it clones from GitHub and this repo's origin isn't (:BudIssue work "
        .. id
        .. " local)",
      vim.log.levels.ERROR
    )
    return nil
  end
  local base = M.base(repo)
  local run = {
    id = id,
    title = title,
    repo = repo,
    branch = M.branch_name(id, title),
    base = base,
    cloud = true,
    state = "working",
    started = os.time(),
  }
  M.runs[id] = run
  M.save()
  local out = {}
  local function fail(why)
    run.state, run.summary = "failed", why
    M.save()
    notify(id .. " failed: " .. why, vim.log.levels.WARN)
  end
  local function launch()
    local job, why
    local function untrusted()
      return fail("Claude Code doesn't trust " .. repo .. " yet: run claude there once and accept it, then try again")
    end
    job, why = M.launch({ "claude", "--cloud", M.cloud_prompt(run) }, repo, function(line)
      table.insert(out, line)
      run.session = run.session or line:match("(session_%w+)")
      run.url = run.url or line:match("(https://claude%.ai/code/session_%w+[^%s]*)")
      -- it waits at this prompt for a key and never exits, so stop it here
      if run.state == "working" and line:find("Quick safety check", 1, true) then
        if job then
          vim.fn.jobstop(job)
        end
        untrusted()
      end
    end, function(code)
      if run.state ~= "working" then
        return
      end
      if run.session then
        run.url = run.url or ("https://claude.ai/code/" .. run.session)
        M.save()
        notify(("%s went to a cloud session: %s"):format(id, run.url))
        M.set_status(id, "In Progress")
        M.watch(run)
        return
      end
      local text = table.concat(out, "\n")
      fail(("claude --cloud exited %d without making a session: %s"):format(code, vim.trim(text):sub(-300)))
    end)
    if not job then
      fail(why)
    end
  end
  notify(("reading %s from Linear, then starting a cloud session…"):format(id))
  local function read_issue()
    M.linear(
      (
        "Print the Linear issue %s for a developer who can't open Linear: its title, status, description, and every comment"
        .. " (author and text), as plain markdown. Print nothing else."
      ):format(id),
      linear_tools.read,
      function(text, why)
        if not text or text == "" then
          return fail("couldn't read the issue from Linear: " .. (why or "empty answer"))
        end
        run.issue = text
        launch()
      end
    )
  end
  -- the base must be on origin, which is where the cloud session clones from;
  -- remember what branches already carry this id so they aren't taken for its
  M.has_branch(repo, base, function(ok)
    if not ok then
      return fail(("%s isn't on origin yet; push it first"):format(base))
    end
    M.on_origin(repo, id, function(existing)
      run.before = {}
      for _, name in ipairs(existing) do
        run.before[name] = true
      end
      read_issue()
    end)
  end)
  return run
end

-- watch polls origin for the run's branch, which the cloud session pushes when
-- its checks pass; that is when it's ready to review
function M.watch(run)
  local timer = vim.uv.new_timer()
  -- a run restored after a restart has already waited since it started
  local waited = math.max(0, (os.time() - (run.started or os.time())) * 1000)
  local checking = false
  local function stop()
    timer:stop()
    timer:close()
  end
  timer:start(
    M.poll_ms,
    M.poll_ms,
    vim.schedule_wrap(function()
      if run.state ~= "working" then
        return stop()
      end
      if checking then
        return
      end
      waited = waited + M.poll_ms
      checking = true
      M.on_origin(run.repo, run.id, function(branches)
        checking = false
        if run.state ~= "working" then
          return stop()
        end
        local found
        for _, name in ipairs(branches) do
          if not (run.before or {})[name] then
            found = name
            break
          end
        end
        if found then
          run.branch = found
          run.state, run.summary = "done", "branch pushed to origin; d fetches and shows it"
          M.set_status(run.id, "In Review")
          M.save()
          notify(("%s is ready to review on %s (d in :BudIssues)"):format(run.id, run.branch))
          stop()
        elseif waited >= M.give_up_ms then
          run.state, run.summary = "failed", "no branch after a long wait; check " .. (run.url or "the cloud session")
          M.save()
          notify(run.id .. ": " .. run.summary, vim.log.levels.WARN)
          stop()
        end
      end)
    end)
  )
end

-- start hands issue id (title, if known) in repo to Claude, in a worktree or,
-- where is "cloud" (else ai.work.where), a cloud session
function M.start(id, title, repo, where)
  id = id:upper()
  local existing = M.runs[id]
  if existing and (existing.state == "working" or existing.state == "waiting") then
    notify(id .. " is already being worked on (:BudIssues)")
    return existing
  end
  if M.where(where) == "cloud" then
    return M.start_cloud(id, title, repo)
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
  ai.session(run.worktree, { key = run.key, resume = "new", extra = true })
  local unsub
  unsub = ai.on("done", function(d, s)
    if s.key ~= run.key then
      return
    end
    on_done(run, d)
    if run.state ~= "waiting" then
      unsub()
    end
  end)
  if not ai.send(M.prompt(run), { key = run.key }) then
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
  return ai.send(text, { key = run.key })
end

-- diff opens what the run changed against its base, in a tab of its own
function M.diff(id)
  local run = M.runs[id]
  if not run then
    return
  end
  if run.cloud then
    local base = run.base
    notify("fetching " .. run.branch .. "…")
    vim.system({ "git", "-C", run.repo, "fetch", "origin", base, run.branch }, { text = true }, function(r)
      vim.schedule(function()
        if r.code ~= 0 then
          return notify(
            ("can't fetch %s: %s"):format(run.branch, vim.trim((r.stderr or "") .. (r.stdout or ""))),
            vim.log.levels.WARN
          )
        end
        vim.cmd("tabnew")
        vim.cmd.tcd(run.repo)
        vim.cmd(("DiffviewOpen origin/%s...origin/%s"):format(base, run.branch))
      end)
    end)
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
  M.load()
  table.insert(require("bud.ai.permissions").policies, M.policy)
  require("bud.statusline").add(M.statusline)
end

return M
