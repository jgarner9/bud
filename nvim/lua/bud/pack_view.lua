-- The plugin list (JGA-166): <leader>P, or P on the dashboard. A float with
-- one row per plugin: loaded or waiting (and on what), its version range and
-- locked revision. Read-only, since versions only change with a bud release
-- (the lockfile); <CR> shows one plugin's details.

local pack = require("bud.pack")
local icons = require("bud.icons")

local M = {}

local function list(v)
  if v == nil then
    return {}
  end
  return type(v) == "table" and v or { v }
end

-- trigger is what a lazy spec waits for: "event InsertEnter", "ft go, lua"
function M.trigger(spec)
  local parts = {}
  for _, field in ipairs({ "event", "ft", "cmd" }) do
    local values = list(spec[field])
    if #values > 0 then
      table.insert(parts, field .. " " .. table.concat(values, ", "))
    end
  end
  local keys = {}
  for _, k in ipairs(list(spec.keys)) do
    table.insert(keys, type(k) == "table" and k[1] or k)
  end
  if #keys > 0 then
    table.insert(parts, "keys " .. table.concat(keys, " "))
  end
  return table.concat(parts, "; ")
end

-- entries is one { name, loaded, trigger, version, rev, path, src } per
-- plugin, sorted by name. plugs is vim.pack.get()'s answer
function M.entries(names, specs, lazy, plugs)
  local by_name = {}
  for _, p in ipairs(plugs or {}) do
    by_name[p.spec.name] = p
  end
  local out = {}
  for _, name in ipairs(names) do
    local spec, plug = specs[name] or {}, by_name[name] or { spec = {} }
    local waiting = lazy[name] and not lazy[name].loaded
    table.insert(out, {
      name = name,
      loaded = not waiting,
      trigger = waiting and M.trigger(spec) or nil,
      version = spec.version and tostring(spec.version) or nil,
      rev = plug.rev,
      path = plug.path,
      src = plug.spec.src or spec.src,
    })
  end
  table.sort(out, function(a, b)
    return a.name:lower() < b.name:lower()
  end)
  return out
end

-- lines lays the entries out as columns: mark, name, revision, version, and
-- what a waiting one loads on
function M.lines(entries)
  local width = 0
  for _, e in ipairs(entries) do
    width = math.max(width, #e.name)
  end
  local lines = {}
  for _, e in ipairs(entries) do
    local mark = e.loaded and "●" or "○"
    local rev = e.rev and e.rev:sub(1, 7) or "       "
    local line = (" %s %s  %s"):format(mark, e.name .. string.rep(" ", width - #e.name), rev)
    if e.version then
      line = line .. "  " .. e.version
    end
    if e.trigger then
      line = line .. "  · on " .. e.trigger
    end
    table.insert(lines, (line:gsub("%s+$", "")))
  end
  return lines
end

local function details(e)
  local lines = {
    " source   " .. (e.src or "?"),
    " revision " .. (e.rev or "?"),
    " version  " .. (e.version or "any (the locked revision)"),
    " path     " .. (e.path and vim.fn.fnamemodify(e.path, ":~") or "?"),
    " status   " .. (e.loaded and "loaded" or ("waiting for " .. e.trigger)),
  }
  require("bud.ui").float({ title = e.name, icon = icons.plugins, lines = lines, footer = "q close", wrap = false })
end

-- open shows the list
function M.open()
  local entries = M.entries(pack.names, pack.specs, pack.lazy, vim.pack.get())
  local loaded = 0
  for _, e in ipairs(entries) do
    loaded = loaded + (e.loaded and 1 or 0)
  end
  local h = require("bud.ui").float({
    title = ("Plugins  %d loaded · %d waiting"):format(loaded, #entries - loaded),
    icon = icons.plugins,
    lines = M.lines(entries),
    footer = "● loaded  ○ waiting   <CR> details   q close",
    cursorline = true,
    wrap = false,
    height = math.min(#entries, 30),
    filetype = "bud-plugins",
  })
  h.map("<CR>", function()
    local e = entries[vim.api.nvim_win_get_cursor(h.win)[1]]
    if e then
      details(e)
    end
  end)
  return h
end

return M
