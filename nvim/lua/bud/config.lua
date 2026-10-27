-- Reads the machine config, ~/.local/state/bud/config.json (D13), which the Go
-- CLI writes. Read-only here. (bud.lang reads it on its own, since Go runs that
-- module in a bare nvim with nothing else from bud on the runtimepath.)

local M = {}

-- tests point this elsewhere
M.file = vim.fn.stdpath("state") .. "/config.json"

-- read returns config.json as a table: {} when it's missing or unreadable
function M.read()
  local fd = io.open(M.file, "r")
  if not fd then
    return {}
  end
  local data = fd:read("*a")
  fd:close()
  local ok, cfg = pcall(vim.json.decode, data, { luanil = { object = true, array = true } })
  if not ok or type(cfg) ~= "table" then
    return {}
  end
  return cfg
end

-- get returns the value at a dotted key like "dashboard.style", or default
function M.get(key, default)
  local cur = M.read()
  for part in key:gmatch("[^.]+") do
    if type(cur) ~= "table" then
      return default
    end
    cur = cur[part]
  end
  if cur == nil then
    return default
  end
  return cur
end

return M
