-- Installs and loads plugins with Neovim's built-in vim.pack (0.12+).
--
-- A spec is a table in a module under lua/bud/plugins/:
--   {
--     src = "owner/repo",                   -- GitHub shorthand, or any git URL or local path
--     name = "repo",                        -- optional, defaults to the repo name
--     version = vim.version.range("1.*"),   -- optional: semver range, tag, branch or commit
--     init = function() ... end,            -- optional, always runs at startup, before loading
--     config = function() ... end,          -- optional, runs once the plugin has loaded
--     restore = function() ... end,         -- optional, see restore() below
--
--     -- any of these makes the plugin lazy: its plugin/ scripts and config
--     -- wait for the first trigger instead of running at startup
--     event = "InsertEnter",                -- autocmd event(s)
--     ft = "go",                            -- filetype(s)
--     cmd = "Name",                         -- user command(s), stubbed until then
--     keys = { { "<leader>x", desc = "...", mode = "n" } },  -- keys, stubbed until then
--   }
--
-- Revisions come from nvim-pack-lock.json, which ships inside the bud binary
-- (D5): a missing plugin is installed at its locked revision on startup, and
-- `bud install` / `bud update` run restore() to move every plugin to it. Nothing
-- updates plugins in place; new versions arrive with a bud release.

local M = {}

-- names of the plugins setup() added, the ones whose config failed, and the
-- lazy ones by name (the smoke test loads each to check it works)
M.names = {}
M.failed = {}
M.lazy = {}
local restore_hooks = {}

-- bud install / bud update start Neovim with BUD_PACK_RESTORE=1 to restore
-- plugins; configs don't run then, since the plugins may be about to change
-- revision and nobody is looking at the editor
local restoring = vim.env.BUD_PACK_RESTORE == "1"

local function url(src)
  if src:find("://", 1, true) or src:find("@", 1, true) or src:sub(1, 1) == "/" then
    return src
  end
  return "https://github.com/" .. src
end

local function list(v)
  if v == nil then
    return {}
  end
  return type(v) == "table" and v or { v }
end

local function is_lazy(spec)
  return spec.event ~= nil or spec.ft ~= nil or spec.cmd ~= nil or spec.keys ~= nil
end

-- one broken config shouldn't take the rest of the editor down with it
local function run_config(name, spec)
  if not spec.config then
    return
  end
  local ok, err = pcall(spec.config)
  if not ok then
    table.insert(M.failed, name)
    vim.notify(("bud: setting up %s failed: %s"):format(name, err), vim.log.levels.ERROR)
  end
end

-- load brings a lazy plugin in: removes its stubs, sources its plugin/ scripts
-- with :packadd, then runs its config. Safe to call more than once
function M.load(name)
  local entry = M.lazy[name]
  if not entry or entry.loaded then
    return
  end
  entry.loaded = true
  -- stubs go first, so the plugin can define the real commands and keys
  for _, remove in ipairs(entry.stubs) do
    pcall(remove)
  end
  vim.cmd.packadd(name)
  run_config(name, entry.spec)
end

-- autocmd groups with a handler for event, so the plugin's new ones can be told apart
local function groups(event)
  local ids = {}
  for _, au in ipairs(vim.api.nvim_get_autocmds({ event = event })) do
    if au.group then
      ids[au.group] = true
    end
  end
  return ids
end

-- after a plugin loads on an event, replay that event for the handlers the
-- plugin just added (and only those), so it sees the event it missed
local function replay(ev, before)
  for id in pairs(groups(ev.event)) do
    if not before[id] then
      local opts = { group = id, modeline = false, data = ev.data }
      if ev.event == "FileType" then
        opts.pattern = ev.match
      else
        opts.buffer = ev.buf
      end
      pcall(vim.api.nvim_exec_autocmds, ev.event, opts)
    end
  end
end

