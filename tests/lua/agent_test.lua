-- Run with: nvim --clean --headless -l tests/lua/agent_test.lua (just lua-test)
-- bud.ai.agent on the fake ACP agent: a session per root, streamed replies,
-- permissions, files through buffers, interrupt, a crash, and the backends'
-- command lines (D131-D133)

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

-- the repo, before any test changes dir
local repo = vim.fn.getcwd()
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/proj", "p")
local proj = vim.uv.fs_realpath(tmp .. "/proj")
require("bud.config").file = tmp .. "/config.json"

local agent = require("bud.ai.agent")
agent.ids_file = function()
  return tmp .. "/agent-sessions.json"
end

-- the backends' command lines
local settings = {}
local function get(key, default)
  local v = settings[key]
  if v == nil then
    return default
  end
  return v
end
eq(agent.argv("openclaw", get), { "openclaw", "acp" }, "openclaw with nothing set")
settings["ai.openclaw.url"] = "wss://home.example.ts.net:18789"
settings["ai.openclaw.token_file"] = "/secrets/gateway-token"
eq(
  agent.argv("openclaw", get),
  { "openclaw", "acp", "--url", "wss://home.example.ts.net:18789", "--token-file", "/secrets/gateway-token" },
  "openclaw with a Gateway"
)
-- no --session: a session OpenClaw's bridge makes keeps its working dir (D139)
eq(#agent.argv("openclaw", get), 6, "no session flag")
eq(agent.bridge_key("abc"), "agent:main:acp-bridge:abc", "the bridge's key")
eq(agent.bridge_key("abc", "coder"), "agent:coder:acp-bridge:abc", "with another agent")
eq(agent.argv("claude", get), { "claude-agent-acp" }, "claude")
eq(agent.argv("codex", get), { "codex-acp" }, "codex")
eq(agent.argv("opencode", get), { "opencode", "acp" }, "opencode")
local argv, why = agent.argv("custom", get)
eq(argv, nil, "custom without a command")
assert(why:find("ai.custom.command", 1, true), why)
settings["ai.custom.command"] = { "gemini", "--experimental-acp" }
eq(agent.argv("custom", get), { "gemini", "--experimental-acp" }, "custom")
argv, why = agent.argv("gpt", get)
eq(argv, nil, "an unknown backend")
assert(why:find('"gpt"', 1, true), why)
eq(agent.backend(), "openclaw", "openclaw is the default backend (D132)")

-- a backend that isn't installed: missing, with why
agent.command = function()
  return { "no-such-acp-agent-bud-test" }
end
local s = agent.session(proj)
eq(s.status, "missing", "missing")
assert(s.why:find("isn't installed", 1, true), s.why)
eq(agent.send("too early", { root = proj }), false, "a send fails")
eq(#s.queue, 0, "and isn't kept for later")
agent._reset()

-- the fake agent from here on
local log = tmp .. "/acp.log"
agent.command = function()
  return { vim.v.progpath, "--clean", "--headless", "-l", repo .. "/tests/lua/lib/fake_acp.lua" }
end
local events = {}
local function record(name)
  agent.on(name, function(data, sess)
    table.insert(events, { name = name, data = data, key = sess.key })
  end)
end
for _, name in ipairs({
  "status",
  "chunk",
  "user_chunk",
  "message",
  "tool_use",
  "tool_update",
  "permission_request",
  "done",
  "exit",
}) do
  record(name)
end
local function named(name)
  local out = {}
  for _, e in ipairs(events) do
    if e.name == name then
      table.insert(out, e.data)
    end
  end
  return out
end
local function turn_done(n)
  wait(function()
    return #named("done") >= n
  end, "turn " .. n)
end

-- a session starts and gets ready
s = agent.session(proj, { env = { FAKE_ACP_LOG = log } })
wait(function()
  return s.status == "ready"
end, "ready")
eq(s.id, "sess-1", "the agent's session id")
eq(agent.status(proj), "ready", "status by root")
eq(agent.get(proj), s, "get by root")

-- a streamed reply: chunks, then the whole message, then done
agent.send("hello", { root = proj })
turn_done(1)
eq(
  vim.tbl_map(function(c)
    return c.text
  end, named("chunk")),
  { "Hello ", "from the fake." },
  "chunks"
)
eq(named("message")[1], { role = "assistant", text = "Hello from the fake." }, "message")
eq(named("done")[1].ok, true, "done ok")
eq(named("done")[1].stop_reason, "end_turn", "stop reason")
eq(s.status, "ready", "ready again")

-- context files go as links, a selection as embedded text
agent.send("echo", {
  root = proj,
  context = { proj .. "/a.lua" },
  selection = { path = proj .. "/b.lua", first = 2, last = 3, text = "two\nthree" },
})
turn_done(2)
eq(
  named("message")[2].text,
  ("text:echo|link:%s|resource:%s#L2-3:two\nthree"):format(
    vim.uri_from_fname(proj .. "/a.lua"),
    vim.uri_from_fname(proj .. "/b.lua")
  ),
  "the prompt's blocks"
)

-- a shared selection (<leader>as) goes with the next message, once
vim.fn.writefile({ "one", "two", "three" }, proj .. "/c.lua")
vim.cmd.cd(proj)
agent.share(proj .. "/c.lua", 2, 3)
agent.send("echo", { root = proj })
turn_done(3)
eq(
  named("message")[3].text,
  ("text:echo|resource:%s#L2-3:two\nthree"):format(vim.uri_from_fname(proj .. "/c.lua")),
  "the shared lines went with it"
)
eq(s.shared, nil, "and only once")

-- two sends in a row: the second waits for the first
agent.send("hello", { root = proj })
agent.send("hello", { root = proj })
turn_done(5)
eq(#named("message"), 5, "both ran, one after the other")

-- an edit: the permission request carries the diff, and an allow writes
-- through the open buffer
local file = proj .. "/notes.txt"
vim.fn.writefile({ "old" }, file)
vim.cmd.edit(file)
local buf = vim.api.nvim_get_current_buf()
local asked
agent.permission = function(req, respond)
  asked = req
  respond("allow")
end
agent.send("edit " .. file, { root = proj })
turn_done(6)
eq(asked.tool, "Edit " .. file, "the tool's title")
eq(asked.diffs, { { path = file, old = "old\n", new = "edited\n" } }, "the diff (D134)")
eq(#asked.options, 3, "the options")
eq(named("permission_request")[1].tool, "Edit " .. file, "the event too")
eq(vim.api.nvim_buf_get_lines(buf, 0, -1, false), { "edited" }, "the buffer has the edit")
eq(vim.fn.readfile(file), { "edited" }, "and it's saved")
eq(vim.bo[buf].modified, false, "not left modified")
eq(named("tool_use")[1].kind, "edit", "tool_use kind")
eq(named("tool_update")[#named("tool_update")].status, "completed", "tool done")
eq(named("done")[6].text, "Edited.", "done.text is the answer after the last tool call")

-- a read sees the unsaved buffer
vim.api.nvim_buf_set_lines(buf, 0, -1, false, { "unsaved", "lines" })
agent.send("read " .. file, { root = proj })
turn_done(7)
eq(named("message")[#named("message")].text, "Read: unsaved\nlines\n", "unsaved changes reach the agent")

-- a file that isn't open is written on disk
local other = proj .. "/sub/new.txt"
agent.send("edit " .. other, { root = proj })
turn_done(8)
eq(vim.fn.readfile(other), { "edited" }, "written on disk, its dir made")

-- interrupt: an open permission question is answered cancelled
agent.permission = function(req, respond)
  asked = { req = req, respond = respond }
end
agent.send("edit " .. file, { root = proj })
wait(function()
  return asked.respond ~= nil
end, "the permission question")
eq(agent.interrupt(proj), true, "interrupted")
turn_done(9)
eq(named("done")[9].stop_reason, "cancelled", "cancelled")

-- interrupt a slow reply
agent.send("slow", { root = proj })
wait(function()
  return s.status == "busy" and (s.reply or "") ~= ""
end, "the slow turn")
agent.interrupt(proj)
turn_done(10)
eq(named("done")[10].stop_reason, "cancelled", "slow turn cancelled")
eq(s.status, "ready", "ready after the cancel")

-- an error from the agent
agent.send("fail", { root = proj })
turn_done(11)
eq(named("done")[11].ok, false, "not ok")
eq(named("done")[11].error, "scripted failure", "the error")

-- a crash: done not ok, exit crashed, status error with the exit code
agent.send("crash", { root = proj })
wait(function()
  return #named("exit") == 1
end, "the exit")
eq(named("exit")[1].crashed, true, "crashed")
eq(named("exit")[1].code, 3, "exit code")
wait(function()
  return s.status == "error"
end, "error status")
assert(s.why:find("exit 3", 1, true), s.why)
eq(named("done")[12].ok, false, "the crashed turn isn't ok")

-- the next send starts it again
agent.send("hello", { root = proj })
turn_done(13)
eq(named("done")[13].ok, true, "works after a restart")

-- the session id is saved per project, for the next start (D139)
eq(agent.saved_id(proj, "openclaw"), s.id, "saved")
eq(agent.saved_id(proj, "codex"), nil, "only for its backend")

-- reattached with session/load when the agent can, its history replayed
agent.stop(proj)
wait(function()
  return s.status == "stopped"
end, "stopped before the reload")
local was = s.id
s.env = { FAKE_ACP_LOG = log, FAKE_ACP_LOAD = "1" }
events = {}
agent.session(proj)
wait(function()
  return s.status == "ready"
end, "reloaded")
eq(s.id, was, "the same session")
eq(named("user_chunk")[1], { text = "earlier question" }, "the history replayed")
local loads = vim.tbl_filter(function(line)
  return line:find('"session/load"', 1, true) ~= nil
end, vim.fn.readfile(log))
eq(#loads, 1, "one session/load")

-- a session the agent no longer has: a new one, saved instead
agent.stop(proj)
wait(function()
  return s.status == "stopped"
end, "stopped again")
s.id = "gone"
agent.session(proj)
wait(function()
  return s.status == "ready"
end, "a fresh session")
eq(s.id ~= "gone", true, "a new id")
eq(agent.saved_id(proj, "openclaw"), s.id, "and it's saved")

-- keyed sessions sit beside the project's
local issue = agent.session(proj, { key = "issue:JGA-1", extra = true, env = { FAKE_ACP_LOG = log } })
wait(function()
  return issue.status == "ready"
end, "the keyed session")
eq(agent.status("issue:JGA-1"), "ready", "status by key")
agent.drop("issue:JGA-1")
eq(agent.get("issue:JGA-1") == issue, false, "dropped")

-- stop is quiet; reap stops an idle one
agent.stop(proj)
wait(function()
  return s.status == "stopped"
end, "stopped")
eq(named("exit")[#named("exit")].crashed, false, "a stop isn't a crash")
agent.session(proj)
wait(function()
  return s.status == "ready"
end, "ready again")
agent.reap(30, vim.uv.hrtime() / 1e6 + 31 * 60 * 1000)
wait(function()
  return s.status == "stopped"
end, "reaped")

-- turns recorded from a real OpenClaw Gateway through openclaw acp
-- (tests/fixtures/openclaw), replayed by the fake
local fixtures = repo .. "/tests/fixtures/openclaw/"
events = {}
agent.session(proj)
wait(function()
  return s.status == "ready"
end, "ready for the replays")
local function replay(name)
  local before = #named("done")
  agent.send("replay " .. fixtures .. name .. ".log", { root = proj })
  wait(function()
    return #named("done") > before
  end, "the " .. name .. " replay")
end

replay("hello")
eq(named("message")[#named("message")].text, "Hello from the scripted model.", "openclaw's streamed reply")

-- an edit runs without asking, and its patch comes afterwards (D134)
replay("edit")
eq(named("tool_use")[#named("tool_use")].kind, "edit", "an edit")
local upd = named("tool_update")[#named("tool_update")]
eq(upd.status, "completed", "edit completed")
assert(upd.patch and upd.patch:find("+gamma", 1, true), vim.inspect(upd.patch))
eq(upd.output, "Successfully replaced 1 block(s) in notes.txt.", "its output text")

-- exec under tools.exec.mode = "ask": a permission request with OpenClaw's options
local options
agent.permission = function(req, respond)
  options = vim.tbl_map(function(o)
    return o.kind .. "=" .. o.optionId
  end, req.options)
  respond("allow-once")
end
replay("perm-allow")
eq(options, { "allow_once=allow-once", "reject_once=deny" }, "openclaw's options")
eq(named("tool_update")[#named("tool_update")].status, "completed", "allowed exec ran")
agent.permission = function(_, respond)
  respond("deny")
end
replay("perm-reject")
upd = named("tool_update")[#named("tool_update")]
eq(upd.status, "failed", "denied exec failed")
assert(upd.output:find("user-denied", 1, true), upd.output)

-- bud.ai.session runs on bud.ai.agent for any backend but claude
local session = require("bud.ai.session")
eq(session.module(), agent, "openclaw: bud.ai.agent")
eq(session.status(proj), agent.status(proj), "calls go to it")
local config = require("bud.config")
local real_get = config.get
config.get = function(key, default)
  if key == "ai.backend" then
    return "claude"
  end
  return real_get(key, default)
end
eq(session.module(), require("bud.ai.claude"), "claude: bud.ai.claude, until task 6")
config.get = real_get

-- the status line names the backend
vim.cmd.cd(proj)
assert(agent.statusline():find("openclaw", 1, true), agent.statusline())

agent._reset()
vim.fn.delete(tmp, "rf")
print("agent_test: ok")
