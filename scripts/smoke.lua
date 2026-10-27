-- Run by scripts/smoke.sh inside headless Neovim, after bud's config has loaded.
-- Quits with exit code 1 and a reason on stderr when anything went wrong.

local problems = {}

if vim.v.errmsg ~= "" then
  table.insert(problems, "error during startup: " .. vim.v.errmsg)
end

local ok, pack = pcall(require, "bud.pack")
if not ok then
  table.insert(problems, "bud.pack didn't load: " .. tostring(pack))
else
  local active = {}
  for _, plugin in ipairs(vim.pack.get(nil, { info = false })) do
    if plugin.active then
      active[plugin.spec.name] = true
    else
      table.insert(problems, "plugin on disk but in no spec: " .. plugin.spec.name)
    end
  end
  for _, name in ipairs(pack.names) do
    if not active[name] then
      table.insert(problems, "plugin not installed: " .. name)
    end
  end
  for _, name in ipairs(pack.failed) do
    table.insert(problems, "plugin config failed: " .. name)
  end
  -- lazy plugins and ones loaded on first use would otherwise never run here
  for name in pairs(pack.lazy) do
    local loaded, err = pcall(pack.load, name)
    if not loaded then
      table.insert(problems, "lazy plugin failed to load: " .. name .. ": " .. tostring(err))
    end
  end
  for _, name in ipairs(pack.failed) do
    if not vim.tbl_contains(problems, "plugin config failed: " .. name) then
      table.insert(problems, "plugin config failed: " .. name)
    end
  end
  for module, load in pairs(pack.deferred) do
    local loaded, err = pcall(load)
    if not loaded then
      table.insert(problems, "deferred plugin failed to load: " .. module .. ": " .. tostring(err))
    end
  end
end

-- every shipped spec loads, enabled or not
local lang_ok, lang = pcall(require, "bud.lang")
if not lang_ok then
  table.insert(problems, "bud.lang didn't load: " .. tostring(lang))
else
  lang.specs()
  for _, name in ipairs(lang.failed) do
    table.insert(problems, "language spec failed to load: " .. name)
  end
end

-- the dashboard draws with the real config (offline: no background jobs here)
local dash_ok, dash_err = pcall(function()
  local dashboard = require("bud.dashboard")
  dashboard.open({ offline = true })
  if #(dashboard.lines() or {}) == 0 then
    error("drew nothing")
  end
end)
if not dash_ok then
  table.insert(problems, "dashboard failed: " .. tostring(dash_err))
end

if #problems > 0 then
  -- Neovim's own error output may not end in a newline. selene's Lua 5.1
  -- definitions leave out file methods on io.stderr, so it flags this wrongly
  -- selene: allow(incorrect_standard_library_use)
  io.stderr:write("\n" .. table.concat(problems, "\n") .. "\n")
  vim.cmd("cquit 1")
end
vim.cmd("qall!")
