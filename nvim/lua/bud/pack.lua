-- Installs and loads plugins with Neovim's built-in vim.pack (0.12+).
--
-- A spec is a table in a module under lua/bud/plugins/:
--   {
--     src = "owner/repo",                   -- GitHub shorthand, or any git URL
--     name = "repo",                        -- optional, defaults to the repo name
--     version = vim.version.range("1.*"),   -- optional: semver range, tag, branch or commit
--     config = function() ... end,          -- optional, runs once the plugin is added
--     restore = function() ... end,         -- optional, see restore() below
--   }
--
-- Revisions come from nvim-pack-lock.json, which ships inside the bud binary
-- (D5): a missing plugin is installed at its locked revision on startup, and
-- `bud install` / `bud update` run restore() to move every plugin to it. Nothing
-- updates plugins in place; new versions arrive with a bud release.

local M = {}

-- names of the plugins setup() added, the ones whose config failed, and the
-- specs with a restore hook
M.names = {}
M.failed = {}
local restore_hooks = {}

-- bud install / bud update start Neovim with BUD_PACK_RESTORE=1 to restore
-- plugins; configs don't run then, since the plugins may be about to change
-- revision and nobody is looking at the editor
local restoring = vim.env.BUD_PACK_RESTORE == "1"

local function url(src)
  if src:find("://", 1, true) or src:find("@", 1, true) then
    return src
  end
  return "https://github.com/" .. src
end

function M.setup(specs)
  local pack_specs = {}
  for _, spec in ipairs(specs) do
    local src = url(spec.src)
    local name = spec.name or src:gsub("%.git$", ""):match("[^/]+$")
    table.insert(M.names, name)
    table.insert(pack_specs, { src = src, name = name, version = spec.version })
    if spec.restore then
      table.insert(restore_hooks, { name = name, fn = spec.restore })
    end
  end

  -- the plugin set is bud's, not the user's, so there's nothing to confirm
  vim.pack.add(pack_specs, { confirm = false })

  if restoring then
    return
  end

  -- one broken config shouldn't take the rest of the editor down with it
  for i, spec in ipairs(specs) do
    if spec.config then
      local ok, err = pcall(spec.config)
      if not ok then
        table.insert(M.failed, M.names[i])
        vim.notify(("bud: setting up %s failed: %s"):format(M.names[i], err), vim.log.levels.ERROR)
      end
    end
  end
end

-- deferred holds the loaders on_first_use made, by module name, so the smoke
-- test can load each one and check it works
M.deferred = {}

-- on_first_use returns a function that requires module and runs setup on it the
-- first time it's called, then returns the module. Keymaps call it, so a plugin
-- that's slow to load costs nothing until it's used. Full lazy loading is JGA-107
function M.on_first_use(module, setup)
  local loaded
  local function get()
    if not loaded then
      loaded = require(module)
      if setup then
        setup(loaded)
      end
    end
    return loaded
  end
  M.deferred[module] = get
  return get
end

-- restore runs headless from `bud install` and `bud update`, after the config
-- sync: it deletes plugins no spec adds any more (a release dropped them),
-- checks every plugin out at the revision in nvim-pack-lock.json, then runs each
-- spec's restore hook, which may block (blink.cmp downloads its matcher). A
-- failing hook is reported and the restore exits non-zero once the others ran
function M.restore()
  local stale = {}
  for _, plugin in ipairs(vim.pack.get(nil, { info = false })) do
    if not plugin.active then
      table.insert(stale, plugin.spec.name)
    end
  end
  if #stale > 0 then
    vim.pack.del(stale)
  end

  if #M.names > 0 then
    vim.pack.update(nil, { target = "lockfile", force = true })
  end

  local failed = false
  for _, hook in ipairs(restore_hooks) do
    local ok, err = pcall(hook.fn)
    if not ok then
      failed = true
      vim.api.nvim_echo({ { ("bud: restoring %s failed: %s\n"):format(hook.name, err) } }, true, { err = true })
    end
  end
  if failed then
    vim.cmd("cquit 1")
  end
end

return M
