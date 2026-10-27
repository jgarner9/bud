-- Full-screen chat (BASE.md 5.5): a chat that isn't tied to code, in its own
-- tab, so your editor layout stays as it was. <leader>c (or q in the
-- transcript) goes back and forth; <leader>h shows the history panel.
--
-- It runs its own Claude Code session in ~/.local/state/bud/chat/, one
-- conversation at a time, and the history is Claude Code's own store for that
-- dir (bud.chat.store).

local claude = require("bud.ai.claude")
local store = require("bud.chat.store")
local view_mod = require("bud.chat.view")
local icons = require("bud.icons")

local M = {}

-- the chat session's key in bud.ai.claude
M.key = "chat"

local state = {} -- tab, view, history = { buf, win, entries, query }, editor

local function chat_dir()
  local dir = store.chat_dir()
  vim.fn.mkdir(dir, "p")
  return vim.uv.fs_realpath(dir) or dir
end

local function in_chat()
  return state.tab and vim.api.nvim_tabpage_is_valid(state.tab) and vim.api.nvim_get_current_tabpage() == state.tab
end

-- lines_for draws the history panel: grouped by day, pinned on top
function M.history_lines(entries, query, now)
  local lines, rows = {}, {}
  local group
  if query and query ~= "" then
    table.insert(lines, " " .. icons.search .. " " .. query)
    table.insert(lines, "")
  end
  for _, e in ipairs(entries) do
    local g = e.pinned and "Pinned" or store.day(e.mtime, now)
    if g ~= group then
      if group then
        table.insert(lines, "")
      end
      table.insert(lines, " " .. g)
      group = g
    end
    table.insert(lines, (e.current and " ▸ " or "   ") .. (e.pinned and (icons.pinned .. " ") or "") .. e.title)
    rows[#lines] = e
  end
  if #entries == 0 then
    table.insert(lines, query and query ~= "" and "   nothing matches" or "   no chats yet")
  end
  vim.list_extend(lines, { "", " ⏎ open  n new  r rename", " p pin  d delete  / search" })
  return lines, rows
end

local function current_id()
  local s = claude.get(M.key)
  return s and s.session_id
end

local function draw_history()
  local h = state.history
  if not h or not vim.api.nvim_buf_is_valid(h.buf) then
    return
  end
  local entries = store.list(chat_dir())
  if h.query and h.query ~= "" then
    entries = vim.tbl_filter(function(e)
      return store.matches(e, h.query)
    end, entries)
  end
  local id = current_id()
  for _, e in ipairs(entries) do
    e.current = e.id == id
  end
  local lines
  lines, h.rows = M.history_lines(entries, h.query)
  vim.bo[h.buf].modifiable = true
  vim.api.nvim_buf_set_lines(h.buf, 0, -1, false, lines)
  vim.bo[h.buf].modifiable = false
end

-- start puts a conversation in the view: entry's (resumed), or a new one
local function start(entry)
  local v = state.view
  claude.drop(M.key)
  v.session_opts = { chat = true, session_id = entry and entry.id, resume = entry and "continue" or "new" }
  v.title = entry and entry.title or "new chat"
  v:set(entry and store.read(entry.path) or {})
  v:session()
  v:header()
  draw_history()
end

local function row()
  local h = state.history
  return h and h.rows and h.rows[vim.fn.line(".")]
end

local function history_keys(buf)
  local function map(lhs, fn, desc)
    vim.keymap.set("n", lhs, fn, { buffer = buf, nowait = true, desc = desc })
  end
  map("<CR>", function()
    local e = row()
    if e then
      start(e)
      state.view:focus_input()
    end
  end, "Open this chat")
  map("n", function()
    start(nil)
    state.view:focus_input()
  end, "New chat")
  map("r", function()
    local e = row()
    if e then
      vim.ui.input({ prompt = "Rename to: ", default = e.title }, function(title)
        if title then
          store.rename(e.id, vim.trim(title))
          if e.id == current_id() then
            state.view.title = title
            state.view:header()
          end
          draw_history()
        end
      end)
    end
  end, "Rename")
  map("p", function()
    local e = row()
    if e then
      store.pin(e.id, not e.pinned)
      draw_history()
    end
  end, "Pin or unpin")
  map("d", function()
    local e = row()
    if e and vim.fn.confirm(("Delete %q? It's gone from Claude Code too."):format(e.title), "&Yes\n&No", 2) == 1 then
      if e.id == current_id() then
        start(nil)
      end
      store.delete(e)
      draw_history()
    end
  end, "Delete")
  map("/", function()
    vim.ui.input({ prompt = "Search chats: ", default = state.history.query or "" }, function(q)
      if q then
        state.history.query = q
        draw_history()
      end
    end)
  end, "Search")
  map("<Esc>", function()
    state.history.query = nil
    draw_history()
  end, "Clear the search")
  map("q", M.toggle, "Back to the editor")
end

-- history toggles the history panel on the chat tab's left
function M.history()
  if not in_chat() then
    return
  end
  local h = state.history
  if h and h.win and vim.api.nvim_win_is_valid(h.win) then
    vim.api.nvim_win_close(h.win, true)
    h.win = nil
    return
  end
  if not h or not vim.api.nvim_buf_is_valid(h.buf) then
    h = { buf = vim.api.nvim_create_buf(false, true) }
    state.history = h
    vim.bo[h.buf].filetype = "budchathistory"
    history_keys(h.buf)
  end
  h.win = vim.api.nvim_open_win(h.buf, true, { split = "left", win = -1, width = 30 })
  for opt, value in pairs({
    number = false,
    relativenumber = false,
    signcolumn = "no",
    winfixwidth = true,
    cursorline = true,
  }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = h.win })
  end
  vim.wo[h.win].winbar = "%#BudChatTitle# History %*"
  draw_history()
