-- The dashboard (BASE.md 5.4, D37): where bare `bud` lands. bud.dashboard.view
-- lays it out; this module gathers what it shows, draws it into a scratch
-- buffer and maps its keys. Everything slow (git per project, bud doctor) runs
-- in the background and redraws when it's back, so opening it costs a few ms.

local view = require("bud.dashboard.view")
local projects = require("bud.projects")
local config = require("bud.config")

local M = {}

local max_projects = 5

-- state for the dashboard on screen; nil when it isn't
local current

local function bud_bin()
  local bin = vim.env.BUD_BIN
  if bin and vim.fn.executable(bin) == 1 then
    return bin
  end
  return vim.fn.exepath("bud") ~= "" and vim.fn.exepath("bud") or nil
end

-- mood picks the buddy's face: sleepy after midnight wins, then a waiting
-- update, then happy once doctor came back clean
function M.mood(state, hour)
  if hour < 5 then
    return "sleepy"
  elseif state.update then
    return "update"
  elseif state.doctor and state.doctor.clean then
    return "happy"
  end
  return "normal"
end

-- doctor_banners turns `bud doctor --json` output into banners
function M.doctor_banners(results)
  local fails, warns = 0, 0
  for _, r in ipairs(results) do
    if r.status == "fail" then
      fails = fails + 1
    elseif r.status == "warn" then
      warns = warns + 1
    end
  end
  local banners = {}
  local function plural(n, word)
    return n == 1 and ("1 " .. word) or (n .. " " .. word .. "s")
  end
  if fails > 0 then
    table.insert(banners, { level = "error", text = plural(fails, "doctor problem") .. ", press d" })
  end
  if warns > 0 then
    table.insert(banners, { level = "warn", text = plural(warns, "doctor warning") .. ", press d" })
  end
  return banners, fails + warns == 0
end

-- parse_git reads `git status --porcelain=v2 --branch`
function M.parse_git(out)
  local git = { changed = 0, ahead = 0, behind = 0 }
  for line in out:gmatch("[^\n]+") do
    local head = line:match("^# branch%.head (.+)$")
    local ahead, behind = line:match("^# branch%.ab %+(%d+) %-(%d+)$")
    if head then
      git.branch = head == "(detached)" and "detached" or head
    elseif ahead then
      git.ahead, git.behind = tonumber(ahead), tonumber(behind)
    elseif not line:match("^#") then
      git.changed = git.changed + 1
    end
  end
  return git
end

