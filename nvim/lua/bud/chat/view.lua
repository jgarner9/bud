-- A chat view: a transcript over an input box, on one Claude Code session
-- (bud.ai.claude). Full-screen chat (bud.chat) and the side pane
-- (bud.chat.pane) are both one of these in different windows.
--
-- In the input, Enter sends (Ctrl-j for a new line), and @file, @selection,
-- @project and @diff pull in what you're working on. In the transcript, ge
-- sends the code block under the cursor to the editor, M picks the model,
-- Ctrl-c stops a reply.

local claude = require("bud.ai.claude")
local icons = require("bud.icons")

local M = {}

local ns = vim.api.nvim_create_namespace("bud.chat")

-- models the switcher offers; "default" is whatever Claude Code picks
M.models = { "default", "opus", "sonnet", "haiku" }

local function set_lines(buf, first, last, lines)
  vim.bo[buf].modifiable = true
  vim.api.nvim_buf_set_lines(buf, first, last, false, lines)
  vim.bo[buf].modifiable = false
end

-- render turns entries ({ role, text } or { role = "tool", name, input })
-- into lines and where each label goes, for the transcript
function M.render(entries)
  local lines, labels = {}, {}
  local last_role
  for _, e in ipairs(entries) do
    if e.role == "tool" then
      local detail = e.input and (e.input.file_path or e.input.command or e.input.pattern or e.input.url) or ""
      local line = ("  %s %s %s"):format(icons.tool, e.name, detail):gsub("%s+$", "")
      table.insert(lines, line)
      table.insert(labels, { #lines - 1, "BudChatTool" })
    else
      if #lines > 0 then
        table.insert(lines, "")
      end
      if e.role ~= last_role or e.role == "user" then
        table.insert(lines, e.role == "user" and "you" or (icons.claude .. " claude"))
        table.insert(labels, { #lines - 1, e.role == "user" and "BudChatYou" or "BudChatClaude" })
      end
      vim.list_extend(lines, vim.split(e.text, "\n", { plain = true }))
    end
    last_role = e.role
  end
  return lines, labels
end

-- expand turns @-words in what you typed into Claude Code context: files to
-- mention, and text to add (the diff). editor is the window you came from
function M.expand(text, editor, root)
  local context, extra = {}, {}
  local ebuf = editor and vim.api.nvim_win_is_valid(editor) and vim.api.nvim_win_get_buf(editor)
  local name = ebuf and vim.api.nvim_buf_get_name(ebuf) or ""
  text = text:gsub("@(%a+)", function(word)
    if word == "file" and name ~= "" then
      table.insert(context, name)
      return "@" .. vim.fn.fnamemodify(name, ":t")
    elseif word == "selection" and name ~= "" then
      local s, e = vim.api.nvim_buf_get_mark(ebuf, "<")[1], vim.api.nvim_buf_get_mark(ebuf, ">")[1]
      if s > 0 then
        table.insert(context, ("%s#L%d-%d"):format(name, s, math.max(s, e)))
        return "the selection"
      end
    elseif word == "project" then
      table.insert(context, (root or vim.fn.getcwd()) .. "/")
      return "the project"
    elseif word == "diff" then
      local res = vim.system({ "git", "diff" }, { cwd = root or vim.fn.getcwd(), text = true }):wait()
      if res.code == 0 and res.stdout ~= "" then
        table.insert(extra, "```diff\n" .. res.stdout .. "```")
        return "the diff below"
      end
    end
  end)
  if #extra > 0 then
    text = text .. "\n\n" .. table.concat(extra, "\n\n")
  end
  return text, context
end

-- code_block finds the fenced block around line (1-based) in lines: its
-- language and body, or nil
function M.code_block(lines, line)
  local open, lang
  for i = line, 1, -1 do
    local fence = lines[i]:match("^```(%S*)")
    if fence then
      if i == line or fence ~= "" then
        open, lang = i, fence
      end
      break
    end
  end
  if not open then
    return nil
  end
  for i = open + 1, #lines do
    if lines[i]:match("^```%s*$") then
      if line > i then
        return nil
      end
      return lang, vim.list_slice(lines, open + 1, i - 1)
    end
  end
end

local View = {}
View.__index = View

-- new makes a view on the session under opts.key in opts.root (opts.session:
-- the bud.ai.claude.session options), with entries already said
function M.new(opts)
  local v = setmetatable({ key = opts.key, root = opts.root, session_opts = opts.session or {}, entries = {} }, View)
  v.transcript = vim.api.nvim_create_buf(false, true)
  v.input = vim.api.nvim_create_buf(false, true)
  vim.bo[v.transcript].filetype = "markdown"
  vim.bo[v.transcript].modifiable = false
  vim.bo[v.input].filetype = "markdown"
  vim.b[v.transcript].bud_chat = true
  vim.b[v.input].bud_chat = true
  v:keys()
  v.unsubscribe = {}
  local function mine(fn)
    return function(data, s)
      if s.key == v.key then
        fn(data, s)
      end
    end
  end
  for event, fn in pairs({
    message = function(d)
      v:add({ role = "assistant", text = d.text })
    end,
    tool_use = function(d)
      v:add({ role = "tool", name = d.name, input = d.input })
    end,
    done = function(d)
      if not d.ok and d.text then
        v:add({ role = "assistant", text = "(" .. d.text .. ")" })
      end
      v:header()
      if v.on_done then
        v.on_done(d)
      end
    end,
    status = function()
      v:header()
    end,
  }) do
    table.insert(v.unsubscribe, claude.on(event, mine(fn)))
  end
  v:set(opts.entries or {})
  return v
end

function View:session()
  local o = vim.tbl_extend("force", { key = self.key }, self.session_opts)
  return claude.session(self.root, o)
end

-- set replaces the transcript with entries
function View:set(entries)
  self.entries = entries
  local lines, labels = M.render(entries)
  if #lines == 0 then
    lines = { "", "  [o.o]  ask away. Enter sends, @file @selection @project @diff add context." }
  end
  set_lines(self.transcript, 0, -1, lines)
  vim.api.nvim_buf_clear_namespace(self.transcript, ns, 0, -1)
  for _, l in ipairs(labels) do
    vim.api.nvim_buf_set_extmark(self.transcript, ns, l[1], 0, { line_hl_group = l[2] })
  end
  self:scroll()
end

function View:add(entry)
  table.insert(self.entries, entry)
  self:set(self.entries)
end

function View:scroll()
  for _, win in ipairs(vim.fn.win_findbuf(self.transcript)) do
    vim.api.nvim_win_set_cursor(win, { vim.api.nvim_buf_line_count(self.transcript), 0 })
  end
end

-- header is the transcript's winbar: the title, the model and the state
function View:header()
  local s = claude.get(self.key)
  local status = s and s.status or "stopped"
  local state = ({ busy = "thinking…", starting = "starting…", missing = s and s.why or "not set up" })[status]
  local model = s and s.model and s.model:gsub("^claude%-", "") or "claude"
  local bar = ("%%#BudChatTitle# %s %%*%%=%s%s "):format(self.title or "chat", state and (state .. "  ") or "", model)
  for _, win in ipairs(vim.fn.win_findbuf(self.transcript)) do
    vim.wo[win].winbar = bar
  end
end

function View:send()
  local lines = vim.api.nvim_buf_get_lines(self.input, 0, -1, false)
  local text = vim.trim(table.concat(lines, "\n"))
  if text == "" then
    return
  end
  local sent, context = M.expand(text, self.editor, self.root)
  self:session()
  if claude.send(sent, { key = self.key, context = context }) then
    vim.api.nvim_buf_set_lines(self.input, 0, -1, false, { "" })
    self:add({ role = "user", text = text })
    self:header()
    if self.on_send then
      self.on_send(text)
    end
  end
end

-- to_editor opens the code block under the cursor in the window you came
-- from: a new buffer with its language, or a diff for a patch
function View:to_editor()
  local lines = vim.api.nvim_buf_get_lines(self.transcript, 0, -1, false)
  local lang, body = M.code_block(lines, vim.fn.line("."))
  if not body then
    vim.notify("bud: put the cursor on a code block", vim.log.levels.INFO)
    return
  end
  local win = self.editor and vim.api.nvim_win_is_valid(self.editor) and self.editor
  if win then
    vim.api.nvim_set_current_win(win)
  else
    vim.cmd.tabprevious()
  end
  vim.cmd.new()
  vim.api.nvim_buf_set_lines(0, 0, -1, false, body)
  vim.bo.filetype = lang ~= "" and lang or "text"
  if lang == "diff" or lang == "patch" then
    vim.notify("bud: this is the patch; apply it with :!git apply after saving it", vim.log.levels.INFO)
  end
end

function View:pick_model()
  vim.ui.select(M.models, { prompt = "Model for this chat" }, function(choice)
    if not choice then
      return
    end
    local s = claude.get(self.key)
    local id = s and s.session_id
    self.session_opts.args = choice ~= "default" and { "--model", choice } or nil
    self.session_opts.session_id = id or self.session_opts.session_id
    claude.drop(self.key)
    self:session()
    self:header()
  end)
end

function View:keys()
  local function map(buf, modes, lhs, fn, desc)
    vim.keymap.set(modes, lhs, fn, { buffer = buf, desc = desc, nowait = true })
  end
  map(self.input, { "n", "i" }, "<CR>", function()
    self:send()
  end, "Send")
  map(self.input, "i", "<C-j>", function()
    vim.api.nvim_put({ "", "" }, "c", false, true)
  end, "New line")
  for _, buf in ipairs({ self.input, self.transcript }) do
    map(buf, { "n", "i" }, "<C-c>", function()
      claude.interrupt(self.key)
    end, "Stop the reply")
    map(buf, "n", "M", function()
      self:pick_model()
    end, "Pick the model")
  end
  map(self.transcript, "n", "ge", function()
    self:to_editor()
  end, "Code block to the editor")
  map(self.transcript, "n", "i", function()
    self:focus_input()
  end, "Type a message")
end

-- show puts the transcript in win and the input in a split under it
function View:show(win)
  vim.api.nvim_win_set_buf(win, self.transcript)
  for opt, value in pairs({ number = false, relativenumber = false, signcolumn = "no", wrap = true, linebreak = true }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = win })
  end
  local input = vim.api.nvim_open_win(self.input, false, { split = "below", win = win, height = 3 })
  for opt, value in pairs({
    number = false,
    relativenumber = false,
    signcolumn = "no",
    winfixheight = true,
    wrap = true,
  }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = input })
  end
  vim.wo[input].winbar = "%#BudChatInput# > %* Enter sends · Ctrl-j new line · Ctrl-c stop · M model"
  self.win, self.input_win = win, input
  self:header()
  self:scroll()
  return win, input
end

function View:focus_input()
  if self.input_win and vim.api.nvim_win_is_valid(self.input_win) then
    vim.api.nvim_set_current_win(self.input_win)
    vim.cmd.startinsert({ bang = true })
  end
end

function View:close()
  for _, unsub in ipairs(self.unsubscribe) do
    unsub()
  end
  for _, buf in ipairs({ self.transcript, self.input }) do
    if vim.api.nvim_buf_is_valid(buf) then
      vim.api.nvim_buf_delete(buf, { force = true })
    end
  end
end

-- highlights links the chat's groups to the colorscheme's, so they follow it
function M.highlights()
  for group, link in pairs({
    BudChatYou = "Title",
    BudChatClaude = "Function",
    BudChatTool = "Comment",
    BudChatTitle = "Title",
    BudChatInput = "Special",
  }) do
    vim.api.nvim_set_hl(0, group, { link = link, default = true })
  end
end

return M
