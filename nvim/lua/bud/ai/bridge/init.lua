-- The IDE bridge (BASE.md 4.2, JGA-81): the WebSocket protocol Claude Code
-- uses for editor integrations, the one the VS Code extension and
-- claudecode.nvim speak. It lets Claude Code see your open files, selection
-- and diagnostics, and shows its proposed edits as diffs you accept or reject
-- (bud.ai.bridge.tools).
--
-- One bridge per Neovim, started the first time a session needs it. Claude
-- Code finds it from the lock file in ~/.claude/ide/<port>.lock, which names
-- the port, the workspace folders and an auth token, and from the
-- CLAUDE_CODE_SSE_PORT bud sets in the session's environment.

local ws = require("bud.ai.bridge.websocket")
local tools = require("bud.ai.bridge.tools")

local M = {}

local server, token, lock
local folders = {}
local clients = {}

-- swappable in tests: where Claude Code looks for lock files
M.lock_dir = function()
  local dir = vim.env.CLAUDE_CONFIG_DIR
  if not dir or dir == "" then
    dir = vim.fn.expand("~/.claude")
  end
  return dir .. "/ide"
end

local function random_token()
  local fd = io.open("/dev/urandom", "rb")
  local bytes = fd and fd:read(16)
  if fd then
    fd:close()
  end
  if not bytes or #bytes < 16 then
    error("bud.ai.bridge: can't read /dev/urandom for the auth token")
  end
  return (bytes:gsub(".", function(c)
    return ("%02x"):format(c:byte())
  end))
end

-- folders lists the workspace folders: every session's root, current dir first
function M.folders()
  local out, seen = {}, {}
  for _, path in ipairs(vim.list_extend({ vim.fn.getcwd() }, folders)) do
    path = vim.uv.fs_realpath(path) or path
    if not seen[path] then
      seen[path] = true
      table.insert(out, path)
    end
  end
  return out
end

local function write_lock()
  if not server then
    return
  end
  vim.fn.mkdir(M.lock_dir(), "p", "0o700")
  lock = M.lock_dir() .. "/" .. server.port .. ".lock"
  local data = vim.json.encode({
    pid = vim.fn.getpid(),
    workspaceFolders = M.folders(),
    ideName = "Neovim",
    transport = "ws",
    authToken = token,
  })
  local fd = vim.uv.fs_open(lock, "w", tonumber("600", 8))
  if fd then
    vim.uv.fs_write(fd, data)
    vim.uv.fs_close(fd)
  end
end

local function send(client, msg)
  msg.jsonrpc = "2.0"
  server.send(client, vim.json.encode(msg))
end

-- notify sends a notification to every connected Claude Code
function M.notify(method, params)
  if not server then
    return
  end
  for client in pairs(clients) do
    send(client, { method = method, params = params })
  end
end

-- approve is the tool headless sessions send permission questions to
-- (--permission-prompt-tool). Only HTTP clients see it: the TUI asks you itself
local approve = {
  description = "Answers Claude Code's permission prompts by asking the user in Neovim",
  schema = {
    type = "object",
    properties = { tool_name = { type = "string" }, input = { type = "object" }, tool_use_id = { type = "string" } },
    required = { "tool_name", "input" },
  },
  run = function(args, respond, ctx)
    require("bud.ai.permissions").request({
      tool = args.tool_name,
      input = args.input or {},
      id = args.tool_use_id,
      root = ctx and ctx.root,
      respond = function(result)
        respond(vim.json.encode(result))
      end,
    })
  end,
}

local function tool(name, ctx)
  if name == "approve" then
    return ctx and ctx.http and approve or nil
  end
  return tools.tools[name]
end

