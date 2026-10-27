-- Claude Code's own TUI in a terminal, on the project's session (BASE.md 4.2,
-- D48). Opening it hands the session over from the headless job, so only one
-- of them drives it; when the TUI exits, the session goes back to bud's UIs,
-- picking up the conversation where the TUI left it. <leader>ac shows and
-- hides it; hiding keeps it running, until it's been hidden and unused for
-- ai.claude.idle_minutes (D49).

local claude = require("bud.ai.claude")

local M = {}

-- how wide the pane opens without a layout: a share of the screen
M.width = 0.4

local terms = {} -- root -> { buf, job, last_used }

local function root_of(root)
  root = root or vim.fn.getcwd()
  return vim.uv.fs_realpath(root) or root
end

local function now()
  return vim.uv.hrtime() / 1e6
end

local function window(t)
  for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if vim.api.nvim_win_get_buf(win) == t.buf then
      return win
    end
  end
end

-- show puts the terminal's buffer in a split on side and returns the window
local function show(t, side)
  local width = M.width < 1 and math.floor(vim.o.columns * M.width) or M.width
  local win = vim.api.nvim_open_win(t.buf, false, { split = side or "right", win = -1, width = width })
  for opt, value in pairs({ winfixwidth = true, number = false, relativenumber = false, signcolumn = "no" }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = win })
  end
  return win
end

-- available says whether there's a Claude Code to open, for the layout's chat pane
function M.available()
  return claude.bin() ~= nil and require("bud.config").get("ai.claude.autostart", true)
end

-- open shows root's TUI (default: the current project's), starting it when
-- it isn't running, and returns its window. opts.side is where it goes;
-- opts.focus = false leaves the cursor where it was (the layout's panes)
function M.open(opts)
  opts = opts or {}
  local root = root_of(opts.root)
  local t = terms[root]
  local win = t and window(t)
  if not t then
    local argv, why = claude.handover(root)
    if not argv then
      vim.notify("bud: " .. why, vim.log.levels.WARN)
      return nil
    end
    t = { buf = vim.api.nvim_create_buf(false, true), last_used = now() }
    terms[root] = t
    vim.bo[t.buf].bufhidden = "hide"
    win = show(t, opts.side)
    vim.api.nvim_win_call(win, function()
      t.job = vim.fn.jobstart(argv, {
        term = true,
        cwd = root,
        on_exit = function()
          vim.schedule(function()
            M._closed(root)
          end)
        end,
      })
    end)
    vim.api.nvim_create_autocmd({ "BufEnter", "TermEnter" }, {
      buffer = t.buf,
      callback = function()
        t.last_used = now()
      end,
    })
  elseif not win then
    win = show(t, opts.side)
  end
  t.last_used = now()
  if opts.focus ~= false then
    vim.api.nvim_set_current_win(win)
    vim.cmd.startinsert()
  end
  return win
end

-- toggle hides the current project's TUI when it's showing, else opens it
function M.toggle()
  local t = terms[root_of()]
  local win = t and window(t)
  if win then
    vim.api.nvim_win_hide(win)
  else
    M.open()
  end
end

-- _closed cleans up after the TUI exits and hands the session back
function M._closed(root)
  local t = terms[root]
  if not t then
    return
  end
  terms[root] = nil
  claude.handback(root)
  if vim.api.nvim_buf_is_valid(t.buf) then
    for _, win in ipairs(vim.fn.win_findbuf(t.buf)) do
      if #vim.api.nvim_list_wins() > 1 then
        vim.api.nvim_win_close(win, true)
      end
    end
    vim.api.nvim_buf_delete(t.buf, { force = true })
  end
end

-- reap stops TUIs that are hidden and haven't been used for minutes (D49);
-- one on screen is never idle
function M.reap(minutes, at)
  at = at or now()
  for _, t in pairs(terms) do
    if not window(t) and #vim.fn.win_findbuf(t.buf) == 0 and at - t.last_used > minutes * 60 * 1000 then
      vim.fn.jobstop(t.job)
    end
  end
end

-- running says whether root has a TUI open, for tests
function M.running(root)
  return terms[root_of(root)] ~= nil
end

return M
