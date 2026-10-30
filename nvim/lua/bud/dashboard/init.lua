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

local icons = require("bud.icons")
local cli = require("bud.cli")

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

-- doctor_banners turns `bud doctor --json` output into banners. Claude Code
-- gets its own, with its own key, since setting it up is one command; its
-- result comes back too, for the health line
function M.doctor_banners(results)
  local fails, warns = 0, 0
  local claude
  for _, r in ipairs(results) do
    if r.name == "claude code" then
      claude = r
    elseif r.status == "fail" then
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
  local claude_ok = not claude or claude.status == "ok"
  if not claude_ok then
    table.insert(banners, { level = "warn", text = "Claude Code isn't set up, press c" })
  end
  return banners, fails + warns == 0 and claude_ok, claude
end

-- claude_health is the health line's Claude Code part: from doctor once it's
-- back, until then from whether claude is on PATH
function M.claude_health(result, executable)
  if result then
    return result.status == "ok" and "claude ready" or "claude not set up"
  end
  return executable and "claude ready" or "no claude yet"
end

-- update_banner turns update.json (what `bud update --background` found, D9)
-- into a banner: a release waiting for `bud update`, or nil
function M.update_banner(st)
  if type(st) ~= "table" or type(st.available) ~= "string" or st.available == "" then
    return nil
  end
  return { level = "update", text = "bud " .. st.available .. " is available, press u" }
end

local function read_update()
  local fd = io.open(vim.fn.stdpath("state") .. "/update.json", "r")
  if not fd then
    return nil
  end
  local ok, st = pcall(vim.json.decode, fd:read("*a"))
  fd:close()
  return ok and st or nil
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

