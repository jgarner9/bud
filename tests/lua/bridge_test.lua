-- Run with: nvim --clean --headless -l tests/lua/bridge_test.lua (just lua-test)
-- Talks to the IDE bridge the way Claude Code does: a WebSocket client with
-- the auth token from the lock file, and MCP over HTTP for headless sessions

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

local ws = require("bud.ai.bridge.websocket")
local bit = require("bit")

-- sha1 and the handshake key against RFC 6455's example
local function hex(s)
  return (s:gsub(".", function(c)
    return ("%02x"):format(c:byte())
  end))
end
eq(hex(ws.sha1("abc")), "a9993e364706816aba3e25717850c26c9cd0d89d", "sha1 abc")
eq(hex(ws.sha1(string.rep("a", 1000))), "291e9a6c66994949b57ba5e650361e98fc36b1ba", "sha1 over several blocks")
eq(ws.accept_key("dGhlIHNhbXBsZSBub25jZQ=="), "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=", "accept key")

-- frames round-trip, short and long, and a partial one waits
for _, n in ipairs({ 5, 300, 70000 }) do
  local f, rest = ws.parse(ws.frame(1, string.rep("x", n)) .. "tail")
  eq({ #f.payload, f.opcode, f.fin, rest }, { n, 1, true, "tail" }, "frame of " .. n)
end
eq(ws.parse(ws.frame(1, "hello"):sub(1, 4)), nil, "partial frame")

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
local bridge = require("bud.ai.bridge")
bridge.lock_dir = function()
  return tmp .. "/ide"
end

local file = tmp .. "/main.go"
vim.fn.writefile({ "package main", "", "func main() {}" }, file)
vim.cmd.edit(file)
vim.fn.chdir(tmp)

assert(bridge.start())
local lock = vim.json.decode(table.concat(vim.fn.readfile(bridge.lock_file()), ""))
eq(lock.transport, "ws", "lock transport")
eq(lock.ideName, "Neovim", "lock ide name")
eq(lock.pid, vim.fn.getpid(), "lock pid")
eq(#lock.authToken, 32, "a 128-bit hex token")
eq(lock.workspaceFolders, { vim.uv.fs_realpath(tmp) }, "workspace folders")
eq(vim.uv.fs_stat(bridge.lock_file()).mode % 512, tonumber("600", 8), "lock file is private")
eq(bridge.env().CLAUDE_CODE_SSE_PORT, tostring(bridge.port()), "env names the port")

-- a client: connect, send raw bytes, collect what comes back
local function connect()
  local c = { buf = "", sock = vim.uv.new_tcp() }
  local connected
  c.sock:connect("127.0.0.1", bridge.port(), function(err)
    assert(not err, err)
    connected = true
    c.sock:read_start(function(_, data)
      if data then
        c.buf = c.buf .. data
      else
        c.eof = true
      end
    end)
  end)
  wait(function()
    return connected
  end, "connect")
  return c
end

local function upgrade(c, auth)
  local headers = {
    "GET / HTTP/1.1",
    "Host: 127.0.0.1",
    "Upgrade: websocket",
    "Connection: Upgrade",
    "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==",
    "Sec-WebSocket-Version: 13",
  }
  if auth then
    table.insert(headers, "x-claude-code-ide-authorization: " .. auth)
  end
  c.sock:write(table.concat(headers, "\r\n") .. "\r\n\r\n")
  wait(function()
    return c.buf:find("\r\n\r\n", 1, true) ~= nil
  end, "the handshake answer")
  local head_end = c.buf:find("\r\n\r\n", 1, true)
  local head = c.buf:sub(1, head_end)
  c.buf = c.buf:sub(head_end + 4)
  return head
end

-- clients send masked frames
local function send(c, msg)
  local payload = vim.json.encode(msg)
  local mask = { 1, 2, 3, 4 }
  local masked = {}
  for i = 1, #payload do
    masked[i] = string.char(bit.bxor(payload:byte(i), mask[(i - 1) % 4 + 1]))
  end
  local len = #payload
  local head = len < 126 and string.char(0x81, 0x80 + len)
    or string.char(0x81, 0x80 + 126, math.floor(len / 256), len % 256)
  c.sock:write(head .. string.char(unpack(mask)) .. table.concat(masked))
end

local function recv(c, what)
  local f
  wait(function()
    local rest
    f, rest = ws.parse(c.buf)
    if f then
      c.buf = rest
    end
    return f ~= nil
  end, what)
  return vim.json.decode(f.payload)
end

-- a wrong token is turned away
local bad = connect()
eq(upgrade(bad, "nope"):match("^HTTP/1.1 (%d+)"), "401", "wrong token")
wait(function()
  return bad.eof
end, "the bad client to be closed")

local c = connect()
eq(upgrade(c, lock.authToken):match("^HTTP/1.1 (%d+)"), "101", "upgraded")
wait(function()
  return bridge.connected() == 1
end, "connected")

send(c, { jsonrpc = "2.0", id = 1, method = "initialize", params = { protocolVersion = "2025-03-26" } })
local init = recv(c, "initialize")
eq({ init.id, init.result.serverInfo.name, init.result.protocolVersion }, { 1, "bud", "2025-03-26" }, "initialize")

send(c, { jsonrpc = "2.0", id = 2, method = "tools/list" })
local names = vim.tbl_map(function(t)
  return t.name
end, recv(c, "tools/list").result.tools)
for _, want in ipairs({ "openDiff", "getOpenEditors", "getDiagnostics", "getCurrentSelection", "close_tab" }) do
  eq(vim.tbl_contains(names, want), true, "tool " .. want)
end

local function call(name, args, id)
  send(c, { jsonrpc = "2.0", id = id, method = "tools/call", params = { name = name, arguments = args or {} } })
  return recv(c, name)
end

local editors = vim.json.decode(call("getOpenEditors", nil, 3).result.content[1].text)
eq(#editors.tabs, 1, "one file open")
eq(editors.tabs[1].label, "main.go", "its label")
eq(editors.tabs[1].isActive, true, "active")

vim.diagnostic.set(vim.api.nvim_create_namespace("t"), 0, { { lnum = 2, col = 0, message = "unused", severity = 2 } })
local diags = vim.json.decode(call("getDiagnostics", nil, 4).result.content[1].text)
eq({ diags[1].diagnostics[1].message, diags[1].diagnostics[1].severity }, { "unused", "Warning" }, "diagnostics")

local dirty = vim.json.decode(call("checkDocumentDirty", { filePath = file }, 5).result.content[1].text)
eq(dirty.isDirty, false, "not dirty")

-- the cursor moving tells Claude Code where you are
vim.api.nvim_win_set_cursor(0, { 3, 5 })
vim.api.nvim_exec_autocmds("CursorMoved", {})
local note = recv(c, "selection_changed")
eq(
  { note.method, note.params.selection.start.line, note.params.selection.isEmpty },
  { "selection_changed", 2, true },
  "selection"
)

-- a proposed edit waits for you; ga accepts it as it stands, edits and all
local editor_tab = vim.api.nvim_get_current_tabpage()
send(c, {
  jsonrpc = "2.0",
  id = 6,
  method = "tools/call",
  params = {
    name = "openDiff",
    arguments = {
      old_file_path = file,
      new_file_path = file,
      new_file_contents = "package main\n\nfunc main() { run() }\n",
      tab_name = "edit-1",
    },
  },
})
wait(function()
  return require("bud.ai.bridge.tools").open_diffs()["edit-1"] ~= nil
end, "the diff to open")
vim.wait(50)
eq(c.buf, "", "no answer before you decide")
local d = require("bud.ai.bridge.tools").open_diffs()["edit-1"]
eq(vim.api.nvim_get_current_buf(), d.proposed, "the proposal has the cursor")
vim.api.nvim_buf_set_lines(d.proposed, 0, 1, false, { "package main // edited" })
vim.api.nvim_feedkeys("ga", "x", false)
local saved = recv(c, "the accepted diff")
eq(saved.result.content[1].text, "FILE_SAVED", "accepted")
eq(saved.result.content[2].text, "package main // edited\n\nfunc main() { run() }\n", "with your edit")
eq(vim.api.nvim_get_current_tabpage(), editor_tab, "back in the editor tab")

-- q rejects
send(c, {
  jsonrpc = "2.0",
  id = 7,
  method = "tools/call",
  params = {
    name = "openDiff",
    arguments = { old_file_path = file, new_file_path = file, new_file_contents = "x\n", tab_name = "edit-2" },
  },
})
wait(function()
  return require("bud.ai.bridge.tools").open_diffs()["edit-2"] ~= nil
end, "the second diff")
vim.api.nvim_feedkeys("q", "x", false)
local rejected = recv(c, "the rejected diff")
eq({ rejected.result.content[1].text, rejected.result.content[2].text }, { "DIFF_REJECTED", "edit-2" }, "rejected")

-- mentions reach the TUI
bridge.mention(file, 0, 2)
eq(recv(c, "at_mentioned").params, { filePath = file, lineStart = 0, lineEnd = 2 }, "at_mentioned")

-- headless sessions use MCP over HTTP on the same port
local h = connect()
local body =
  vim.json.encode({ jsonrpc = "2.0", id = 9, method = "tools/call", params = { name = "getWorkspaceFolders" } })
h.sock:write(table.concat({
  "POST /mcp HTTP/1.1",
  "Host: 127.0.0.1",
  "Content-Type: application/json",
  "x-claude-code-ide-authorization: " .. lock.authToken,
  "Content-Length: " .. #body,
  "",
  body,
}, "\r\n"))
wait(function()
  local head_end = h.buf:find("\r\n\r\n", 1, true)
  local len = head_end and tonumber(h.buf:match("Content%-Length: (%d+)"))
  return len and #h.buf >= head_end + 3 + len
end, "the HTTP answer")
eq(h.buf:match("^HTTP/1.1 (%d+)"), "200", "HTTP ok")
local answer = vim.json.decode(h.buf:sub(h.buf:find("\r\n\r\n", 1, true) + 4))
eq(vim.json.decode(answer.result.content[1].text).folders[1].path, vim.uv.fs_realpath(tmp), "folders over HTTP")

local args = bridge.headless_args()
eq(args[1], "--mcp-config", "headless sessions get the bridge as an MCP server")
local cfg_file = args[2]
eq(args[2]:find("authorization", 1, true), nil, "no token on the command line")
eq(vim.uv.fs_stat(cfg_file).mode % 512, tonumber("600", 8), "config readable only by you")
local cfg = vim.json.decode(table.concat(vim.fn.readfile(cfg_file), "\n"))
eq(cfg.mcpServers["bud-ide"].headers["x-claude-code-ide-authorization"], bridge.token(), "token in the file")
eq(
  vim.startswith(cfg.mcpServers["bud-ide"].url, "http://127.0.0.1:" .. bridge.port() .. "/mcp?root="),
  true,
  "its URL names the session's root"
)
eq(args[4]:match("mcp__bud%-ide__openDiff"), nil, "edits aren't pre-allowed")
eq(args[4]:match("mcp__bud%-ide__getOpenEditors") ~= nil, true, "looking is")

local lock_file = bridge.lock_file()
bridge.stop()
eq(vim.uv.fs_stat(lock_file), nil, "lock file removed")
eq(vim.uv.fs_stat(cfg_file), nil, "config file removed")
vim.fn.delete(tmp, "rf")
print("bridge_test: ok")
