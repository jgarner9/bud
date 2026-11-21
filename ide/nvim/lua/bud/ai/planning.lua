-- Home planning (Suite D49, D110, JGA-293): OpenCode's own TUI attached to the
-- home server's planning OpenCode (`opencode --server <url>`), in a pane of
-- its own. It's a separate process from local coding (opencode.nvim on the
-- laptop's OpenCode), so the two can't share sessions, permissions or paths.
-- <leader>aP and :BudPlan open and hide it. Before starting it, bud checks
-- the server with its password; when the server or the tailnet is down, it
-- says "home planning unavailable: <why>" and local coding carries on.
-- Closing the pane only detaches: the server keeps running.

local M = {}

-- how wide the pane opens: a share of the screen
M.width = 0.4

-- swappable in tests: the command that checks the server and the TUI's
M.curl = "curl"
M.opencode = "opencode"

local state = { status = nil } -- status: "home" while running, "off" after a failed check
local term -- { buf, job }

local function setting(key, default)
  return require("bud.config").get("ai.planning." .. key, default)
end

local function expand(path)
  if path:sub(1, 2) == "~/" then
    return vim.env.HOME .. path:sub(2)
  end
  return path
end

-- password reads ai.planning.password_file, which must be private (0600)
function M.password()
  local file = expand(setting("password_file", "~/.config/bud/planning-password"))
  local stat = vim.uv.fs_stat(file)
  if not stat then
    return nil, ("no password file at %s: put the server's OPENCODE_PASSWORD there, mode 600"):format(file)
  end
  -- the low six bits are the group's and others' permissions
  if stat.mode % 64 ~= 0 then
    return nil, ("%s can be read by others: chmod 600 it"):format(file)
  end
  local lines = vim.fn.readfile(file, "", 1)
  local pw = vim.trim(lines[1] or "")
  if pw == "" then
    return nil, ("%s is empty: put the server's OPENCODE_PASSWORD there"):format(file)
  end
  return pw
end

-- url is ai.planning.url without a trailing slash, or nil and how to set it
function M.url()
  local u = setting("url", "")
  if type(u) ~= "string" or u == "" then
    return nil, "ai.planning.url isn't set: bud config set ai.planning.url http://<server tailnet IP>:8797"
  end
  return (u:gsub("/+$", ""))
end

-- curl_config quotes a value for curl's --config syntax
local function curl_quote(s)
  return '"' .. s:gsub("\\", "\\\\"):gsub('"', '\\"') .. '"'
end

-- check asks the server for /api/info with the password, and calls
-- done(true) or done(false, why) on the main loop. The password goes to curl
-- on stdin, so it never shows in a process listing
function M.check(url, password, done)
  local cmd =
    { M.curl, "-sS", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", "-K", "-", url .. "/api/info" }
  local stdin = "user = " .. curl_quote("opencode:" .. password) .. "\n"
  local ok, err = pcall(vim.system, cmd, { text = true, stdin = stdin }, function(res)
    local code = vim.trim(res.stdout or "")
    local why
    if res.code ~= 0 then
      why = ("can't reach %s: %s"):format(
        url,
        vim.trim(res.stderr or "") ~= "" and vim.trim(res.stderr) or "curl failed"
      )
    elseif code == "401" then
      why = "the server refused the password: check ai.planning.password_file"
    elseif code ~= "200" then
      why = ("%s answered %s, not 200"):format(url, code)
    end
    vim.schedule(function()
      done(why == nil, why)
    end)
  end)
  if not ok then
    vim.schedule(function()
      done(false, ("can't run %s: %s"):format(M.curl, err))
    end)
  end
end

local function unavailable(why)
  state.status = "off"
  vim.notify("bud: home planning unavailable: " .. why, vim.log.levels.WARN)
  vim.cmd.redrawstatus()
end

local function window()
  if not term then
    return nil
  end
  for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if vim.api.nvim_win_get_buf(win) == term.buf then
      return win
    end
  end
end

local function show()
  local width = M.width < 1 and math.floor(vim.o.columns * M.width) or M.width
  local win = vim.api.nvim_open_win(term.buf, true, { split = "right", win = -1, width = width })
  for opt, value in pairs({ winfixwidth = true, number = false, relativenumber = false, signcolumn = "no" }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = win })
  end
  vim.w[win].bud_side = true
  -- locked (D136): only <leader>aP hides it
  require("bud.lock").lock(win, {
    name = "home planning",
    key = "<leader>aP",
    reopen = function()
      return M.open()
    end,
  })
  return win
end

local function closed()
  local t = term
  term = nil
  state.status = nil
  if t and vim.api.nvim_buf_is_valid(t.buf) then
    for _, win in ipairs(vim.fn.win_findbuf(t.buf)) do
      if #vim.api.nvim_list_wins() > 1 then
        require("bud.lock").close(win, true)
      else
        require("bud.lock").release(win)
      end
    end
    vim.api.nvim_buf_delete(t.buf, { force = true })
  end
  vim.cmd.redrawstatus()
end

local function start(url, password)
  term = { buf = vim.api.nvim_create_buf(false, true) }
  vim.bo[term.buf].bufhidden = "hide"
  require("bud.escape").attach(term.buf)
  local win = show()
  vim.api.nvim_win_call(win, function()
    term.job = vim.fn.jobstart({ M.opencode, "--server", url }, {
      term = true,
      -- in the environment, not on the command line (D110)
      env = { OPENCODE_PASSWORD = password },
      on_exit = function()
        vim.schedule(closed)
      end,
    })
  end)
  if term.job <= 0 then
    closed()
    return unavailable(("can't start %s: install OpenCode (bud install)"):format(M.opencode))
  end
  state.status = "home"
  vim.cmd.redrawstatus()
  vim.cmd.startinsert()
end

-- open shows the planning pane, checking the server and starting the TUI
-- when it isn't running
function M.open()
  if term then
    local win = window() or show()
    vim.api.nvim_set_current_win(win)
    vim.cmd.startinsert()
    return
  end
  local url, why = M.url()
  if not url then
    return unavailable(why)
  end
  local password, perr = M.password()
  if not password then
    return unavailable(perr)
  end
  if vim.fn.executable(M.opencode) ~= 1 then
    return unavailable(("%s isn't installed: run bud install"):format(M.opencode))
  end
  M.check(url, password, function(ok, cerr)
    if not ok then
      return unavailable(cerr)
    end
    if not term then
      start(url, password)
    end
  end)
end

-- toggle is <leader>aP and :BudPlan: hide the pane when it's showing (the
-- TUI keeps running), else open it
function M.toggle()
  local win = window()
  if win then
    require("bud.lock").hide(win)
  else
    M.open()
  end
end

-- stop ends the TUI; the server keeps running, since bud didn't start it
function M.stop()
  if term and term.job then
    vim.fn.jobstop(term.job)
  end
end

-- statusline is "plan:home" while the pane's TUI runs and "plan:off" after
-- a failed check, else nothing
function M.statusline()
  if state.status then
    return "plan:" .. state.status
  end
  return ""
end

function M.setup()
  vim.keymap.set("n", "<leader>aP", M.toggle, { desc = "Home planning (OpenCode on the home server)" })
  vim.api.nvim_create_user_command("BudPlan", M.toggle, { desc = "Home planning: open or hide the planning pane" })
  require("bud.statusline").add(M.statusline)
end

return M
