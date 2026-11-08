-- Times bud's first screen (D35) for the dashboard's health line: from when
-- bud started (BUD_LAUNCHED, set by nvim.Launch, so the CLI's own time counts)
-- or, for a plain `NVIM_APPNAME=bud nvim`, from init.lua, to the end of
-- Neovim's first redraw. UIEnter and the first draw (the statusline, the
-- dashboard) are inside it; what runs on vim.schedule after it isn't. bud
-- doctor measures the same span from outside (nvim.StartupTime).

local M = {}

-- now is the wall clock in microseconds, the unit BUD_LAUNCHED comes in;
-- swapped in tests
M.now = function()
  local s, us = vim.uv.gettimeofday()
  return s * 1e6 + us
end

-- start records when startup began, for the timing: launched (microseconds
-- since the epoch) when it's from the last 10 seconds, else now, since an
-- older one is usually inherited. M.launched keeps launched as given, for
-- what needs bud's own start however long it took (bud.app)
function M.start(launched)
  local now = M.now()
  launched = tonumber(launched)
  -- as given, however old: bud.app pairs Bud.app's two launches on it
  M.launched = launched
  if launched and launched <= now and now - launched < 10e6 then
    M.began = launched
  else
    M.began = now
  end
end

local ns = vim.api.nvim_create_namespace("bud.startup")
local done = false

-- finish records the time since start in vim.g.bud_startup_ms, once, and
-- then fires User BudStartup for whoever shows it. It runs inside a redraw,
-- where autocmds can't, so they wait a tick
function M.finish()
  if done or not M.began then
    return
  end
  done = true
  vim.g.bud_startup_ms = math.floor((M.now() - M.began) / 1000)
  vim.schedule(function()
    vim.api.nvim_set_decoration_provider(ns, {})
    vim.api.nvim_exec_autocmds("User", { pattern = "BudStartup", modeline = false })
  end)
end

-- watch calls finish at the end of the first redraw. Only with a screen:
-- headless Neovim has no first screen to time
function M.watch()
  if #vim.api.nvim_list_uis() == 0 then
    return
  end
  vim.api.nvim_set_decoration_provider(ns, { on_end = M.finish })
end

return M
