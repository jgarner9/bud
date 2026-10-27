-- Reads the project registry the Go CLI writes, ~/.local/state/bud/projects.json
-- (D36), for the dashboard and the project picker. Read-only: bud registers
-- projects when it opens them, and `bud projects` edits the list.

local M = {}

-- tests point this elsewhere
M.file = vim.fn.stdpath("state") .. "/projects.json"

-- list returns the registered projects, pinned first, then the most recently
-- opened, then by name. Each is { name, path, pinned, last_opened, missing };
-- last_opened is an RFC 3339 string, or nil when never opened. A missing or
-- unreadable file is an empty list
function M.list()
  local fd = io.open(M.file, "r")
  if not fd then
    return {}
  end
  local data = fd:read("*a")
  fd:close()
  local ok, doc = pcall(vim.json.decode, data, { luanil = { object = true, array = true } })
  if not ok or type(doc) ~= "table" or type(doc.projects) ~= "table" then
    return {}
  end

  local out = {}
  for _, p in ipairs(doc.projects) do
    if type(p.name) == "string" and type(p.path) == "string" then
      -- left out when never opened (and the zero time from older files)
      local opened = p.last_opened
      if type(opened) ~= "string" or opened:sub(1, 4) == "0001" then
        opened = nil
      end
      table.insert(out, {
        name = p.name,
        path = p.path,
        pinned = p.pinned == true,
        last_opened = opened,
        missing = vim.fn.isdirectory(p.path) == 0,
      })
    end
  end
  -- RFC 3339 in UTC sorts as a string
  table.sort(out, function(a, b)
    if a.pinned ~= b.pinned then
      return a.pinned
    end
    if (a.last_opened or "") ~= (b.last_opened or "") then
      return (a.last_opened or "") > (b.last_opened or "")
    end
    return a.name < b.name
  end)
  return out
end

-- find returns the registered project dir holds, the deepest one when they nest
function M.find(dir)
  dir = vim.fs.normalize(vim.uv.fs_realpath(dir) or dir)
  local best
  for _, p in ipairs(M.list()) do
    if (dir == p.path or vim.startswith(dir, p.path .. "/")) and (not best or #p.path > #best.path) then
      best = p
    end
  end
  return best
end

return M