local function stub_events(name, entry, events, pattern)
  local augroup = vim.api.nvim_create_augroup("bud.pack." .. name, { clear = false })
  local id = vim.api.nvim_create_autocmd(events, {
    group = augroup,
    pattern = pattern,
    once = true,
    desc = "load " .. name,
    callback = function(ev)
      local before = groups(ev.event)
      M.load(name)
      replay(ev, before)
    end,
  })
  table.insert(entry.stubs, function()
    vim.api.nvim_del_autocmd(id)
  end)
end

local function stub_command(name, entry, cmd)
  vim.api.nvim_create_user_command(cmd, function(args)
    M.load(name)
    local real = { cmd = cmd, args = args.fargs, bang = args.bang }
    if args.range == 1 then
      real.range = { args.line1 }
    elseif args.range == 2 then
      real.range = { args.line1, args.line2 }
    end
    vim.api.nvim_cmd(real, {})
  end, {
    nargs = "*",
    bang = true,
    range = true,
    desc = "load " .. name,
    -- completing an argument loads the plugin and asks the real command
    complete = function(_, line)
      M.load(name)
      return vim.fn.getcompletion(line, "cmdline")
    end,
  })
  table.insert(entry.stubs, function()
    vim.api.nvim_del_user_command(cmd)
  end)
end

local function stub_key(name, entry, key)
  local lhs = key[1]
  local modes = list(key.mode or "n")
  vim.keymap.set(modes, lhs, function()
    local count = vim.v.count > 0 and tostring(vim.v.count) or ""
    M.load(name)
    -- the config has mapped lhs by now, so replaying it reaches the real mapping
    vim.api.nvim_feedkeys(count .. vim.keycode(lhs), "m", false)
  end, { desc = key.desc })
  table.insert(entry.stubs, function()
    vim.keymap.del(modes, lhs)
  end)
end

function M.setup(specs)
  local pack_specs = {}
  local by_name = {}
  for _, spec in ipairs(specs) do
    local src = url(spec.src)
    local name = spec.name or src:gsub("%.git$", ""):match("[^/]+$")
    table.insert(M.names, name)
    table.insert(pack_specs, { src = src, name = name, version = spec.version })
    by_name[name] = spec
    if spec.restore then
      table.insert(restore_hooks, { name = name, fn = spec.restore })
    end
  end

  -- the plugin set is bud's, not the user's, so there's nothing to confirm.
  -- Eager plugins go on the runtimepath now (:packadd!, so their plugin/
  -- scripts run with Neovim's own); lazy ones wait for M.load. While restoring,
  -- everything goes on the path so restore hooks can require their plugin
  vim.pack.add(pack_specs, {
    confirm = false,
    load = function(plug)
      local spec = by_name[plug.spec.name]
      if restoring or not is_lazy(spec) then
        -- during startup Neovim sources plugin/ itself once init.lua is done
        vim.cmd.packadd({ plug.spec.name, bang = vim.v.vim_did_enter == 0 })
      end
    end,
  })

  if restoring then
    return
  end

  for i, spec in ipairs(specs) do
    local name = M.names[i]
    if spec.init then
      local ok, err = pcall(spec.init)
      if not ok then
        table.insert(M.failed, name)
        vim.notify(("bud: init for %s failed: %s"):format(name, err), vim.log.levels.ERROR)
      end
    end

    if not is_lazy(spec) then
      run_config(name, spec)
    else
      local entry = { spec = spec, stubs = {}, loaded = false }
      M.lazy[name] = entry
      if spec.event then
        stub_events(name, entry, list(spec.event), nil)
      end
      if spec.ft then
        stub_events(name, entry, { "FileType" }, list(spec.ft))
      end
      for _, cmd in ipairs(list(spec.cmd)) do
        stub_command(name, entry, cmd)
      end
      for _, key in ipairs(list(spec.keys)) do
        stub_key(name, entry, key)
      end
    end
  end
end

-- deferred holds the loaders on_first_use made, by module name, so the smoke
-- test can load each one and check it works
M.deferred = {}

-- on_first_use returns a function that requires module and runs setup on it the
-- first time it's called, then returns the module. For a plugin that's on the
-- runtimepath at startup but slow to require (fzf-lua starts a server socket)
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
