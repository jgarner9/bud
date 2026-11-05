-- A terminal along the bottom that <leader>t shows and hides (JGA-110): under
-- the editor and Claude Code's pane, right of the explorer (arrange). jk
-- goes to Normal mode (bud.escape), where <leader>t hides it and <leader>w
-- moves to another window. Hiding keeps the shell running, so the next press
-- brings back the same session; it's only replaced once its shell exits.

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

local function plain(w)
  return vim.api.nvim_win_get_config(w).relative == ""
end

-- arrange lays out the current tab around the terminal: side panes (windows
-- marked vim.w.bud_side, Claude Code's pane) beside the editor, the terminal
-- under both, and the explorer the full height on the left. Nothing changes
-- when the terminal isn't open in this tab
function M.arrange()
  if not M.is_open() or vim.api.nvim_win_get_tabpage(win) ~= vim.api.nvim_get_current_tabpage() then
    return
  end
  local cur = vim.api.nvim_get_current_win()
  local explorer = package.loaded["bud.explorer"]
  local tree = explorer and explorer.win()
  local sides, editor = {}, nil
  for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if w ~= win and w ~= tree and plain(w) then
      if vim.w[w].bud_side then
        table.insert(sides, w)
      elseif not editor or w == cur then
        editor = w
      end
    end
  end
  -- widths to put back: the explorer first, then side panes, which then
  -- take what they need from the editor beside them
  local order = vim.list_extend({ tree }, sides)
  local widths = {}
  for _, w in ipairs(order) do
    widths[w] = vim.api.nvim_win_get_width(w)
  end
  if editor then
    for _, w in ipairs(sides) do
      vim.api.nvim_win_set_config(w, { split = "right", win = editor })
    end
  end
  vim.api.nvim_win_set_config(win, { split = "below", win = -1 })
  if tree then
    vim.api.nvim_win_set_config(tree, { split = "left", win = -1 })
  end
  for _, w in ipairs(order) do
    vim.api.nvim_win_set_width(w, widths[w])
  end
  vim.api.nvim_win_set_height(win, rows())
  if vim.api.nvim_win_is_valid(cur) then
    vim.api.nvim_set_current_win(cur)
  end
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
    require("bud.escape").attach(buf)
  end
  win = vim.api.nvim_open_win(buf, true, { split = "below", win = -1, height = rows() })
  for opt, value in pairs({ winfixheight = true, number = false, relativenumber = false, signcolumn = "no" }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = win })
  end
  M.arrange()
  if fresh then
    -- the shell itself, as :terminal starts it, not under sh -c: bud.quit
    -- tells an idle shell by it having no child processes
    vim.fn.jobstart(vim.split(vim.o.shell, "%s+", { trimempty = true }), {
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

-- job is the terminal's shell job, nil when there's none (bud.quit)
function M.job()
  if buf and vim.api.nvim_buf_is_valid(buf) then
    local chan = vim.bo[buf].channel
    return chan > 0 and chan or nil
  end
  return nil
end

-- buf_id is the terminal's buffer, for tests
function M.buf_id()
  return buf
end

function M.setup()
  -- not <leader>t in terminal mode: every space typed in the shell would wait
  -- to see if a t follows, so jk goes to Normal mode first (bud.escape)
  vim.keymap.set("n", "<leader>t", M.toggle, { desc = "Terminal" })
end

return M
