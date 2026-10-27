-- Run with: nvim --clean --headless -l tests/lua/claude_test.lua (just lua-test)
-- Drives bud.ai.claude against a fake claude that answers every message

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(5000, cond, 10) then
    error("timed out waiting for " .. what, 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
require("bud.config").file = tmp .. "/config.json"
-- never the real ~/.claude/ide
require("bud.ai.bridge").lock_dir = function()
  return tmp .. "/ide"
end

-- the fake logs its argv and every line it's sent, answers each with a text
-- block, a tool use and a result, and exits 3 when told to crash
local fake = tmp .. "/claude"
vim.fn.writefile({
  "#!/bin/sh",
  'echo "$@" >> "$(dirname "$0")/argv.log"',
  "n=0",
  "while IFS= read -r line; do",
  [[  printf '%s\n' "$line" >> "$(dirname "$0")/stdin.log"]],
  '  case "$line" in *crash*) exit 3;; *control_request*) continue;; esac',
  "  n=$((n+1))",
  [[  printf '{"type":"system","subtype":"init","session_id":"sess-1","model":"m"}\n']],
  [[  printf '{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"text","text":"reply %s"},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"x"}}]}}\n' "$n"]],
  [[  printf '{"type":"result","subtype":"success","session_id":"sess-1","result":"reply %s","total_cost_usd":0.5}\n' "$n"]],
  "done",
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")

local function lines(name)
  local f = tmp .. "/" .. name
  return vim.fn.filereadable(f) == 1 and vim.fn.readfile(f) or {}
end

local claude = require("bud.ai.claude")
claude.bin = function()
  return fake
end
local logged_in = true
claude.secret_env = function(cb)
  cb({})
end
claude.check_login = function(_, cb)
  vim.schedule(function()
    cb(logged_in, not logged_in and "not logged in" or nil)
  end)
end

local function project(name)
  local dir = tmp .. "/" .. name
  vim.fn.mkdir(dir, "p")
  return vim.uv.fs_realpath(dir)
end

local events = {}
for _, ev in ipairs({ "message", "tool_use", "done", "exit" }) do
  claude.on(ev, function(data, s)
    table.insert(events, { ev, data.text or data.name or data.code, s.root })
  end)
end

-- argv: --continue the first time, the session's own id after
eq(claude.argv("claude", {}, { resume = "continue", args = { "--model", "x" } }), {
  "claude",
  "-p",
  "--input-format",
  "stream-json",
  "--output-format",
  "stream-json",
  "--verbose",
  "--continue",
  "--model",
  "x",
}, "argv continues")
eq(claude.argv("claude", {}, { resume = "new" })[8], nil, "argv new")
eq(claude.argv("claude", { session_id = "abc" }, { resume = "new" })[9], "abc", "argv resumes its own")

-- a message mentions context files first
local msg = vim.json.decode(claude.message("hi", { "a.go" }))
eq(msg.message.content, "Context: @a.go\n\nhi", "context mentions")

-- starting, sending, replies
local a = project("a")
local s = claude.session(a, { context = { "main.go" } })
eq(s.status, "starting", "starting while the login is checked")
wait(function()
  return s.status == "ready"
end, "ready")
eq(claude.send("hello", { root = a }), true, "sent")
eq(claude.status(a), "busy", "busy until the result")
wait(function()
  return claude.status(a) == "ready"
end, "the result")
eq(events, {
  { "message", "reply 1", a },
  { "tool_use", "Read", a },
  { "done", "reply 1", a },
}, "events")
eq(s.session_id, "sess-1", "session id kept")
eq(s.cost, 0.5, "cost adds up")
local sent = vim.json.decode(lines("stdin.log")[1])
eq(sent.message.content, "Context: @main.go\n\nhello", "first message carries the opened files")

claude.send("again", { root = a })
wait(function()
  return #lines("stdin.log") == 2 and claude.status(a) == "ready"
end, "second reply")
eq(vim.json.decode(lines("stdin.log")[2]).message.content, "again", "context only once")

-- interrupt sends a control request
claude.send("long one", { root = a })
claude.interrupt(a)
wait(function()
  return #lines("stdin.log") >= 4
end, "interrupt")
eq(vim.json.decode(lines("stdin.log")[4]).request.subtype, "interrupt", "interrupt request")
wait(function()
  return claude.status(a) == "ready"
end, "settled")

-- stopping keeps the conversation: the next send resumes it
events = {}
claude.stop(a)
wait(function()
  return claude.status(a) == "stopped"
end, "stopped")
eq(events, { { "exit", 143, a } }, "exit event")
eq(s.crashed, false, "a stop isn't a crash")
claude.send("after", { root = a })
wait(function()
  return claude.status(a) == "ready" and #lines("argv.log") == 2
end, "restarted")
eq(lines("argv.log")[2]:match("%-%-resume (%S+)"), "sess-1", "resumed its own conversation")

-- a crash shows in the status line, and restart brings it back
vim.fn.chdir(a)
claude.send("crash", { root = a })
wait(function()
  return s.crashed
end, "crash")
eq(claude.statusline():match("crashed, <leader>ar") ~= nil, true, "statusline offers the restart")
claude.restart(a)
wait(function()
  return claude.status(a) == "ready"
end, "restart")
eq(s.crashed, false, "restarted")

-- the last three projects keep their sessions; a fourth stops the oldest (D49)
local b, c, d = project("b"), project("c"), project("d")
for _, root in ipairs({ b, c }) do
  claude.session(root)
  wait(function()
    return claude.status(root) == "ready"
  end, "ready " .. root)
end
claude.session(a) -- a is used again, so b is the oldest
claude.session(d)
wait(function()
  return claude.status(d) == "ready" and claude.status(b) == "stopped"
end, "b evicted")
eq({ claude.status(a), claude.status(c) }, { "ready", "ready" }, "the rest keep running")

-- idle ones stop; a reply in progress isn't idle
claude.send("busy", { root = c })
claude.reap(30, vim.uv.hrtime() / 1e6 + 31 * 60 * 1000)
wait(function()
  return claude.status(a) == "stopped" and claude.status(d) == "stopped"
end, "idle stopped")
eq(claude.status(c) ~= "stopped", true, "busy kept")

-- no claude, or logged out: no session, and send says why
claude._reset()
claude.bin = function()
  return nil
end
local e = project("e")
claude.session(e)
eq(claude.status(e), "missing", "not installed")
eq(claude.send("hi", { root = e }), false, "can't send")
claude.bin = function()
  return fake
end
logged_in = false
local f = project("f")
claude.session(f)
wait(function()
  return claude.status(f) == "missing"
end, "logged out")
eq(select(2, claude.status(f)), "not logged in", "why")

claude._reset()
require("bud.ai.bridge").stop()
vim.fn.delete(tmp, "rf")
print("claude_test: ok")
