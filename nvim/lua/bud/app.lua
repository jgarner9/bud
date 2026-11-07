-- Bud.app's cold drop (JGA-186). Dropping a file or folder on Bud.app when it
-- isn't running starts kitty with its startup command (the dashboard) and then
-- the drop's launch action, so two windows open. Bud.app tells bud which is
-- which in BUD_APP: the drop's nvim leaves a marker in the state dir, and the
-- dashboard a cold start showed quits when one turns up in its first seconds,
-- while it's still the untouched dashboard. It never does anything outside
-- Bud.app (no BUD_APP), and never quits with anything unsaved.

local M = {}

-- how long a cold start's dashboard watches for a drop, in ms (swappable in tests)
M.window = 2000
M.interval = 100

-- the marker a drop leaves
function M.file()
  return vim.fn.stdpath("state") .. "/app-drop"
end

-- now is the wall-clock time in seconds, with a fraction (swappable in tests)
function M.now()
  local sec, usec = vim.uv.gettimeofday()
  return sec + usec / 1e6
end

-- quit ends this nvim, which closes its Bud window (swappable in tests)
function M.quit()
  pcall(vim.cmd.qall)
end

-- dropped says whether a drop left its marker at or after since
function M.dropped(since)
  local stat = vim.uv.fs_stat(M.file())
  return stat ~= nil and stat.mtime.sec + stat.mtime.nsec / 1e9 >= since
end

-- untouched says whether this nvim still shows only the dashboard
local function untouched()
  return #vim.api.nvim_list_tabpages() == 1
    and #vim.api.nvim_tabpage_list_wins(0) == 1
    and vim.bo.filetype == "bud-dashboard"
end

-- watch quits a cold start's dashboard if a drop arrives within M.window
local function watch()
  -- a drop is written a moment after this nvim starts, never long before
  local since = M.now() - 1
  local timer = assert(vim.uv.new_timer())
  local waited = 0
  timer:start(
    M.interval,
    M.interval,
    vim.schedule_wrap(function()
      -- a tick already queued when the timer stopped
      if timer:is_closing() then
        return
      end
      waited = waited + M.interval
      if M.dropped(since) then
        timer:close()
        if untouched() then
          M.quit()
        end
      elseif waited >= M.window then
        timer:close()
      end
    end)
  )
end

-- setup acts on BUD_APP once, from VimEnter, and clears it so nothing started
-- inside Bud (a terminal running bud) inherits it
function M.setup()
  local kind = vim.env.BUD_APP
  vim.env.BUD_APP = nil
  if kind == "drop" then
    vim.fn.mkdir(vim.fn.fnamemodify(M.file(), ":h"), "p")
    vim.fn.writefile({}, M.file())
  elseif kind == "start" then
    watch()
  end
end

return M
