-- The agent alongside the editor, on whichever backend ai.backend names
-- (D131-D133, BASE.md 4.4). It takes bud.ai.claude's place with the same
-- calls and events, so a consumer moves over by changing its require. Under
-- it, bud.ai.acp speaks the Agent Client Protocol to the backend's process.
--
-- One agent process per project root, started after the first screen. A
-- Neovim keeps the sessions of its last three projects and stops any nothing
-- has used for ai.agent.idle_minutes (D49); the next use starts it again.
--
--   local agent = require("bud.ai.agent")
--   agent.session()                         -- this project's (starting it)
--   agent.send("explain this", { context = { "lua/x.lua" } })
--   agent.on("message", function(ev, session) ... end)
--   agent.interrupt()
--   agent.status()      -- starting|ready|busy|stopped|missing|error
--
-- Events: status, chunk (a piece of the reply as it streams), user_chunk (a
-- message of the user's replayed by session/load), message (the whole reply
-- when the turn ends), thought, tool_use, tool_update, plan,
-- permission_request, done, exit.

local acp = require("bud.ai.acp")

local M = {}

-- how many projects keep a running agent (D49)
M.max_sessions = 3

local sessions = {} -- key -> session
local listeners = {} -- event -> { fn, ... }

local function now()
  return vim.uv.hrtime() / 1e6
end

local function cfg()
  return require("bud.config")
end

local function emit(event, data, s)
  for _, fn in ipairs(listeners[event] or {}) do
    local ok, err = pcall(fn, data, s)
    if not ok then
      vim.notify("bud.ai.agent: " .. event .. " listener: " .. tostring(err), vim.log.levels.WARN)
    end
  end
end

local function set_status(s, status, why)
  s.status, s.why = status, why
  emit("status", { status = status, why = why }, s)
  pcall(vim.cmd.redrawstatus, { bang = true })
end

-- on registers fn for an event and returns a function that unregisters it
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

-- backend is the configured backend's name
function M.backend()
  return cfg().get("ai.backend", "claude")
end

