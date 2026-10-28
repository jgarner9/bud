-- The settings screen (JGA-94): <leader>, or s on the dashboard. Every
-- setting in config.json with its value, the languages and the tools, each
-- changed with Enter. It writes nothing itself: the CLI owns bud's files, so
-- a change runs bud config set, bud lang enable/disable or bud tools
-- enable/disable, then the screen redraws from what the CLI saved.

local cli = require("bud.cli")

local M = {}

-- the settings shown, in order, under their headings. kind is how Enter
-- changes one: "bool" flips it, "choice" picks from choices, "number" and
-- "list" ask
M.sections = {
  {
    title = "General",
    rows = {
      { key = "profile", label = "Install profile", kind = "choice", choices = { "full", "minimal" } },
      { key = "terminal", label = "Terminal", kind = "choice", choices = { "kitty", "ghostty" } },
      { key = "layout", label = "Default layout", kind = "choice", choices = "layouts" },
      { key = "format_on_save", label = "Format on save", kind = "bool" },
    },
  },
  {
    title = "Updates",
    rows = {
      { key = "update.channel", label = "Channel", kind = "choice", choices = { "stable", "nightly" } },
      { key = "update.auto", label = "Install in the background", kind = "bool" },
    },
  },
  {
    title = "Dashboard",
    rows = {
      { key = "dashboard.style", label = "Style", kind = "choice", choices = { "silly", "minimal" } },
      { key = "dashboard.git", label = "Git status per project", kind = "bool" },
      { key = "dashboard.health", label = "Health line", kind = "bool" },
      { key = "dashboard.banners", label = "Banners", kind = "bool" },
      { key = "dashboard.tip", label = "Tip of the day", kind = "bool" },
      { key = "dashboard.scratchpad", label = "Scratchpad (x)", kind = "bool" },
      { key = "dashboard.sessions", label = "Restore sessions", kind = "bool" },
      { key = "dashboard.prs", label = "Open PRs (gh)", kind = "bool" },
      { key = "dashboard.todos", label = "TODO count", kind = "bool" },
      { key = "dashboard.chats", label = "Recent chats", kind = "bool" },
      { key = "dashboard.clock", label = "Clock", kind = "bool" },
    },
  },
  {
    title = "Claude Code",
    rows = {
      { key = "ai.claude.autostart", label = "Start with a project", kind = "bool" },
      { key = "ai.claude.resume", label = "Reopening a project", kind = "choice", choices = { "continue", "new" } },
      { key = "ai.claude.idle_minutes", label = "Stop idle sessions after (min)", kind = "number" },
      { key = "ai.chat.pane", label = "Layout's chat pane", kind = "choice", choices = { "native", "term" } },
      {
        key = "ai.claude.config",
        label = "Claude Code config (restart bud)",
        kind = "choice",
        choices = { "bud", "home" },
      },
      { key = "ai.claude.command", label = "Run through", kind = "list" },
      { key = "ai.claude.args", label = "Extra flags", kind = "list" },
    },
  },
}

local state = {} -- buf, win, rows (line -> row), settings

-- swappable in tests: runs the CLI, cb(ok, output)
M.run = function(args, cb)
  local bin = cli.bin()
  if not bin then
    vim.notify("bud: can't find the bud binary", vim.log.levels.ERROR)
    return
  end
  vim.system(vim.list_extend({ bin }, args), { text = true }, function(res)
    vim.schedule(function()
      cb(res.code == 0, vim.trim((res.stdout or "") .. (res.stderr or "")))
    end)
  end)
end

local function get(tree, key)
  local cur = tree
  for part in key:gmatch("[^.]+") do
    if type(cur) ~= "table" then
      return nil
    end
    cur = cur[part]
  end
  return cur
end