-- tip picks a leader mapping with a description, a different one each day
function M.tip(day)
  local leader = vim.g.mapleader or "\\"
  local maps = {}
  for _, m in ipairs(vim.api.nvim_get_keymap("n")) do
    if m.desc and m.desc ~= "" and vim.startswith(m.lhs, leader) and #m.lhs > #leader then
      table.insert(maps, m)
    end
  end
  if #maps == 0 then
    return nil
  end
  table.sort(maps, function(a, b)
    return a.lhs < b.lhs
  end)
  local m = maps[(day % #maps) + 1]
  local name = leader == " " and "<Space>" or leader
  return { lhs = name .. m.lhs:sub(#leader + 1), desc = m.desc }
end

local function health()
  local parts = {}
  if vim.g.bud_startup_ms then
    table.insert(parts, (view.icons.startup .. " %d ms"):format(vim.g.bud_startup_ms))
  end
  table.insert(parts, view.icons.plugins .. " " .. #require("bud.pack").names .. " plugins")
  table.insert(
    parts,
    view.icons.claude .. " " .. (vim.fn.executable("claude") == 1 and "claude ready" or "no claude yet")
  )
  return table.concat(parts, " · ")
end

local function menu(settings)
  local items = {
    { key = "p", icon = "project", label = "Open project", short = "project" },
    { key = "f", icon = "file", label = "Open file", short = "file" },
    { key = "r", icon = "recent", label = "Recent files", short = "recent" },
    { key = "n", icon = "new", label = "New file", short = "new" },
  }
  if settings.scratchpad then
    table.insert(items, { key = "x", icon = "scratchpad", label = "Scratchpad", short = "notes" })
  end
  vim.list_extend(items, {
    { key = "L", icon = "plugins", label = "Plugins", short = "plugins" },
    { key = "M", icon = "mason", label = "Mason", short = "mason" },
    { key = "d", icon = "doctor", label = "Doctor", short = "doctor" },
    { key = "u", icon = "update", label = "Update", short = "update" },
    { key = "t", icon = "style", label = "Minimal view", short = "silly" },
    { key = "q", icon = "quit", label = "Quit", short = "quit" },
  })
  return items
end

local function data()
  local list = {}
  for _, p in ipairs(projects.list()) do
    if not p.missing and #list < max_projects then
      table.insert(list, vim.tbl_extend("force", p, { key = tostring(#list + 1), git = current.git[p.path] }))
    end
  end
  return {
    mood = M.mood(current, tonumber(os.date("%H"))),
    tagline = current.tagline,
    style = current.style,
    menu = menu(current.settings),
    projects = list,
    banners = current.banners,
    health = health(),
    tip = current.tip,
  }
end

local ns = vim.api.nvim_create_namespace("bud.dashboard")

local function draw()
  if not current or not vim.api.nvim_buf_is_valid(current.buf) then
    current = nil
    return
  end
  local win = vim.fn.bufwinid(current.buf)
  if win == -1 then
    return
  end
  current.data = data()
  local lines, hls = view.build(current.data, vim.api.nvim_win_get_width(win), vim.api.nvim_win_get_height(win))
  vim.bo[current.buf].modifiable = true
  vim.api.nvim_buf_set_lines(current.buf, 0, -1, false, lines)
  vim.bo[current.buf].modifiable = false
  vim.api.nvim_buf_clear_namespace(current.buf, ns, 0, -1)
  for _, h in ipairs(hls) do
    vim.api.nvim_buf_set_extmark(current.buf, ns, h[1], h[2], { end_col = h[3], hl_group = h[4] })
  end
end

-- terminal runs a bud command in a new buffer, for d and u
local function terminal(args)
  local bin = bud_bin()
  if not bin then
    vim.notify("bud: can't find the bud binary", vim.log.levels.ERROR)
    return
  end
  vim.cmd.enew()
  vim.fn.jobstart(vim.list_extend({ bin }, args), { term = true })
  vim.cmd.startinsert()
end

-- leader replays a global leader mapping, so the dashboard follows whatever it does
local function leader(keys)
  return function()
    vim.api.nvim_feedkeys(vim.keycode("<leader>" .. keys), "m", false)
  end
end

local function session_file(path)
  return vim.fn.stdpath("state") .. "/sessions/" .. path:gsub("[/\\:]", "%%") .. ".vim"
end

-- open_project goes to a registered project: its last session when sessions
-- are on and it has one, otherwise the file picker in it
function M.open_project(p)
  if vim.fn.isdirectory(p.path) == 0 then
    vim.notify("bud: " .. p.path .. " is missing", vim.log.levels.WARN)
    return
  end
  vim.cmd.cd(vim.fn.fnameescape(p.path))
  local session = session_file(p.path)
  if config.get("dashboard.sessions", false) and vim.fn.filereadable(session) == 1 then
    local ok, err = pcall(vim.cmd.source, vim.fn.fnameescape(session))
    if ok then
      return
    end
    vim.notify("bud: restoring the session failed: " .. tostring(err), vim.log.levels.WARN)
  end
  leader("f")()
end

local function pick_project()
  local list = vim.tbl_filter(function(p)
    return not p.missing
  end, projects.list())
  if #list == 0 then
    vim.notify("bud: no projects yet: open one with `bud <dir>`", vim.log.levels.INFO)
    return
  end
  vim.ui.select(list, {
    prompt = "Project",
    format_item = function(p)
      return p.name .. "  " .. vim.fn.fnamemodify(p.path, ":~")
    end,
  }, function(p)
    if p then
      M.open_project(p)
    end
  end)
end

local function keys(buf)
  local function map(lhs, fn, desc)
    vim.keymap.set("n", lhs, fn, { buffer = buf, nowait = true, silent = true, desc = desc })
  end
  map("p", pick_project, "Open project")
  map("f", leader("f"), "Open file")
  map("r", leader("r"), "Recent files")
  map("n", function()
    vim.cmd.enew()
  end, "New file")
  map("L", leader("L"), "Plugins")
  map("M", function()
    vim.cmd("Mason")
  end, "Mason")
  map("d", function()
    terminal({ "doctor" })
  end, "Doctor")
  map("u", function()
    terminal({ "update" })
  end, "Update")
  map("t", function()
    current.style = current.style == "minimal" and "silly" or "minimal"
    draw()
  end, "Switch style")
  map("q", function()
    vim.cmd("confirm qall")
  end, "Quit")
  if current.settings.scratchpad then
    map("x", function()
      vim.cmd.edit(vim.fn.fnameescape(vim.fn.stdpath("data") .. "/scratchpad.md"))
    end, "Scratchpad")
  end
  for i = 1, max_projects do
    map(tostring(i), function()
      local p = current and current.data and current.data.projects[i]
      if p then
        M.open_project(p)
      end
    end, "Open project " .. i)
  end
end

-- background work, each piece redrawing when it's back
local function load()
  local bin = bud_bin()
  if bin then
    vim.system({ bin, "doctor", "--quick", "--json" }, { text = true }, function(res)
      vim.schedule(function()
        if not current then
          return
        end
        local ok, results = pcall(vim.json.decode, res.stdout or "")
        if ok and type(results) == "table" then
          local clean
          current.banners, clean = M.doctor_banners(results)
          current.doctor = { clean = clean }
          draw()
        end
      end)
    end)
  end

  for _, p in ipairs(current.data.projects) do
    vim.system({ "git", "-C", p.path, "status", "--porcelain=v2", "--branch" }, { text = true }, function(res)
      if res.code ~= 0 then
        return
      end
      vim.schedule(function()
        if current then
          current.git[p.path] = M.parse_git(res.stdout)
          draw()
        end
      end)
    end)
  end
end

local function highlights()
  local links = {
    BudDashboardArt = "Function",
    BudDashboardTitle = "Title",
    BudDashboardKey = "Keyword",
    BudDashboardIcon = "Special",
    BudDashboardHeader = "Title",
    BudDashboardDim = "Comment",
    BudDashboardOk = "DiagnosticOk",
    BudDashboardWarn = "DiagnosticWarn",
    BudDashboardError = "DiagnosticError",
  }
  for group, link in pairs(links) do
    vim.api.nvim_set_hl(0, group, { link = link, default = true })
  end
end

-- open shows the dashboard in the current window. opts.offline skips the
-- background work (tests, the smoke test)
function M.open(opts)
  opts = opts or {}
  highlights()

  local buf = vim.api.nvim_create_buf(false, true)
  vim.bo[buf].bufhidden = "wipe"
  vim.bo[buf].filetype = "bud-dashboard"
  vim.api.nvim_win_set_buf(0, buf)
  -- window options are local to this buffer, so the next file gets its own back
  for opt, value in pairs({
    number = false,
    relativenumber = false,
    signcolumn = "no",
    cursorline = false,
    list = false,
    foldcolumn = "0",
    statuscolumn = "",
  }) do
    vim.api.nvim_set_option_value(opt, value, { scope = "local", win = 0 })
  end

  local taglines = require("bud.dashboard.taglines")
  math.randomseed(vim.uv.hrtime())
  current = {
    buf = buf,
    git = {},
    banners = {},
    tagline = taglines[math.random(#taglines)],
    tip = M.tip(tonumber(os.date("%j"))),
    -- t switches it for this session; config.json's dashboard.style is the default
    style = config.get("dashboard.style", "silly"),
    settings = {
      scratchpad = config.get("dashboard.scratchpad", false),
      sessions = config.get("dashboard.sessions", false),
    },
  }
  keys(buf)
  draw()

  vim.api.nvim_create_autocmd({ "VimResized", "WinResized" }, {
    buffer = buf,
    callback = draw,
  })
  vim.api.nvim_create_autocmd("BufWipeout", {
    buffer = buf,
    callback = function()
      current = nil
    end,
  })

  if not opts.offline then
    load()
  end
  return buf
end

-- lines returns what's drawn, for tests
function M.lines()
  if not current then
    return nil
  end
  return vim.api.nvim_buf_get_lines(current.buf, 0, -1, false)
end

-- save_session writes the project's session on exit, when sessions are on and
-- there are files open in a registered project
function M.save_session()
  if not config.get("dashboard.sessions", false) then
    return
  end
  local project = projects.find(vim.fn.getcwd())
  if not project then
    return
  end
  local has_files = vim.iter(vim.api.nvim_list_bufs()):any(function(b)
    return vim.bo[b].buflisted and vim.bo[b].buftype == "" and vim.api.nvim_buf_get_name(b) ~= ""
  end)
  if not has_files then
    return
  end
  local file = session_file(project.path)
  vim.fn.mkdir(vim.fs.dirname(file), "p")
  local saved = vim.o.sessionoptions
  vim.o.sessionoptions = "buffers,curdir,folds,tabpages,winsize"
  pcall(vim.cmd.mksession, { vim.fn.fnameescape(file), bang = true })
  vim.o.sessionoptions = saved
end

return M
