-- A fake ACP agent for tests (D131): run as
--   nvim --clean --headless -l tests/lua/lib/fake_acp.lua
-- It speaks ACP v1 on stdin and stdout, one JSON message per line, and plays
-- a scripted turn picked by the prompt's first word:
--
--   hello        two message chunks, then end_turn
--   echo         a message listing the prompt's blocks (text, links, resources)
--   edit <path>  a tool call with a diff, asks permission; when allowed it
--                writes "edited\n" through the client's fs/write_text_file
--   read <path>  reads the file through the client's fs/read_text_file
--   slow         one chunk, then waits for session/cancel and stops: cancelled
--   fail         answers the prompt with a JSON-RPC error
--   crash        exits with status 3 mid-turn
--
-- Everything it receives is appended to $FAKE_ACP_LOG, one JSON line each.
-- FAKE_ACP_LOAD=1 advertises loadSession.

local log = os.getenv("FAKE_ACP_LOG")

local function record(line)
  if log then
    local f = io.open(log, "a")
    if f then
      f:write(line, "\n")
      f:close()
    end
  end
end

local function send(msg)
  msg.jsonrpc = "2.0"
  io.write(vim.json.encode(msg), "\n")
  io.flush()
end

local function read()
  local line = io.read("l")
  if not line then
    os.exit(0)
  end
  record(line)
  return vim.json.decode(line, { luanil = { object = true, array = true } })
end

local next_id = 1000
local queued = {} -- messages read while waiting for an answer

-- ask sends the client a request and waits for its answer, keeping whatever
-- else arrives meanwhile for the main loop
local function ask(method, params)
  next_id = next_id + 1
  local id = next_id
  send({ id = id, method = method, params = params })
  while true do
    local msg = read()
    if msg.id == id and not msg.method then
      return msg.error, msg.result
    end
    table.insert(queued, msg)
  end
end

local function update(session, u)
  send({ method = "session/update", params = { sessionId = session, update = u } })
end

local function say(session, text)
  update(session, { sessionUpdate = "agent_message_chunk", content = { type = "text", text = text } })
end

local sessions = 0

local function prompt_text(blocks)
  for _, b in ipairs(blocks or {}) do
    if b.type == "text" then
      return b.text
    end
  end
  return ""
end

local function turn(msg)
  local p = msg.params
  local sid = p.sessionId
  local text = prompt_text(p.prompt)
  local word, rest = text:match("^(%S+)%s*(.*)$")
  word = word or ""

  if word == "hello" then
    say(sid, "Hello ")
    say(sid, "from the fake.")
    return { stopReason = "end_turn" }
  elseif word == "echo" then
    local parts = {}
    for _, b in ipairs(p.prompt) do
      if b.type == "text" then
        table.insert(parts, "text:" .. b.text)
      elseif b.type == "resource_link" then
        table.insert(parts, "link:" .. b.uri)
      elseif b.type == "resource" then
        table.insert(parts, "resource:" .. b.resource.uri .. ":" .. (b.resource.text or ""))
      end
    end
    say(sid, table.concat(parts, "|"))
    return { stopReason = "end_turn" }
  elseif word == "edit" then
    local call = {
      toolCallId = "call-1",
      title = "Edit " .. rest,
      kind = "edit",
      status = "pending",
      locations = { { path = rest } },
      content = { { type = "diff", path = rest, oldText = "old\n", newText = "edited\n" } },
      rawInput = { path = rest },
    }
    update(sid, vim.tbl_extend("force", { sessionUpdate = "tool_call" }, call))
    local err, res = ask("session/request_permission", {
      sessionId = sid,
      toolCall = call,
      options = {
        { optionId = "allow", name = "Allow", kind = "allow_once" },
        { optionId = "always", name = "Always allow", kind = "allow_always" },
        { optionId = "reject", name = "Reject", kind = "reject_once" },
      },
    })
    local outcome = res and res.outcome or {}
    if err or outcome.outcome ~= "selected" or outcome.optionId == "reject" then
      update(sid, { sessionUpdate = "tool_call_update", toolCallId = "call-1", status = "failed" })
      say(sid, "Not edited.")
      return { stopReason = outcome.outcome == "cancelled" and "cancelled" or "end_turn" }
    end
    local werr = ask("fs/write_text_file", { sessionId = sid, path = rest, content = "edited\n" })
    update(sid, {
      sessionUpdate = "tool_call_update",
      toolCallId = "call-1",
      status = werr and "failed" or "completed",
    })
    say(sid, werr and ("Write failed: " .. werr.message) or "Edited.")
    return { stopReason = "end_turn" }
  elseif word == "read" then
    local err, res = ask("fs/read_text_file", { sessionId = sid, path = rest })
    say(sid, err and ("Read failed: " .. err.message) or ("Read: " .. res.content))
    return { stopReason = "end_turn" }
  elseif word == "slow" then
    say(sid, "Working")
    while true do
      local m = table.remove(queued, 1) or read()
      if m.method == "session/cancel" and m.params.sessionId == sid then
        return { stopReason = "cancelled" }
      end
    end
  elseif word == "fail" then
    return nil, { code = -32000, message = "scripted failure" }
  elseif word == "crash" then
    say(sid, "about to crash")
    os.exit(3)
  end
  say(sid, "I don't know " .. word)
  return { stopReason = "end_turn" }
end

while true do
  local msg = table.remove(queued, 1) or read()
  if msg.method == "initialize" then
    send({
      id = msg.id,
      result = {
        protocolVersion = 1,
        agentCapabilities = {
          loadSession = os.getenv("FAKE_ACP_LOAD") == "1",
          promptCapabilities = { embeddedContext = true, image = false, audio = false },
        },
        agentInfo = { name = "fake-acp", version = "1.0.0" },
        authMethods = {},
      },
    })
  elseif msg.method == "session/new" then
    sessions = sessions + 1
    send({ id = msg.id, result = { sessionId = "sess-" .. sessions } })
  elseif msg.method == "session/load" then
    update(msg.params.sessionId, {
      sessionUpdate = "user_message_chunk",
      content = { type = "text", text = "earlier question" },
    })
    send({ id = msg.id, result = vim.NIL })
  elseif msg.method == "session/prompt" then
    local res, err = turn(msg)
    send({ id = msg.id, result = res, error = err })
  elseif msg.method and msg.id ~= nil then
    send({ id = msg.id, error = { code = -32601, message = "method not found: " .. msg.method } })
  end
end
