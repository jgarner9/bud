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
-- bud's own Claude Code dir, and never the real conversation ids
vim.env.CLAUDE_CONFIG_DIR = tmp .. "/cc"
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
  -- what a Bedrock session says once its AWS login ran out (JGA-150)
  -- and how Claude Code says it first: retrying a 403 for minutes
  [[  case "$line" in *retry*) printf '{"type":"system","subtype":"api_retry","attempt":1,"error_status":403,"error":"authentication_failed","session_id":"sess-1"}\n'; continue;; esac]],
  [[  case "$line" in *expire*) printf '{"type":"result","subtype":"success","is_error":true,"session_id":"sess-1","result":"API Error: 403 The security token included in the request is expired"}\n'; continue;; esac]],
  "  n=$((n+1))",
  -- Claude Code saves the conversation once it answers
  [=[  d="$CLAUDE_CONFIG_DIR/projects/$(pwd -P | sed 's/[^[:alnum:]]/-/g')"; mkdir -p "$d"; echo '{}' > "$d/sess-1.jsonl"]=],
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
claude.ids_file = function()
  return tmp .. "/claude-sessions.json"
end
claude.bin = function()
  return fake
end
local logged_in = true
claude.secret_env = function(_, cb)
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

-- argv: --continue the first time, the session's own id after, once Claude
-- Code has them; the TUI exits on a conversation it can't find
local here = { root = project("here") }
eq(claude.argv("claude", here, { resume = "continue" })[8], nil, "nothing to continue yet")
eq(
  claude.argv("claude", { root = here.root, session_id = "abc" }, { resume = "new" })[8],
  nil,
  "nor an id not saved yet"
)
local here_store = require("bud.chat.store").project_dir(here.root)
vim.fn.mkdir(here_store, "p")
vim.fn.writefile({ "{}" }, here_store .. "/abc.jsonl")
eq(claude.argv("claude", here, { resume = "continue", args = { "--model", "x" } }), {
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
eq(claude.argv("claude", here, { resume = "new" })[8], nil, "argv new")
eq(
  claude.argv("claude", { root = here.root, session_id = "abc" }, { resume = "new" })[9],
  "abc",
  "argv resumes its own"
)

-- a project resumes its conversation from an earlier session, while Claude
-- Code still has it (JGA-147)
do
  local root = tmp .. "/saved"
  local proj = { key = root, root = root }
  vim.fn.writefile({ vim.json.encode({ [root] = "old" }) }, tmp .. "/claude-sessions.json")
  local store = require("bud.chat.store").project_dir(root)
  vim.fn.mkdir(store, "p")
  vim.fn.writefile({ "{}" }, store .. "/other.jsonl")
  eq(claude.resume_args(proj, { resume = "continue" }), { "--continue" }, "a saved id Claude Code lost")
  vim.fn.writefile({ "{}" }, store .. "/old.jsonl")
  eq(claude.resume_args(proj, { resume = "continue" }), { "--resume", "old" }, "the saved id")
  eq(claude.resume_args(proj, { resume = "new" }), {}, "resume new ignores it")
  eq(
    claude.resume_args({ key = "chat", root = root, chat = true }, { resume = "continue" }),
    { "--continue" },
    "keyed sessions keep their own"
  )
  -- a shared ~/.claude never continues the newest, which may be yours
  local old_dir = vim.env.CLAUDE_CONFIG_DIR
  vim.env.CLAUDE_CONFIG_DIR = vim.env.HOME .. "/.claude"
  eq(claude.shared(), true, "~/.claude is shared")
  eq(claude.resume_args({}, { resume = "continue" }), {}, "shared starts a new one")
  vim.env.CLAUDE_CONFIG_DIR = ""
  eq(claude.shared(), true, "no CLAUDE_CONFIG_DIR is ~/.claude")
  vim.env.CLAUDE_CONFIG_DIR = old_dir
  eq(claude.shared(), false, "bud's own dir")
  vim.fn.delete(tmp .. "/claude-sessions.json")
end

-- print mode ignores outputStyle in settings.json, so the session passes it
eq(
  vim.list_slice(claude.argv("claude", {}, { resume = "new", output_style = "bud" }), 8, 9),
  { "--settings", '{"outputStyle":"bud"}' },
  "argv carries the output style"
)
eq(claude.argv("claude", {}, { resume = "new" })[8], nil, "no style, no --settings")
do
  local dir = vim.fn.tempname()
  vim.fn.mkdir(dir, "p")
  local old = vim.env.CLAUDE_CONFIG_DIR
  vim.env.CLAUDE_CONFIG_DIR = dir
  eq(claude.output_style(), nil, "no settings.json, no style")
  vim.fn.writefile({ '{"outputStyle": "bud", "theme": "dark"}' }, dir .. "/settings.json")
  eq(claude.output_style(), "bud", "style read from the config dir")
  vim.fn.writefile({ "{not json" }, dir .. "/settings.json")
  eq(claude.output_style(), nil, "a broken settings.json is no style")
  vim.env.CLAUDE_CONFIG_DIR = old
end

-- the AWS errors a Bedrock session can hit, and how to log in again
eq(claude.aws_expired("ExpiredTokenException: The security token included in the request is expired"), true, "expired")
eq(claude.aws_expired("Error: The SSO session associated with this profile has expired"), true, "sso")
eq(claude.aws_expired("Could not load credentials from any providers"), true, "none")
eq(claude.aws_expired("API Error: 529 overloaded"), false, "not AWS")
-- what Claude Code 2.1.312 says after its retries, with fake AWS keys
eq(
  claude.aws_expired(
    "AWS authentication failed · refresh your AWS credentials (SSO sign-in, access keys, API key or proxy token) and retry · API Error: 403 The security token included in the request is invalid."
  ),
  true,
  "Claude Code's own message"
)
eq(claude.aws_expired(nil), false, "nothing")
do
  local dir = vim.fn.tempname()
  vim.fn.mkdir(dir, "p")
  local old_dir, old_profile = vim.env.CLAUDE_CONFIG_DIR, vim.env.AWS_PROFILE
  vim.env.CLAUDE_CONFIG_DIR, vim.env.AWS_PROFILE = dir, nil
  eq(claude.aws_fix(), "aws sso login", "the default")
  vim.fn.writefile({ '{"env": {"AWS_PROFILE": "work"}}' }, dir .. "/settings.json")
  eq(claude.aws_fix(), "aws sso login --profile work", "the profile from settings.json")
  vim.fn.writefile(
    { '{"awsAuthRefresh": "aws sso login --sso-session corp", "env": {"AWS_PROFILE": "work"}}' },
    dir .. "/settings.json"
  )
  eq(claude.aws_fix(), "aws sso login --sso-session corp", "awsAuthRefresh wins")
  vim.env.CLAUDE_CONFIG_DIR, vim.env.AWS_PROFILE = old_dir, old_profile
end

-- a cloud provider in ~/.claude, which bud's own config dir doesn't see
do
  local old_home, old_dir = vim.env.HOME, vim.env.CLAUDE_CONFIG_DIR
  vim.env.HOME = vim.fn.tempname()
  vim.fn.mkdir(vim.env.HOME .. "/.claude", "p")
  vim.env.CLAUDE_CONFIG_DIR = vim.env.HOME .. "/bud"
  eq(claude.home_provider(), nil, "no settings.json, no provider")
  local settings = vim.env.HOME .. "/.claude/settings.json"
  vim.fn.writefile({ '{"env": {"CLAUDE_CODE_USE_BEDROCK": "1", "AWS_REGION": "us-east-1"}}' }, settings)
  eq(claude.home_provider(), "Amazon Bedrock", "Bedrock in ~/.claude")
  vim.fn.writefile({ '{"env": {"CLAUDE_CODE_USE_BEDROCK": "0"}}' }, settings)
  eq(claude.home_provider(), nil, "switched off")
  vim.fn.writefile({ '{"env": {"CLAUDE_CODE_USE_VERTEX": true}}' }, settings)
  eq(claude.home_provider(), "Google Vertex AI", "Vertex in ~/.claude")
  vim.env.CLAUDE_CONFIG_DIR = vim.env.HOME .. "/.claude"
  eq(claude.home_provider(), nil, "~/.claude itself sees its own settings")
  vim.env.HOME, vim.env.CLAUDE_CONFIG_DIR = old_home, old_dir
end

-- ai.claude.command wraps claude (Headroom), passing the rest on
eq(
  vim.list_slice(claude.argv("claude", {}, { resume = "new", command = { "headroom", "wrap", "claude", "--" } }), 1, 5),
  { "headroom", "wrap", "claude", "--", "-p" },
  "a wrapper goes first"
)

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
eq(vim.json.decode(lines("claude-sessions.json")[1])[a], "sess-1", "the project's conversation is saved")

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

-- expired AWS credentials say how to log in again, and the next send starts
-- a new process that reads the new ones
do
  local notes = {}
  local orig_notify = vim.notify
  vim.notify = function(note)
    table.insert(notes, note)
  end
  claude.send("expire", { root = a })
  wait(function()
    return claude.status(a) == "auth"
  end, "auth status")
  vim.notify = orig_notify
  eq(s.crashed, false, "an expired login isn't a crash")
  eq(s.why:match("run aws sso login") ~= nil, true, "names the fix")
  eq(notes[1]:match("AWS credentials expired") ~= nil, true, "tells you")
  eq(claude.statusline():match("AWS login expired") ~= nil, true, "statusline says so")
  local old = s.job
  claude.send("again", { root = a })
  wait(function()
    return claude.status(a) == "ready" and s.job ~= nil and s.job ~= old
  end, "a new process after the login")

  -- on Bedrock, the first retry of a rejected login says so and stops the turn
  vim.notify = function() end
  claude.send("retry", { root = a })
  wait(function()
    return claude.status(a) == "busy"
  end, "busy")
  eq(claude.bedrock(s), false, "model m, no env: not Bedrock")
  vim.env.CLAUDE_CODE_USE_BEDROCK = "1"
  claude.send("retry", { root = a })
  wait(function()
    return claude.status(a) == "auth"
  end, "auth after a retry")
  vim.env.CLAUDE_CODE_USE_BEDROCK = nil
  vim.notify = orig_notify
  wait(function()
    local got = lines("stdin.log")
    return got[#got]:find('"interrupt"', 1, true) ~= nil
  end, "the turn stopped")
  eq(claude.bedrock({ model = "us.anthropic.claude-opus-5-5" }), true, "a Bedrock model id")
  claude.send("again", { root = a })
  wait(function()
    return claude.status(a) == "ready"
  end, "back after the retry")
end

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

-- a large project's session gets the kit's extra MCP config (JGA-117)
claude._reset()
claude.bin = function()
  return fake
end
logged_in = true
claude.secret_env = function(_, cb)
  cb({ SOME_KEY = "v" }, '{"mcpServers":{"code-index":{}}}')
end
local big = project("big")
claude.session(big)
wait(function()
  return claude.status(big) == "ready"
end, "the big project's session")
local last
wait(function()
  for _, l in ipairs(lines("argv.log")) do
    if l:find("code-index", 1, true) then
      last = l
    end
  end
  return last ~= nil
end, "the big project's claude to start")
eq(last:find('--mcp-config {"mcpServers":{"code-index":{}}}', 1, true) ~= nil, true, "extra MCP config passed")

claude._reset()
require("bud.ai.bridge").stop()
vim.fn.delete(tmp, "rf")
print("claude_test: ok")
