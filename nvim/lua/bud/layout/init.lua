-- Pane layouts (BASE.md 5.6, D12). A layout is a file in the synced layouts/
-- dir that lists panes around the editor; each pane has a kind, and a kind's
-- provider opens it. A kind with no provider yet (chat, until JGA-89) is
-- skipped, so a layout can name panes before they exist.
--
-- A layout file returns:
--   description  string, for :BudLayout and bud layout ls
--   panes        list of { kind = "tree", side = "left" | "right" | "bottom", size = n }
--                (size is columns, rows for bottom; under 1 it's a share of the screen)
--   terminal     optional { height = n }: the <leader>t terminal's rows, or share under 1
--   diff         optional true: open the working tree's diff in its own tab

local M = {}

-- tests point this at the repo's layouts/
M.dir = vim.fn.stdpath("config") .. "/layouts"

-- the layout last applied, and the pane windows it opened
M.current = nil
local wins = {}

-- providers by kind: { available = fn() -> bool, open = fn(pane) -> win or nil }
M.providers = {}

-- register adds a provider for a kind of pane
function M.register(kind, provider)
  M.providers[kind] = provider
end

-- names lists the layouts in M.dir, sorted
function M.names()
  local names = {}
  for name, kind in vim.fs.dir(M.dir) do
    if kind == "file" and name:match("%.lua$") then
      table.insert(names, (name:gsub("%.lua$", "")))
    end
  end
  table.sort(names)
  return names
end

local fields = { description = "string", panes = "table", terminal = "table", diff = "boolean" }
local pane_fields = { kind = "string", side = "string", size = "number" }
local sides = { left = true, right = true, bottom = true }

-- validate returns an error message for a malformed layout, or nil
function M.validate(spec)
  if type(spec) ~= "table" then
    return "doesn't return a table"
  end
  for k, v in pairs(spec) do
    if not fields[k] then
      return ("unknown field %q"):format(tostring(k))
    elseif type(v) ~= fields[k] then
      return ("%s should be a %s"):format(k, fields[k])
    end
  end
  if type(spec.description) ~= "string" then
    return "needs a description"
  end
  for i, pane in ipairs(spec.panes or {}) do
    if type(pane) ~= "table" then
      return ("pane %d isn't a table"):format(i)
    end
    for k, v in pairs(pane) do
      if not pane_fields[k] then
        return ("pane %d: unknown field %q"):format(i, tostring(k))
      elseif type(v) ~= pane_fields[k] then
        return ("pane %d: %s should be a %s"):format(i, k, pane_fields[k])
      end
    end
    if not pane.kind then
      return ("pane %d needs a kind"):format(i)
    end
    if not sides[pane.side] then
      return ("pane %d: side must be left, right or bottom"):format(i)
    end
    if not pane.size or pane.size <= 0 then
      return ("pane %d needs a size above 0"):format(i)
    end
  end
  if spec.terminal and (type(spec.terminal.height) ~= "number" or spec.terminal.height <= 0) then
    return "terminal.height should be a number above 0"
  end
  return nil
end

-- load reads a layout by name: the spec, or nil and why not
function M.load(name)
  local file = M.dir .. "/" .. name .. ".lua"
  if vim.fn.filereadable(file) == 0 then
    return nil, ("no layout %q (have %s)"):format(name, table.concat(M.names(), ", "))
  end
  local chunk, err = loadfile(file)
  if not chunk then
    return nil, err
  end
  local ok, spec = pcall(chunk)
  if not ok then
    return nil, spec
  end
  local problem = M.validate(spec)
  if problem then
    return nil, ("layout %s: %s"):format(name, problem)
  end
  return spec
end

-- default is the layout to open with: --layout (BUD_LAYOUT), then config.json's
-- layout, then "default"
function M.default()
  local env = vim.env.BUD_LAYOUT
  if env and env ~= "" then
    return env
  end
  return require("bud.config").get("layout", "default")
end

local function cells(size, total)
  if size < 1 then
    return math.max(1, math.floor(total * size))
  end
  return math.floor(size)
end

-- close shuts the pane windows the last apply opened
local function close()
  for _, win in ipairs(wins) do
    if vim.api.nvim_win_is_valid(win) and #vim.api.nvim_tabpage_list_wins(0) > 1 then
      pcall(vim.api.nvim_win_close, win, false)
    end
  end
  wins = {}
end

-- apply lays out the current tab: closes the panes a previous layout opened,
-- opens this one's around the current window and returns focus to it. name is
-- a layout name (default: M.default()); it returns false and notifies when the
-- layout can't be loaded
function M.apply(name)
  require("bud.layout.panes")
  name = name or M.default()
  local spec, err = M.load(name)
  if not spec then
    vim.notify("bud: " .. err, vim.log.levels.WARN)
    return false
  end

  close()
  local editor = vim.api.nvim_get_current_win()
  for _, pane in ipairs(spec.panes or {}) do
    local provider = M.providers[pane.kind]
    if provider and (not provider.available or provider.available()) then
      vim.api.nvim_set_current_win(editor)
      local ok, win = pcall(provider.open, pane)
      if not ok then
        vim.notify(("bud: opening the %s pane failed: %s"):format(pane.kind, win), vim.log.levels.WARN)
      elseif win and vim.api.nvim_win_is_valid(win) then
        if pane.side == "bottom" then
          vim.api.nvim_win_set_height(win, cells(pane.size, vim.o.lines - vim.o.cmdheight))
          vim.wo[win].winfixheight = true
        else
          vim.api.nvim_win_set_width(win, cells(pane.size, vim.o.columns))
          vim.wo[win].winfixwidth = true
        end
        table.insert(wins, win)
      end
    end
  end
  if vim.api.nvim_win_is_valid(editor) then
    vim.api.nvim_set_current_win(editor)
  end

  local terminal = require("bud.terminal")
  terminal.height = spec.terminal and spec.terminal.height or terminal.default_height

  if spec.diff then
    local ok, diff_err = pcall(vim.cmd, "DiffviewOpen")
    if not ok then
      vim.notify("bud: no diff to show: " .. tostring(diff_err), vim.log.levels.INFO)
    end
  end

  M.current = name
  return true
end

-- open_project sets up the window for a project just opened: its saved
-- session when there is one (dashboard.sessions), else the layout. --layout
-- (BUD_LAYOUT) always applies, over the session's windows. opts.files means
-- files were named on the command line, which a session would replace. It
-- returns false when a session took over, true when it was a fresh layout
function M.open_project(path, opts)
  opts = opts or {}
  local explicit = vim.env.BUD_LAYOUT ~= nil and vim.env.BUD_LAYOUT ~= ""
  if not opts.files and require("bud.sessions").restore(path) then
    if not explicit then
      return false
    end
    vim.cmd("silent! only")
  end
  M.apply()
  return true
end

-- reload re-reads the current layout and applies it again, for editing one
-- with bud dev link
function M.reload()
  return M.apply(M.current)
end

-- wins_open is the pane windows the last apply opened, for tests
function M.wins_open()
  return vim.tbl_filter(vim.api.nvim_win_is_valid, wins)
end

return M
