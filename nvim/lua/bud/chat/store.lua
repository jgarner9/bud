-- Chat history (BASE.md 5.5, D14, D23). Conversations are Claude Code's own:
-- each is a .jsonl in its store, ~/.claude/projects/<dir>/, for the dir the
-- session ran in. bud only keeps what Claude Code doesn't: titles you gave
-- and pins, in ~/.local/state/bud/chat/history.json.

local M = {}

-- swappable in tests
M.claude_dir = function()
  local dir = vim.env.CLAUDE_CONFIG_DIR
  return (dir and dir ~= "") and dir or vim.fn.expand("~/.claude")
end

-- chat_dir is where full-screen chat runs, so its conversations aren't any
-- project's
M.chat_dir = function()
  return vim.fn.stdpath("state") .. "/chat"
end

local function meta_file()
  return M.chat_dir() .. "/history.json"
end

-- project_dir is Claude Code's store for conversations run in dir: the path
-- with everything but letters and digits turned into -
function M.project_dir(dir)
  dir = vim.uv.fs_realpath(dir) or dir
  return M.claude_dir() .. "/projects/" .. dir:gsub("[^%w]", "-")
end

local function read_meta()
  local fd = io.open(meta_file(), "r")
  if not fd then
    return {}
  end
  local ok, meta = pcall(vim.json.decode, fd:read("*a"), { luanil = { object = true, array = true } })
  fd:close()
  return ok and type(meta) == "table" and meta or {}
end

-- write_meta saves history.json atomically: a temp file renamed over it
local function write_meta(meta)
  vim.fn.mkdir(M.chat_dir(), "p")
  local tmp = meta_file() .. ".tmp"
  local fd = assert(io.open(tmp, "w"))
  fd:write(vim.json.encode(meta))
  fd:close()
  assert(vim.uv.fs_rename(tmp, meta_file()))
end

local function edit_meta(id, fn)
  local meta = read_meta()
  meta[id] = meta[id] or {}
  fn(meta[id])
  if vim.tbl_isempty(meta[id]) then
    meta[id] = nil
  end
  write_meta(meta)
end

-- text_of is a message's text: a string, or its text blocks joined
local function text_of(content)
  if type(content) == "string" then
    return content
  end
  local parts = {}
  for _, block in ipairs(type(content) == "table" and content or {}) do
    if block.type == "text" and block.text then
      table.insert(parts, block.text)
    end
  end
  return table.concat(parts, "\n")
end

-- what bud adds in front of a message (claude.message) isn't part of the title
local function strip_context(text)
  return (text:gsub("^Context: [^\n]*\n\n", ""))
end

-- read parses one conversation's .jsonl into { role, text } and
-- { role = "tool", name, input } entries, skipping Claude Code's bookkeeping,
-- subagents and tool results
function M.read(path)
  local out = {}
  local fd = io.open(path, "r")
  if not fd then
    return out
  end
  for line in fd:lines() do
    local ok, ev = pcall(vim.json.decode, line, { luanil = { object = true, array = true } })
    if ok and type(ev) == "table" and not ev.isSidechain and not ev.isMeta and type(ev.message) == "table" then
      if ev.type == "user" then
        local text = text_of(ev.message.content)
        if text ~= "" and not text:match("^<") then
          table.insert(out, { role = "user", text = strip_context(text) })
        end
      elseif ev.type == "assistant" then
        for _, block in ipairs(type(ev.message.content) == "table" and ev.message.content or {}) do
          if block.type == "text" and block.text and block.text ~= "" then
            table.insert(out, { role = "assistant", text = block.text })
          elseif block.type == "tool_use" then
            table.insert(out, { role = "tool", name = block.name, input = block.input })
          end
        end
      end
    end
  end
  fd:close()
  return out
end

-- first_message reads just far enough into a conversation for its title
local function first_message(path)
  local fd = io.open(path, "r")
  if not fd then
    return nil
  end
  for line in fd:lines() do
    if line:find('"type":"user"', 1, true) then
      local ok, ev = pcall(vim.json.decode, line, { luanil = { object = true, array = true } })
      if ok and type(ev) == "table" and type(ev.message) == "table" and not ev.isSidechain then
        local text = text_of(ev.message.content)
        if text ~= "" and not text:match("^<") then
          fd:close()
          return strip_context(text)
        end
      end
    end
  end
  fd:close()
end

-- title shortens a first message into a title: its first line, at most 50 columns
function M.title(text)
  local line = vim.trim((text or ""):match("[^\n]*") or "")
  if line == "" then
    return "untitled chat"
  end
  if vim.fn.strchars(line) > 50 then
    line = vim.fn.strcharpart(line, 0, 49) .. "…"
  end
  return line
end

-- list returns dir's conversations (default: chat's), pinned first, then
-- newest: { id, path, mtime, title, pinned }
function M.list(dir)
  local store = M.project_dir(dir or M.chat_dir())
  local meta = read_meta()
  local out = {}
  for name, kind in vim.fs.dir(store) do
    local id = kind == "file" and name:match("^(.+)%.jsonl$")
    if id then
      local path = store .. "/" .. name
      local first = first_message(path)
      -- a conversation that never got a message isn't one
      if first then
        local m = meta[id] or {}
        table.insert(out, {
          id = id,
          path = path,
          mtime = vim.uv.fs_stat(path).mtime.sec,
          title = m.title or M.title(first),
          pinned = m.pinned or false,
          first = first,
        })
      end
    end
  end
  table.sort(out, function(a, b)
    if a.pinned ~= b.pinned then
      return a.pinned
    end
    return a.mtime > b.mtime
  end)
  return out
end

function M.rename(id, title)
  edit_meta(id, function(m)
    m.title = title ~= "" and title or nil
  end)
end

function M.pin(id, pinned)
  edit_meta(id, function(m)
    m.pinned = pinned or nil
  end)
end

-- delete removes a conversation from Claude Code's store, and bud's notes on it
function M.delete(entry)
  os.remove(entry.path)
  local meta = read_meta()
  meta[entry.id] = nil
  write_meta(meta)
end

-- matches is whether a search finds a conversation: its title, then its text
function M.matches(entry, query)
  query = query:lower()
  if entry.title:lower():find(query, 1, true) then
    return true
  end
  local fd = io.open(entry.path, "r")
  if not fd then
    return false
  end
  local data = fd:read("*a"):lower()
  fd:close()
  return data:find(query, 1, true) ~= nil
end

-- day groups a time into Today, Yesterday or its date, for the history panel
function M.day(sec, now)
  now = now or os.time()
  local function ymd(t)
    return os.date("%Y-%m-%d", t)
  end
  if ymd(sec) == ymd(now) then
    return "Today"
  elseif ymd(sec) == ymd(now - 86400) then
    return "Yesterday"
  end
  return os.date("%a %b %d", sec)
end

return M
