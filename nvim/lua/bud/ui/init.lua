-- bud's dialogs, menus, inputs and panels (JGA-136), on nui.nvim, so every
-- Claude screen and custom menu looks and behaves the same: rounded borders,
-- a title with bud's accent, the keys in the footer, q or <Esc> to close.
--
--   ui.float({ title, lines, footer, width, height, enter })  -> handle
--   ui.dialog({ title, lines, actions = {{ key, label, fn }}, cancel }) -> handle
--   ui.menu(items, { title, format }, on_choice)    -- items any values
--   ui.input({ title, default }, on_confirm)
--   ui.panel({ title, dir, boxes = {{ name, size, title, lines, enter }} }) -> handle
--
-- setup() makes vim.ui.select and vim.ui.input use menu and input, so every
-- picker in bud (and in plugins) gets the same look. A handle has buf, win,
-- map(lhs, fn) and close(); a panel's has boxes[name] instead of buf and win.

local M = {}

local function icons()
  return require("bud.icons")
end

-- title is what a title or footer shows: padded, and nil for nothing
local function text(s)
  if not s or s == "" then
    return nil
  end
  return " " .. s .. " "
end

-- the border every bud float has; title gets the robot's accent
local function border(opts)
  local top = opts.title and text((opts.icon or icons().claude) .. " " .. opts.title) or nil
  return {
    style = "rounded",
    padding = opts.padding,
    text = {
      top = top,
      top_align = "center",
      bottom = text(opts.footer),
      bottom_align = "center",
    },
  }
end

-- width fits lines (and the title and footer), within the editor
local function fit_width(lines, opts)
  local width = opts.min_width or 30
  for _, l in ipairs(lines) do
    width = math.max(width, vim.fn.strdisplaywidth(l) + 2)
  end
  for _, s in ipairs({ opts.title, opts.footer }) do
    width = math.max(width, vim.fn.strdisplaywidth(s or "") + 6)
  end
  return math.min(opts.width or width, vim.o.columns - 4)
end

local function handle(popup)
  local h = { popup = popup }
  h.buf, h.win = popup.bufnr, popup.winid
  function h.map(lhs, fn, mode)
    popup:map(mode or "n", lhs, fn, { noremap = true, nowait = true })
  end
  function h.close()
    popup:unmount()
  end
  return h
end

