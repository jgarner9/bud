-- The Claude Code session alongside the editor (BASE.md 4.2, D25). Every AI
-- part of bud talks to Claude Code through this module, and nothing else
-- starts its own process.
--
-- One Claude Code process per project root, run headless with JSON on stdin
-- and stdout (`claude -p --input-format stream-json --output-format
-- stream-json`). Starting one costs nothing: Claude Code says nothing and
-- spends no tokens until the first message. A Neovim keeps the sessions of its
-- last three projects and stops any nothing has used for
-- ai.claude.idle_minutes; the next use starts it again on the same
-- conversation (D49). Each Neovim has its own, even on one project (D50).
--
-- The interface is Claude Code-agnostic (D26): events carry plain tables, and
-- the raw stream-json event rides along as `raw` for anyone who needs more.
--
--   local claude = require("bud.ai.claude")
--   claude.session()                       -- this project's (starting it)
--   claude.send("explain this", { context = { "lua/x.lua" } })
--   claude.on("message", function(ev, session) ... end)
--   claude.interrupt()
--   claude.status()                        -- starting|ready|busy|stopped|missing|term

local M = {}

-- how many projects keep a running session (D49)
M.max_sessions = 3

local sessions = {} -- root -> session
local listeners = {} -- event -> { fn, ... }
local idle_timer

-- now is in milliseconds. Not vim.uv.now(), which is cached per loop tick, so
-- two sessions used in one tick would tie for least recently used
local function now()
  return vim.uv.hrtime() / 1e6
end

-- swappable in tests: the command a session runs, and whether claude is
-- logged in (cb(ok, why))
M.bin = function()
  local path = vim.fn.exepath("claude")
  if path ~= "" then
    return path
  end
  local native = vim.fn.expand("~/.local/bin/claude")
  return vim.fn.executable(native) == 1 and native or nil
end

M.check_login = function(bin, cb)
  vim.system({ bin, "auth", "status", "--json" }, { text = true }, function(res)
    local ok, st = pcall(vim.json.decode, res.stdout or "")
    vim.schedule(function()
      if ok and type(st) == "table" and st.loggedIn then
        cb(true)
      else
        cb(false, "Claude Code isn't logged in: run bud claude setup")
      end
    end)
  end)
end

local function settings()
  local cfg = require("bud.config")
  return {
    autostart = cfg.get("ai.claude.autostart", true),
    resume = cfg.get("ai.claude.resume", "continue"),
    idle_minutes = cfg.get("ai.claude.idle_minutes", 30),
    args = cfg.get("ai.claude.args", {}),
  }
end

local function emit(event, data, session)
  for _, fn in ipairs(listeners[event] or {}) do
    local ok, err = pcall(fn, data, session)
    if not ok then
      vim.notify("bud.ai.claude: " .. event .. " listener: " .. tostring(err), vim.log.levels.WARN)
    end
  end
end

local function set_status(s, status, why)
  s.status, s.why = status, why
  emit("status", { status = status, why = why }, s)
  vim.cmd.redrawstatus({ bang = true })
end

-- _emit lets bud's own modules (permissions) raise an event on root's session
function M._emit(event, data, root)
  root = root or vim.fn.getcwd()
  root = vim.uv.fs_realpath(root) or root
  emit(event, data, sessions[root] or { root = root })
end

-- on registers fn for event: message, tool_use, permission_request, done,
-- exit or status. It returns a function that unregisters it
function M.on(event, fn)
  listeners[event] = listeners[event] or {}
  table.insert(listeners[event], fn)
  return function()
    for i, f in ipairs(listeners[event]) do
      if f == fn then
        table.remove(listeners[event], i)
        return
      end
    end
  end
end

