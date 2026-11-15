-- Locked panes (D136). The explorer, the git pane, the AI pane and the
-- <leader>t terminal are opened and closed by their toggles only. Closing
-- one any other way (:q, :close, <C-w>c, :only from beside it) opens it again
-- at once with an error naming its toggle, and a buffer that lands in one
-- (:e, :b, a picker) is put back and opened in the editor window instead,
-- with the same error. :qa and the quit keys still quit bud, and :q in the
-- last window that isn't a pane takes the panes with it.
--
--   local lock = require("bud.lock")
--   lock.lock(win, { name = "the explorer", key = "<leader>e", reopen = fn, allow = fn })
--   lock.close(win)  -- what a toggle calls instead of nvim_win_close
--   lock.hide(win)   -- ... instead of nvim_win_hide
--
-- reopen opens the pane again and returns its window (which it locks itself,
-- since the pane's own open does). allow(buf) says whether a buffer other than
-- the pane's own may show in it: the explorer's next directory, neo-tree's.

local M = {}

local locked = {} -- win -> { name, key, reopen, allow, buf, tab }
local exiting = false
local suspended = 0

local function err(spec, what)
  vim.notify(("bud: %s is locked; %s. %s opens and closes it"):format(spec.name, what, spec.key), vim.log.levels.ERROR)
end

-- lock marks win as a locked pane. spec: name (as said in errors), key (its
-- toggle), reopen (opens it again), allow (optional, buf -> bool)
function M.lock(win, spec)
  if not (win and vim.api.nvim_win_is_valid(win)) then
    return
  end
  locked[win] = vim.tbl_extend("force", spec, {
    buf = vim.api.nvim_win_get_buf(win),
    tab = vim.api.nvim_win_get_tabpage(win),
  })
  vim.w[win].bud_locked = spec.name
end

-- release unlocks win, so the next close is the toggle's own
function M.release(win)
  if win then
    locked[win] = nil
    if vim.api.nvim_win_is_valid(win) then
      vim.w[win].bud_locked = nil
    end
  end
end

-- close and hide are the toggles' way to put a locked pane away
function M.close(win, force)
  M.release(win)
  if win and vim.api.nvim_win_is_valid(win) then
    vim.api.nvim_win_close(win, force or false)
  end
end

function M.hide(win)
  M.release(win)
  if win and vim.api.nvim_win_is_valid(win) then
    vim.api.nvim_win_hide(win)
  end
end

-- quietly runs fn with the lock off, for code that rearranges every pane
-- (the layout reapplying itself)
function M.quietly(fn, ...)
  suspended = suspended + 1
  local ok, a, b = pcall(fn, ...)
  suspended = suspended - 1
  if not ok then
    error(a, 0)
  end
  return a, b
end

-- locked_spec is win's lock, nil when it isn't locked
function M.spec(win)
  return locked[win]
end

-- editor is the window files go to: the current one when it isn't a pane,
-- else the first ordinary window in the tab that isn't
function M.editor(tab)
  tab = tab or vim.api.nvim_get_current_tabpage()
  local cur = vim.api.nvim_get_current_win()
  local function ok(w)
    return not locked[w]
      and not vim.w[w].bud_side
      and vim.api.nvim_win_get_config(w).relative == ""
      and vim.bo[vim.api.nvim_win_get_buf(w)].buftype == ""
  end
  if vim.api.nvim_win_get_tabpage(cur) == tab and ok(cur) then
    return cur
  end
  for _, w in ipairs(vim.api.nvim_tabpage_list_wins(tab)) do
    if ok(w) then
      return w
    end
  end
end

-- closed collects the panes closed without their toggle in one go (an :only
-- closes several), reopened once each, with one error per pane
local closed = {}

local function reopen_all()
  local specs = closed
  closed = {}
  if exiting then
    return
  end
  local back = vim.api.nvim_get_current_win()
  local seen = {}
  for _, spec in ipairs(specs) do
    -- a pane of two windows (bud's chat) comes back once
    local id = spec.name .. "\0" .. tostring(spec.tab)
    if not seen[id] and vim.api.nvim_tabpage_is_valid(spec.tab) then
      seen[id] = true
      if vim.api.nvim_get_current_tabpage() ~= spec.tab then
        vim.api.nvim_set_current_tabpage(spec.tab)
      end
      local ok, e = pcall(spec.reopen)
      if not ok then
        vim.notify(("bud: reopening %s failed: %s"):format(spec.name, e), vim.log.levels.WARN)
      end
      err(spec, "it only closes with its toggle")
    end
  end
  if vim.api.nvim_win_is_valid(back) then
    vim.api.nvim_set_current_win(back)
  end
end

-- reopened handles a locked pane closed without its toggle: it opens again
local function reopened(win)
  local spec = locked[win]
  locked[win] = nil
  if not spec or exiting or suspended > 0 then
    return
  end
  if #closed == 0 then
    vim.schedule(reopen_all)
  end
  table.insert(closed, spec)
end

-- check puts each locked pane's own buffer back when another one landed in
-- it, opening that one in the editor window
function M.check()
  if suspended > 0 then
    return
  end
  for win, spec in pairs(locked) do
    if not vim.api.nvim_win_is_valid(win) then
      locked[win] = nil
    else
      local buf = vim.api.nvim_win_get_buf(win)
      if buf ~= spec.buf then
        -- its own kind (the explorer's next directory), or its own buffer is
        -- gone (:bd): either way this is the pane's buffer now
        local takes = (spec.allow and spec.allow(buf)) or not vim.api.nvim_buf_is_valid(spec.buf)
        if takes then
          spec.buf = buf
        else
          vim.api.nvim_win_set_buf(win, spec.buf)
          local target = M.editor(spec.tab)
          if target then
            vim.api.nvim_win_set_buf(target, buf)
            vim.api.nvim_set_current_win(target)
            err(
              spec,
              ("%s opened in the editor"):format(
                vim.fn.bufname(buf) ~= "" and vim.fn.fnamemodify(vim.fn.bufname(buf), ":.") or "the buffer"
              )
            )
          else
            err(spec, "its buffer can't change")
          end
        end
      end
    end
  end
end

function M.setup()
  local group = vim.api.nvim_create_augroup("bud.lock", { clear = true })
  vim.api.nvim_create_autocmd("ExitPre", {
    group = group,
    callback = function()
      exiting = true
      -- Neovim may still refuse to exit (unsaved changes): then it's running
      -- a tick later, and the lock is back on
      vim.schedule(function()
        exiting = false
      end)
    end,
  })
  -- :q in the tab's last window that isn't a pane takes the panes with it,
  -- so bud quits (or the tab closes) as it would without them
  vim.api.nvim_create_autocmd("QuitPre", {
    group = group,
    callback = function()
      local cur = vim.api.nvim_get_current_win()
      if locked[cur] or vim.api.nvim_win_get_config(cur).relative ~= "" then
        return
      end
      local panes = {}
      for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
        if w ~= cur and vim.api.nvim_win_get_config(w).relative == "" then
          if not locked[w] then
            return
          end
          table.insert(panes, w)
        end
      end
      local specs = {}
      for _, w in ipairs(panes) do
        table.insert(specs, locked[w])
        M.release(w)
        pcall(vim.api.nvim_win_close, w, true)
      end
      -- Neovim may still refuse the quit (unsaved changes): then the window
      -- is still there and bud didn't exit, so the panes come back
      local tab = vim.api.nvim_get_current_tabpage()
      vim.schedule(function()
        -- not exiting: the ExitPre a refused :q fires has been undone by now
        if not vim.api.nvim_win_is_valid(cur) or not vim.api.nvim_tabpage_is_valid(tab) then
          return
        end
        local seen = {}
        for _, spec in ipairs(specs) do
          if not seen[spec.name] then
            seen[spec.name] = true
            pcall(spec.reopen)
          end
        end
        if vim.api.nvim_win_is_valid(cur) then
          vim.api.nvim_set_current_win(cur)
        end
      end)
    end,
  })
  vim.api.nvim_create_autocmd("WinClosed", {
    group = group,
    callback = function(ev)
      local win = tonumber(ev.match)
      if win and locked[win] then
        reopened(win)
      end
    end,
  })
  vim.api.nvim_create_autocmd("BufWinEnter", {
    group = group,
    callback = function()
      if next(locked) then
        vim.schedule(M.check)
      end
    end,
  })
end

-- _reset forgets every lock, for tests
function M._reset()
  locked, exiting, suspended, closed = {}, false, 0, {}
end

return M
