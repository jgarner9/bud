-- bud's own chat with the agent (D138): the conversation drawn in Neovim from
-- bud.ai.agent's events, on any ACP backend, so it takes bud's colours, keys
-- and layout instead of a backend's TUI. A markdown buffer holds the
-- conversation, replies stream into it and tool calls show as one line each
-- with their state; a prompt line under it sends. It opens as a pane on the
-- right or full screen (ai.agent.chat_layout), locked like the other panes
-- (D136): <leader>av opens and closes it.
--
--   require("bud.ai.view").toggle()        -- <leader>av
--   require("bud.ai.view").open({ layout = "full" })
--
-- In the prompt: <CR> sends, <C-c> stops the reply. In the conversation: q
-- goes back to the editor.

local M = {}

-- how wide the pane is: a share of the screen
M.width = 0.4
-- how tall the prompt is
M.prompt_height = 3

local views = {} -- root -> { log, input, wins = {}, tools = {}, unsub = {} }

local function agent()
  return require("bud.ai.agent")
end

local function root_of(root)
  root = root or vim.fn.getcwd()
  return vim.uv.fs_realpath(root) or root
end

local marks = { pending = "○", in_progress = "◐", completed = "✓", failed = "✗" }

-- append adds lines to the conversation; text joins the last line when
-- joining, as a streamed reply does
local function append(v, lines, joining)
  local buf = v.log
  if not vim.api.nvim_buf_is_valid(buf) then
    return
  end
  vim.bo[buf].modifiable = true
  if joining then
    local last = vim.api.nvim_buf_line_count(buf)
    local cur = vim.api.nvim_buf_get_lines(buf, last - 1, last, false)[1] or ""
    lines = vim.deepcopy(lines)
    lines[1] = cur .. lines[1]
    vim.api.nvim_buf_set_lines(buf, last - 1, last, false, lines)
  else
    vim.api.nvim_buf_set_lines(buf, -1, -1, false, lines)
  end
  vim.bo[buf].modifiable = false
  -- follow the end in every window showing it, unless the cursor is elsewhere
  for _, win in ipairs(vim.fn.win_findbuf(buf)) do
    if win ~= vim.api.nvim_get_current_win() then
      vim.api.nvim_win_set_cursor(win, { vim.api.nvim_buf_line_count(buf), 0 })
    end
  end
end

local function set_line(v, n, text)
  if not vim.api.nvim_buf_is_valid(v.log) then
    return
  end
  vim.bo[v.log].modifiable = true
  vim.api.nvim_buf_set_lines(v.log, n - 1, n, false, { text })
  vim.bo[v.log].modifiable = false
end

local function title(v)
  local s = agent().get(v.root)
  local status = s and s.status or "stopped"
  return (" %s · %s · %s"):format(agent().backend(), vim.fs.basename(v.root), status)
end

local function redraw_title(v)
  for _, win in ipairs(v.wins) do
    if vim.api.nvim_win_is_valid(win) then
      vim.wo[win].winbar = title(v)
    end
  end
end

-- listen hooks the view to its session's events
local function listen(v)
  local function mine(s)
    return s.key == v.root
  end
  local on = agent().on
  v.unsub = {
    on("user_chunk", function(d, s)
      -- an earlier message of yours, as session/load replays the history
      if mine(s) then
        if not v.in_user then
          v.in_user, v.replying = true, false
          append(v, { "", "## you", "", "" })
        end
        append(v, vim.split(d.text, "\n", { plain = true }), true)
      end
    end),
    on("chunk", function(d, s)
      if mine(s) then
        v.in_user = false
        if not v.replying then
          v.replying = true
          append(v, { "", "## " .. agent().backend(), "", "" })
        end
        append(v, vim.split(d.text, "\n", { plain = true }), true)
      end
    end),
    on("tool_use", function(d, s)
      if mine(s) then
        v.replying = false
        local line = ("> %s %s"):format(marks.pending, d.name)
        append(v, { "", line })
        v.tools[d.id] = { line = vim.api.nvim_buf_line_count(v.log), name = d.name }
      end
    end),
    on("tool_update", function(d, s)
      local t = mine(s) and v.tools[d.id]
      if t then
        set_line(v, t.line, ("> %s %s"):format(marks[d.status] or marks.in_progress, t.name))
        if d.patch and d.status == "completed" then
          -- an edit the backend made without asking (D134): show what changed
          local diff = vim.split(vim.trim(d.patch), "\n", { plain = true })
          table.insert(diff, 1, "```diff")
          table.insert(diff, "```")
          append(v, diff)
        end
      end
    end),
    on("done", function(d, s)
      if mine(s) then
        v.replying = false
        if not d.ok then
          append(v, { "", "> ✗ " .. (d.error or d.stop_reason or "stopped") })
        elseif d.stop_reason == "cancelled" then
          append(v, { "", "> stopped" })
        end
        append(v, { "" })
      end
    end),
    on("status", function(_, s)
      if mine(s) then
        redraw_title(v)
      end
    end),
  }
