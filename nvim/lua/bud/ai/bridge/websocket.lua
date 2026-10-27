-- A small WebSocket server (RFC 6455) on vim.uv, just enough for Claude
-- Code's IDE bridge: one listener on 127.0.0.1, text frames, ping/pong and
-- close. Clients must send the auth header before the upgrade is accepted.
-- The same port also takes plain HTTP POSTs (opts.on_http), for MCP's
-- streamable HTTP transport, which headless Claude Code uses.

local bit = require("bit")

local M = {}

local GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

-- sha1 returns the raw 20-byte SHA-1 of s; the handshake needs it and Neovim
-- only ships sha256
function M.sha1(s)
  local band, bor, bxor, bnot, lshift, rshift, rol, tobit =
    bit.band, bit.bor, bit.bxor, bit.bnot, bit.lshift, bit.rshift, bit.rol, bit.tobit
  local h0, h1, h2, h3, h4 = 0x67452301, tobit(0xEFCDAB89), tobit(0x98BADCFE), 0x10325476, tobit(0xC3D2E1F0)
  local len = #s
  local msg = s .. "\128" .. string.rep("\0", (55 - len) % 64)
  local bits = len * 8
  -- 64-bit big-endian length; a message over 512 MB never comes through here
  msg = msg
    .. "\0\0\0\0"
    .. string.char(rshift(bits, 24) % 256, rshift(bits, 16) % 256, rshift(bits, 8) % 256, bits % 256)
  local w = {}
  for chunk = 1, #msg, 64 do
    for i = 0, 15 do
      local a, b, c, d = msg:byte(chunk + i * 4, chunk + i * 4 + 3)
      w[i] = bor(lshift(a, 24), lshift(b, 16), lshift(c, 8), d)
    end
    for i = 16, 79 do
      w[i] = rol(bxor(w[i - 3], w[i - 8], w[i - 14], w[i - 16]), 1)
    end
    local a, b, c, d, e = h0, h1, h2, h3, h4
    for i = 0, 79 do
      local f, k
      if i < 20 then
        f, k = bor(band(b, c), band(bnot(b), d)), 0x5A827999
      elseif i < 40 then
        f, k = bxor(b, c, d), 0x6ED9EBA1
      elseif i < 60 then
        f, k = bor(band(b, c), band(b, d), band(c, d)), tobit(0x8F1BBCDC)
      else
        f, k = bxor(b, c, d), tobit(0xCA62C1D6)
      end
      local t = tobit(rol(a, 5) + f + e + k + w[i])
      e, d, c, b, a = d, c, rol(b, 30), a, t
    end
    h0, h1, h2, h3, h4 = tobit(h0 + a), tobit(h1 + b), tobit(h2 + c), tobit(h3 + d), tobit(h4 + e)
  end
  local out = {}
  for _, h in ipairs({ h0, h1, h2, h3, h4 }) do
    table.insert(
      out,
      string.char(band(rshift(h, 24), 255), band(rshift(h, 16), 255), band(rshift(h, 8), 255), band(h, 255))
    )
  end
  return table.concat(out)
end

-- accept_key is the Sec-WebSocket-Accept answer to a client's key
function M.accept_key(key)
  return vim.base64.encode(M.sha1(key .. GUID))
end

