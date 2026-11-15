-- Hides the cursor in Normal mode (JGA-158, JGA-162) for screens moved
-- through by a highlighted row: the dashboard, the explorer pane. A highlight
-- with blend 100 makes the TUI hide the cursor (:help tui-cursor-shape), and
-- only Normal mode's shape uses it, so insert, visual and the command line
-- keep theirs. Each screen hides it under its own name; it comes back once
-- none of them want it hidden.

local M = {}

M.group = "BudHiddenCursor"

-- owners is who wants it hidden; saved is the user's 'guicursor' to give back
local owners = {}
local saved

function M.hide(owner)
  owners[owner] = true
  if saved then
    return
  end
  vim.api.nvim_set_hl(0, M.group, { blend = 100, nocombine = true, default = true })
  saved = vim.o.guicursor
  vim.o.guicursor = saved .. ",n:block-" .. M.group
end

function M.show(owner)
  owners[owner] = nil
  if saved and next(owners) == nil then
    vim.o.guicursor, saved = saved, nil
  end
end

return M
