-- Bud.app's cold drop (JGA-186). Dropping a file or folder on Bud.app when it
-- isn't running starts kitty with its startup command (the dashboard) and then
-- the drop's launch action, so two windows open. Bud.app tells bud which is
-- which in BUD_APP: the drop's nvim leaves a marker in the state dir with when
-- its bud started and its pid, and the dashboard a cold start showed quits when
-- a drop that started with it turns up, while that drop's window is still open
-- and the dashboard is still untouched. It never does anything outside Bud.app
-- (no BUD_APP) or headless, and never quits with anything unsaved.

local M = {}

-- how long a cold start's dashboard watches for its drop, and how close
-- together the two buds must have started, in ms (swappable in tests)
M.window = 5000
M.together = 2000
M.interval = 100

-- the marker a drop leaves
function M.file()
  return vim.fn.stdpath("state") .. "/app-drop"
end

-- launched is when this nvim's bud started, in seconds: BUD_LAUNCHED (Unix
-- microseconds, set by nvim.Launch), else now (swappable in tests)
function M.launched()
  local us = tonumber(vim.env.BUD_LAUNCHED or "")
  if us then
    return us / 1e6
  end
  local sec, usec = vim.uv.gettimeofday()
  return sec + usec / 1e6
end

-- quit ends this nvim, which closes its Bud window (swappable in tests)
function M.quit()
  pcall(vim.cmd.qall)
end

-- drop is the marker's drop as { launched, pid }, or nil
function M.drop()
  local ok, lines = pcall(vim.fn.readfile, M.file())
  if not ok then
    return nil
  end
  local launched, pid = tonumber(lines[1] or ""), tonumber(lines[2] or "")
  if not launched or not pid then
    return nil
  end
  return { launched = launched, pid = pid }
end

-- came says whether a drop that started together with a bud launched at
-- since is still open: one from an earlier launch, or whose window is gone,
-- would leave Bud with no window at all
local function came(since)
  local d = M.drop()
  return d ~= nil and math.abs(d.launched - since) * 1000 <= M.together and vim.uv.kill(d.pid, 0) == 0
end

-- untouched says whether this nvim still shows only the dashboard. Floating
-- windows don't count: ui2 keeps its message and command-line floats open
local function untouched()
  if #vim.api.nvim_list_tabpages() ~= 1 or vim.bo.filetype ~= "bud-dashboard" then
    return false
  end
  local normal = vim.tbl_filter(function(w)
    return vim.api.nvim_win_get_config(w).relative == ""
  end, vim.api.nvim_tabpage_list_wins(0))
  return #normal == 1
end

-- watch quits a cold start's dashboard when its drop arrives within M.window
local function watch()
  local since = M.launched()
  local timer = assert(vim.uv.new_timer())
  local waited = 0
  timer:start(
    0,
    M.interval,
    vim.schedule_wrap(function()
      -- a tick already queued when the timer stopped
      if timer:is_closing() then
        return
      end
      waited = waited + M.interval
      if came(since) then
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
-- inside Bud (a terminal running bud) inherits it. Headless, it only clears it
function M.setup(opts)
  local kind = vim.env.BUD_APP
  vim.env.BUD_APP = nil
  if opts and opts.headless then
    return
  end
  if kind == "drop" then
    vim.fn.mkdir(vim.fn.fnamemodify(M.file(), ":h"), "p")
    vim.fn.writefile({ ("%.6f"):format(M.launched()), tostring(vim.fn.getpid()) }, M.file())
  elseif kind == "start" then
    watch()
  end
end

return M
