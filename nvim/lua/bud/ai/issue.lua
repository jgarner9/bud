-- Filing an issue from the editor (JGA-125): <leader>ai for this project,
-- <leader>aB for bud itself. A float takes the report, bud attaches where you
-- were (the file and lines, diagnostics there, recent messages, versions), and
-- a session of its own runs the kit's file-issue skill (JGA-124) in two
-- rounds: it proposes issues as a bud-issues block, you review them, and only
-- on y does it file them in Linear and answer with a bud-filed block. Where
-- they go comes from bud issue target (JGA-123).
--
-- Linear writes are bud's call, not a permission prompt: a policy denies them
-- while a report is being proposed or reviewed and allows them once you've
-- said y; reads are allowed throughout (bud.ai.permissions.policies).

local claude = require("bud.ai.claude")
local icons = require("bud.icons")

local M = {}

-- reports in flight, by session key: { key, root, target, state, proposal }
-- state: "proposing", "review", "filing"
M.runs = {}
-- what was filed this Neovim session, newest last: { id, url, title, action }
M.filed = {}
-- unsent reports, by kind ("project" or "bud"), kept when the float is cancelled
M.drafts = {}

local count = 0

-- bud runs bud's CLI and returns its stdout, or nil and why; swapped in tests
function M.bud(args)
  local bin = require("bud.cli").bin()
  if not bin then
    return nil, "can't find the bud binary"
  end
  local res = vim.system(vim.list_extend({ bin }, args), { text = true }):wait()
  if res.code ~= 0 then
    return nil, vim.trim(res.stderr ~= "" and res.stderr or res.stdout)
  end
  return res.stdout
end

-- skill_installed says whether the kit's file-issue skill is in Claude Code
function M.skill_installed()
  local dir = vim.env.CLAUDE_CONFIG_DIR or (vim.env.HOME .. "/.claude")
  return vim.fn.filereadable(dir .. "/skills/file-issue/SKILL.md") == 1
end

-- linear_tool splits a Linear MCP tool's name into whether it only reads
local function linear_tool(tool)
  local name = tool:match("^mcp__.*[Ll]inear[^_]*__(.+)$")
  if not name then
    return nil
  end
  local reads = name:match("^list_") or name:match("^get_") or name:match("^search") or name:match("^extract")
  return name, reads ~= nil
end

-- policy answers Linear requests from a root with a report in flight
function M.policy(req)
  local name, reads = linear_tool(req.tool or "")
  if not name then
    return nil
  end
  for _, run in pairs(M.runs) do
    if run.root == req.root then
      if reads or run.state == "filing" then
        return "allow"
      end
      return "deny", "Not yet: propose the issues as a bud-issues block; bud files them after the user's yes."
    end
  end
  return nil
end