local function health()
  local parts = {}
  if vim.g.bud_startup_ms then
    table.insert(parts, (icons.startup .. " %d ms"):format(vim.g.bud_startup_ms))
  end
  table.insert(parts, icons.plugins .. " " .. #require("bud.pack").names .. " plugins")
  table.insert(parts, icons.claude .. " " .. M.claude_health(current.claude, vim.fn.executable("claude") == 1))
  return table.concat(parts, " · ")
end

local function menu(settings)
  local items = {
    { key = "p", icon = "project", label = "Open project", short = "project" },
    { key = "f", icon = "file", label = "Open file", short = "file" },
    { key = "r", icon = "recent", label = "Recent files", short = "recent" },
    { key = "n", icon = "new", label = "New file", short = "new" },
    { key = "c", icon = "chat", label = "Chat", short = "chat" },
  }
  if settings.scratchpad then
    table.insert(items, { key = "x", icon = "scratchpad", label = "Scratchpad", short = "notes" })
  end
  vim.list_extend(items, {
    { key = "L", icon = "plugins", label = "Plugins", short = "plugins" },
    { key = "M", icon = "mason", label = "Mason", short = "mason" },
    { key = "s", icon = "settings", label = "Settings", short = "settings" },
    { key = "d", icon = "doctor", label = "Doctor", short = "doctor" },
    { key = "u", icon = "update", label = "Update", short = "update" },
    { key = "t", icon = "style", label = "Minimal view", short = "silly" },
    { key = "q", icon = "quit", label = "Quit", short = "quit" },
  })
  return items
end

-- recent chats open on these keys
local chat_keys = { "a", "b", "e" }

local function data()
  local list = {}
  for _, p in ipairs(projects.list()) do
    if not p.missing and #list < max_projects then
      table.insert(
        list,
        vim.tbl_extend("force", p, {
          key = tostring(#list + 1),
          git = current.git[p.path],
          prs = current.prs[p.path],
          todos = current.todos[p.path],
        })
      )
    end
  end
  return {
    mood = M.mood(current, tonumber(os.date("%H"))),
    tagline = current.tagline,
    style = current.style,
    menu = menu(current.settings),
    projects = list,
    banners = current.update and { current.update, unpack(current.banners) } or current.banners,
    health = current.settings.health and health() or nil,
    clock = current.settings.clock and os.date("%a %b %d  %H:%M") or nil,
    chats = current.chats,
  }
end

local ns = vim.api.nvim_create_namespace("bud.dashboard")

-- render draws the data last gathered, with the buddy's current frame
local function render(win)
  if not current or not current.data or not vim.api.nvim_buf_is_valid(current.buf) then
    return
  end
  win = win or vim.fn.bufwinid(current.buf)
  if win == -1 then
    return
  end
  local shown = vim.tbl_extend("force", current.data, { frame = current.frame })
  local lines, hls = view.build(shown, vim.api.nvim_win_get_width(win), vim.api.nvim_win_get_height(win))
  vim.bo[current.buf].modifiable = true
  vim.api.nvim_buf_set_lines(current.buf, 0, -1, false, lines)
  vim.bo[current.buf].modifiable = false
  vim.api.nvim_buf_clear_namespace(current.buf, ns, 0, -1)
  for _, h in ipairs(hls) do
    vim.api.nvim_buf_set_extmark(current.buf, ns, h[1], h[2], { end_col = h[3], hl_group = h[4] })
  end
end

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
  render(win)
end

-- animate plays one of the buddy's moves every few seconds while the
-- dashboard is on screen (JGA-155): each frame redraws from the data already
-- gathered, so nothing runs again; asleep, it only snores
-- how long the buddy waits between moves, and shows each frame; swapped in tests
M.idle_ms = function()
  return math.random(4000, 9000)
end
M.frame_ms = 150

local function animate()
  local timer = vim.uv.new_timer()
  current.anim = timer
  local function alive()
    return current ~= nil and current.anim == timer
  end
  local function after(ms, fn)
    timer:start(
      ms,
      0,
      vim.schedule_wrap(function()
        if alive() then
          fn()
        end
      end)
    )
  end
  local idle
  local function play(frames, i)
    current.frame = frames[i]
    render()
    if i < #frames then
      after(M.frame_ms, function()
        play(frames, i + 1)
      end)
    else
      after(M.frame_ms, function()
        current.frame = nil
        render()
        idle()
      end)
    end
  end
  idle = function()
    after(M.idle_ms(), function()
      if current.style == "minimal" or vim.fn.bufwinid(current.buf) == -1 then
        return idle()
      end
      if current.data and current.data.mood == "sleepy" then
        return play(view.snore, 1)
      end
      local names = vim.tbl_keys(view.moves)
      table.sort(names)
      play(view.moves[names[math.random(#names)]], 1)
    end)
  end
  idle()
end

-- leader replays a global leader mapping, so the dashboard follows whatever it does
local function leader(keys)
  return function()
    vim.api.nvim_feedkeys(vim.keycode("<leader>" .. keys), "m", false)
  end
end

-- open_project goes to a registered project (bud.open.go)
function M.open_project(p)
  require("bud.open").go(p.path)
end

local function keys(buf)
  local function map(lhs, fn, desc)
    vim.keymap.set("n", lhs, fn, { buffer = buf, nowait = true, silent = true, desc = desc })
  end
  map("p", function()
    require("bud.open").project()
  end, "Open project")
  map("f", function()
    require("bud.open").file()
  end, "Open file")
  map("r", leader("r"), "Recent files")
  map("n", function()
    vim.cmd.enew()
  end, "New file")
  map("L", leader("L"), "Plugins")
  map("M", function()
    vim.cmd("Mason")
  end, "Mason")
  map("s", function()
    require("bud.settings").open()
  end, "Settings")
  map("d", function()
    cli.terminal({ "doctor" })
  end, "Doctor")
  map("u", function()
    cli.terminal({ "update" })
  end, "Update")
  -- chat, or setting up Claude Code first when the banner says it isn't
  map("c", function()
    if current.claude and current.claude.status ~= "ok" then
      cli.terminal({ "claude", "setup" })
    else
      require("bud.chat").open()
    end
  end, "Chat")
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
  -- recent chats on a b c (JGA-97)
  if current.settings.chats then
    for i, key in ipairs(chat_keys) do
      map(key, function()
        local c = current and current.chats and current.chats[i]
        if c then
          require("bud.chat").open({ resume = c.title })
        end
      end, "Resume chat " .. i)
    end
  end
end

-- background work, each piece redrawing when it's back
-- parse_prs reads `gh pr list --json author,reviewRequests`: how many are
-- open, and how many ask login for a review
function M.parse_prs(out, login)
  local ok, list = pcall(vim.json.decode, out or "")
  if not ok or type(list) ~= "table" then
    return nil
  end
  local review = 0
  for _, pr in ipairs(list) do
    for _, r in ipairs(pr.reviewRequests or {}) do
      if login and r.login == login then
        review = review + 1
      end
    end
  end
  return { open = #list, review = review }
end

-- count_lines counts non-empty lines, for rg's matches
function M.count_lines(out)
  local n = 0
  for line in (out or ""):gmatch("[^\n]+") do
    if line ~= "" then
      n = n + 1
    end
  end
  return n
end

-- the optional extras' background work: each redraws when it's back
local function extras()
  if current.settings.prs and vim.fn.executable("gh") == 1 then
    vim.system({ "gh", "api", "user", "--jq", ".login" }, { text = true }, function(who)
      local login = who.code == 0 and vim.trim(who.stdout or "") or nil
      for _, p in ipairs(current and current.data and current.data.projects or {}) do
        vim.system(
          { "gh", "pr", "list", "--state", "open", "--limit", "100", "--json", "author,reviewRequests" },
          { cwd = p.path, text = true },
          function(res)
            if res.code ~= 0 then
              return
            end
            vim.schedule(function()
              if current then
                current.prs[p.path] = M.parse_prs(res.stdout, login)
                draw()
              end
            end)
          end
        )
      end
    end)
  end
  if current.settings.todos and vim.fn.executable("rg") == 1 then
    for _, p in ipairs(current.data and current.data.projects or {}) do
      vim.system(
        { "rg", "--no-messages", "-o", "--no-filename", "\\b(TODO|FIXME)\\b", "." },
        { cwd = p.path, text = true },
        function(res)
          vim.schedule(function()
            if current then
              current.todos[p.path] = M.count_lines(res.stdout)
              draw()
            end
          end)
        end
      )
    end
  end
  if current.settings.chats then
    local ok, store = pcall(require, "bud.chat.store")
    if ok then
      local recent = {}
      for _, e in ipairs(store.list()) do
        table.insert(recent, e)
      end
      table.sort(recent, function(a, b)
        return a.mtime > b.mtime
      end)
      current.chats = {}
      for i = 1, math.min(#chat_keys, #recent) do
        table.insert(current.chats, { key = chat_keys[i], title = recent[i].title })
      end
    end
  end
  if current.settings.clock then
    -- the minute changes, so redraw on it while the dashboard is open
    local timer = vim.uv.new_timer()
    timer:start(
      60000 - (os.time() % 60) * 1000,
      60000,
      vim.schedule_wrap(function()
        if not current then
          timer:stop()
          timer:close()
          return
        end
        draw()
      end)
    )
  end
end

local function load()
  extras()
  local bin = cli.bin()
  if bin and current.settings.banners then
    vim.system({ bin, "doctor", "--quick", "--json" }, { text = true }, function(res)
      vim.schedule(function()
        if not current then
          return
        end
        local ok, results = pcall(vim.json.decode, res.stdout or "")
        if ok and type(results) == "table" then
          local clean
          current.banners, clean, current.claude = M.doctor_banners(results)
          current.doctor = { clean = clean }
          draw()
        end
      end)
    end)
  end

  for _, p in ipairs(current.settings.git and current.data.projects or {}) do
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
  -- FloatBorder's colour only: themes (catppuccin) give it a float's darker
  -- background, which drew a dark outline round every row
  local float = vim.api.nvim_get_hl(0, { name = "FloatBorder", link = false })
  vim.api.nvim_set_hl(0, "BudDashboardBorder", { fg = float.fg, ctermfg = float.ctermfg, default = true })
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

  -- config.json's dashboard section, read once
  local settings = config.get("dashboard", {})
  local taglines = require("bud.dashboard.taglines")
  math.randomseed(vim.uv.hrtime())
  current = {
    buf = buf,
    git = {},
    prs = {},
    todos = {},
    banners = {},
    update = settings.banners ~= false and M.update_banner(read_update()) or nil,
    tagline = taglines[math.random(#taglines)],
    -- t switches it for this session; config.json's dashboard.style is the default
    style = settings.style or "silly",
    settings = {
      scratchpad = settings.scratchpad == true,
      -- optional extras (JGA-97), off unless config.json turns them on
      prs = settings.prs == true,
      todos = settings.todos == true,
      chats = settings.chats == true,
      clock = settings.clock == true,
      -- the buddy's idle moves (JGA-155), on unless config.json turns them off
      animate = settings.animate ~= false,
      -- the default extras (JGA-62), each on unless config.json turns it off
      git = settings.git ~= false,
      health = settings.health ~= false,
      banners = settings.banners ~= false,
    },
  }
  keys(buf)
  draw()
  if current.settings.animate then
    animate()
  end

  vim.api.nvim_create_autocmd({ "VimResized", "WinResized" }, {
    buffer = buf,
    callback = draw,
  })
  vim.api.nvim_create_autocmd("BufWipeout", {
    buffer = buf,
    callback = function()
      if current and current.anim then
        current.anim:stop()
        current.anim:close()
      end
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

return M