end

-- open goes to the chat tab, making it the first time. opts.resume picks a
-- conversation by title (bud chat --resume), else the last one carries on
function M.open(opts)
  opts = opts or {}
  view_mod.highlights()
  if state.tab and vim.api.nvim_tabpage_is_valid(state.tab) then
    vim.api.nvim_set_current_tabpage(state.tab)
  else
    state.editor = vim.api.nvim_get_current_win()
    vim.cmd.tabnew()
    state.tab = vim.api.nvim_get_current_tabpage()
    local scratch = vim.api.nvim_get_current_buf()
    state.view = view_mod.new({ key = M.key, root = chat_dir(), session = { chat = true } })
    state.view.editor = state.editor
    state.view.on_done = draw_history
    state.view:show(vim.api.nvim_get_current_win())
    if vim.api.nvim_buf_is_valid(scratch) and scratch ~= state.view.transcript then
      vim.api.nvim_buf_delete(scratch, { force = true })
    end
    for _, buf in ipairs({ state.view.transcript, state.view.input }) do
      vim.keymap.set("n", "q", M.toggle, { buffer = buf, nowait = true, desc = "Back to the editor" })
      vim.keymap.set("n", "<leader>h", M.history, { buffer = buf, nowait = true, desc = "Chat history" })
    end
    local entries = store.list(chat_dir())
    local pick
    if opts.resume then
      for _, e in ipairs(entries) do
        if e.title:lower():find(opts.resume:lower(), 1, true) then
          pick = e
          break
        end
      end
      if not pick then
        vim.notify("bud: no chat titled " .. opts.resume, vim.log.levels.WARN)
      end
    else
      -- the newest that isn't just pinned, so it carries on where you were
      table.sort(entries, function(a, b)
        return a.mtime > b.mtime
      end)
      pick = entries[1]
    end
    start(pick)
  end
  state.view:focus_input()
end

-- toggle goes to chat, or back to the editor from it
function M.toggle()
  if in_chat() then
    vim.cmd.stopinsert()
    vim.cmd.tabprevious()
  else
    M.open()
  end
end

-- view is the open chat view, for tests
function M.view()
  return state.view
end

-- _reset closes the chat, for tests
function M._reset()
  if state.view then
    state.view:close()
  end
  claude.drop(M.key)
  state = {}
end

return M
