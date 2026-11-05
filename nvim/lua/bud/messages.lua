-- The command line and messages in floats (JGA-137): Neovim's own ui2, which
-- does what noice.nvim did, built in. It's experimental in 0.12, so
-- ui.messages = false in config.json turns it off. Loaded from bud.core, since
-- it has to be on before the first message.
--
-- ui2 always draws the command line along the bottom, so float() (D86) moves
-- its window into a centered, bordered float while you type a command, search
-- or input, and puts it back on leaving, where the command stays visible and
-- messages show as before. ui.cmdline_float = false keeps it at the bottom.

local M = {}

-- the module's name moved during 0.12; nil when this Neovim has neither
function M.module()
  for _, name in ipairs({ "vim._core.ui2", "vim._extui" }) do
    local ok, mod = pcall(require, name)
    if ok and type(mod) == "table" and mod.enable then
      return mod
    end
  end
end

-- the float's title for each command-line type; any other (a debug prompt,
-- an Insert-mode <C-v>) stays at the bottom
M.titles = { [":"] = "Command", ["/"] = "Search", ["?"] = "Search", ["="] = "Expression", ["@"] = "Input" }

-- where the float goes: a quarter of the way down, centered, at most 80 wide
function M.geometry()
  local width = math.max(math.min(80, vim.o.columns - 4), 1)
  return {
    relative = "editor",
    row = math.floor(vim.o.lines * 0.25),
    col = math.max(math.floor((vim.o.columns - width) / 2) - 1, 0),
    width = width,
  }
end

-- rows is how many rows the command line takes in a float width wide: its
-- type character, the text and the cursor cell after it, wrapped
function M.rows(text, width)
  return math.max(math.ceil((vim.fn.strdisplaywidth(text) + 2) / math.max(width, 1)), 1)
end

-- float moves ui2's command-line window (ui2.wins.cmd) while the command line
-- is open. A nested one (<C-r>= inside :) retitles it, and leaving that goes
-- back to the outer one's title, so only the last leave puts it back, the way
-- it was when the first one opened (its config and highlights as ui2 left them)
function M.float(ui2)
  local api = vim.api
  local stack = {}
  local saved
  local group = api.nvim_create_augroup("bud.messages.cmdline", {})
  local function win()
    local w = ui2.wins and ui2.wins.cmd
    return w and api.nvim_win_is_valid(w) and w or nil
  end
  -- where completion menus that read it (blink.cmp, as noice set it) open:
  -- (1, 0)-indexed, the first row under the bottom border. ui2 resizes the
  -- float only after CmdlineChanged, so the rows come from the text
  local function place_menu(w)
    local cfg = api.nvim_win_get_config(w)
    if cfg.relative ~= "editor" then
      -- ui2 has taken it back to the bottom (an expanded message): follow it
      vim.g.ui_cmdline_pos = nil
      return
    end
    local rows = math.max(M.rows(vim.fn.getcmdline(), cfg.width), api.nvim_win_get_height(w))
    vim.g.ui_cmdline_pos = { cfg.row + 2 + rows, cfg.col + 1 }
  end
  local function show(type)
    local w = win()
    if not w then
      return
    end
    if not saved then
      local cfg = api.nvim_win_get_config(w)
      -- ui2 keeps these itself ('cmdheight' and the text decide them)
      cfg.height, cfg.hide = nil, nil
      cfg.title = cfg.title or ""
      saved = { config = cfg, winhighlight = vim.wo[w].winhighlight }
    end
    local cfg = M.geometry()
    cfg.border = "rounded"
    cfg.title = " " .. require("bud.icons").command .. " " .. M.titles[type] .. " "
    cfg.title_pos = "center"
    -- the column the completion menu lines up with: past the left border
    cfg._cmdline_offset = 1
    api.nvim_win_set_config(w, cfg)
    place_menu(w)
    vim.wo[w].winhighlight = "Normal:NormalFloat,FloatBorder:FloatBorder,Search:,CurSearch:,IncSearch:"
  end
  local function restore()
    vim.g.ui_cmdline_pos = nil
    local w, was = win(), saved
    saved = nil
    if not w or not was then
      return
    end
    api.nvim_win_set_config(w, was.config)
    vim.wo[w].winhighlight = was.winhighlight
  end
  api.nvim_create_autocmd("CmdlineEnter", {
    group = group,
    callback = function(ev)
      local type = M.titles[ev.match] and ev.match or false
      table.insert(stack, type)
      if type then
        pcall(show, type)
      end
    end,
  })
  api.nvim_create_autocmd("CmdlineChanged", {
    group = group,
    callback = function()
      local w = win()
      if stack[#stack] and saved and w then
        pcall(place_menu, w)
      end
    end,
  })
  api.nvim_create_autocmd("CmdlineLeave", {
    group = group,
    callback = function()
      table.remove(stack)
      local outer = stack[#stack]
      if outer then
        pcall(show, outer)
      else
        pcall(restore)
      end
    end,
  })
  api.nvim_create_autocmd("VimResized", {
    group = group,
    callback = function()
      if stack[#stack] then
        pcall(show, stack[#stack])
      end
    end,
  })
  return group
end

function M.setup()
  local config = require("bud.config")
  if config.get("ui.messages", true) == false then
    return false
  end
  local ui2 = M.module()
  if not ui2 then
    return false
  end
  -- the defaults: messages in the cmdline area like before, but long ones
  -- collapse to a [+N] marker instead of the hit-enter prompt (g< shows them)
  if not pcall(ui2.enable, {}) then
    return false
  end
  if config.get("ui.cmdline_float", true) ~= false then
    M.float(ui2)
  end
  return true
end

return M
