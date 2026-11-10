-- The corner buddy (D108): the dashboard's robot in a small float at the
-- bottom right of the editor, playing the same moves (bud.dashboard.moves,
-- bud.dashboard.player) while you work. <leader>ub turns him on and off for
-- this nvim; ui.buddy = true starts him with the editor. He takes no focus and
-- no keys, steps aside while the dashboard (which has its own) is on screen,
-- and the moves load only once he's turned on, so setup costs the first
-- screen nothing (D35).

local M = {}

local ns = vim.api.nvim_create_namespace("bud_buddy")
local groups = { bot = "BudBuddy", fx = "BudBuddySpark" }

-- the moves and their player, loaded by open
local moves, player

-- state while he's on: { buf, win, stop }; nil when he's off
local current

local function sleepy()
  return tonumber(os.date("%H")) < 5
end

local function dashboard_showing()
  for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if vim.bo[vim.api.nvim_win_get_buf(win)].filetype == "bud-dashboard" then
      return true
    end
  end
  return false
end

-- place is the float's spot: the bottom right corner, above the statusline
-- and the command line
local function place()
  return {
    relative = "editor",
    anchor = "SE",
    row = vim.o.lines - vim.o.cmdheight - 1,
    col = vim.o.columns,
    width = moves.width,
    height = moves.height,
  }
end

local function hide()
  if current.win and vim.api.nvim_win_is_valid(current.win) then
    vim.api.nvim_win_close(current.win, true)
  end
  current.win = nil
end

-- room is whether the editor is big enough to give him a corner
local function room()
  return vim.o.columns >= moves.width * 3 and vim.o.lines >= moves.height * 3
end

-- window opens the float in the current tab page, or moves it back to its
-- corner; false when he's stepping aside
local function window()
  if dashboard_showing() or not room() then
    hide()
    return false
  end
  if current.win and vim.api.nvim_win_is_valid(current.win) then
    if vim.api.nvim_win_get_tabpage(current.win) == vim.api.nvim_get_current_tabpage() then
      vim.api.nvim_win_set_config(current.win, place())
      return true
    end
    hide()
  end
  current.win = vim.api.nvim_open_win(
    current.buf,
    false,
    vim.tbl_extend("force", place(), { style = "minimal", focusable = false, noautocmd = true, zindex = 20 })
  )
  vim.wo[current.win].winhighlight = "NormalFloat:Normal"
  return true
end

-- show draws a frame (nil: standing still) into his buffer
local function show(frame)
  if not window() then
    return
  end
  local asleep = sleepy()
  local lines, marks = {}, {}
  for y, runs in ipairs(moves.draw(frame, asleep and "-.-" or nil, asleep)) do
    local text = ""
    for _, run in ipairs(runs) do
      if run[2] then
        table.insert(marks, { y - 1, #text, #text + #run[1], groups[run[2]] })
      end
      text = text .. run[1]
    end
    lines[y] = text
  end
  vim.api.nvim_buf_set_lines(current.buf, 0, -1, false, lines)
  vim.api.nvim_buf_clear_namespace(current.buf, ns, 0, -1)
  for _, m in ipairs(marks) do
    vim.api.nvim_buf_set_extmark(current.buf, ns, m[1], m[2], { end_col = m[3], hl_group = m[4] })
  end
end

--- on says whether the buddy is turned on (he may still be stepping aside).
function M.on()
  return current ~= nil
end

--- lines returns what he's showing, or nil while he's off or aside; for tests.
function M.lines()
  if not current or not current.win then
    return nil
  end
  return vim.api.nvim_buf_get_lines(current.buf, 0, -1, false)
end

--- open turns the buddy on.
function M.open()
  if current then
    return
  end
  moves = require("bud.dashboard.moves")
  player = require("bud.dashboard.player")
  vim.api.nvim_set_hl(0, "BudBuddy", { link = "Function", default = true })
  vim.api.nvim_set_hl(0, "BudBuddySpark", { link = "DiagnosticWarn", default = true })
  local buf = vim.api.nvim_create_buf(false, true)
  vim.bo[buf].bufhidden = "hide"
  vim.bo[buf].filetype = "bud-buddy"
  current = { buf = buf }
  show(nil)
  current.stop = player.start({ show = show, ready = window, sleepy = sleepy })
  current.resize = vim.api.nvim_create_autocmd({ "VimResized", "TabEnter" }, {
    callback = function()
      show(nil)
    end,
  })
end

--- close turns him off.
function M.close()
  if not current then
    return
  end
  current.stop()
  vim.api.nvim_del_autocmd(current.resize)
  hide()
  vim.api.nvim_buf_delete(current.buf, { force = true })
  current = nil
end

--- toggle turns him on or off.
function M.toggle()
  if current then
    M.close()
  else
    M.open()
  end
end

--- setup maps <leader>ub and, with ui.buddy on, starts him once the first
--- screen is up.
function M.setup()
  vim.keymap.set("n", "<leader>ub", M.toggle, { desc = "Buddy in the corner" })
  vim.api.nvim_create_autocmd("UIEnter", {
    once = true,
    callback = vim.schedule_wrap(function()
      if require("bud.config").get("ui.buddy", false) == true then
        M.open()
      end
    end),
  })
end

return M
