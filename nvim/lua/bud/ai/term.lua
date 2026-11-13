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

-- layout_choice is how the AI chat opens: "pane" (the pull-out) or "full"
local function layout_choice()
  local v = require("bud.config").get("ai.agent.chat_layout", "pane")
  return v == "full" and "full" or "pane"
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
  -- locked (D136): only <leader>ac hides it
  require("bud.lock").lock(win, {
    name = "the AI pane",
    key = "<leader>ac",
    reopen = function()
      return M.open({ root = t.root, side = side, focus = false })
    end,
  })
  require("bud.terminal").arrange()
  return win
end

-- backend is the configured AI backend (D131): "claude" runs Claude Code's
-- own TUI on bud's Claude session, as before; the others run their own TUI
-- (bud.ai.agent.tui), OpenClaw's on the project's Gateway session
local function backend()
  return require("bud.ai.agent").backend()
end

-- available says whether there's a TUI to open, for the layout's chat pane
function M.available()
  if backend() == "claude" then
    return claude.bin() ~= nil and require("bud.config").get("ai.claude.autostart", true)
  end
  -- cheap: the session starts only when the pane opens
  local bin = require("bud.ai.agent").tui_bin()
  return bin ~= nil and vim.fn.executable(bin) == 1
end

-- launch is the TUI's command line and environment for root
local function launch(root)
  if backend() == "claude" then
    local argv, why = claude.handover(root)
    return argv, why, claude.bridge_env(root), true
  end
  local agent = require("bud.ai.agent")
  local bin = agent.tui_bin()
  if bin and vim.fn.executable(bin) ~= 1 then
    return nil, ("%s isn't installed: run bud ai setup"):format(bin)
  end
  local spec, why = agent.tui(root)
  if not spec then
    return nil, why
  end
  return spec.argv, nil, spec.env, false
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
    local argv, why, env, handed = launch(root)
    if not argv then
      vim.notify("bud: " .. why, vim.log.levels.WARN)
      return nil
    end
    t = { buf = vim.api.nvim_create_buf(false, true), last_used = now(), root = root, claude = handed }
    terms[root] = t
    vim.bo[t.buf].bufhidden = "hide"
    -- jk to Normal mode, where <leader>ac hides it and <leader>w moves on
    require("bud.escape").attach(t.buf)
    win = show(t, opts.side)
    vim.api.nvim_win_call(win, function()
      t.job = vim.fn.jobstart(argv, {
        term = true,
        cwd = root,
        env = env,
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
-- submits it (unless opts.submit is false), opening the TUI first, without taking the cursor, when it
-- isn't showing. A TUI that's just started gets it once it has drawn
function M.send(text, opts)
  opts = opts or {}
  local submit = opts.submit ~= false
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
    if not submit then
      return
    end
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

-- full_tab is the tab holding root's TUI full screen, nil when it isn't
local function full_tab(t)
  return t and t.full and vim.api.nvim_tabpage_is_valid(t.full) and t.full or nil
end

-- full shows root's TUI full screen (D137): a tab of its own, the editor and
-- panes left as they were in theirs. Called again from that tab, it goes
-- back, to the pane when ai.agent.chat_layout is "pane"
function M.full(opts)
  opts = opts or {}
  if require("bud.config").get("ai.agent.chat_ui", "tui") == "bud" then
    return require("bud.ai.view").full(opts.root)
  end
  local lock = require("bud.lock")
  local root = root_of(opts.root)
  local t = terms[root]
  local tab = full_tab(t)
  if tab and vim.api.nvim_get_current_tabpage() == tab then
    t.full = nil
    for _, w in ipairs(vim.api.nvim_tabpage_list_wins(tab)) do
      lock.release(w)
    end
    if #vim.api.nvim_list_tabpages() > 1 then
      vim.cmd.tabclose(vim.api.nvim_tabpage_get_number(tab))
    end
    if layout_choice() == "pane" then
      M.open({ root = root })
    end
    return
  elseif tab then
    vim.api.nvim_set_current_tabpage(tab)
    vim.cmd.startinsert()
    return
  end
  -- start it, or take it out of its pane
  if not M.open({ root = root, focus = false }) then
    return
  end
  t = terms[root]
  local pane = window(t)
  if pane then
    lock.hide(pane)
  end
  vim.cmd.tabnew()
  local blank = vim.api.nvim_get_current_buf()
  local win = vim.api.nvim_get_current_win()
  vim.api.nvim_win_set_buf(win, t.buf)
  if blank ~= t.buf and vim.api.nvim_buf_is_valid(blank) then
    vim.api.nvim_buf_delete(blank, { force = true })
  end
  for opt, value in pairs({ number = false, relativenumber = false, signcolumn = "no" }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = win })
  end
  t.full = vim.api.nvim_get_current_tabpage()
  lock.lock(win, {
    name = "the full-screen AI chat",
    key = "<leader>aF",
    reopen = function()
      t.full = nil
      return M.full({ root = root })
    end,
  })
  t.last_used = now()
  vim.cmd.startinsert()
end

-- toggle hides the current project's TUI when it's showing, else opens it,
-- as a pane or full screen by ai.agent.chat_layout (D137)
function M.toggle()
  -- bud's own chat instead of a TUI (D138)
  if require("bud.config").get("ai.agent.chat_ui", "tui") == "bud" then
    return require("bud.ai.view").toggle()
  end
  local t = terms[root_of()]
  if full_tab(t) or layout_choice() == "full" then
    return M.full()
  end
  local win = t and window(t)
  if win then
    require("bud.lock").hide(win)
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
  if t.claude then
    claude.handback(root)
  end
  if vim.api.nvim_buf_is_valid(t.buf) then
    for _, win in ipairs(vim.fn.win_findbuf(t.buf)) do
      if #vim.api.nvim_list_wins() > 1 then
        require("bud.lock").close(win, true)
      else
        require("bud.lock").release(win)
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
