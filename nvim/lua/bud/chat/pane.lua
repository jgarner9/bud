-- The side-pane chat (BASE.md 5.5, JGA-89): a slim chat on the project's own
-- session, for questions in the middle of work. It's the default layout's
-- chat pane, unless ai.chat.pane is "term" (Claude Code's own TUI, D48).
-- <leader>aa shows and hides it.

local claude = require("bud.ai.claude")
local store = require("bud.chat.store")
local view_mod = require("bud.chat.view")

local M = {}

local views = {} -- root -> view

local function root_of(root)
  root = root or vim.fn.getcwd()
  return vim.uv.fs_realpath(root) or root
end

-- scrollback is what the session has said so far: its own conversation once
-- it has one, else the one the session will resume (claude.resume_args): the
-- project's from an earlier session, else the newest in the project, but not
-- in a shared ~/.claude
local function scrollback(root)
  local s = claude.get(root)
  local dir = store.project_dir(root)
  if s and s.session_id then
    return store.read(dir .. "/" .. s.session_id .. ".jsonl")
  end
  if require("bud.config").get("ai.claude.resume", "continue") ~= "continue" then
    return {}
  end
  local saved = claude.saved_id(root)
  if saved then
    return store.read(dir .. "/" .. saved .. ".jsonl")
  elseif claude.shared() then
    return {}
  end
  local newest
  for _, e in ipairs(store.list(root)) do
    if not newest or e.mtime > newest.mtime then
      newest = e
    end
  end
  return newest and store.read(newest.path) or {}
end

local function window(v)
  return v and v.win and vim.api.nvim_win_is_valid(v.win) and vim.api.nvim_win_get_buf(v.win) == v.transcript and v.win
end

-- open shows root's chat pane (default: the current project's) and returns
-- its window. opts.side and opts.size place it; opts.focus = false leaves the
-- cursor where it was (the layout's panes)
function M.open(opts)
  opts = opts or {}
  view_mod.highlights()
  local root = root_of(opts.root)
  local editor = vim.api.nvim_get_current_win()
  local v = views[root]
  if not v then
    v = view_mod.new({ key = root, root = root, entries = scrollback(root) })
    v.title = vim.fs.basename(root)
    views[root] = v
  end
  v.editor = editor
  local win = window(v)
  if not win then
    local size = opts.size or 60
    win = vim.api.nvim_open_win(v.transcript, false, { split = opts.side or "right", win = -1, width = size })
    vim.wo[win].winfixwidth = true
    v:show(win)
  end
  v:session()
  if opts.focus ~= false then
    v:focus_input()
  end
  return win
end

-- toggle hides the current project's pane when it's showing, else opens it
function M.toggle()
  local v = views[root_of()]
  local win = window(v)
  if win then
    for _, w in ipairs({ v.input_win, win }) do
      if w and vim.api.nvim_win_is_valid(w) then
        vim.api.nvim_win_close(w, true)
      end
    end
  else
    M.open()
  end
end

-- view is root's pane view, for tests
function M.view(root)
  return views[root_of(root)]
end

function M._reset()
  for _, v in pairs(views) do
    v:close()
  end
  views = {}
end

return M
