-- The explorer pane (JGA-162): oil.nvim in a narrow split on the left, the
-- layout's tree pane. Its cursor is hidden in Normal mode, so j/k and the
-- arrows move a highlighted row; oil's own editing (insert, visual) shows it.
--
--   <leader>e  open it and go there; from it, back to where you came from
--   <leader>E  close it
--
-- <CR> on a file opens it in the window <leader>e was pressed from, not in
-- the pane; on a directory, the pane goes into it.

local cursor = require("bud.ui.cursor")

local M = {}

-- the pane's width in columns, when nothing (a layout) says otherwise
M.width = 24

-- pane is the explorer's window; from is the window to go back to
local pane, from

local function valid(win)
  return win and vim.api.nvim_win_is_valid(win)
end

-- win is the pane's window, or nil when it's closed
function M.win()
  return valid(pane) and pane or nil
end

-- editor is a window to open files in: the one we came from, else the first
-- ordinary window that isn't the pane
local function editor()
  if valid(from) and from ~= pane then
    return from
  end
  for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if win ~= pane and vim.api.nvim_win_get_config(win).relative == "" then
      return win
    end
  end
end

-- open opens the pane on opts.side (left) at opts.size columns, showing the
-- cwd, and returns its window. The cursor stays where it was unless opts.focus
function M.open(opts)
  opts = opts or {}
  if M.win() then
    if opts.focus then
      vim.api.nvim_set_current_win(pane)
    end
    return pane
  end
  local back = vim.api.nvim_get_current_win()
  from = back
  vim.cmd((opts.side == "right" and "botright" or "topleft") .. " vsplit")
  pane = vim.api.nvim_get_current_win()
  vim.w[pane].bud_explorer = true
  vim.api.nvim_win_set_width(pane, opts.size or M.width)
  vim.wo[pane].winfixwidth = true
  vim.wo[pane].cursorline = true
  vim.wo[pane].number = false
  vim.wo[pane].relativenumber = false
  vim.wo[pane].signcolumn = "no"
  require("oil").open(vim.fn.getcwd())
  -- locked (D136): <leader>e and <leader>E open and close it, and a file
  -- that lands in it goes to the editor window
  require("bud.lock").lock(pane, {
    name = "the explorer",
    key = "<leader>e",
    reopen = function()
      return M.open({ side = opts.side, size = opts.size })
    end,
    allow = function(b)
      return vim.bo[b].filetype == "oil"
    end,
  })
  if opts.focus then
    cursor.hide("explorer")
  else
    vim.api.nvim_set_current_win(back)
  end
  return pane
end

-- close closes the pane, going back to where <leader>e came from when the
-- cursor was in it
function M.close()
  if not M.win() then
    return
  end
  local was_in = vim.api.nvim_get_current_win() == pane
  if #vim.api.nvim_tabpage_list_wins(0) > 1 then
    require("bud.lock").close(pane)
  end
  pane = nil
  cursor.show("explorer")
  local back = editor()
  if was_in and back then
    vim.api.nvim_set_current_win(back)
  end
end

-- toggle is <leader>e: open the pane and go there, go there when it's open,
-- or go back from it
function M.toggle()
  local cur = vim.api.nvim_get_current_win()
  if not M.win() then
    M.open({ focus = true })
  elseif cur ~= pane then
    from = cur
    vim.api.nvim_set_current_win(pane)
  else
    local back = editor()
    if back then
      vim.api.nvim_set_current_win(back)
    end
  end
end

-- select is <CR> in an oil buffer: in the pane, a file opens in the editor
-- window and a directory opens in the pane; anywhere else it's oil's own
function M.select()
  local oil = require("oil")
  if vim.api.nvim_get_current_win() ~= M.win() then
    return oil.select()
  end
  local entry, dir = oil.get_cursor_entry(), oil.get_current_dir()
  if not entry or not dir then
    return
  end
  local path = dir .. entry.name
  local target = editor()
  if entry.type == "directory" or vim.fn.isdirectory(path) == 1 or not target then
    return oil.select()
  end
  vim.api.nvim_set_current_win(target)
  vim.cmd.edit(vim.fn.fnameescape(vim.fn.fnamemodify(path, ":.")))
end

-- setup hides the cursor while the pane is the current window and adds the
-- keys; it runs from oil's spec
function M.setup()
  local group = vim.api.nvim_create_augroup("bud.explorer", { clear = true })
  vim.api.nvim_create_autocmd("WinEnter", {
    group = group,
    callback = function()
      if vim.api.nvim_get_current_win() == M.win() then
        cursor.hide("explorer")
      end
    end,
  })
  vim.api.nvim_create_autocmd("WinLeave", {
    group = group,
    callback = function()
      if vim.api.nvim_get_current_win() == M.win() then
        cursor.show("explorer")
      end
    end,
  })
  vim.api.nvim_create_autocmd("WinClosed", {
    group = group,
    callback = function(ev)
      if tonumber(ev.match) == pane then
        pane = nil
        cursor.show("explorer")
      end
    end,
  })
  vim.keymap.set("n", "<leader>e", M.toggle, { desc = "Explorer" })
  vim.keymap.set("n", "<leader>E", M.close, { desc = "Close the explorer" })
end

return M