end

-- send sends what's typed in the prompt
local function send(v, text)
  text = vim.trim(text or "")
  if text == "" then
    return
  end
  append(v, { "", "## you", "" })
  append(v, vim.split(text, "\n", { plain = true }))
  v.replying, v.in_user = false, false
  agent().send(text, { root = v.root })
end

-- create makes root's two buffers and hooks them to the session
local function create(root)
  local v = { root = root, tools = {}, wins = {} }
  v.log = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_name(v.log, "bud://chat/" .. vim.fs.basename(root))
  vim.bo[v.log].filetype = "markdown"
  vim.bo[v.log].bufhidden = "hide"
  vim.bo[v.log].modifiable = false
  v.input = vim.api.nvim_create_buf(false, true)
  vim.bo[v.input].buftype = "prompt"
  vim.bo[v.input].bufhidden = "hide"
  vim.fn.prompt_setprompt(v.input, "› ")
  vim.fn.prompt_setcallback(v.input, function(text)
    send(v, text)
  end)
  local function map(buf, mode, lhs, fn, desc)
    vim.keymap.set(mode, lhs, fn, { buffer = buf, desc = desc })
  end
  map(v.input, { "i", "n" }, "<C-c>", function()
    agent().interrupt(root)
  end, "Stop the reply")
  map(v.log, "n", "q", function()
    local editor = require("bud.lock").editor()
    if editor then
      vim.api.nvim_set_current_win(editor)
    end
  end, "Back to the editor")
  map(v.log, "n", "i", function()
    M.focus(root)
  end, "Type a message")
  listen(v)
  append(v, { ("# Chat with %s about %s"):format(agent().backend(), vim.fs.basename(root)) })
  agent().session(root)
  return v
end

local function valid_wins(v)
  return vim.tbl_filter(function(w)
    return vim.api.nvim_win_is_valid(w)
  end, v.wins)
end

-- showing says whether root's view is on screen
function M.showing(root)
  local v = views[root_of(root)]
  return v ~= nil and #valid_wins(v) > 0
end

-- focus puts the cursor in root's prompt, ready to type
function M.focus(root)
  local v = views[root_of(root)]
  if not v then
    return
  end
  for _, w in ipairs(valid_wins(v)) do
    if vim.api.nvim_win_get_buf(w) == v.input then
      vim.api.nvim_set_current_win(w)
      vim.cmd.startinsert({ bang = true })
      return
    end
  end
end

