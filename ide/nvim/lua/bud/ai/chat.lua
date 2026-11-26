-- Chat (JGA-165): OpenCode's own TUI in its own tab, in a dir that's no
-- project's, so no project instructions load. Conversations are OpenCode's
-- own, kept for that dir: its /sessions picks an earlier one. When the TUI
-- exits the tab closes, back to where you were (the dashboard, for its c).
--
-- It never touches a project's session: that's bud.ai.term's.

local M = {}

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

-- own says whether chat is bud's own (ai.agent.chat_ui = "bud", D138):
-- bud.ai.view, full screen, on a session in chat's dir
local function own()
  return require("bud.ai.agent").own_chat()
end

-- open goes to the chat tab, starting the TUI when it isn't running
function M.open()
  if own() then
    return require("bud.ai.view").open({ root = chat_dir(), layout = "full" })
  end
  if tab_valid() then
    vim.api.nvim_set_current_tabpage(state.tab)
    vim.cmd.startinsert()
    return
  end
  local spec = require("bud.ai.agent").tui(chat_dir())
  if vim.fn.executable(spec.argv[1]) ~= 1 then
    vim.notify(("bud: %s isn't installed: run bud ai setup"):format(spec.argv[1]), vim.log.levels.WARN)
    return
  end
  vim.cmd.tabnew()
  state = { tab = vim.api.nvim_get_current_tabpage(), buf = vim.api.nvim_get_current_buf() }
  -- jk to Normal mode, where <leader>ch goes back and <leader>w moves on
  require("bud.escape").attach(state.buf)
  for opt, value in pairs({ number = false, relativenumber = false, signcolumn = "no" }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = 0 })
  end
  local mine = state
  state.job = vim.fn.jobstart(spec.argv, {
    term = true,
    cwd = chat_dir(),
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
  if own() then
    local view = require("bud.ai.view")
    if view.showing(chat_dir()) then
      return view.close(chat_dir())
    end
    return M.open()
  end
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