-- float shows lines in a centred popup. Leaving it (q, <Esc>, or the window
-- closing any other way) runs opts.on_close once
function M.float(opts)
  local Popup = require("nui.popup")
  local lines = opts.lines or {}
  local height = math.min(math.max(#lines, opts.height or 1), vim.o.lines - 6)
  local popup = Popup({
    enter = opts.enter ~= false,
    focusable = true,
    relative = "editor",
    position = "50%",
    size = { width = fit_width(lines, opts), height = height },
    border = border(opts),
    buf_options = { modifiable = true, bufhidden = "wipe", filetype = opts.filetype or "bud-ui" },
    win_options = { wrap = opts.wrap ~= false, cursorline = opts.cursorline or false },
  })
  popup:mount()
  vim.api.nvim_buf_set_lines(popup.bufnr, 0, -1, false, lines)
  if opts.modifiable ~= true then
    vim.bo[popup.bufnr].modifiable = false
  end
  local ns = vim.api.nvim_create_namespace("bud.ui")
  for _, hl in ipairs(opts.highlights or {}) do
    vim.api.nvim_buf_set_extmark(popup.bufnr, ns, hl[1], 0, { line_hl_group = hl[2] })
  end
  local h = handle(popup)
  local closed = false
  local function on_close()
    if closed then
      return
    end
    closed = true
    if opts.on_close then
      opts.on_close()
    end
  end
  h.closed = function()
    return closed
  end
  -- closing any way at all counts, so nothing waits on an answer forever
  popup:on("BufWipeout", function()
    vim.schedule(on_close)
  end, { once = true })
  if opts.close_keys ~= false then
    for _, lhs in ipairs({ "q", "<Esc>" }) do
      h.map(lhs, h.close)
    end
  end
  return h
end

-- actions_footer is the keys line: "y allow  n deny"
function M.actions_footer(actions)
  local parts = {}
  for _, a in ipairs(actions) do
    if a.label then
      -- an action with several keys shows the first
      table.insert(parts, (type(a.key) == "table" and a.key[1] or a.key) .. " " .. a.label)
    end
  end
  return table.concat(parts, "   ")
end

-- dialog is a float whose keys are its actions: pressing one closes it and
-- runs its fn. Closing it any other way runs opts.cancel, so a question
-- always gets an answer
function M.dialog(opts)
  local answered = false
  local h
  local function run(fn)
    if answered then
      return
    end
    answered = true
    h.close()
    if fn then
      fn()
    end
  end
  local float = vim.tbl_extend("force", {}, opts, {
    footer = opts.footer or M.actions_footer(opts.actions or {}),
    on_close = function()
      if not answered then
        answered = true
        if opts.cancel then
          opts.cancel()
        end
      end
    end,
  })
  h = M.float(float)
  for _, a in ipairs(opts.actions or {}) do
    for _, lhs in ipairs(type(a.key) == "table" and a.key or { a.key }) do
      h.map(lhs, function()
        run(a.fn)
      end)
    end
  end
  return h
end

-- menu picks one of items. format turns an item into its line (tostring by
-- default); the first 9 also get number keys. on_choice gets the item and its
-- index, or nil when closed without one
function M.menu(items, opts, on_choice)
  opts = opts or {}
  local Menu = require("nui.menu")
  local format = opts.format or tostring
  local lines, width = {}, 0
  for i, item in ipairs(items) do
    local label = (i <= 9 and (i .. "  ") or "   ") .. format(item)
    table.insert(lines, Menu.item(label, { index = i }))
    width = math.max(width, vim.fn.strdisplaywidth(label))
  end
  local chosen = false
  local function choose(i)
    if chosen then
      return
    end
    chosen = true
    on_choice(i and items[i] or nil, i)
  end
  local menu = Menu({
    relative = "editor",
    position = "50%",
    size = {
      width = math.min(math.max(width + 2, vim.fn.strdisplaywidth(opts.title or "") + 6, 30), vim.o.columns - 4),
      height = math.min(#items, vim.o.lines - 8),
    },
    border = border({ title = opts.title, icon = opts.icon or icons().todo, footer = "<CR> pick   q close" }),
    buf_options = { filetype = "bud-ui" },
    win_options = { cursorline = true },
  }, {
    lines = lines,
    keymap = {
      focus_next = { "j", "<Down>", "<Tab>", "<C-n>" },
      focus_prev = { "k", "<Up>", "<S-Tab>", "<C-p>" },
      close = { "q", "<Esc>", "<C-c>" },
      submit = { "<CR>", "<Space>" },
    },
    on_submit = function(item)
      choose(item.index)
    end,
    on_close = function()
      vim.schedule(function()
        choose(nil)
      end)
    end,
  })
  menu:mount()
  -- leaving it is closing it, so the caller always hears back
  menu:on("BufLeave", function()
    menu:unmount()
    vim.schedule(function()
      choose(nil)
    end)
  end, { once = true })
  for i = 1, math.min(#items, 9) do
    menu:map("n", tostring(i), function()
      chosen = true
      menu:unmount()
      on_choice(items[i], i)
    end, { nowait = true })
  end
  return handle(menu)
end

-- input asks for one line. on_confirm gets the text, or nil when cancelled
function M.input(opts, on_confirm)
  opts = opts or {}
  local Input = require("nui.input")
  local title = (opts.title or opts.prompt or ""):gsub("[:%s]+$", "")
  local done = false
  local function finish(value)
    if done then
      return
    end
    done = true
    on_confirm(value)
  end
  local input = Input({
    relative = "editor",
    position = "50%",
    size = { width = math.min(math.max(60, vim.fn.strdisplaywidth(opts.default or "") + 4), vim.o.columns - 4) },
    border = border({ title = title, icon = opts.icon or icons().scratchpad, footer = "<CR> ok   <Esc> cancel" }),
    buf_options = { filetype = "bud-ui-input" },
  }, {
    prompt = " ",
    default_value = opts.default or "",
    on_submit = function(value)
      finish(value)
    end,
    on_close = function()
      vim.schedule(function()
        finish(nil)
      end)
    end,
  })
  input:mount()
  input:on("BufLeave", function()
    input:unmount()
  end, { once = true })
  local function cancel()
    input:unmount()
  end
  input:map("n", "<Esc>", cancel, { nowait = true })
  input:map("n", "q", cancel, { nowait = true })
  input:map("i", "<C-c>", cancel, { nowait = true })
  return handle(input)
end

-- panel lays several boxes out in one float (a nui Layout): a transcript
-- above a prompt, a list beside a preview. Each box is { name, size ("60%" or
-- a number), title, lines, enter, modifiable }; dir is "col" (stacked, the
-- default) or "row". Closing any box closes them all and runs opts.on_close
function M.panel(opts)
  local Layout = require("nui.layout")
  local Popup = require("nui.popup")
  local h = { boxes = {} }
  local entries, popups = {}, {}
  for _, b in ipairs(opts.boxes) do
    local popup = Popup({
      enter = b.enter or false,
      focusable = true,
      border = border({ title = b.title, icon = b.icon or icons().chat, footer = b.footer }),
      buf_options = { bufhidden = "wipe", filetype = b.filetype or "bud-ui" },
      win_options = { wrap = true },
    })
    table.insert(popups, popup)
    table.insert(entries, Layout.Box(popup, { size = b.size }))
    h.boxes[b.name] = { popup = popup, spec = b }
  end
  local layout = Layout({
    relative = "editor",
    position = "50%",
    size = { width = opts.width or "80%", height = opts.height or "80%" },
  }, Layout.Box(entries, { dir = opts.dir or "col" }))
  layout:mount()

  local closed = false
  function h.close()
    if closed then
      return
    end
    closed = true
    layout:unmount()
    if opts.on_close then
      opts.on_close()
    end
  end
  for name, box in pairs(h.boxes) do
    box.buf, box.win = box.popup.bufnr, box.popup.winid
    vim.api.nvim_buf_set_lines(box.buf, 0, -1, false, box.spec.lines or {})
    if box.spec.modifiable == false then
      vim.bo[box.buf].modifiable = false
    end
    function box.map(lhs, fn, mode)
      box.popup:map(mode or "n", lhs, fn, { noremap = true, nowait = true })
    end
    box.map("q", h.close)
    box.popup:on("BufWipeout", function()
      vim.schedule(h.close)
    end, { once = true })
    h.boxes[name] = box
  end
  -- <Tab> moves between the boxes
  for i, popup in ipairs(popups) do
    local next_popup = popups[i % #popups + 1]
    popup:map("n", "<Tab>", function()
      vim.api.nvim_set_current_win(next_popup.winid)
    end, { nowait = true })
  end
  return h
end

-- select is vim.ui.select on menu
function M.select(items, opts, on_choice)
  opts = opts or {}
  if #items == 0 then
    on_choice(nil, nil)
    return
  end
  local title = opts.prompt and opts.prompt:gsub("[:%s]+$", "")
  M.menu(items, { title = title, format = opts.format_item }, on_choice)
end

-- ui_input is vim.ui.input on input. Asking with completion keeps Neovim's
-- own, orig (nui's prompt buffer has none)
function M.ui_input(opts, on_confirm, orig)
  opts = opts or {}
  if opts.completion and orig then
    return orig(opts, on_confirm)
  end
  M.input(opts, on_confirm)
end

-- setup routes vim.ui.select and vim.ui.input through select and ui_input;
-- bud runs bud.ui.hook at startup instead, which does the same without
-- loading this module. Running it once is enough (and again would wrap the
-- wrappers)
function M.setup()
  require("bud.ui.hook")
end

return M
