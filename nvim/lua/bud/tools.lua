-- The tool loader (BASE.md 4, JGA-92). A tool is a dir holding manifest.json
-- (name, description, the commands and keymaps it adds) and init.lua, which
-- returns a table whose setup() runs once when the tool is enabled.
--
-- bud's own tools come synced in ~/.config/bud/tools; yours live in
-- ~/.local/share/bud/user-tools/<name>/, and one of yours wins over bud's of
-- the same name. config.json's tools.disabled turns some off. A tool that
-- fails to load is disabled through the CLI, which owns config.json, with the
-- error kept so bud tools ls can show it, and you get a notification.

local M = {}

-- swappable in tests
M.dirs = function()
  return {
    { dir = vim.fn.stdpath("config") .. "/tools", builtin = true },
    { dir = vim.fn.stdpath("data") .. "/user-tools", builtin = false },
  }
end

-- reserved names in user-tools/ that aren't tools
local reserved = { languages = true }

local loaded = {} -- name -> { tool, module }

local function read_manifest(path)
  local fd = io.open(path, "r")
  if not fd then
    return nil, "no manifest.json"
  end
  local ok, m = pcall(vim.json.decode, fd:read("*a"), { luanil = { object = true, array = true } })
  fd:close()
  if not ok or type(m) ~= "table" then
    return nil, "manifest.json isn't valid JSON"
  end
  return m
end

-- list finds every tool: { name, dir, builtin, manifest, error }, yours over
-- bud's of the same name, sorted by name
function M.list()
  local by_name = {}
  for _, place in ipairs(M.dirs()) do
    for name, kind in vim.fs.dir(place.dir) do
      if (kind == "directory" or kind == "link") and not reserved[name] and not name:match("^[._]") then
        local dir = place.dir .. "/" .. name
        local manifest, err = read_manifest(dir .. "/manifest.json")
        if manifest or vim.uv.fs_stat(dir .. "/init.lua") then
          by_name[name] = { name = name, dir = dir, builtin = place.builtin, manifest = manifest or {}, error = err }
        end
      end
    end
  end
  local out = vim.tbl_values(by_name)
  table.sort(out, function(a, b)
    return a.name < b.name
  end)
  return out
end

local function disabled()
  local list = require("bud.config").get("tools.disabled", {})
  local set = {}
  for _, name in ipairs(type(list) == "table" and list or {}) do
    set[name] = true
  end
  return set
end

-- load runs one tool's init.lua and its setup(). It returns true, or false
-- and the error
function M.load(tool)
  if tool.error then
    return false, tool.error
  end
  local chunk, err = loadfile(tool.dir .. "/init.lua")
  if not chunk then
    return false, err
  end
  local ok, mod = pcall(chunk)
  if not ok then
    return false, mod
  end
  if type(mod) == "table" and type(mod.setup) == "function" then
    local ok_setup, setup_err = pcall(mod.setup, tool.manifest)
    if not ok_setup then
      return false, setup_err
    end
  end
  loaded[tool.name] = { tool = tool, module = mod }
  return true
end

-- broken disables a tool that failed, through the CLI, and says so
local function broken(tool, err)
  local msg = tostring(err):gsub("\n.*", "")
  vim.notify(("bud: the %s tool failed to load, so it's off now: %s"):format(tool.name, msg), vim.log.levels.WARN)
  local bin = require("bud.cli").bin()
  if bin then
    vim.system({ bin, "tools", "disable", "--reason", msg, tool.name })
  end
end

-- setup loads every enabled tool. It runs after the first screen
function M.setup()
  local off = disabled()
  for _, tool in ipairs(M.list()) do
    if not off[tool.name] and not loaded[tool.name] then
      local ok, err = M.load(tool)
      if not ok then
        broken(tool, err)
      end
    end
  end
  vim.api.nvim_create_user_command("BudTools", function()
    local lines = {}
    for _, tool in ipairs(M.list()) do
      local state = loaded[tool.name] and "on" or (off[tool.name] and "off" or "not loaded")
      table.insert(lines, ("%-20s %-10s %s"):format(tool.name, state, tool.manifest.description or ""))
    end
    vim.notify(#lines > 0 and table.concat(lines, "\n") or "no tools", vim.log.levels.INFO)
  end, { desc = "List bud's tools" })
end

-- reload loads one tool again (the generator, after you accept one), no restart
function M.reload(name)
  loaded[name] = nil
  for _, tool in ipairs(M.list()) do
    if tool.name == name then
      return M.load(tool)
    end
  end
  return false, "no tool named " .. name
end

-- is_loaded says whether a tool is running, for tests
function M.is_loaded(name)
  return loaded[name] ~= nil
end

function M._reset()
  loaded = {}
end

return M