-- argv is the command line a session runs: resuming its own conversation
-- once it has one, else the most recent unless resume is "new"
function M.argv(bin, s, cfg)
  local argv = { bin, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose" }
  return vim.list_extend(vim.list_extend(argv, M.resume_args(s, cfg)), cfg.args or {})
end

-- resume_args picks the conversation: the session's own once it has one, the
-- most recent after the terminal had it (its id may have moved on) or when
-- resume is "continue", else a new one
function M.resume_args(s, cfg)
  if s.session_id and not s.continue_next then
    return { "--resume", s.session_id }
  elseif s.continue_next or cfg.resume == "continue" then
    return { "--continue" }
  end
  return {}
end

-- handle turns one stream-json event into bud's events
function M.handle(s, ev)
  if ev.session_id then
    s.session_id = ev.session_id
  end
  if ev.type == "system" and ev.subtype == "init" then
    s.model = ev.model
  elseif ev.type == "assistant" and type(ev.message) == "table" then
    for _, block in ipairs(ev.message.content or {}) do
      if block.type == "text" then
        emit("message", { role = "assistant", text = block.text, raw = ev }, s)
      elseif block.type == "tool_use" then
        emit("tool_use", { id = block.id, name = block.name, input = block.input, raw = ev }, s)
      end
    end
  elseif ev.type == "result" then
    s.cost = (s.cost or 0) + (ev.total_cost_usd or 0)
    s.last_used = now()
    set_status(s, "ready")
    emit("done", {
      ok = ev.subtype == "success" and not ev.is_error,
      text = ev.result,
      cost = ev.total_cost_usd,
      denials = ev.permission_denials,
      raw = ev,
    }, s)
  end
end

-- feed splits job output into lines, holding a partial last line back
local function feed(s, data)
  data[1] = (s.partial or "") .. data[1]
  s.partial = table.remove(data)
  for _, line in ipairs(data) do
    if line ~= "" then
      local ok, ev = pcall(vim.json.decode, line, { luanil = { object = true, array = true } })
      if ok and type(ev) == "table" then
        M.handle(s, ev)
      end
    end
  end
end

-- least recently used running session other than keep, to make room
local function evict(keep)
  local running, oldest = 0, nil
  for _, s in pairs(sessions) do
    if s.job and s ~= keep then
      running = running + 1
      if not oldest or s.last_used < oldest.last_used then
        oldest = s
      end
    end
  end
  if running >= M.max_sessions and oldest then
    M.stop(oldest.root)
  end
end

local function spawn(s)
  local bin = M.bin()
  if not bin then
    set_status(s, "missing", "Claude Code isn't installed: run bud claude setup")
    return
  end
  evict(s)
  s.starting = true
  set_status(s, "starting")
  M.check_login(bin, function(ok, why)
    if not s.starting then
      return -- stopped while checking
    end
    s.starting = nil
    if not ok then
      set_status(s, "missing", why)
      return
    end
    local env = M.bridge_env(s.root)
    local argv = vim.list_extend(M.argv(bin, s, settings()), require("bud.ai.bridge").headless_args(s.root))
    local job = vim.fn.jobstart(argv, {
      cwd = s.root,
      env = env,
      on_stdout = function(_, data)
        feed(s, data)
      end,
      on_stderr = function(_, data)
        local text = vim.trim(table.concat(data, "\n"))
        if text ~= "" then
          s.stderr = text
        end
      end,
      on_exit = function(_, code)
        local wanted = s.stopping
        s.job, s.stopping, s.partial = nil, nil, nil
        s.crashed = not wanted
        -- a handover stopped it, and the terminal has the session now
        if not s.term then
          set_status(
            s,
            "stopped",
            s.crashed and ("Claude Code exited (" .. code .. ")" .. (s.stderr and ": " .. s.stderr or "")) or nil
          )
        end
        emit("exit", { code = code, crashed = s.crashed }, s)
      end,
    })
    if job <= 0 then
      set_status(s, "stopped", "couldn't start " .. bin)
      s.crashed = true
      return
    end
    s.job, s.crashed, s.stderr, s.continue_next = job, false, nil, nil
    -- what was sent while it started
    for _, line in ipairs(s.queue) do
      vim.fn.chansend(job, line)
    end
    set_status(s, #s.queue > 0 and "busy" or "ready")
    s.queue = {}
  end)
end

-- bridge_env brings up the IDE bridge (JGA-81) and returns what a Claude
-- Code process in root needs to connect to it; {} when it can't start
function M.bridge_env(root)
  local bridge = require("bud.ai.bridge")
  local ok, why = bridge.start()
  if not ok then
    vim.notify_once("bud: the IDE bridge didn't start: " .. tostring(why), vim.log.levels.WARN)
    return {}
  end
  bridge.add_folder(root)
  return bridge.env()
end

local function root_of(root)
  root = root or vim.fn.getcwd()
  return vim.uv.fs_realpath(root) or root
end

-- session returns root's session (default: the current dir), starting it if
-- it isn't running. opts.context lists files to mention with the first message
function M.session(root, opts)
  root = root_of(root)
  local s = sessions[root]
  if not s then
    s = { root = root, status = "stopped", queue = {}, context = {}, last_used = now() }
    sessions[root] = s
  end
  if opts and opts.context then
    vim.list_extend(s.context, opts.context)
  end
  s.last_used = now()
  if not s.job and not s.starting and not s.term then
    spawn(s)
  end
  return s
end

-- handover gives root's session to Claude Code's own TUI (D48): the headless
-- job stops, and nothing can send until handback. It returns the TUI's argv,
-- or nil and why it can't run
function M.handover(root)
  local bin = M.bin()
  if not bin then
    return nil, "Claude Code isn't installed: run bud claude setup"
  end
  root = root_of(root)
  local s = sessions[root] or { root = root, queue = {}, context = {} }
  sessions[root] = s
  if s.job then
    s.stopping = true
    vim.fn.jobstop(s.job)
  end
  s.starting, s.queue, s.term, s.last_used = nil, {}, true, now()
  set_status(s, "term")
  local cfg = settings()
  return vim.list_extend(vim.list_extend({ bin }, M.resume_args(s, cfg)), cfg.args or {})
end

-- handback returns root's session from the TUI. The TUI may have moved to
-- another conversation, so the next start continues the most recent one
function M.handback(root)
  local s = sessions[root_of(root)]
  if s and s.term then
    s.term, s.continue_next = nil, true
    set_status(s, "stopped")
  end
end

-- message is the stream-json line for text, with files to mention first
function M.message(text, context)
  if context and #context > 0 then
    local mentions = vim.tbl_map(function(f)
      return "@" .. f
    end, context)
    text = "Context: " .. table.concat(mentions, " ") .. "\n\n" .. text
  end
  return vim.json.encode({ type = "user", message = { role = "user", content = text } }) .. "\n"
end

-- send sends text to the current project's session (or opts.root's),
-- starting it again if it was stopped. opts.context lists files to mention
function M.send(text, opts)
  opts = opts or {}
  local s = M.session(opts.root)
  if s.term then
    vim.notify("bud: Claude Code is open in its terminal (<leader>ac)", vim.log.levels.INFO)
    return false
  end
  if s.status == "missing" then
    vim.notify("bud: " .. (s.why or "Claude Code isn't available"), vim.log.levels.WARN)
    return false
  end
  local context = vim.list_extend(vim.list_slice(s.context), opts.context or {})
  s.context = {}
  local line = M.message(text, context)
  if s.job then
    vim.fn.chansend(s.job, line)
  else
    table.insert(s.queue, line)
  end
  set_status(s, "busy")
  return true
end

-- interrupt stops the reply in progress, keeping the session
function M.interrupt(root)
  local s = sessions[root_of(root)]
  if s and s.job and s.status == "busy" then
    s.requests = (s.requests or 0) + 1
    local req = { type = "control_request", request_id = "bud-" .. s.requests, request = { subtype = "interrupt" } }
    vim.fn.chansend(s.job, vim.json.encode(req) .. "\n")
  end
end

-- status is root's session status (default: the current dir's), "stopped"
-- when there's none
function M.status(root)
  local s = sessions[root_of(root)]
  return s and s.status or "stopped", s and s.why
