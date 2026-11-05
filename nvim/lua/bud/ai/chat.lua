-- Chat (JGA-165): Claude Code's own TUI in chat mode, in its own tab. It runs
-- as bud-ide's chat agent (conversational; it reads and looks things up but
-- doesn't edit or run commands), in a dir that's no project's, so no project
-- instructions load. Conversations are Claude Code's own, kept for that dir:
-- its /resume (or opts.resume) picks an earlier one. When the TUI exits the
-- tab closes, back to where you were (the dashboard, for its c).
--
-- It never touches a project's session: that's bud.ai.term's.

local claude = require("bud.ai.claude")

local M = {}

-- the agent chat runs as, from the bud-ide plugin (D77)
M.agent = "bud-ide:chat"

local state = {} -- tab, buf, job

-- dir is where chat runs, so its conversations aren't any project's;
-- swappable in tests
M.dir = function()
  return vim.fn.stdpath("state") .. "/chat"
end

local function chat_dir()
  local dir = M.dir()
  vim.fn.mkdir(dir, "p")
  return vim.uv.fs_realpath(dir) or dir
end

local function tab_valid()
  return state.tab and vim.api.nvim_tabpage_is_valid(state.tab)
end

-- env is nvim's environment without BUD_BIN, so bud-ide's SessionStart hook
-- leaves out its notes on working inside bud (chat isn't a project)
local function env()
  local e = vim.fn.environ()
  e.BUD_BIN = nil
  return e
end

-- argv is the TUI's command line: as the chat agent when Claude Code has it,
-- else plain Claude Code, saying how to get it
function M.argv(bin, opts)
  local flags = {}
  if claude.plugin_file("bud-ide", "agents/chat.md") then
    vim.list_extend(flags, { "--agent", M.agent })
  else
    vim.notify(
      "bud: Claude Code doesn't have bud-ide's chat agent yet, so this is plain Claude Code: run bud kit sync",
      vim.log.levels.WARN
    )
  end
  if opts.resume then
    table.insert(flags, "--resume")
  end
  return claude.tui_argv(bin, flags)
end

-- open goes to the chat tab, starting the TUI when it isn't running.
-- opts.resume opens Claude Code's picker of earlier chats
function M.open(opts)
  opts = opts or {}
  if tab_valid() then
    vim.api.nvim_set_current_tabpage(state.tab)
    vim.cmd.startinsert()
    return
  end
  local bin = claude.bin()
  if not bin then
    vim.notify("bud: Claude Code isn't installed: run bud claude setup", vim.log.levels.WARN)
    return
  end
  local argv = M.argv(bin, opts)
  vim.cmd.tabnew()
  state = { tab = vim.api.nvim_get_current_tabpage(), buf = vim.api.nvim_get_current_buf() }
  -- Esc Esc to Normal mode, where <leader>ch goes back and <leader>w moves on
  require("bud.termkeys").attach(state.buf)
  for opt, value in pairs({ number = false, relativenumber = false, signcolumn = "no" }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = 0 })
  end
  local mine = state
  state.job = vim.fn.jobstart(argv, {
    term = true,
    cwd = chat_dir(),
    env = env(),
    clear_env = true,
    on_exit = function()
      vim.schedule(function()
        M._closed(mine)
      end)
    end,
  })
  vim.cmd.startinsert()
end

-- toggle goes to chat, or back to the tab you came from
function M.toggle()
  if tab_valid() and vim.api.nvim_get_current_tabpage() == state.tab then
    vim.cmd.stopinsert()
    vim.cmd.tabprevious()
  else
    M.open()
  end
end

-- _closed cleans up after the TUI exits: its tab goes (unless it's the last
-- one) and so does its buffer
function M._closed(s)
  if state == s then
    state = {}
  end
  if s.tab and vim.api.nvim_tabpage_is_valid(s.tab) and #vim.api.nvim_list_tabpages() > 1 then
    vim.cmd.tabclose(vim.api.nvim_tabpage_get_number(s.tab))
  end
  if s.buf and vim.api.nvim_buf_is_valid(s.buf) then
    vim.api.nvim_buf_delete(s.buf, { force = true })
  end
end

-- stop_all stops the chat TUI, for quitting (bud.quit), and returns its job
function M.stop_all()
  if state.job then
    vim.fn.jobstop(state.job)
    return { state.job }
  end
  return {}
end

-- running says whether the chat TUI is open, for tests
function M.running()
  return state.job ~= nil
end

-- _reset stops the chat, for tests
function M._reset()
  if state.job then
    vim.fn.jobstop(state.job)
    vim.fn.jobwait({ state.job }, 2000)
  end
  M._closed(state)
  state = {}
end

return M
