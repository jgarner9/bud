-- Run with: nvim --clean --headless -l tests/lua/term_backend_test.lua (just lua-test)
-- The AI pane on a backend other than claude (D131, D139): OpenClaw's own TUI
-- on the project's Gateway session, the token passed in the environment

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
vim.o.shell = "sh"

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
vim.fn.mkdir(tmp .. "/bin", "p")
vim.fn.mkdir(tmp .. "/proj", "p")
local proj = vim.uv.fs_realpath(tmp .. "/proj")
vim.fn.writefile({ "secret-token" }, tmp .. "/gateway-token")
vim.fn.writefile({
  vim.json.encode({
    ai = {
      backend = "openclaw",
      openclaw = { url = "wss://home.example.ts.net:18789", token_file = tmp .. "/gateway-token" },
    },
  }),
}, tmp .. "/config.json")
require("bud.config").file = tmp .. "/config.json"

-- a fake openclaw: logs its argv and token, then waits for a q line
vim.fn.writefile({
  "#!/bin/sh",
  'echo "$@" > "$(dirname "$0")/argv.log"',
  'echo "$OPENCLAW_GATEWAY_TOKEN" > "$(dirname "$0")/token.log"',
  'while IFS= read -r line; do [ "$line" = q ] && exit 0; done',
}, tmp .. "/bin/openclaw")
vim.fn.setfperm(tmp .. "/bin/openclaw", "rwxr-xr-x")

local repo = vim.fn.getcwd()
local agent = require("bud.ai.agent")
local term = require("bud.ai.term")
-- the session itself runs on the fake ACP agent, which can load saved ones
agent.command = function()
  return {
    "env",
    "FAKE_ACP_LOAD=1",
    vim.v.progpath,
    "--clean",
    "--headless",
    "-l",
    repo .. "/tests/lua/lib/fake_acp.lua",
  }
end
-- the project's saved ACP session (D139), which the TUI opens
agent.ids_file = function()
  return tmp .. "/agent-sessions.json"
end
vim.fn.writefile(
  { vim.json.encode({ [proj] = { backend = "openclaw", id = "abc123" } }) },
  tmp .. "/agent-sessions.json"
)

-- not installed: the layout's chat pane isn't available, and opening says why
local path = vim.env.PATH
vim.env.PATH = "/nonexistent"
eq(term.available(), false, "unavailable without openclaw")
local notes = {}
local notify = vim.notify
vim.notify = function(msg)
  table.insert(notes, msg)
end
eq(term.open({ root = proj, focus = false }), nil, "nothing opens")
assert(notes[1]:find("openclaw isn't installed: run bud ai setup", 1, true), notes[1])
vim.notify = notify

vim.env.PATH = tmp .. "/bin:" .. path
vim.cmd.cd(proj)
eq(term.available(), true, "available with openclaw")
local win = term.open({ root = proj, focus = false })
assert(win, "a pane")
wait(function()
  return vim.fn.filereadable(tmp .. "/bin/argv.log") == 1
end, "the TUI to start")
eq(
  vim.fn.readfile(tmp .. "/bin/argv.log")[1],
  "tui --url wss://home.example.ts.net:18789 --session agent:main:acp-bridge:abc123",
  "openclaw tui on the project's saved session, once the backend loaded it"
)
eq(vim.fn.readfile(tmp .. "/bin/token.log")[1], "secret-token", "the token from the file, in the environment")
eq(require("bud.ai.claude").status(proj), "stopped", "no Claude Code session touched")

-- the toggle hides it and the TUI keeps running
term.toggle()
eq(term.running(proj), true, "still running when hidden")

term.stop_all()
wait(function()
  return not term.running(proj)
end, "the TUI to stop")

-- a saved session the backend no longer has: the pane opens the new one
-- bud's agent made, never the dead id (D139)
term.stop_all()
wait(function()
  return not term.running(proj)
end, "the TUI to stop again")
agent._reset()
vim.fn.writefile({ vim.json.encode({ [proj] = { backend = "openclaw", id = "gone" } }) }, tmp .. "/agent-sessions.json")
vim.fn.delete(tmp .. "/bin/argv.log")
term.open({ root = proj, focus = false })
wait(function()
  return vim.fn.filereadable(tmp .. "/bin/argv.log") == 1
end, "the TUI on the new session")
local live = agent.get(proj).id
eq(live ~= "gone", true, "a new session")
assert(
  vim.fn.readfile(tmp .. "/bin/argv.log")[1]:find("acp-bridge:" .. live, 1, true),
  "the pane is on bud's live session"
)
term.stop_all()

-- chat (<leader>ch) runs it on the Gateway's chat session, outside the project
vim.fn.delete(tmp .. "/bin/argv.log")
local chat = require("bud.ai.chat")
chat.dir = function()
  return tmp .. "/chat"
end
chat.open()
wait(function()
  return vim.fn.filereadable(tmp .. "/bin/argv.log") == 1
end, "the chat TUI")
eq(
  vim.fn.readfile(tmp .. "/bin/argv.log")[1],
  "tui --url wss://home.example.ts.net:18789 --session agent:main:bud:chat",
  "openclaw tui on the chat session"
)
eq(chat.running(), true, "chat running")
chat._reset()
vim.fn.delete(tmp, "rf")
print("term_backend_test: ok")
