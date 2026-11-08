-- Bud.app's cold drop (JGA-186). Dropping a file or folder on Bud.app when it
-- isn't running starts kitty with its startup command (the dashboard) and then
-- the drop's launch action, so two windows open. Bud.app tells bud which is
-- which in BUD_APP. Each of the two nvims leaves a record in the state dir,
-- when its bud started and its server's address, and looks for the other's:
-- whichever comes second pairs them, when the two buds started within
-- M.together of each other and the other nvim still answers, and the
-- dashboard then quits, if it's still untouched. No polling, and a record from
-- an earlier launch or a closed window never matches. It never does anything
-- outside Bud.app (no BUD_APP) or headless, and never quits with anything
-- unsaved.

local M = {}

-- how close together the dashboard's bud and the drop's must have started, in
-- seconds (swappable in tests)
M.together = 3

-- role is "start" in a cold start's dashboard, "drop" in a drop's nvim
M.role = nil

-- file is where a role's record lives
function M.file(role)
  return vim.fn.stdpath("state") .. "/app-" .. role
end

-- began is when this nvim's bud started, in seconds (bud.startup reads
-- BUD_LAUNCHED before init.lua clears it)
function M.began()
  return require("bud.startup").began / 1e6
end

-- quit ends this nvim, which closes its Bud window (swappable in tests)
function M.quit()
  pcall(vim.cmd.qall)
end

-- server is this nvim's address, starting a server when it has none
local function server()
  if vim.v.servername ~= "" then
    return vim.v.servername
  end
  local ok, addr = pcall(vim.fn.serverstart)
  return ok and addr or nil
end

-- record is the other role's record when it pairs with this launch, else nil
local function record(role)
  local ok, lines = pcall(vim.fn.readfile, M.file(role))
  if not ok then
    return nil
  end
  local began, addr = tonumber(lines[1] or ""), lines[2]
  if not began or not addr or addr == "" or math.abs(began - M.began()) > M.together then
    return nil
  end
  return addr
end

-- call runs lua in the nvim at addr, and says whether it answered
local function call(addr, lua)
  local ok, ch = pcall(vim.fn.sockconnect, "pipe", addr, { rpc = true })
  if not ok or ch == 0 then
    return false
  end
  local done = pcall(vim.rpcrequest, ch, "nvim_exec_lua", lua, {})
  pcall(vim.fn.chanclose, ch)
  return done
end

-- untouched says whether this nvim still shows only the dashboard. Floating
-- windows don't count (ui2 keeps its message and command-line floats open,
-- and one may have the cursor): it's the one normal window's buffer
local function untouched()
  if #vim.api.nvim_list_tabpages() ~= 1 then
    return false
  end
  local normal = vim.tbl_filter(function(w)
    return vim.api.nvim_win_get_config(w).relative == ""
  end, vim.api.nvim_tabpage_list_wins(0))
  return #normal == 1 and vim.bo[vim.api.nvim_win_get_buf(normal[1])].filetype == "bud-dashboard"
end

-- yield quits a cold start's dashboard that a drop came with, if it's untouched.
-- The drop's nvim calls it over RPC; anything else is left alone
function M.yield()
  if M.role ~= "start" then
    return
  end
  -- after the dashboard has drawn, and after the RPC call has returned
  vim.schedule(function()
    if untouched() then
      M.quit()
    end
  end)
end

-- setup acts on BUD_APP once, from VimEnter, and clears it so nothing started
-- inside Bud (a terminal running bud) inherits it. Headless, it only clears it
function M.setup(opts)
  local kind = vim.env.BUD_APP
  vim.env.BUD_APP = nil
  if (opts and opts.headless) or (kind ~= "start" and kind ~= "drop") then
    return
  end
  M.role = kind
  local addr = server()
  if addr then
    vim.fn.mkdir(vim.fn.fnamemodify(M.file(kind), ":h"), "p")
    vim.fn.writefile({ ("%.6f"):format(M.began()), addr }, M.file(kind))
  end
  local other = record(kind == "start" and "drop" or "start")
  if not other then
    return
  end
  if kind == "drop" then
    call(other, "require('bud.app').yield()")
  elseif call(other, "return true") then
    -- the drop came first and its window is still open
    M.yield()
  end
end

return M
