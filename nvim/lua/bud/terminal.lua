-- A terminal along the bottom of the screen that <leader>t shows and Esc Esc
-- (or <leader>t again) hides (JGA-110). Hiding keeps the shell running, so the
-- next press brings back the same session; it's only replaced once its shell exits.

local M = {}

-- the terminal's height: a share of the screen under 1, else rows. The
-- layout sets it (bud.layout); default_height is what it is without one
M.default_height = 0.35
M.height = M.default_height

local buf, win

local function rows()
  if M.height >= 1 then
    return math.floor(M.height)
  end
  return math.max(3, math.floor((vim.o.lines - vim.o.cmdheight) * M.height))
end

function M.is_open()
  return win ~= nil and vim.api.nvim_win_is_valid(win)
end

function M.toggle()
  if M.is_open() then
    vim.api.nvim_win_hide(win)
    win = nil
    return
  end

  local fresh = not (buf and vim.api.nvim_buf_is_valid(buf))
  if fresh then
    buf = vim.api.nvim_create_buf(false, true)
    -- buffer-local: fzf-lua's pickers are terminals too, and a global Esc Esc
    -- would make a single Esc there wait for a second one
    vim.keymap.set("t", "<Esc><Esc>", M.toggle, { buffer = buf, desc = "Hide terminal" })
  end
  win = vim.api.nvim_open_win(buf, true, { split = "below", win = -1, height = rows() })
  for opt, value in pairs({ winfixheight = true, number = false, relativenumber = false, signcolumn = "no" }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = win })
  end
  if fresh then
    vim.fn.jobstart(vim.o.shell, {
      term = true,
      on_exit = function()
        -- the shell is gone, so the next press starts a new one
        vim.schedule(function()
          if M.is_open() and vim.api.nvim_win_get_buf(win) == buf then
            vim.api.nvim_win_close(win, true)
          end
          if buf and vim.api.nvim_buf_is_valid(buf) then
            vim.api.nvim_buf_delete(buf, { force = true })
          end
          buf, win = nil, nil
        end)
      end,
    })
  end
  vim.cmd.startinsert()
end

-- buf_id is the terminal's buffer, for tests
function M.buf_id()
  return buf
end

function M.setup()
  -- not <leader>t in terminal mode: every space typed in the shell would wait
  -- to see if a t follows, so the terminal hides itself with Esc Esc instead
  vim.keymap.set("n", "<leader>t", M.toggle, { desc = "Terminal" })
end

return M