end

-- stop ends root's session; its conversation stays, for the next start
function M.stop(root)
  local s = sessions[root_of(root)]
  if s and s.job then
    s.stopping = true
    vim.fn.jobstop(s.job)
  elseif s then
    s.starting, s.queue = nil, {}
    set_status(s, "stopped")
  end
end

-- restart starts root's session again on the same conversation
function M.restart(root)
  local s = sessions[root_of(root)]
  if s and s.job then
    s.stopping = true
    vim.fn.jobstop(s.job)
    vim.fn.jobwait({ s.job }, 2000)
  end
  return M.session(root)
end

function M.stop_all()
  for root in pairs(sessions) do
    M.stop(root)
  end
end

-- sessions lists the known sessions, running or not
function M.list()
  return vim.tbl_values(sessions)
end

-- reap stops sessions idle for longer than minutes (D49). A reply in
-- progress isn't idle, however long it takes
function M.reap(minutes, at)
  at = at or now()
  for _, s in pairs(sessions) do
    if s.job and s.status ~= "busy" and at - s.last_used > minutes * 60 * 1000 then
      M.stop(s.root)
    end
  end
  require("bud.ai.term").reap(minutes, at)
end

-- statusline is the session's part of the status line: nothing until there's
-- a session for this project, and a key to press when it crashed
function M.statusline()
  local s = sessions[root_of()]
  if not s then
    return ""
  end
  local icon = require("bud.icons").claude
  if s.crashed then
    return icon .. " crashed, <leader>ar"
  end
  local label = ({
    starting = "starting",
    ready = "",
    busy = "thinking",
    stopped = "idle",
    missing = "not set up",
    term = "in terminal",
  })[s.status]
  return label == "" and icon or (icon .. " " .. label)
