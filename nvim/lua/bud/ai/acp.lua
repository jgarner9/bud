-- The Agent Client Protocol (ACP v1) over a job's stdin and stdout (D131).
-- Every backend bud talks to (OpenClaw through `openclaw acp`, Claude Code
-- through claude-agent-acp, Codex through codex-acp, OpenCode's `opencode
-- acp`) is an ACP agent: JSON-RPC 2.0, one message per line, with requests
-- going both ways. This module is only the protocol; bud.ai.agent is the
-- session on top of it. See docs/internals/acp.md.
--
--   local acp = require("bud.ai.acp")
--   local conn = acp.connect({ "openclaw", "acp" }, {
--     cwd = root,
--     on_notification = function(method, params) end,   -- session/update
--     on_request = function(method, params, respond) end, -- respond(result) or respond(nil, err)
--     on_exit = function(code, stderr) end,
--   })
--   conn:request("initialize", { protocolVersion = 1 }, function(err, result) end)
--   conn:notify("session/cancel", { sessionId = id })
--   conn:close()

local M = {}

-- the ACP version bud speaks
M.protocol_version = 1

-- how much of the agent's stderr to keep, for saying why it stopped
local stderr_keep = 4000

local Conn = {}
Conn.__index = Conn

-- decode turns a JSON line into a table, nulls as nil
local function decode(line)
  local ok, msg = pcall(vim.json.decode, line, { luanil = { object = true, array = true } })
  if ok and type(msg) == "table" then
    return msg
  end
end

function Conn:_send(msg)
  if not self.job then
    return false
  end
  msg.jsonrpc = "2.0"
  return vim.fn.chansend(self.job, vim.json.encode(msg) .. "\n") > 0
end

-- request sends method with params and calls cb(err, result) with the
-- answer. err is { code, message, data } from the agent, or { message } when
-- the agent went away first. It returns the request's id
function Conn:request(method, params, cb)
  self.next_id = self.next_id + 1
  local id = self.next_id
  self.pending[id] = { method = method, cb = cb }
  if not self:_send({ id = id, method = method, params = params or vim.empty_dict() }) then
    self.pending[id] = nil
    if cb then
      cb({ message = "the agent isn't running" })
    end
  end
  return id
end

-- notify sends method with params, expecting no answer
function Conn:notify(method, params)
  return self:_send({ method = method, params = params or vim.empty_dict() })
end

-- running says whether the agent's process is still there
function Conn:running()
  return self.job ~= nil
end

-- close stops the agent
function Conn:close()
  if self.job then
    pcall(vim.fn.jobstop, self.job)
  end
end

-- _dispatch handles one message from the agent
function Conn:_dispatch(msg)
  if msg.method and msg.id ~= nil then
    -- the agent's request: permissions, file reads and writes
    local answered = false
    local function respond(result, err)
      if answered then
        return
      end
      answered = true
      if err then
        self:_send({ id = msg.id, error = { code = err.code or -32603, message = err.message or tostring(err) } })
      else
        self:_send({ id = msg.id, result = result == nil and vim.NIL or result })
      end
    end
    if self.on_request then
      local ok, e = pcall(self.on_request, msg.method, msg.params or {}, respond)
      if not ok then
        respond(nil, { message = tostring(e) })
      end
    else
      respond(nil, { code = -32601, message = "method not found: " .. msg.method })
    end
  elseif msg.method then
    if self.on_notification then
      local ok, e = pcall(self.on_notification, msg.method, msg.params or {})
      if not ok then
        vim.notify("bud.ai.acp: " .. msg.method .. ": " .. tostring(e), vim.log.levels.WARN)
      end
    end
  elseif msg.id ~= nil then
    local p = self.pending[msg.id]
    self.pending[msg.id] = nil
    if p and p.cb then
      local ok, e = pcall(p.cb, msg.error, msg.result)
      if not ok then
        vim.notify("bud.ai.acp: " .. p.method .. ": " .. tostring(e), vim.log.levels.WARN)
      end
    end
  end
end

-- _feed splits the agent's output into lines, holding a partial last line
-- back until the rest arrives
function Conn:_feed(data)
  data[1] = self.partial .. data[1]
  self.partial = table.remove(data)
  for _, line in ipairs(data) do
    line = line:gsub("\r$", "")
    if line ~= "" then
      local msg = decode(line)
      if msg then
        self:_dispatch(msg)
      end
    end
  end
end

-- _exited fails what's still waiting and tells on_exit
function Conn:_exited(code)
  self.job = nil
  if self.partial ~= "" then
    local msg = decode(self.partial)
    self.partial = ""
    if msg then
      self:_dispatch(msg)
    end
  end
  local pending = self.pending
  self.pending = {}
  local why = ("the agent stopped (exit %d)"):format(code)
  for _, p in pairs(pending) do
    if p.cb then
      pcall(p.cb, { message = why })
    end
  end
  if self.on_exit then
    self.on_exit(code, self.stderr)
  end
end

-- connect starts argv as an ACP agent. opts: cwd, env, on_notification,
-- on_request, on_exit. It returns the connection, or nil and why it
-- couldn't start
function M.connect(argv, opts)
  opts = opts or {}
  local conn = setmetatable({
    next_id = 0,
    pending = {},
    partial = "",
    stderr = "",
    on_notification = opts.on_notification,
    on_request = opts.on_request,
    on_exit = opts.on_exit,
  }, Conn)
  if type(argv) ~= "table" or not argv[1] or vim.fn.executable(argv[1]) ~= 1 then
    return nil, ("%s isn't installed (or isn't on PATH)"):format(type(argv) == "table" and argv[1] or "the agent")
  end
  local ok, job = pcall(vim.fn.jobstart, argv, {
    cwd = opts.cwd,
    env = opts.env,
    on_stdout = function(_, data)
      conn:_feed(data)
    end,
    on_stderr = function(_, data)
      conn.stderr = (conn.stderr .. table.concat(data, "\n")):sub(-stderr_keep)
    end,
    on_exit = function(_, code)
      conn:_exited(code)
    end,
  })
  if not ok or job <= 0 then
    return nil, ("couldn't start %s: %s"):format(argv[1], ok and job or tostring(job))
  end
  conn.job = job
  return conn
end

-- text pulls the text out of a content block or a list of them (agent
-- messages, tool call content)
function M.text(content)
  if type(content) ~= "table" then
    return ""
  end
  if content.type == "text" then
    return content.text or ""
  elseif content.type == "content" then
    return M.text(content.content)
  elseif content.type == "resource" and content.resource then
    return content.resource.text or ""
  elseif content[1] then
    local parts = {}
    for _, c in ipairs(content) do
      table.insert(parts, M.text(c))
    end
    return table.concat(parts, "")
  end
  return ""
end

-- why is an error's message with the detail an agent put in its data
-- (OpenClaw answers "Internal error" with the reason in data.details)
function M.why(err)
  if type(err) ~= "table" then
    return tostring(err)
  end
  local msg = err.message or "failed"
  local data = err.data
  local detail = type(data) == "table" and (data.details or data.message) or (type(data) == "string" and data or nil)
  if type(detail) == "string" and detail ~= "" and detail ~= msg then
    msg = msg .. ": " .. detail
  end
  return msg
end

-- diffs lists the diff items in a tool call's content: { path, old, new }
function M.diffs(tool_call)
  local found = {}
  for _, c in ipairs(type(tool_call) == "table" and tool_call.content or {}) do
    if c.type == "diff" then
      table.insert(found, { path = c.path, old = c.oldText, new = c.newText })
    end
  end
  return found
end

return M
