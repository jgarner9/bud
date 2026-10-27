-- The language engine (BASE.md 2): reads each spec in languages/ and works out
-- which ones this machine enables. The plugin specs in bud.plugins.lang ask it
-- what to wire up: LSP servers, linters and formatters by filetype, and the
-- parsers and Mason packages to install while restoring.
--
-- A spec is languages/<name>.lua returning a table (see languages/_template.lua).
-- config.json's "languages" list picks the enabled ones; with no list, the
-- profile's preset does: every spec for "full", the ones with minimal = true for
-- "minimal" (D30). Names in the list with no spec are left out quietly and kept
-- in M.unknown, since a spec can be missing until the release that adds it.

local M = {}

-- where specs and the machine config live; tests point these elsewhere
M.dirs = { vim.fn.stdpath("config") .. "/languages" }
M.config_file = vim.fn.stdpath("state") .. "/config.json"

-- specs that failed to load, and enabled names with no spec, by name
M.failed = {}
M.unknown = {}

-- the field each spec may set, with its type; "list" is a list of strings
local fields = {
  filetypes = "list",
  treesitter = "list",
  lsp = "table",
  lint = "list",
  format = "list",
  mason = "list",
  system = "list",
  root = "list",
  minimal = "boolean",
  dap = "string",
}

local function is_list(v)
  if type(v) ~= "table" or not vim.islist(v) then
    return false
  end
  for _, item in ipairs(v) do
    if type(item) ~= "string" then
      return false
    end
  end
  return true
end

-- validate returns an error for the first thing wrong with spec, or nil
local function validate(spec)
  if type(spec) ~= "table" then
    return "should return a table, got " .. type(spec)
  end
  for key, value in pairs(spec) do
    local want = fields[key]
    if not want then
      return ("unknown field %q"):format(tostring(key))
    end
    local ok = want == "list" and is_list(value) or (want ~= "list" and type(value) == want)
    if not ok then
      return ("%s should be a %s"):format(key, want == "list" and "list of strings" or want)
    end
  end
  if not spec.filetypes or #spec.filetypes == 0 then
    return "filetypes is required"
  end
  for server, settings in pairs(spec.lsp or {}) do
    if type(server) ~= "string" or type(settings) ~= "table" then
      return "lsp maps server names to settings tables"
    end
  end
  return nil
end

-- read_config returns the parts of config.json bud.lang uses. A missing file
-- means the defaults; a broken one is reported (bud doctor says so too) and
-- treated as missing, so the editor still starts
local function read_config()
  local fd = io.open(M.config_file, "r")
  if not fd then
    return {}
  end
  local data = fd:read("*a")
  fd:close()
  local ok, cfg = pcall(vim.json.decode, data, { luanil = { object = true, array = true } })
  if not ok or type(cfg) ~= "table" then
    vim.notify(("bud: can't read %s, using the default languages"):format(M.config_file), vim.log.levels.WARN)
    return {}
  end
  return cfg
end

local state

local function load()
  state = { specs = {}, enabled = {}, failed = {}, unknown = {} }

  local files = {}
  for _, dir in ipairs(M.dirs) do
    vim.list_extend(files, vim.fn.glob(dir .. "/*.lua", true, true))
  end
  table.sort(files)
  for _, file in ipairs(files) do
    local name = vim.fn.fnamemodify(file, ":t:r")
    if name:sub(1, 1) ~= "_" and not state.specs[name] then
      local ok, spec = pcall(dofile, file)
      local err = spec
      if ok then
        err = validate(spec)
      end
      if err then
        table.insert(state.failed, name)
        vim.notify(("bud: language %s: %s"):format(name, err), vim.log.levels.ERROR)
      else
        spec.name = name
        state.specs[name] = spec
      end
    end
  end

  local cfg = read_config()
  state.format_on_save = cfg.format_on_save ~= false

  local names = cfg.languages
  if type(names) ~= "table" then
    names = {}
    for name, spec in pairs(state.specs) do
      if cfg.profile ~= "minimal" or spec.minimal then
        table.insert(names, name)
      end
    end
    table.sort(names)
  end
  for _, name in ipairs(names) do
    if state.specs[name] then
      table.insert(state.enabled, state.specs[name])
    elseif not vim.tbl_contains(state.failed, name) then
      table.insert(state.unknown, name)
    end
  end

  M.failed = state.failed
  M.unknown = state.unknown
end

-- reload reads the specs and config.json again
function M.reload()
  load()
end

-- specs returns every valid spec by name, enabled or not
function M.specs()
  if not state then
    load()
  end
  return state.specs
end

-- enabled returns the enabled specs, in config.json's order (by name for a preset)
function M.enabled()
  if not state then
    load()
  end
  return state.enabled
end

function M.format_on_save()
  if not state then
    load()
  end
  return state.format_on_save
end

-- filetypes returns the enabled filetypes as a set
function M.filetypes()
  local set = {}
  for _, spec in ipairs(M.enabled()) do
    for _, ft in ipairs(spec.filetypes) do
      set[ft] = true
    end
  end
  return set
end

-- by_ft maps each enabled filetype to the names a list field holds, for
-- conform's formatters_by_ft and nvim-lint's linters_by_ft. Two specs sharing a
-- filetype get their lists joined, without repeats
function M.by_ft(field)
  local out = {}
  for _, spec in ipairs(M.enabled()) do
    for _, ft in ipairs(spec.filetypes) do
      for _, name in ipairs(spec[field] or {}) do
        out[ft] = out[ft] or {}
        if not vim.tbl_contains(out[ft], name) then
          table.insert(out[ft], name)
        end
      end
    end
  end
  return out
end

-- collect returns every name a list field holds across the enabled specs, without repeats
function M.collect(field)
  local out = {}
  for _, spec in ipairs(M.enabled()) do
    for _, name in ipairs(spec[field] or {}) do
      if not vim.tbl_contains(out, name) then
        table.insert(out, name)
      end
    end
  end
  return out
end

-- servers returns the enabled LSP servers by name, with their settings. A
-- server two specs both set gets their settings merged
function M.servers()
  local out = {}
  for _, spec in ipairs(M.enabled()) do
    for server, settings in pairs(spec.lsp or {}) do
      out[server] = vim.tbl_deep_extend("force", out[server] or {}, settings)
    end
  end
  return out
end

return M