-- recent_messages is the tail of :messages, where errors from the last few
-- minutes usually are
local function recent_messages(n)
  local lines = vim.split(vim.fn.execute("messages"), "\n", { trimempty = true })
  return vim.list_slice(lines, math.max(1, #lines - n + 1))
end

-- context gathers where the report was made from: the selection (or cursor
-- line) as @path#L, diagnostics on those lines, recent messages, versions
function M.context()
  local ctx = { messages = recent_messages(15), diagnostics = {} }
  local v = vim.version()
  ctx.nvim = ("%d.%d.%d"):format(v.major, v.minor, v.patch)
  ctx.layout = require("bud.layout").current
  local where = require("bud.ai.actions").range()
  if where then
    ctx.where = where
    local first, last = where:match("#L(%d+)%-?(%d*)$")
    first = tonumber(first)
    last = tonumber(last) or first
    for _, d in ipairs(vim.diagnostic.get(0)) do
      if d.lnum + 1 >= first and d.lnum + 1 <= last then
        table.insert(
          ctx.diagnostics,
          ("L%d %s: %s"):format(d.lnum + 1, d.source or vim.diagnostic.severity[d.severity], d.message)
        )
      end
    end
  end
  return ctx
end

-- context_lines is the context as the float shows it and the prompt sends it
function M.context_lines(ctx)
  local lines = {}
  if ctx.where then
    table.insert(lines, "Where: @" .. ctx.where)
  end
  for _, d in ipairs(ctx.diagnostics or {}) do
    table.insert(lines, "Diagnostic: " .. d)
  end
  table.insert(
    lines,
    ("Versions: bud %s, Neovim %s%s"):format(
      ctx.bud or "?",
      ctx.nvim or "?",
      ctx.layout and (", layout " .. ctx.layout) or ""
    )
  )
  if ctx.messages and #ctx.messages > 0 then
    table.insert(lines, "Recent messages:")
    for _, m in ipairs(ctx.messages) do
      table.insert(lines, "  " .. m)
    end
  end
  return lines
end

local function describe(target)
  return target.project ~= "" and (target.team .. " / " .. target.project) or target.team
end

-- prompt is round 1's message: the report, the target and the context
function M.prompt(report, target, ctx)
  local quoted = vim.tbl_map(function(l)
    return "> " .. l
  end, vim.split(report, "\n", { plain = true }))
  return table.concat({
    "Use the file-issue skill. This is round 1: propose only, don't file anything yet.",
    "",
    ("Target: team %q%s."):format(
      target.team,
      target.project ~= "" and (", project " .. ("%q"):format(target.project)) or ""
    ),
    "",
    "Report:",
    table.concat(quoted, "\n"),
    "",
    "Context from the editor:",
    "```",
    table.concat(M.context_lines(ctx), "\n"),
    "```",
  }, "\n")
end

-- parse finds the fenced block tagged tag (bud-issues, bud-filed) in text and
-- decodes it, or returns nil and why
function M.parse(text, tag)
  local body = (text or ""):match("```" .. vim.pesc(tag) .. "%s*\n(.-)\n```")
  if not body then
    return nil, "no " .. tag .. " block in the reply"
  end
  local ok, value = pcall(vim.json.decode, body, { luanil = { object = true, array = true } })
  if not ok or type(value) ~= "table" then
    return nil, "the " .. tag .. " block isn't valid JSON"
  end
  return value
end

local priorities = { "Urgent", "High", "Medium", "Low" }

-- review_lines draws a proposal: one row per issue, its description under it
-- when expanded[key]; rows[line] says which issue a line belongs to
function M.review_lines(p, expanded)
  local issues = p.issues or {}
  local lines = {
    "",
    ("  [o.o]  %d %s for %s"):format(
      #issues,
      #issues == 1 and "issue" or "issues",
      describe({ team = p.team or "?", project = p.project or "" })
    ),
    "",
  }
  local rows = {}
  for _, issue in ipairs(issues) do
    local head
    if issue.duplicate_of then
      head = ("  %s  comment on %s: %s"):format(issue.key or "?", issue.duplicate_of, issue.title or "")
    else
      head = ("  %s  %-6s  %s"):format(issue.key or "?", priorities[issue.priority] or "", issue.title or "")
      if issue.labels and #issue.labels > 0 then
        head = head .. "  [" .. table.concat(issue.labels, ", ") .. "]"
      end
      if issue.blocked_by and #issue.blocked_by > 0 then
        head = head .. "  (after " .. table.concat(issue.blocked_by, ", ") .. ")"
      end
    end
    table.insert(lines, head)
    rows[#lines] = issue.key
    if expanded[issue.key] then
      for _, l in ipairs(vim.split(issue.description or "", "\n", { plain = true })) do
        table.insert(lines, "       " .. l)
        rows[#lines] = issue.key
      end
      table.insert(lines, "")
    end
  end
  if p.notes and p.notes ~= "" then
    vim.list_extend(lines, { "", "  " .. p.notes })
  end
  vim.list_extend(lines, { "", "  y file   <CR> details   e edit   n drop", "" })
  return lines, rows
end

local function float(lines, opts)
  local width = opts.width
  if not width then
    width = 40
    for _, l in ipairs(lines) do
      width = math.max(width, vim.fn.strdisplaywidth(l) + 2)
    end
  end
  width = math.min(width, vim.o.columns - 4)
  local height = math.min(math.max(#lines, opts.height or 1), vim.o.lines - 6)
  local buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].bufhidden = "wipe"
  local win = vim.api.nvim_open_win(buf, true, {
    relative = "editor",
    width = width,
    height = height,
    row = math.floor((vim.o.lines - height) / 2) - 1,
    col = math.floor((vim.o.columns - width) / 2),
    style = "minimal",
    border = "rounded",
    title = " " .. icons.claude .. " " .. opts.title .. " ",
    title_pos = "center",
    footer = opts.footer and (" " .. opts.footer .. " ") or nil,
    footer_pos = opts.footer and "center" or nil,
  })
  vim.wo[win].wrap = true
  return buf, win
end

local function close(win)
  if win and vim.api.nvim_win_is_valid(win) then
    vim.api.nvim_win_close(win, true)
  end
end

local function finish(run)
  M.runs[run.key] = nil
  claude.drop(run.key)
end

local show_review

-- on_filed records round 2's answer
local function on_filed(run, d)
  finish(run)
  local filed, why = M.parse(d.text, "bud-filed")
  if not filed then
    vim.notify("bud: Claude didn't confirm what it filed (" .. why .. "):\n" .. (d.text or ""), vim.log.levels.WARN)
    return
  end
  local lines = {}
  for _, f in ipairs(filed) do
    table.insert(M.filed, f)
    table.insert(lines, ("%s %s  %s"):format(f.action or "filed", f.id or "?", f.title or f.error or ""))
  end
  vim.notify("bud: " .. table.concat(lines, "\n"), vim.log.levels.INFO)
end

-- file sends round 2: the proposal as reviewed
function M.file(run)
  run.state = "filing"
  local text = "File these:\n```bud-issues\n" .. vim.json.encode(run.proposal) .. "\n```"
  if claude.send(text, { key = run.key }) then
    vim.notify("bud: filing in " .. describe(run.target) .. "…")
  else
    finish(run)
  end
end

-- the order a proposal's keys are written in, for editing; others follow by name
local key_order = {
  "team",
  "project",
  "issues",
  "notes",
  "key",
  "title",
  "priority",
  "labels",
  "milestone",
  "blocked_by",
  "duplicate_of",
  "description",
}

-- pretty writes v as indented JSON (vim.json.encode has no indent in 0.12)
function M.pretty(v, indent)
  indent = indent or ""
  local inner = indent .. "  "
  if type(v) ~= "table" then
    return vim.json.encode(v)
  end
  if vim.islist(v) then
    if #v == 0 then
      return "[]"
    end
    local scalars = vim.iter(v):all(function(x)
      return type(x) ~= "table"
    end)
    local parts = vim.tbl_map(function(x)
      return scalars and vim.json.encode(x) or (inner .. M.pretty(x, inner))
    end, v)
    return scalars and ("[" .. table.concat(parts, ", ") .. "]")
      or ("[\n" .. table.concat(parts, ",\n") .. "\n" .. indent .. "]")
  end
  local keys = vim.tbl_keys(v)
  local rank = {}
  for i, k in ipairs(key_order) do
    rank[k] = i
  end
  table.sort(keys, function(a, b)
    local ra, rb = rank[a] or 99, rank[b] or 99
    return ra ~= rb and ra < rb or (ra == rb and a < b)
  end)
  local parts = {}
  for _, k in ipairs(keys) do
    table.insert(parts, inner .. vim.json.encode(k) .. ": " .. M.pretty(v[k], inner))
  end
  return "{\n" .. table.concat(parts, ",\n") .. "\n" .. indent .. "}"
end

-- edit opens the proposal as JSON; :w puts it back in the review
local function edit(run)
  local buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_name(buf, "bud-issue://" .. run.key)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, vim.split(M.pretty(run.proposal), "\n", { plain = true }))
  vim.bo[buf].filetype = "json"
  vim.bo[buf].buftype = "acwrite"
  vim.bo[buf].bufhidden = "wipe"
  vim.cmd("tab sbuffer " .. buf)
  vim.api.nvim_create_autocmd("BufWriteCmd", {
    buffer = buf,
    callback = function()
      local text = table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n")
      local ok, value = pcall(vim.json.decode, text, { luanil = { object = true, array = true } })
      if not ok or type(value) ~= "table" or type(value.issues) ~= "table" then
        vim.notify("bud: that isn't a proposal (JSON with an issues list)", vim.log.levels.ERROR)
        return
      end
      run.proposal = value
      vim.bo[buf].modified = false
      vim.cmd("tabclose")
      show_review(run)
    end,
  })
end

show_review = function(run)
  local expanded = {}
  local lines, rows = M.review_lines(run.proposal, expanded)
  local buf, win = float(lines, { title = "file issues", width = math.min(100, vim.o.columns - 4) })
  vim.bo[buf].modifiable = false
  local function redraw()
    lines, rows = M.review_lines(run.proposal, expanded)
    vim.bo[buf].modifiable = true
    vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
    vim.bo[buf].modifiable = false
    vim.api.nvim_win_set_height(win, math.min(#lines, vim.o.lines - 6))
  end
  local function map(lhs, fn)
    vim.keymap.set("n", lhs, fn, { buffer = buf, nowait = true })
  end
  map("<CR>", function()
    local key = rows[vim.fn.line(".")]
    if key then
      expanded[key] = not expanded[key] or nil
      redraw()
    end
  end)
  map("y", function()
    close(win)
    M.file(run)
  end)
  map("e", function()
    close(win)
    edit(run)
  end)
  -- <Esc> only folds what's open, so reading a long description never loses the proposal
  map("<Esc>", function()
    if next(expanded) then
      expanded = {}
      redraw()
    end
  end)
  for _, lhs in ipairs({ "n", "q" }) do
    map(lhs, function()
      close(win)
      finish(run)
      vim.notify("bud: dropped; nothing was filed")
    end)
  end
end

-- on_proposed shows round 1's answer for review
local function on_proposed(run, d)
  local proposal, why = M.parse(d.text, "bud-issues")
  if not proposal then
    finish(run)
    vim.notify("bud: no issues came back (" .. why .. "):\n" .. (d.text or ""), vim.log.levels.WARN)
    return
  end
  if type(proposal.issues) ~= "table" or #proposal.issues == 0 then
    finish(run)
    vim.notify("bud: Claude proposed nothing" .. (proposal.notes and (": " .. proposal.notes) or ""))
    return
  end
  run.state, run.proposal = "review", proposal
  show_review(run)
end

-- start sends a report for target from root, and follows the session's answers
function M.start(report, target, root, ctx)
  count = count + 1
  local key = "issue:" .. count
  local run = { key = key, root = vim.uv.fs_realpath(root) or root, target = target, state = "proposing" }
  M.runs[key] = run
  -- a conversation of its own, never the project's
  claude.session(run.root, { key = key, resume = "new", chat = true })
  local unsub
  unsub = claude.on("done", function(d, s)
    if s.key ~= key then
      return
    end
    if not M.runs[key] then
      return unsub()
    end
    if not d.ok then
      unsub()
      finish(run)
      vim.notify("bud: filing the issue failed: " .. (d.text or "no answer"), vim.log.levels.WARN)
    elseif run.state == "proposing" then
      on_proposed(run, d)
    elseif run.state == "filing" then
      unsub()
      on_filed(run, d)
    end
  end)
  if not claude.send(M.prompt(report, target, ctx), { key = key }) then
    unsub()
    finish(run)
    return nil
  end
  vim.notify("bud: Claude is looking into it; the issues to file show up here")
  return run
end

-- target asks bud where kind's issues go; with none set it asks and offers
-- to save, then calls cb(target)
function M.target(kind, cb)
  local args = { "issue", "target", "--json" }
  if kind == "bud" then
    table.insert(args, "--bud")
  end
  table.insert(args, vim.fn.getcwd())
  local out, why = M.bud(args)
  local ok, target = pcall(vim.json.decode, out or "")
  if not out or not ok then
    vim.notify("bud: " .. (why or "bud issue target answered oddly"), vim.log.levels.ERROR)
    return
  end
  if target.source ~= "" then
    return cb(target)
  end
  if kind == "bud" then
    vim.notify("bud: no tracker for bud itself; set ai.issues.bud.team with bud config set", vim.log.levels.WARN)
    return
  end
  vim.ui.input({ prompt = "Linear team for this project's issues: " }, function(team)
    if not team or team == "" then
      return
    end
    vim.ui.input({ prompt = "Linear project in " .. team .. " (empty for none): " }, function(project)
      target.team, target.project = team, project or ""
      vim.ui.select({ "Yes", "Just this once" }, { prompt = "Use it for this project from now on?" }, function(choice)
        if choice == "Yes" then
          local set = { "issue", "set", "--team", team }
          if target.project ~= "" then
            vim.list_extend(set, { "--project", target.project })
          end
          table.insert(set, target.root)
          local _, err = M.bud(set)
          if err then
            vim.notify("bud: couldn't save it: " .. err, vim.log.levels.WARN)
          end
        end
        cb(target)
      end)
    end)
  end)
end

-- open takes a report for this project (kind "project") or bud ("bud")
function M.open(kind)
  kind = kind or "project"
  if not M.skill_installed() then
    vim.notify("bud: the file-issue skill isn't in Claude Code yet: run bud kit sync", vim.log.levels.WARN)
    return
  end
  -- read before the float takes the cursor
  local ctx = M.context()
  vim.api.nvim_feedkeys(vim.keycode("<Esc>"), "n", false)
  M.target(kind, function(target)
    ctx.bud = vim.trim(M.bud({ "version" }) or ""):gsub("^bud ", "")
    -- bud's own issues are looked into in its checkout, else the synced config
    local root = target.root ~= "" and target.root or vim.fn.stdpath("config")
    M.capture(kind, target, root, ctx)
  end)
end

-- capture is the report float: the first line is the summary, the rest
-- details; <C-s> sends, q cancels and keeps the draft, <Tab> shows the context
function M.capture(kind, target, root, ctx)
  local draft = M.drafts[kind] or { "" }
  local buf, win = float(draft, {
    title = "issue for " .. describe(target),
    width = math.min(80, vim.o.columns - 4),
    height = 8,
    footer = "<C-s> send · q cancel · <Tab> context",
  })
  vim.bo[buf].filetype = "markdown"
  local ns = vim.api.nvim_create_namespace("bud.ai.issue")
  local showing = false
  local function toggle()
    vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
    showing = not showing
    if showing then
      local virt = { { { "", "Comment" } } }
      for _, l in ipairs(M.context_lines(ctx)) do
        table.insert(virt, { { l, "Comment" } })
      end
      vim.api.nvim_buf_set_extmark(buf, ns, vim.api.nvim_buf_line_count(buf) - 1, 0, { virt_lines = virt })
    end
  end
  local function text()
    return vim.api.nvim_buf_get_lines(buf, 0, -1, false)
  end
  local function send()
    local lines = text()
    local report = vim.trim(table.concat(lines, "\n"))
    if report == "" then
      vim.notify("bud: say what's wrong first", vim.log.levels.INFO)
      return
    end
    M.drafts[kind] = nil
    close(win)
    vim.cmd.stopinsert()
    M.start(report, target, root, ctx)
  end
  local function cancel()
    local lines = text()
    M.drafts[kind] = vim.trim(table.concat(lines, "\n")) ~= "" and lines or nil
    close(win)
  end
  vim.keymap.set({ "n", "i" }, "<C-s>", send, { buffer = buf })
  vim.keymap.set("n", "<Tab>", toggle, { buffer = buf })
  for _, lhs in ipairs({ "q", "<Esc>" }) do
    vim.keymap.set("n", lhs, cancel, { buffer = buf, nowait = true })
  end
  vim.cmd.startinsert({ bang = true })
end

-- list shows what was filed this session; <CR> opens one in the browser
function M.list()
  if #M.filed == 0 then
    vim.notify("bud: nothing filed yet this session (<leader>ai)")
    return
  end
  local lines, urls = { "" }, {}
  for _, f in ipairs(M.filed) do
    table.insert(lines, ("  %-9s %-10s %s"):format(f.action or "filed", f.id or "?", f.title or f.error or ""))
    urls[#lines] = f.url
  end
  table.insert(lines, "")
  local buf, win = float(lines, { title = "issues filed", footer = "<CR> open · q close" })
  vim.bo[buf].modifiable = false
  vim.keymap.set("n", "<CR>", function()
    local url = urls[vim.fn.line(".")]
    if url then
      vim.ui.open(url)
    end
  end, { buffer = buf })
  for _, lhs in ipairs({ "q", "<Esc>" }) do
    vim.keymap.set("n", lhs, function()
      close(win)
    end, { buffer = buf, nowait = true })
  end
end

function M.setup()
  table.insert(require("bud.ai.permissions").policies, M.policy)
  vim.keymap.set({ "n", "x" }, "<leader>ai", function()
    M.open("project")
  end, { desc = "File an issue about this project" })
  vim.keymap.set({ "n", "x" }, "<leader>aB", function()
    M.open("bud")
  end, { desc = "File an issue about bud" })
  vim.api.nvim_create_user_command("BudIssue", function(o)
    M.open(o.args == "bud" and "bud" or "project")
  end, {
    nargs = "?",
    desc = "File an issue with Claude: about this project, or bud",
    complete = function()
      return { "bud" }
    end,
  })
  vim.api.nvim_create_user_command("BudIssues", M.list, { desc = "Issues filed this session" })
end

return M
