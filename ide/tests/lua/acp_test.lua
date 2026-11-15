-- Run with: nvim --clean --headless -l tests/lua/acp_test.lua (just lua-test)
-- bud.ai.acp against the fake ACP agent (tests/lua/lib/fake_acp.lua): a
-- prompt, a permission, a file write, a cancel, an error and a crash

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(10000, cond, 10) then
    error("timed out waiting for " .. what, 2)
  end
end

local acp = require("bud.ai.acp")
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
local log = tmp .. "/acp.log"
local fake = { vim.v.progpath, "--clean", "--headless", "-l", vim.fn.getcwd() .. "/tests/lua/lib/fake_acp.lua" }

-- received is what the fake was sent, decoded
local function received()
  local out = {}
  for _, line in ipairs(vim.fn.filereadable(log) == 1 and vim.fn.readfile(log) or {}) do
    table.insert(out, vim.json.decode(line))
  end
  return out
end

-- call is a request that waits for its answer
local function call(conn, method, params)
  local done, err, result = false, nil, nil
  conn:request(method, params, function(e, r)
    done, err, result = true, e, r
  end)
  wait(function()
    return done
  end, method)
  return err, result
end

-- not installed
local conn, why = acp.connect({ "no-such-agent-bud-test" })
eq(conn, nil, "no connection without the binary")
assert(why:find("no-such-agent-bud-test isn't installed", 1, true), why)

local updates, requests, exited = {}, {}, nil
local answer = {} -- method -> function(params) -> result, err
conn = assert(acp.connect(fake, {
  cwd = tmp,
  env = { FAKE_ACP_LOG = log },
  on_notification = function(method, params)
    if method == "session/update" then
      table.insert(updates, params.update)
    end
  end,
  on_request = function(method, params, respond)
    table.insert(requests, { method = method, params = params })
    local fn = answer[method]
    if fn then
      respond(fn(params))
    else
      respond(nil, { code = -32601, message = "no" })
    end
  end,
  on_exit = function(code)
    exited = code
  end,
}))

-- initialize: v1, with bud's file capabilities
local err, init = call(conn, "initialize", {
  protocolVersion = acp.protocol_version,
  clientCapabilities = { fs = { readTextFile = true, writeTextFile = true }, terminal = false },
})
eq(err, nil, "initialize error")
eq(init.protocolVersion, 1, "protocol version")
eq(init.agentInfo.name, "fake-acp", "agent info")
eq(received()[1].params.clientCapabilities.fs, { readTextFile = true, writeTextFile = true }, "capabilities sent")

-- a new session; mcpServers goes as an empty array, not an object
local _, new = call(conn, "session/new", { cwd = tmp, mcpServers = {} })
eq(new.sessionId, "sess-1", "session id")
local raw = vim.fn.readfile(log)
assert(raw[#raw]:find('"mcpServers":[]', 1, true), raw[#raw])
local sid = new.sessionId

local function prompt(text, extra)
  local blocks = { { type = "text", text = text } }
  vim.list_extend(blocks, extra or {})
  updates = {}
  return call(conn, "session/prompt", { sessionId = sid, prompt = blocks })
end

-- a reply in two chunks
local _, res = prompt("hello")
eq(res.stopReason, "end_turn", "hello stops at end_turn")
local text = ""
for _, u in ipairs(updates) do
  if u.sessionUpdate == "agent_message_chunk" then
    text = text .. acp.text(u.content)
  end
end
eq(text, "Hello from the fake.", "the chunks make the reply")

-- context blocks reach the agent as they were sent
_, res = prompt("echo", {
  { type = "resource_link", uri = "file:///x.lua", name = "x.lua" },
  { type = "resource", resource = { uri = "file:///y.lua", text = "sel", mimeType = "text/plain" } },
})
eq(acp.text(updates[1].content), "text:echo|link:file:///x.lua|resource:file:///y.lua:sel", "blocks")

-- an edit: the tool call carries its diff, permission is asked, and an allow
-- writes through the client
local path = tmp .. "/file.txt"
local written
answer["session/request_permission"] = function()
  return { outcome = { outcome = "selected", optionId = "allow" } }
end
answer["fs/write_text_file"] = function(params)
  written = params
  return nil
end
_, res = prompt("edit " .. path)
eq(res.stopReason, "end_turn", "edit turn ends")
eq(updates[1].sessionUpdate, "tool_call", "a tool call first")
eq(acp.diffs(updates[1]), { { path = path, old = "old\n", new = "edited\n" } }, "its diff")
eq(requests[1].method, "session/request_permission", "permission asked")
eq(#requests[1].params.options, 3, "with its options")
eq(written, { sessionId = sid, path = path, content = "edited\n" }, "written through the client")
local answered = received()
local perm_answer
for _, m in ipairs(answered) do
  if m.id == 1001 then
    perm_answer = m
  end
end
eq(perm_answer.result.outcome, { outcome = "selected", optionId = "allow" }, "the answer the fake got")
eq(acp.text(updates[#updates].content), "Edited.", "and it says so")

-- a rejection leaves the file alone
requests, written = {}, nil
answer["session/request_permission"] = function()
  return { outcome = { outcome = "selected", optionId = "reject" } }
end
prompt("edit " .. path)
eq(written, nil, "nothing written after a reject")
eq(acp.text(updates[#updates].content), "Not edited.", "says it didn't")

-- a read the client answers with an error
answer["fs/read_text_file"] = nil
prompt("read " .. path)
eq(acp.text(updates[#updates].content), "Read failed: no", "the client's error reaches the agent")

-- cancel: the prompt answers cancelled
updates = {}
local done
conn:request("session/prompt", { sessionId = sid, prompt = { { type = "text", text = "slow" } } }, function(e, r)
  done = { e, r }
end)
wait(function()
  return #updates > 0
end, "the slow turn to start")
conn:notify("session/cancel", { sessionId = sid })
wait(function()
  return done
end, "the cancelled turn")
eq(done[2].stopReason, "cancelled", "cancelled")

-- an error answer
err = prompt("fail")
eq(err.message, "scripted failure", "the agent's error")

-- a crash fails what's waiting and reports the exit
done = nil
conn:request("session/prompt", { sessionId = sid, prompt = { { type = "text", text = "crash" } } }, function(e)
  done = e
end)
wait(function()
  return exited ~= nil and done ~= nil
end, "the crash")
eq(exited, 3, "exit code")
assert(done.message:find("exit 3", 1, true), done.message)
eq(conn:running(), false, "not running")
local failed
conn:request("session/prompt", {}, function(e)
  failed = e
end)
eq(failed.message, "the agent isn't running", "requests after the exit fail at once")

-- an error's detail (OpenClaw's data.details) joins its message
eq(
  acp.why({ message = "Internal error", data = { details = "No session found" } }),
  "Internal error: No session found",
  "detail"
)
eq(acp.why({ message = "Invalid params", data = { sessionId = {} } }), "Invalid params", "no detail")

vim.fn.delete(tmp, "rf")
print("acp_test: ok")