-- open shows root's chat (default: the current project's), as a pane on the
-- right or full screen (opts.layout, else ai.agent.chat_layout), and returns
-- the conversation's window. opts.focus = false doesn't start typing: a pane
-- leaves the cursor where it was, full screen puts it on the conversation
function M.open(opts)
  opts = opts or {}
  local root = root_of(opts.root)
  local v = views[root]
  if not v then
    v = create(root)
    views[root] = v
  end
  if #valid_wins(v) > 0 then
    if opts.focus ~= false then
      M.focus(root)
    end
    return v.wins[1]
  end
  local layout = opts.layout or require("bud.config").get("ai.agent.chat_layout", "pane")
  local back = vim.api.nvim_get_current_win()
  local log_win
  if layout == "full" then
    vim.cmd.tabnew()
    local blank = vim.api.nvim_get_current_buf()
    log_win = vim.api.nvim_get_current_win()
    vim.api.nvim_win_set_buf(log_win, v.log)
    if blank ~= v.log and vim.api.nvim_buf_is_valid(blank) then
      vim.api.nvim_buf_delete(blank, { force = true })
    end
  else
    local width = M.width < 1 and math.floor(vim.o.columns * M.width) or M.width
    log_win = vim.api.nvim_open_win(v.log, false, { split = "right", win = -1, width = width })
    vim.wo[log_win].winfixwidth = true
    vim.w[log_win].bud_side = true
  end
  local input_win = vim.api.nvim_open_win(v.input, false, { split = "below", win = log_win, height = M.prompt_height })
  vim.wo[input_win].winfixheight = true
  vim.w[input_win].bud_side = layout ~= "full" or nil
  for _, w in ipairs({ log_win, input_win }) do
    for opt, value in pairs({ number = false, relativenumber = false, signcolumn = "no", wrap = true }) do
      vim.api.nvim_set_option_value(opt, value, { scope = "local", win = w })
    end
  end
  v.wins = { log_win, input_win }
  v.layout = layout
  redraw_title(v)
  -- locked (D136): only <leader>av closes it
  for _, w in ipairs(v.wins) do
    require("bud.lock").lock(w, {
      name = "the chat",
      key = "<leader>av",
      reopen = function()
        v.wins = vim.tbl_filter(function(x)
          return x ~= w and vim.api.nvim_win_is_valid(x)
        end, v.wins)
        for _, other in ipairs(v.wins) do
          require("bud.lock").close(other)
        end
        v.wins = {}
        return M.open({ root = root, layout = layout, focus = false })
      end,
    })
  end
  if layout ~= "full" then
    pcall(require("bud.terminal").arrange)
  end
  vim.api.nvim_win_set_cursor(log_win, { vim.api.nvim_buf_line_count(v.log), 0 })
  if opts.focus ~= false then
    M.focus(root)
  elseif layout == "full" then
    -- full screen is a tab of its own: stay there, just not typing yet
    vim.api.nvim_set_current_win(log_win)
  elseif vim.api.nvim_win_is_valid(back) then
    vim.api.nvim_set_current_win(back)
  end
  return log_win
end

-- close puts root's chat away; the conversation stays for the next open
function M.close(root)
  local v = views[root_of(root)]
  if not v then
    return
  end
  local lock = require("bud.lock")
  local wins = valid_wins(v)
  v.wins = {}
  if v.layout == "full" and wins[1] then
    local tab = vim.api.nvim_win_get_tabpage(wins[1])
    for _, w in ipairs(wins) do
      lock.release(w)
    end
    if #vim.api.nvim_list_tabpages() > 1 then
      vim.cmd.tabclose(vim.api.nvim_tabpage_get_number(tab))
    end
    return
  end
  for _, w in ipairs(wins) do
    if #vim.api.nvim_list_wins() > 1 then
      lock.close(w)
    else
      lock.release(w)
    end
  end
end

-- full switches root's chat between the pane and full screen (<leader>aF)
function M.full(root)
  root = root_of(root)
  local v = views[root]
  local layout = (v and M.showing(root) and v.layout == "full") and "pane" or "full"
  M.close(root)
  M.open({ root = root, layout = layout })
end

-- toggle is <leader>av: open root's chat, or close it when it's showing
function M.toggle()
  if M.showing() then
    M.close()
  else
    M.open()
  end
end

-- send sends text from root's chat, as typing it in the prompt does,
-- opening the chat first when it's not open
function M.send(text, root)
  root = root_of(root)
  if not views[root] then
    M.open({ root = root, focus = false })
  end
  send(views[root], text)
end

-- lines is the conversation so far, for tests
function M.lines(root)
  local v = views[root_of(root)]
  return v and vim.api.nvim_buf_get_lines(v.log, 0, -1, false) or {}
end

function M.setup()
  vim.keymap.set("n", "<leader>av", M.toggle, { desc = "Chat in bud (any backend)" })
end

-- _reset forgets every view, for tests
function M._reset()
  for root, v in pairs(views) do
    for _, fn in ipairs(v.unsub or {}) do
      fn()
    end
    M.close(root)
  end
  views = {}
end

return M