end

-- setup starts the session for how bud was opened (the 4.2 table): a project
-- dir, or files from their root (mentioned with the first message), and
-- nothing for bare bud; then opening a project from inside starts its own
function M.setup()
  local cfg = settings()
  vim.api.nvim_create_user_command("BudClaude", function(o)
    local sub = o.fargs[1] or "status"
    if sub == "term" then
      require("bud.ai.term").open()
    elseif sub == "restart" then
      M.restart()
    elseif sub == "stop" then
      M.stop()
    else
      local status, why = M.status()
      local bridge = require("bud.ai.bridge")
      local port = bridge.port()
      vim.notify(
        ("Claude Code: %s%s; IDE bridge %s"):format(
          status,
          why and (" (" .. why .. ")") or "",
          port and ("on port " .. port .. ", " .. bridge.connected() .. " TUI connected") or "not started"
        )
      )
    end
  end, {
    nargs = "?",
    desc = "Claude Code: its own TUI (term), or the session's status, restart or stop",
    complete = function()
      return { "term", "status", "restart", "stop" }
    end,
  })
  vim.keymap.set("n", "<leader>ac", function()
    require("bud.ai.term").toggle()
  end, { desc = "Claude Code (its own TUI)" })
  vim.keymap.set("n", "<leader>ar", function()
    M.restart()
  end, { desc = "Restart Claude Code" })
  vim.keymap.set({ "n", "x" }, "<leader>as", function()
    local first, last = vim.fn.line("v"), vim.fn.line(".")
    if vim.fn.mode() == "n" then
      first, last = last, last
    end
    M.share(vim.api.nvim_buf_get_name(0), math.min(first, last), math.max(first, last))
    vim.api.nvim_feedkeys(vim.keycode("<Esc>"), "n", false)
  end, { desc = "Share the selection with Claude Code" })
  require("bud.statusline").add(M.statusline)

  local group = vim.api.nvim_create_augroup("bud.ai.claude", { clear = true })
  vim.api.nvim_create_autocmd("VimLeavePre", {
    group = group,
    desc = "Stop the Claude Code sessions",
    callback = M.stop_all,
  })
  if not cfg.autostart then
    return
  end
  local minutes = tonumber(cfg.idle_minutes) or 30
  if minutes > 0 then
    idle_timer = idle_timer or vim.uv.new_timer()
    idle_timer:start(
      60 * 1000,
      60 * 1000,
      vim.schedule_wrap(function()
        M.reap(minutes)
      end)
    )
  end

  if vim.env.BUD_OPEN == "project" then
    M.session()
  elseif vim.fn.argc() > 0 then
    local files = {}
    for i = 0, vim.fn.argc() - 1 do
      table.insert(files, vim.fn.fnamemodify(vim.fn.argv(i), ":."))
    end
    M.session(nil, { context = files })
  end
end

-- share points Claude Code at lines first..last (1-based) of path: the TUI
-- gets it as an @-mention right away, the headless session with the next
-- message
function M.share(path, first, last)
  if path == "" then
    return
  end
  local s = sessions[root_of()]
  if s and s.term then
    require("bud.ai.bridge").mention(path, first - 1, last - 1)
  else
    local rel = vim.fn.fnamemodify(path, ":.")
    s = M.session()
    table.insert(s.context, first == last and ("%s#L%d"):format(rel, first) or ("%s#L%d-%d"):format(rel, first, last))
  end
  vim.notify(("bud: shared %s:%d-%d with Claude Code"):format(vim.fn.fnamemodify(path, ":~:."), first, last))
end

-- opened starts the session for a project opened from inside bud (the
-- dashboard, <leader>p), unless ai.claude.autostart is off
function M.opened(root)
  if settings().autostart then
    M.session(root)
  end
end

-- reset forgets every session, for tests
function M._reset()
  M.stop_all()
  sessions, listeners = {}, {}
end

return M
