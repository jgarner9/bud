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
  -- a side pane: above the <leader>t terminal, beside the editor
  vim.w[win].bud_side = true
  require("bud.terminal").arrange()
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
    -- Esc Esc to Normal mode, where <leader>ac hides it and <leader>w moves on
    require("bud.termkeys").attach(t.buf)
    win = show(t, opts.side)
    vim.api.nvim_win_call(win, function()
      t.job = vim.fn.jobstart(argv, {
        term = true,
        cwd = root,
        env = claude.bridge_env(root),
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

-- how long send waits for a TUI it just started to draw, and then for it to
-- settle before the prompt is typed in, in ms; swappable in tests
M.ready_timeout, M.settle = 15000, 300

-- drawn says whether the TUI has put anything on its screen yet
local function drawn(t)
  for _, line in ipairs(vim.api.nvim_buf_get_lines(t.buf, 0, -1, false)) do
    if vim.trim(line) ~= "" then
      return true
    end
  end
  return false
end

-- send types text into root's TUI (default: the current project's) and
-- submits it, opening the TUI first, without taking the cursor, when it
-- isn't showing. A TUI that's just started gets it once it has drawn
function M.send(text, opts)
  opts = opts or {}
  local root = root_of(opts.root)
  if not M.open({ root = root, focus = false }) then
    return false
  end
  local t = terms[root]
  local function type_in()
    if terms[root] ~= t then
      return
    end
    vim.fn.chansend(t.job, text)
    -- a beat apart, so Claude Code reads the Enter as submit, not as part of a paste
    vim.defer_fn(function()
      if terms[root] == t then
        vim.fn.chansend(t.job, "\r")
      end
    end, 50)
  end
  local waited = 0
  local function wait()
    if terms[root] ~= t then
      return
    elseif drawn(t) then
      vim.defer_fn(type_in, M.settle)
    elseif waited < M.ready_timeout then
      waited = waited + 100
      vim.defer_fn(wait, 100)
    else
      vim.notify("bud: Claude Code's TUI didn't start in time", vim.log.levels.WARN)
    end
  end
  wait()
  t.last_used = now()
  return true
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

-- stop_all stops every TUI, for quitting (bud.quit), and returns their jobs
function M.stop_all()
  local jobs = {}
  for _, t in pairs(terms) do
    if t.job then
      vim.fn.jobstop(t.job)
      table.insert(jobs, t.job)
    end
  end
  return jobs
end

-- running says whether root has a TUI open, for tests
function M.running(root)
  return terms[root_of(root)] ~= nil
end

return M