-- show is how a value reads on screen
function M.show(row, value)
  if row.kind == "bool" then
    return value and "on" or "off"
  elseif row.kind == "list" then
    return (type(value) == "table" and #value > 0) and table.concat(value, " ") or "(none)"
  end
  return value == nil and "" or tostring(value)
end

-- lines draws the screen from settings (bud config ls), the language specs
-- with which are on, and the tools with which are off
function M.lines(settings, langs, tools)
  local lines, rows, hls = {}, {}, {}
  local function add(text, row, hl)
    table.insert(lines, text)
    if row then
      rows[#lines] = row
    end
    if hl then
      table.insert(hls, { #lines - 1, hl })
    end
  end
  add("")
  add("  [o.o]  bud settings", nil, "Title")
  add("  Enter changes the setting under the cursor · u update bud · q close", nil, "Comment")
  for _, section in ipairs(M.sections) do
    add("")
    add("  " .. section.title, nil, "Title")
    for _, row in ipairs(section.rows) do
      add(("    %-32s %s"):format(row.label, M.show(row, get(settings, row.key))), row)
    end
  end
  add("")
  add("  Languages  (Enter turns one on or off, installing its tools)", nil, "Title")
  for _, l in ipairs(langs) do
    add(("    %s %s"):format(l.on and "[x]" or "[ ]", l.name), { lang = l.name, on = l.on })
  end
  add("")
  add("  Tools", nil, "Title")
  if #tools == 0 then
    add("    none yet: :BudGen makes one", nil, "Comment")
  end
  for _, t in ipairs(tools) do
    add(("    %s %-20s %s"):format(t.on and "[x]" or "[ ]", t.name, t.description or ""), { tool = t.name, on = t.on })
  end
  return lines, rows, hls
end

local function langs()
  local ok, lang = pcall(require, "bud.lang")
  if not ok then
    return {}
  end
  local on = {}
  for _, s in ipairs(lang.enabled() or {}) do
    on[s.name] = true
  end
  local out = {}
  for name in pairs(lang.specs() or {}) do
    table.insert(out, { name = name, on = on[name] or false })
  end
  table.sort(out, function(a, b)
    return a.name < b.name
  end)
  return out
end

local function tools(settings)
  local off = {}
  for _, name in ipairs(get(settings, "tools.disabled") or {}) do
    off[name] = true
  end
  local out = {}
  for _, t in ipairs(require("bud.tools").list()) do
    table.insert(out, { name = t.name, description = t.manifest.description, on = not off[t.name] })
  end
  return out
end

local ns = vim.api.nvim_create_namespace("bud.settings")

local function draw()
  if not (state.buf and vim.api.nvim_buf_is_valid(state.buf)) then
    return
  end
  local lines, rows, hls = M.lines(state.settings or {}, langs(), tools(state.settings or {}))
  state.rows = rows
  vim.bo[state.buf].modifiable = true
  vim.api.nvim_buf_set_lines(state.buf, 0, -1, false, lines)
  vim.bo[state.buf].modifiable = false
  vim.api.nvim_buf_clear_namespace(state.buf, ns, 0, -1)
  for _, h in ipairs(hls) do
    vim.api.nvim_buf_set_extmark(state.buf, ns, h[1], 0, { line_hl_group = h[2] })
  end
end

-- refresh reads the settings back from the CLI and redraws
function M.refresh(cb)
  M.run({ "config", "ls" }, function(ok, out)
    if ok then
      -- unset keys come back as null: nil, not vim.NIL, so `or {}` defaults work
      local decoded, settings = pcall(vim.json.decode, out, { luanil = { object = true, array = true } })
      if decoded then
        state.settings = settings
      end
    end
    package.loaded["bud.lang"] = nil -- picks up a language turned on or off
    draw()
    if cb then
      cb()
    end
  end)
end

local function set(key, value)
  M.run({ "config", "set", key, vim.json.encode(value) }, function(ok, out)
    if not ok then
      vim.notify("bud: " .. out, vim.log.levels.WARN)
    end
    M.refresh()
  end)
end

-- change acts on the row under the cursor
function M.change()
  local row = state.rows and state.rows[vim.fn.line(".")]
  if not row then
    return
  end
  if row.lang then
    -- enabling installs system deps and plugins, which can ask, so on a terminal
    vim.cmd("botright split | resize 12")
    vim.fn.jobstart({ cli.bin(), "lang", row.on and "disable" or "enable", row.lang }, {
      term = true,
      on_exit = function()
        vim.schedule(M.refresh)
      end,
    })
    vim.cmd.startinsert()
    return
  end
  if row.tool then
    M.run({ "tools", row.on and "disable" or "enable", row.tool }, function()
      M.refresh()
    end)
    return
  end
  local value = get(state.settings or {}, row.key)
  if row.kind == "bool" then
    set(row.key, not value)
  elseif row.kind == "choice" then
    local choices = row.choices == "layouts" and require("bud.layout").names() or row.choices
    vim.ui.select(choices, { prompt = row.label }, function(choice)
      if choice then
        set(row.key, choice)
      end
    end)
  elseif row.kind == "number" then
    vim.ui.input({ prompt = row.label .. ": ", default = tostring(value or "") }, function(text)
      local n = tonumber(text or "")
      if n then
        set(row.key, n)
      end
    end)
  elseif row.kind == "list" then
    vim.ui.input(
      { prompt = row.label .. " (space-separated): ", default = M.show(row, value):gsub("^%(none%)$", "") },
      function(text)
        if text then
          set(row.key, vim.split(vim.trim(text), "%s+", { trimempty = true }))
        end
      end
    )
  end
end

-- open shows the settings in its own scratch buffer
function M.open()
  if not (state.buf and vim.api.nvim_buf_is_valid(state.buf)) then
    state.buf = vim.api.nvim_create_buf(false, true)
    vim.bo[state.buf].bufhidden = "wipe"
    vim.bo[state.buf].filetype = "budsettings"
    local function map(lhs, fn, desc)
      vim.keymap.set("n", lhs, fn, { buffer = state.buf, nowait = true, desc = desc })
    end
    map("<CR>", M.change, "Change this setting")
    map("q", function()
      vim.cmd.bdelete()
    end, "Close settings")
    map("u", function()
      vim.cmd("botright split | resize 12")
      vim.fn.jobstart({ cli.bin(), "update" }, { term = true })
      vim.cmd.startinsert()
    end, "Update bud")
  end
  vim.api.nvim_set_current_buf(state.buf)
  vim.wo.number, vim.wo.relativenumber, vim.wo.cursorline = false, false, true
  draw()
  M.refresh(function()
    -- the cursor on the first setting
    for line in pairs(state.rows or {}) do
      if line < (state.first or math.huge) then
        state.first = line
      end
    end
    if state.first then
      pcall(vim.api.nvim_win_set_cursor, 0, { state.first, 4 })
    end
  end)
end

-- buf is the settings buffer, for tests
function M.buf()
  return state.buf
end

function M.setup()
  vim.keymap.set("n", "<leader>,", M.open, { desc = "Settings" })
  vim.api.nvim_create_user_command("BudSettings", M.open, { desc = "bud settings" })
end

return M
