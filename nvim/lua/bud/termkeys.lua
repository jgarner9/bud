-- Keys for bud's terminal panes (the <leader>t terminal, Claude Code's TUI
-- pane and chat): Esc Esc leaves the program for Normal mode, where the
-- pane's toggle (<leader>t, <leader>ac, <leader>ch) closes it and <leader>w
-- moves between windows. A single Esc still reaches the program (Claude
-- Code stops a reply on it), just M.window ms later, when no second one
-- came; a plain <Esc><Esc> mapping would hold it for all of 'timeoutlen'.

local M = {}

-- how long a second Esc may take to count as Esc Esc, in ms
M.window = 300

-- normal leaves Terminal mode
local function normal()
  vim.api.nvim_feedkeys(vim.keycode("<C-\\><C-n>"), "n", false)
end

-- attach maps Esc in buf's Terminal mode (buffer-local, so fzf-lua's
-- pickers, which are terminals too, keep a plain Esc)
function M.attach(buf)
  local pending
  vim.keymap.set("t", "<Esc>", function()
    if pending then
      pending:stop()
      if not pending:is_closing() then
        pending:close()
      end
      pending = nil
      normal()
      return
    end
    pending = vim.defer_fn(function()
      pending = nil
      local chan = vim.api.nvim_buf_is_valid(buf) and vim.bo[buf].channel or 0
      if chan > 0 then
        vim.fn.chansend(chan, "\27")
      end
    end, M.window)
  end, { buffer = buf, desc = "Esc to the program; Esc Esc to Normal mode" })
end

return M