-- frame encodes one unmasked server frame
function M.frame(opcode, payload)
  local len = #payload
  local head = string.char(0x80 + opcode)
  if len < 126 then
    head = head .. string.char(len)
  elseif len < 65536 then
    head = head .. string.char(126, math.floor(len / 256), len % 256)
  else
    local b = {}
    for i = 7, 0, -1 do
      b[#b + 1] = math.floor(len / 2 ^ (8 * i)) % 256
    end
    head = head .. string.char(127, unpack(b))
  end
  return head .. payload
end

-- parse reads one frame from the front of buf. It returns the frame and what's
-- left, or nil when buf doesn't hold a whole frame yet
function M.parse(buf)
  if #buf < 2 then
    return nil
  end
  local b1, b2 = buf:byte(1, 2)
  local fin, opcode = b1 >= 128, b1 % 16
  local masked, len = b2 >= 128, b2 % 128
  local pos = 3
  if len == 126 then
    if #buf < 4 then
      return nil
    end
    len = buf:byte(3) * 256 + buf:byte(4)
    pos = 5
  elseif len == 127 then
    if #buf < 10 then
      return nil
    end
    len = 0
    for i = 3, 10 do
      len = len * 256 + buf:byte(i)
    end
    pos = 11
  end
  local mask
  if masked then
    if #buf < pos + 3 then
      return nil
    end
    mask = { buf:byte(pos, pos + 3) }
    pos = pos + 4
  end
  if #buf < pos + len - 1 then
    return nil
  end
  local payload = buf:sub(pos, pos + len - 1)
  if mask then
    local out = {}
    for i = 1, len do
      out[i] = string.char(bit.bxor(payload:byte(i), mask[(i - 1) % 4 + 1]))
    end
    payload = table.concat(out)
  end
  return { fin = fin, opcode = opcode, payload = payload }, buf:sub(pos + len)
end

-- parse_request reads an HTTP upgrade request: the request line and the
-- headers (lowercased names), or nil until the blank line has arrived
function M.parse_request(buf)
  local head_end = buf:find("\r\n\r\n", 1, true)
  if not head_end then
    return nil
  end
  local lines = vim.split(buf:sub(1, head_end - 1), "\r\n", { plain = true })
  local headers = {}
  for i = 2, #lines do
    local name, value = lines[i]:match("^([^:]+):%s*(.-)%s*$")
    if name then
      headers[name:lower()] = value
    end
  end
  return { line = lines[1], headers = headers }, buf:sub(head_end + 4)
end

-- serve listens on 127.0.0.1 (a free port) and returns the server, whose
-- port, send(client, text) and close() are what callers use. opts.token is
-- the auth header's required value; opts.on_open(client),
-- opts.on_message(client, text) and opts.on_close(client) run in vim.schedule
function M.serve(opts)
  local tcp = assert(vim.uv.new_tcp())
  assert(tcp:bind("127.0.0.1", 0))
  local server = { clients = {} }

  local function close(client)
    if client.closed then
      return
    end
    client.closed = true
    server.clients[client] = nil
    if not client.sock:is_closing() then
      client.sock:close()
    end
    if client.open and opts.on_close then
      vim.schedule(function()
        opts.on_close(client)
      end)
    end
  end

  local function reject(client, status)
    client.sock:write("HTTP/1.1 " .. status .. "\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", function()
      close(client)
    end)
  end

  local function http(client, req)
    local len = tonumber(req.headers["content-length"] or "0") or 0
    if #client.buf < len then
      return false -- the body isn't all here yet
    end
    local body = client.buf:sub(1, len)
    client.buf = client.buf:sub(len + 1)
    client.request = nil
    if req.headers["x-claude-code-ide-authorization"] ~= opts.token then
      reject(client, "401 Unauthorized")
      return true
    end
    if not req.line:match("^POST ") or not opts.on_http then
      reject(client, "405 Method Not Allowed")
      return true
    end
    vim.schedule(function()
      opts.on_http(body, function(status, out, content_type)
        if client.closed then
          return
        end
        out = out or ""
        local head = { "HTTP/1.1 " .. status, "Content-Length: " .. #out }
        if out ~= "" then
          table.insert(head, "Content-Type: " .. (content_type or "application/json"))
        end
        client.sock:write(table.concat(head, "\r\n") .. "\r\n\r\n" .. out)
      end)
    end)
    return true
  end

  local function handshake(client)
    local req = client.request
    if not req then
      local rest
      req, rest = M.parse_request(client.buf)
      if not req then
        if #client.buf > 16384 then
          reject(client, "431 Request Header Fields Too Large")
        end
        return
      end
      client.buf = rest
    end
    local h = req.headers
    if (h.upgrade or ""):lower() ~= "websocket" then
      -- plain HTTP; keep-alive, so more requests may follow on this socket
      client.request = req
      if http(client, req) and not client.closed and #client.buf > 0 then
        handshake(client)
      end
      return
    end
    if not req.line:match("^GET ") or (h.upgrade or ""):lower() ~= "websocket" or not h["sec-websocket-key"] then
      return reject(client, "400 Bad Request")
    end
    if h["x-claude-code-ide-authorization"] ~= opts.token then
      return reject(client, "401 Unauthorized")
    end
    client.sock:write(table.concat({
      "HTTP/1.1 101 Switching Protocols",
      "Upgrade: websocket",
      "Connection: Upgrade",
      "Sec-WebSocket-Accept: " .. M.accept_key(h["sec-websocket-key"]),
      "",
      "",
    }, "\r\n"))
    client.open = true
    if opts.on_open then
      vim.schedule(function()
        opts.on_open(client)
      end)
    end
  end

  local function frames(client)
    while not client.closed do
      local f, rest = M.parse(client.buf)
      if not f then
        return
      end
      client.buf = rest
      if f.opcode == 8 then
        client.sock:write(M.frame(8, ""), function()
          close(client)
        end)
        return
      elseif f.opcode == 9 then
        client.sock:write(M.frame(10, f.payload))
      elseif f.opcode == 1 or f.opcode == 0 then
        client.parts = (client.parts or "") .. f.payload
        if f.fin then
          local text = client.parts
          client.parts = nil
          vim.schedule(function()
            opts.on_message(client, text)
          end)
        end
      end
    end
  end

  tcp:listen(16, function(err)
    if err then
      return
    end
    local sock = vim.uv.new_tcp()
    tcp:accept(sock)
    local client = { sock = sock, buf = "" }
    server.clients[client] = true
    sock:read_start(function(read_err, data)
      if read_err or not data then
        return close(client)
      end
      client.buf = client.buf .. data
      if client.open then
        frames(client)
      else
        handshake(client)
        if client.open then
          frames(client)
        end
      end
    end)
  end)

  server.port = tcp:getsockname().port

  function server.send(client, text)
    if client.open and not client.closed then
      client.sock:write(M.frame(1, text))
    end
  end

  function server.broadcast(text)
    for client in pairs(server.clients) do
      server.send(client, text)
    end
  end

  function server.close()
    for client in pairs(server.clients) do
      close(client)
    end
    if not tcp:is_closing() then
      tcp:close()
    end
  end

  return server
end

return M