-- Each project's ACP session id is saved, so the next start reattaches
-- with session/load and the conversation (and, on OpenClaw, the TUI and the
-- web UI's thread) carries on (D139). OpenClaw names a session its bridge
-- made agent:<agent>:acp-bridge:<id>, and keeps its working dir: one named
-- with --session runs its tools in the agent's workspace instead.

M.ids_file = function()
  return vim.fn.stdpath("state") .. "/agent-sessions.json"
end

local function read_ids()
  local f = io.open(M.ids_file())
  if not f then
    return {}
  end
  local ok, ids = pcall(vim.json.decode, f:read("*a"))
  f:close()
  return ok and type(ids) == "table" and ids or {}
end

-- saved_id is root's saved session id for backend, nil when there's none
function M.saved_id(root, backend)
  local saved = read_ids()[root]
  if type(saved) == "table" and saved.backend == (backend or M.backend()) then
    return saved.id
  end
end

local function save_id(root, backend, id)
  local ids = read_ids()
  local saved = ids[root]
  if type(saved) == "table" and saved.backend == backend and saved.id == id then
    return
  end
  ids[root] = id and { backend = backend, id = id } or nil
  local file = M.ids_file()
  vim.fn.mkdir(vim.fs.dirname(file), "p")
  -- a temp file renamed over it, so a crash never leaves half a file
  local tmp = file .. ".tmp"
  local f = io.open(tmp, "w")
  if f then
    f:write(vim.json.encode(ids))
    f:close()
    vim.uv.fs_rename(tmp, file)
  end
end

-- bridge_key is the Gateway session key of the ACP session id
function M.bridge_key(id, agent_id)
  return ("agent:%s:acp-bridge:%s"):format(agent_id or "main", id)
end

-- argv is the command line that starts backend as an ACP agent, or nil and
-- why it can't. get reads a setting (bud.config.get)
function M.argv(backend, get)
  get = get or function(key, default)
    return cfg().get(key, default)
  end
  if backend == "openclaw" then
    local argv = { "openclaw", "acp" }
    local url, token = get("ai.openclaw.url", ""), get("ai.openclaw.token_file", "")
    if url ~= "" then
      vim.list_extend(argv, { "--url", url })
    end
    if token ~= "" then
      vim.list_extend(argv, { "--token-file", vim.fn.expand(token) })
    end
    return argv
  elseif backend == "claude" then
    return { "claude-agent-acp" }
  elseif backend == "codex" then
    return { "codex-acp" }
  elseif backend == "opencode" then
    return { "opencode", "acp" }
  elseif backend == "custom" then
    local cmd = get("ai.custom.command", {})
    if type(cmd) ~= "table" or not cmd[1] then
      return nil, "ai.backend is custom but ai.custom.command is empty"
    end
    return vim.deepcopy(cmd)
  end
  return nil, ("ai.backend: %q isn't a backend bud knows"):format(tostring(backend))
end

-- token reads the Gateway token from ai.openclaw.token_file, for the TUI,
-- which takes it only from the environment (OPENCLAW_GATEWAY_TOKEN) or argv
local function token(get)
  local file = get("ai.openclaw.token_file", "")
  if file == "" then
    return nil
  end
  local ok, lines = pcall(vim.fn.readfile, vim.fn.expand(file), "", 1)
  return ok and lines[1] and vim.trim(lines[1]) or nil
end

-- tui is the backend's own TUI on root's session, for the AI pane: { argv,
-- env }, or nil and why there's none. OpenClaw's opens the same Gateway
-- session bud's calls use (D139), so the pane and bud share one thread
function M.tui(root, get)
  get = get or function(key, default)
    return cfg().get(key, default)
  end
  local backend = M.backend()
  if backend == "openclaw" then
    local argv = { "openclaw", "tui" }
    local url = get("ai.openclaw.url", "")
    if url ~= "" then
      vim.list_extend(argv, { "--url", url })
    end
    local id = M.session_id(root)
    if not id then
      return nil, "the project's OpenClaw session didn't start: " .. (M.get(root) and M.get(root).why or "no answer")
    end
    vim.list_extend(argv, { "--session", M.bridge_key(id, get("ai.openclaw.agent", "main")) })
    local t = token(get)
    return { argv = argv, env = t and { OPENCLAW_GATEWAY_TOKEN = t } or nil }
  elseif backend == "codex" then
    return { argv = { "codex" } }
  elseif backend == "opencode" then
    return { argv = { "opencode", root } }
  elseif backend == "claude" then
    return nil, "the claude backend's TUI is Claude Code's own (bud.ai.claude)"
  end
  return nil, ("the %s backend has no TUI bud knows"):format(backend)
end

-- tui_bin is the command the backend's TUI runs, nil when it has none
function M.tui_bin(backend)
  return ({ openclaw = "openclaw", codex = "codex", opencode = "opencode" })[backend or M.backend()]
end

-- session_id is root's ACP session id: the live one, else the saved one,
-- else it starts the session and waits up to wait ms (default 15 s) for it
function M.session_id(root, wait)
  root = root or vim.fn.getcwd()
  root = vim.uv.fs_realpath(root) or root
  local s = sessions[root]
  if s and s.id then
    return s.id
  end
  local saved = M.saved_id(root)
  if saved then
    return saved
  end
  s = M.session(root)
  vim.wait(wait or 15000, function()
    return s.id ~= nil or s.status == "error" or s.status == "missing"
  end, 20)
  return s.id
end

-- chat_tui is the backend's own TUI for chat (<leader>ch, bud chat): on
-- OpenClaw, the Gateway's one chat session, agent:<agent>:bud:chat, the
-- same key bud chat opens (internal/agent.ChatKey); elsewhere in dir
function M.chat_tui(dir, get)
  get = get or function(key, default)
    return cfg().get(key, default)
  end
  if M.backend() == "openclaw" then
    local argv = { "openclaw", "tui" }
    local url = get("ai.openclaw.url", "")
    if url ~= "" then
      vim.list_extend(argv, { "--url", url })
    end
    vim.list_extend(argv, { "--session", ("agent:%s:bud:chat"):format(get("ai.openclaw.agent", "main")) })
    local t = token(get)
    return { argv = argv, env = t and { OPENCLAW_GATEWAY_TOKEN = t } or nil }
  end
  return M.tui(dir, get)
end

-- M.command lets tests swap the backend's command line for the fake agent's
M.command = function(_root)
  return M.argv(M.backend())
end

local function root_of(root)
  root = root or vim.fn.getcwd()
  return vim.uv.fs_realpath(root) or root
end

-- lookup turns what callers pass (a key, or a project dir) into a key
local function lookup(id)
  if id and sessions[id] then
    return id
  end
  return root_of(id)
end

-- file reads and writes, through the buffer when the file is open (D131), so
-- the agent sees unsaved changes and its writes land in the editor

local function loaded(path)
  local buf = vim.fn.bufnr(path)
  if buf > 0 and vim.api.nvim_buf_is_loaded(buf) then
    return buf
  end
end

-- inside says whether path is root or under it, after resolving symlinks
-- and .. (a file that doesn't exist yet by its nearest existing parent)
function M.inside(root, path)
  if type(path) ~= "string" or path == "" then
    return false
  end
  path = vim.fs.normalize(vim.fn.fnamemodify(path, ":p"))
  local probe, rest = path, ""
  while probe ~= "/" and not vim.uv.fs_realpath(probe) do
    rest = "/" .. vim.fs.basename(probe) .. rest
    probe = vim.fs.dirname(probe)
  end
  local real = (vim.uv.fs_realpath(probe) or probe) .. rest
  root = vim.uv.fs_realpath(root) or root
  return real == root or real:sub(1, #root + 1) == root .. "/"
end

function M.read_file(path, line, limit)
  local lines
  local buf = loaded(path)
  if buf then
    lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
  elseif vim.fn.filereadable(path) == 1 then
    lines = vim.fn.readfile(path)
  else
    return nil, "no such file: " .. path
  end
  local first = math.max(1, tonumber(line) or 1)
  local last = limit and (first + limit - 1) or #lines
  local out = {}
  for i = first, math.min(last, #lines) do
    table.insert(out, lines[i])
  end
  return table.concat(out, "\n") .. (#out > 0 and "\n" or "")
end

function M.write_file(path, content)
  local lines = vim.split(content, "\n", { plain = true })
  local newline = lines[#lines] == ""
  if newline then
    table.remove(lines)
  end
  local buf = loaded(path)
  if buf then
    vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
    vim.bo[buf].endofline = newline
    vim.api.nvim_buf_call(buf, function()
      vim.cmd("silent noautocmd write")
    end)
    return true
  end
  vim.fn.mkdir(vim.fs.dirname(path), "p")
  if vim.fn.writefile(lines, path, newline and "" or "b") ~= 0 then
    return nil, "couldn't write " .. path
  end
  return true
end

-- M.permission answers a permission request: req is { session, tool, options,
-- diffs, title, input, raw }, and respond takes the chosen option's id, or
-- nil for cancelled. It may return a function that takes the question back,
-- which interrupt calls. It's bud's permission prompt, and the diff review when
-- the request carries a diff (D134); tests swap it
M.permission = function(req, respond)
  return require("bud.ai.permissions").acp(req, respond)
end

-- patch is the unified diff a finished edit reports, when the backend says
-- what it changed only afterwards: OpenClaw puts it in rawOutput.details
-- (D134: review after the write)
function M.patch(update)
  local out = type(update.rawOutput) == "table" and update.rawOutput or {}
  local details = type(out.details) == "table" and out.details or {}
  return type(details.patch) == "string" and details.patch or nil
end

local function tool_name(call)
  return call.title or call.name or call.kind or "a tool"
end

-- handle turns one session/update into bud's events
function M.handle(s, update)
  local kind = update.sessionUpdate
  if kind == "agent_message_chunk" then
    local text = acp.text(update.content)
    s.reply = (s.reply or "") .. text
    s.final = (s.final or "") .. text
    emit("chunk", { text = text }, s)
  elseif kind == "user_message_chunk" then
    -- the history session/load replays
    emit("user_chunk", { text = acp.text(update.content) }, s)
  elseif kind == "agent_thought_chunk" then
    emit("thought", { text = acp.text(update.content) }, s)
  elseif kind == "tool_call" then
    -- the answer is what comes after the last tool call
    s.final = ""
    s.tools[update.toolCallId] = update
    emit("tool_use", {
      id = update.toolCallId,
      name = tool_name(update),
      kind = update.kind,
      input = update.rawInput,
      diffs = acp.diffs(update),
      raw = update,
    }, s)
  elseif kind == "tool_call_update" then
    local call = s.tools[update.toolCallId] or { toolCallId = update.toolCallId }
    for k, v in pairs(update) do
      call[k] = v
    end
    s.tools[update.toolCallId] = call
    emit("tool_update", {
      id = update.toolCallId,
      status = call.status,
      name = tool_name(call),
      kind = call.kind,
      output = acp.text(update.content),
      diffs = acp.diffs(call),
      patch = M.patch(update),
      raw = call,
    }, s)
  elseif kind == "plan" then
    s.final = ""
    emit("plan", { entries = update.entries or {} }, s)
  elseif kind == "available_commands_update" then
    s.commands = update.availableCommands or {}
  elseif kind == "config_option_update" then
    s.options = update.configOptions or s.options
  elseif kind == "usage_update" then
    s.usage = update
  end
end

local start, flush

-- request handles the agent's own requests: permissions and files
local function on_request(s, method, params, respond)
  s.last_used = now()
  if method == "session/request_permission" then
    local call = params.toolCall or {}
    local req = {
      session = s,
      tool = tool_name(call),
      title = call.title,
      input = call.rawInput,
      options = params.options or {},
      diffs = acp.diffs(call),
      raw = params,
    }
    local done = false
    local function answer(option_id)
      if done then
        return
      end
      done = true
      s.asking[answer] = nil
      if option_id then
        respond({ outcome = { outcome = "selected", optionId = option_id } })
      else
        respond({ outcome = { outcome = "cancelled" } })
      end
    end
    s.asking[answer] = true
    emit("permission_request", req, s)
    -- the permission UI may hand back a way to take the question back
    local withdraw = M.permission(req, answer)
    if not done and type(withdraw) == "function" then
      s.asking[answer] = withdraw
    end
  elseif (method == "fs/read_text_file" or method == "fs/write_text_file") and not M.inside(s.root, params.path) then
    -- only the session's project: a backend reached over the network must
    -- not read or write the rest of this machine through bud
    respond(nil, { code = -32602, message = ("%s is outside the project (%s)"):format(tostring(params.path), s.root) })
  elseif method == "fs/read_text_file" then
    local text, err = M.read_file(params.path, params.line, params.limit)
    if text then
      respond({ content = text })
    else
      respond(nil, { code = -32002, message = err })
    end
  elseif method == "fs/write_text_file" then
    local ok, err = M.write_file(params.path, params.content or "")
    if ok then
      respond(nil)
    else
      respond(nil, { code = -32603, message = err })
    end
  else
    respond(nil, { code = -32601, message = "bud doesn't handle " .. method })
  end
end

-- evict stops the least recently used project agents beyond max_sessions,
-- never keep and never one that's busy
local function evict(keep)
  local running = {}
  for key, s in pairs(sessions) do
    if key ~= keep and not s.extra and s.conn and s.conn:running() and s.status ~= "busy" then
      table.insert(running, s)
    end
  end
  table.sort(running, function(a, b)
    return (a.last_used or 0) < (b.last_used or 0)
  end)
  local over = #running + 1 - M.max_sessions
  for i = 1, over do
    M.stop(running[i].key)
  end
end

start = function(s)
  local argv, why = M.command(s.root)
  if not argv then
    set_status(s, "error", why)
    return
  end
  s.backend = s.backend or M.backend()
  s.tools, s.asking, s.queue, s.reply = {}, {}, s.queue or {}, nil
  set_status(s, "starting")
  -- declared first: the callbacks below compare against it
  local conn, err
  conn, err = acp.connect(argv, {
    cwd = s.root,
    env = s.env,
    on_notification = function(method, params)
      if method == "session/update" and params.sessionId == s.id then
        M.handle(s, params.update or {})
      end
    end,
    on_request = function(method, params, respond)
      on_request(s, method, params, respond)
    end,
    on_exit = function(code, stderr)
      vim.schedule(function()
        if s.conn ~= conn then
          return
        end
        s.conn = nil
        local crashed = not s.stopping
        s.stopping = false
        if crashed and s.status == "busy" then
          emit("done", { ok = false, error = "the agent stopped", text = s.reply }, s)
        end
        emit("exit", { code = code, crashed = crashed, stderr = stderr }, s)
        if crashed then
          local last = vim.trim(stderr or ""):match("([^\n]*)$") or ""
          set_status(s, "error", ("%s stopped (exit %d)%s"):format(argv[1], code, last ~= "" and (": " .. last) or ""))
        else
          set_status(s, "stopped")
        end
      end)
    end,
  })
  if not conn then
    set_status(s, vim.fn.executable(argv[1]) ~= 1 and "missing" or "error", err)
    return
  end
  s.conn = conn
  conn:request("initialize", {
    protocolVersion = acp.protocol_version,
    clientCapabilities = { fs = { readTextFile = true, writeTextFile = true }, terminal = false },
    clientInfo = { name = "bud", version = vim.env.BUD_VERSION or "dev" },
  }, function(ierr, init)
    if ierr then
      set_status(s, "error", "initialize: " .. acp.why(ierr))
      return
    end
    s.caps = (init and init.agentCapabilities) or {}
    s.agent_info = init and init.agentInfo
    local function opened(nerr, res)
      if nerr or not (res and (res.sessionId or s.id)) then
        set_status(s, "error", "session: " .. (nerr and acp.why(nerr) or "no session id"))
        return
      end
      s.id = (res and res.sessionId) or s.id
      if res and res.configOptions then
        s.options = res.configOptions
      end
      if s.key == s.root then
        save_id(s.root, s.backend, s.id)
      end
      set_status(s, "ready")
      flush(s)
    end
    local function new()
      s.id = nil
      conn:request("session/new", { cwd = s.root, mcpServers = {} }, opened)
    end
    if s.key == s.root and not s.id then
      s.id = M.saved_id(s.root, s.backend)
    end
    if s.id and s.caps.loadSession then
      s.loading = true
      conn:request("session/load", { sessionId = s.id, cwd = s.root, mcpServers = {} }, function(lerr, res)
        s.loading = false
        if lerr then
          -- gone from the backend: start a new one
          return new()
        end
        opened(nil, res or { sessionId = s.id })
      end)
    else
      new()
    end
  end)
end

-- session returns root's session (default: the current dir), starting it
-- when it isn't running. opts.key names another one in root (an issue's,
-- gen's); opts.extra keeps it out of the max_sessions count
function M.session(root, opts)
  opts = opts or {}
  root = root_of(root)
  local key = opts.key or root
  local s = sessions[key]
  if not s then
    s = { key = key, root = root, extra = opts.extra, status = "stopped", last_used = now(), env = opts.env }
    sessions[key] = s
  end
  s.last_used = now()
  if not (s.conn and s.conn:running()) and s.status ~= "starting" then
    if not s.extra then
      evict(key)
    end
    start(s)
  end
  return s
end

-- blocks is the prompt for text: the text, then each context file as a link
-- the agent reads itself, then a selection as embedded text when the agent
-- takes it (else folded into the text)
function M.blocks(text, opts, caps)
  opts = opts or {}
  local blocks = { { type = "text", text = text } }
  for _, f in ipairs(opts.context or {}) do
    local abs = vim.fn.fnamemodify(f, ":p")
    table.insert(blocks, { type = "resource_link", uri = vim.uri_from_fname(abs), name = vim.fn.fnamemodify(f, ":t") })
  end
  local sel = opts.selection
  if sel then
    local uri = vim.uri_from_fname(vim.fn.fnamemodify(sel.path, ":p"))
    if caps and caps.promptCapabilities and caps.promptCapabilities.embeddedContext then
      table.insert(blocks, {
        type = "resource",
        resource = { uri = ("%s#L%d-%d"):format(uri, sel.first, sel.last), text = sel.text, mimeType = "text/plain" },
      })
    else
      blocks[1].text = ("%s\n\nLines %d-%d of %s:\n```\n%s\n```"):format(
        text,
        sel.first,
        sel.last,
        vim.fn.fnamemodify(sel.path, ":."),
        sel.text
      )
    end
  end
  return blocks
end

-- prompt runs one turn
local function prompt(s, item)
  s.reply, s.final = "", ""
  s.tools = {}
  set_status(s, "busy")
  s.conn:request(
    "session/prompt",
    { sessionId = s.id, prompt = M.blocks(item.text, item.opts, s.caps) },
    function(err, res)
      s.last_used = now()
      local reply = s.reply
      s.reply = nil
      if s.status ~= "busy" then
        return
      end
      local stop = res and res.stopReason
      if reply ~= "" then
        emit("message", { role = "assistant", text = reply }, s)
      end
      emit("done", {
        ok = err == nil and stop ~= "refusal",
        text = reply,
        stop_reason = stop,
        error = err and acp.why(err),
      }, s)
      set_status(s, "ready")
      flush(s)
    end
  )
end

flush = function(s)
  if s.status ~= "ready" or not (s.conn and s.conn:running()) then
    return
  end
  local item = table.remove(s.queue, 1)
  if item then
    prompt(s, item)
  end
end

-- send sends text to the current project's agent (or opts.root's, or the
-- session under opts.key), starting it when it isn't running; it's queued
-- while a turn is under way. opts.context lists files, opts.selection is
-- { path, first, last, text }
function M.send(text, opts)
  opts = opts or {}
  local s = opts.key and sessions[opts.key] or M.session(opts.root)
  if not (s.conn and s.conn:running()) and s.status ~= "starting" then
    start(s)
  end
  -- a backend that can't start takes nothing: a queued message would go out
  -- whenever it next starts, long after it mattered
  if s.status == "missing" or s.status == "error" then
    return false
  end
  -- a selection shared since the last message goes with this one
  if s.shared and not opts.selection then
    opts = vim.tbl_extend("force", opts, { selection = s.shared })
    s.shared = nil
  end
  table.insert(s.queue, { text = text, opts = opts })
  s.last_used = now()
  flush(s)
  return true
end

-- interrupt stops the turn in progress, keeping the session; an open
-- permission question is answered cancelled
function M.interrupt(root)
  local s = sessions[lookup(root)]
  if not (s and s.conn and s.id) then
    return false
  end
  for answer, withdraw in pairs(s.asking or {}) do
    answer(nil)
    if type(withdraw) == "function" then
      withdraw()
    end
  end
  s.queue = {}
  s.conn:notify("session/cancel", { sessionId = s.id })
  return true
end

-- status is the session's status, "stopped" when there's none
function M.status(root)
  local s = sessions[lookup(root)]
  return s and s.status or "stopped"
end

-- stop ends the session's agent; the next use starts it again
function M.stop(root)
  local s = sessions[lookup(root)]
  if s and s.conn then
    s.stopping = true
    s.conn:close()
  end
end

-- restart starts the agent again, on the same session when it can load it
function M.restart(root)
  local key = lookup(root)
  local s = sessions[key]
  if not s then
    return M.session(root)
  end
  if s.conn then
    local old = s.conn
    s.conn = nil
    s.stopping = true
    old:close()
  end
  start(s)
  return s
end

-- drop stops a session and forgets it, so the next one under its key starts fresh
function M.drop(key)
  local s = sessions[key]
  if s then
    M.stop(key)
    sessions[key] = nil
  end
end

function M.get(id)
  return sessions[lookup(id)]
end

function M.list()
  local out = {}
  for _, s in pairs(sessions) do
    table.insert(out, s)
  end
  return out
end

function M.stop_all()
  for key in pairs(sessions) do
    M.stop(key)
  end
end

-- reap stops agents idle for longer than minutes (D49); a turn in progress
-- isn't idle, however long it takes
function M.reap(minutes, at)
  at = at or now()
  for key, s in pairs(sessions) do
    if s.conn and s.status ~= "busy" and at - (s.last_used or at) > minutes * 60000 then
      M.stop(key)
    end
  end
end

-- statusline is the agent's part of the status line: nothing until there's a
-- session for this project
function M.statusline()
  local s = sessions[root_of()]
  if not s then
    return ""
  end
  local marks = { starting = "…", ready = "●", busy = "◐", stopped = "○", missing = "✗", error = "✗" }
  return ("%s %s"):format(s.backend or M.backend(), marks[s.status] or s.status)
end

-- share points the agent at lines first..last (1-based) of path (D131):
-- typed into the backend's TUI when it's running, without pressing Enter,
-- else sent with the next message as its selection
function M.share(path, first, last)
  local rel = vim.fn.fnamemodify(path, ":.")
  local where = first == last and ("%s line %d"):format(rel, first) or ("%s lines %d-%d"):format(rel, first, last)
  local term = require("bud.ai.term")
  if term.running() then
    term.send(where .. " ", { submit = false })
  else
    local s = M.session()
    local lines = vim.fn.bufnr(path) > 0 and vim.api.nvim_buf_get_lines(vim.fn.bufnr(path), first - 1, last, false)
      or vim.list_slice(vim.fn.readfile(path), first, last)
    s.shared = { path = path, first = first, last = last, text = table.concat(lines, "\n") }
  end
  vim.notify(("bud: shared %s with %s"):format(where, M.backend()))
end

-- autostart says whether a project's agent starts when it opens
-- (ai.agent.autostart); off, it starts on the first use
function M.autostart()
  return cfg().get("ai.agent.autostart", true) ~= false
end

-- setup starts the agent for how bud was opened, like bud.ai.claude's (the
-- 4.2 table): a project dir, or files from their root, and nothing for bare
-- bud; it adds the status line and the idle reap
local idle_timer
-- options lists the session's settings the backend offers (ACP config
-- options: OpenClaw's thought_level, reasoning_level, fast_mode and the like)
function M.options(root)
  local s = M.get(root)
  return s and s.options or {}
end

-- set_option sets one of them; cb(err) when the backend has answered
function M.set_option(root, id, value, cb)
  local s = M.get(root)
  local opt
  for _, o in ipairs(s and s.options or {}) do
    if o.id == id then
      opt = o
    end
  end
  local function fail(why)
    if cb then
      cb(why)
    else
      vim.notify("bud: " .. why, vim.log.levels.WARN)
    end
  end
  if not (s and s.conn and s.id) then
    return fail("no session to set it on")
  elseif not opt then
    return fail(("%s offers no setting %q"):format(s.backend or M.backend(), id))
  end
  local params = { sessionId = s.id, configId = id }
  if opt.type == "boolean" then
    params.type, params.value = "boolean", value == true or value == "true" or value == "on"
  else
    params.value = tostring(value)
  end
  s.conn:request("session/set_config_option", params, function(err, res)
    if not err and res and res.configOptions then
      s.options = res.configOptions
    end
    if cb then
      cb(err and acp.why(err) or nil)
    elseif err then
      vim.notify("bud: " .. acp.why(err), vim.log.levels.WARN)
    else
      vim.notify(("bud: %s is %s"):format(id, tostring(params.value)))
    end
  end)
end

-- pick_option is :BudAgent option with no arguments: pick a setting, then
-- its value
local function pick_option(root)
  local opts = M.options(root)
  if #opts == 0 then
    return vim.notify(("bud: %s offers no settings"):format(M.backend()))
  end
  vim.ui.select(opts, {
    prompt = "Setting",
    format_item = function(o)
      return ("%s (%s)"):format(o.name or o.id, tostring(o.currentValue))
    end,
  }, function(o)
    if not o then
      return
    end
    local values = o.type == "boolean" and { { value = "true" }, { value = "false" } } or (o.options or {})
    vim.ui.select(values, {
      prompt = o.name or o.id,
      format_item = function(v)
        return v.name or v.value
      end,
    }, function(v)
      if v then
        M.set_option(root, o.id, v.value)
      end
    end)
  end)
end

-- describe is :BudAgent's one-line status of root's session
function M.describe(root)
  local s = M.get(root)
  if not s then
    return ("%s: no session for this project yet"):format(M.backend())
  end
  local parts = { ("%s: %s"):format(s.backend or M.backend(), s.status) }
  if s.why then
    table.insert(parts, "(" .. s.why .. ")")
  end
  if s.agent_info and s.agent_info.name then
    table.insert(parts, ("· %s %s"):format(s.agent_info.name, s.agent_info.version or ""))
  end
  if s.id then
    table.insert(parts, "· session " .. s.id)
  end
  return table.concat(parts, " ")
end

-- command is :BudAgent [status|restart|stop|option [id value]]
function M.command_line(args)
  local words = vim.split(vim.trim(args or ""), "%s+", { trimempty = true })
  local sub = words[1] or "status"
  if sub == "option" then
    if words[2] and words[3] then
      return M.set_option(nil, words[2], words[3])
    end
    return pick_option()
  elseif sub == "restart" then
    M.restart()
  elseif sub == "stop" then
    M.stop()
  elseif sub ~= "status" then
    return vim.notify("bud: :BudAgent takes status, restart, stop or option", vim.log.levels.WARN)
  end
  vim.notify(M.describe())
end

function M.setup()
  require("bud.statusline").add(M.statusline)
  vim.api.nvim_create_user_command("BudAgent", function(o)
    M.command_line(o.args)
  end, {
    nargs = "?",
    desc = "The AI backend's session: its status, restart or stop it, or set an option",
    complete = function()
      return { "status", "restart", "stop", "option" }
    end,
  })
  local group = vim.api.nvim_create_augroup("bud.ai.agent", { clear = true })
  vim.api.nvim_create_autocmd("VimLeavePre", { group = group, desc = "Stop the AI agents", callback = M.stop_all })
  local minutes = tonumber(cfg().get("ai.agent.idle_minutes", 30)) or 30
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
  if M.autostart() and (vim.env.BUD_OPEN == "project" or vim.fn.argc() > 0) then
    M.session()
  end
end

-- _reset forgets every session and listener, for tests
function M._reset()
  for _, s in pairs(sessions) do
    if s.conn then
      s.stopping = true
      s.conn:close()
    end
  end
  sessions, listeners = {}, {}
end

return M