-- handle answers one JSON-RPC message from a client; reply(msg) sends. ctx
-- says how it came: { http = true, root = the session's } for headless ones
function M.handle(msg, reply, ctx)
  local id, method, params = msg.id, msg.method, msg.params or {}
  if method == nil then
    return -- a response to something we sent
  end
  local function result(r)
    if id ~= nil then
      reply({ id = id, result = r })
    end
  end
  local function err(code, message)
    if id ~= nil then
      reply({ id = id, error = { code = code, message = message } })
    end
  end

  if method == "initialize" then
    result({
      protocolVersion = params.protocolVersion or "2025-03-26",
      capabilities = { tools = { listChanged = true }, logging = vim.empty_dict() },
      serverInfo = { name = "bud", version = "1" },
    })
  elseif method:match("^notifications/") then
    return
  elseif method == "ping" then
    result(vim.empty_dict())
  elseif method == "tools/list" then
    local list = {}
    for name, t in pairs(tools.tools) do
      table.insert(list, { name = name, description = t.description, inputSchema = t.schema })
    end
    if ctx and ctx.http then
      table.insert(list, { name = "approve", description = approve.description, inputSchema = approve.schema })
    end
    table.sort(list, function(a, b)
      return a.name < b.name
    end)
    result({ tools = list })
  elseif method == "tools/call" then
    local t = tool(params.name, ctx)
    if not t then
      return err(-32601, "no tool " .. tostring(params.name))
    end
    local ok, run_err = pcall(t.run, params.arguments or {}, function(answer)
      result({ content = tools.content(answer) })
    end, ctx)
    if not ok then
      result({ content = tools.content(tostring(run_err)), isError = true })
    end
  elseif method == "prompts/list" then
    result({ prompts = {} })
  elseif method == "resources/list" then
    result({ resources = {} })
  else
    err(-32601, "method not found: " .. method)
  end
end

-- selection_changed tells Claude Code what's selected, at most every 100 ms
local pending
local function selection_changed()
  if pending or not next(clients) then
    return
  end
  pending = true
  vim.defer_fn(function()
    pending = false
    local sel = tools.selection()
    if not sel then
      return
    end
    local key = vim.json.encode(sel)
    if key == M._last_sent then
      return
    end
    M._last_sent = key
    tools.latest = sel
    M.notify("selection_changed", sel)
  end, 100)
end

-- start brings the bridge up, once. It returns false (and why) when it can't
function M.start()
  if server then
    return true
  end
  local ok, err = pcall(function()
    token = random_token()
    server = ws.serve({
      token = token,
      on_open = function(client)
        clients[client] = true
      end,
      on_close = function(client)
        clients[client] = nil
      end,
      -- MCP over streamable HTTP: one JSON-RPC message per POST, answered
      -- in the response (a notification gets 202 and no body)
      on_http = function(body, respond, path)
        local ok_decode, msg = pcall(vim.json.decode, body, { luanil = { object = true, array = true } })
        if not ok_decode or type(msg) ~= "table" then
          return respond("400 Bad Request")
        end
        -- the session's root rides in the URL, for its permission prompts
        local ctx = { http = true, root = path and vim.uri_decode(path:match("[?&]root=([^&]+)") or "") or nil }
        if ctx.root == "" then
          ctx.root = nil
        end
        if msg.id == nil then
          M.handle(msg, function() end, ctx)
          return respond("202 Accepted")
        end
        M.handle(msg, function(reply)
          reply.jsonrpc = "2.0"
          respond("200 OK", vim.json.encode(reply))
        end, ctx)
      end,
      on_message = function(client, text)
        local ok_decode, msg = pcall(vim.json.decode, text, { luanil = { object = true, array = true } })
        if not ok_decode or type(msg) ~= "table" then
          return
        end
        M.handle(msg, function(reply)
          send(client, reply)
        end)
      end,
    })
  end)
  if not ok then
    server = nil
    return false, tostring(err)
  end
  write_lock()

  local group = vim.api.nvim_create_augroup("bud.ai.bridge", { clear = true })
  vim.api.nvim_create_autocmd({ "CursorMoved", "ModeChanged", "BufEnter" }, {
    group = group,
    callback = selection_changed,
  })
  vim.api.nvim_create_autocmd("DirChanged", {
    group = group,
    callback = write_lock,
  })
  vim.api.nvim_create_autocmd("VimLeavePre", {
    group = group,
    callback = M.stop,
  })
  return true
end

-- add_folder adds a session's root to the workspace folders
function M.add_folder(root)
  if not vim.tbl_contains(folders, root) then
    table.insert(folders, root)
    write_lock()
  end
end

-- env is what a Claude Code process needs in its environment to connect, or
-- {} when the bridge isn't up
function M.env()
  if not server then
    return {}
  end
  return { CLAUDE_CODE_SSE_PORT = tostring(server.port), ENABLE_IDE_INTEGRATION = "true" }
end

-- read_only are the tools that only look, which a headless session may call
-- without asking; everything that changes the editor still asks (JGA-82)
M.read_only = {
  "getCurrentSelection",
  "getLatestSelection",
  "getOpenEditors",
  "getWorkspaceFolders",
  "getDiagnostics",
  "checkDocumentDirty",
}

-- server_name is what Claude Code calls the bridge in a headless session, so
-- its tools are mcp__bud-ide__<tool>
M.server_name = "bud-ide"

-- headless_args are the flags that give a headless (-p) Claude Code in root
-- the bridge's tools: it ignores the IDE lock file, so it gets them as an MCP
-- server over HTTP on the same port, with the same token
function M.headless_args(root)
  if not server then
    return {}
  end
  local cfg = {
    mcpServers = {
      [M.server_name] = {
        type = "http",
        url = ("http://127.0.0.1:%d/mcp?root=%s"):format(
          server.port,
          vim.uri_encode(root or vim.fn.getcwd(), "rfc3986")
        ),
        headers = { ["x-claude-code-ide-authorization"] = token },
      },
    },
  }
  local allowed = vim.tbl_map(function(t)
    return ("mcp__%s__%s"):format(M.server_name, t)
  end, M.read_only)
  return {
    "--mcp-config",
    vim.json.encode(cfg),
    "--allowedTools",
    table.concat(allowed, ","),
    -- permission questions come to Neovim (bud.ai.permissions); this asks,
    -- it doesn't change what's allowed
    "--permission-prompt-tool",
    ("mcp__%s__approve"):format(M.server_name),
  }
end

-- mention shares a range of a file with Claude Code (lines 0-based)
function M.mention(path, first, last)
  M.notify("at_mentioned", { filePath = path, lineStart = first, lineEnd = last })
end

-- connected is how many Claude Code processes are connected
function M.connected()
  return vim.tbl_count(clients)
end

-- port and lock_file, for tests and :BudClaude status
function M.port()
  return server and server.port
end

function M.lock_file()
  return lock
end

function M.token()
  return token
end

function M.stop()
  if lock then
    os.remove(lock)
    lock = nil
  end
  if server then
    server.close()
    server = nil
  end
  clients = {}
end

return M
