-- Lays out the silly dashboard (BASE.md 5.4) as text plus highlights. Pure: it
-- takes the data and the window size and touches no buffer, so it's tested
-- headless. bud.dashboard gathers the data and draws the result.

local M = {}

-- the buddy's faces, by mood (JGA-99)
M.faces = {
  normal = "[o.o]",
  happy = "[^.^]", -- doctor came back clean
  update = "[O.O]", -- an update is waiting
  sleepy = "[-.-]", -- after midnight
}

-- Nerd Font glyphs, bud's accent alongside the robot. Only long-standing ones
-- (Font Awesome, Octicons, Devicons), which every Nerd Font has
M.icons = {
  project = "", -- nf-fa-folder_open
  file = "", -- nf-fa-file_o
  recent = "", -- nf-fa-history
  new = "", -- nf-fa-plus
  scratchpad = "", -- nf-fa-pencil
  plugins = "", -- nf-fa-plug
  mason = "", -- nf-fa-wrench
  doctor = "", -- nf-fa-stethoscope
  update = "", -- nf-fa-refresh
  style = "", -- nf-fa-exchange
  quit = "", -- nf-fa-power_off
  projects = "", -- nf-fa-folder
  pinned = "", -- nf-oct-pin
  unpinned = "", -- nf-fa-folder
  branch = "", -- nf-dev-git_branch
  changed = "", -- nf-fa-pencil_square_o
  clean = "", -- nf-fa-check
  ahead = "", -- nf-fa-arrow_up
  behind = "", -- nf-fa-arrow_down
  error = "", -- nf-fa-times_circle
  warn = "", -- nf-fa-warning
  startup = "", -- nf-fa-bolt
  claude = "", -- nf-oct-hubot
  tip = "", -- nf-fa-lightbulb_o
}

local icons = M.icons
local menu_width = 26

-- a line is a list of { text, group } segments; group may be nil
local function seg(text, group)
  return { text, group }
end

local function width(line)
  local w = 0
  for _, s in ipairs(line) do
    w = w + vim.fn.strdisplaywidth(s[1])
  end
  return w
end

local function pad(text, cols)
  return text .. string.rep(" ", math.max(0, cols - vim.fn.strdisplaywidth(text)))
end

local function art(data)
  local face = M.faces[data.mood] or M.faces.normal
  local sleepy = data.mood == "sleepy"
  return {
    { seg("   ___", "BudDashboardArt"), seg(sleepy and "     z" or "", "BudDashboardDim") },
    {
      seg("  " .. face, "BudDashboardArt"),
      seg(sleepy and "  z     " or "        ", "BudDashboardDim"),
      seg("hey bud.", "BudDashboardTitle"),
    },
    { seg("  /[_]\\", "BudDashboardArt"), seg("        "), seg('"' .. data.tagline .. '"', "BudDashboardDim") },
    { seg("   ] [", "BudDashboardArt") },
  }
end

local function menu(items)
  local lines = {}
  local half = math.ceil(#items / 2)
  for i = 1, half do
    local line = {}
    for _, item in ipairs({ items[i], items[i + half] }) do
      table.insert(line, seg("  " .. item.key, "BudDashboardKey"))
      table.insert(line, seg("  " .. (icons[item.icon] or " "), "BudDashboardIcon"))
      table.insert(line, seg(pad(" " .. item.label, menu_width - 6)))
    end
    table.insert(lines, line)
  end
  return lines
end

-- git is { branch, changed, ahead, behind }, or nil before it's known or outside git
local function git_status(git)
  if not git or not git.branch then
    return {}
  end
  local out = { seg(icons.branch .. " " .. git.branch .. " ", "BudDashboardDim") }
  if git.changed > 0 then
    table.insert(out, seg(icons.changed .. " " .. git.changed, "BudDashboardWarn"))
  else
    table.insert(out, seg(icons.clean, "BudDashboardOk"))
  end
  if git.ahead > 0 then
    table.insert(out, seg(" " .. icons.ahead .. git.ahead, "BudDashboardDim"))
  end
  if git.behind > 0 then
    table.insert(out, seg(" " .. icons.behind .. git.behind, "BudDashboardDim"))
  end
  return out
end

local function projects(list)
  local lines = { { seg("  " .. icons.projects .. " ", "BudDashboardIcon"), seg("Projects", "BudDashboardHeader") } }
  if #list == 0 then
    table.insert(lines, { seg("  none yet: open one with bud <dir>", "BudDashboardDim") })
    return lines
  end
  local name_width = 0
  for _, p in ipairs(list) do
    name_width = math.max(name_width, vim.fn.strdisplaywidth(p.name))
  end
  for _, p in ipairs(list) do
    local line = {
      seg("  " .. p.key, "BudDashboardKey"),
      seg(
        "  " .. (p.pinned and icons.pinned or icons.unpinned) .. " ",
        p.pinned and "BudDashboardIcon" or "BudDashboardDim"
      ),
      seg(pad(p.name, name_width + 2)),
    }
    vim.list_extend(line, git_status(p.git))
    table.insert(lines, line)
  end
  return lines
end

local levels = { error = "BudDashboardError", warn = "BudDashboardWarn", info = "BudDashboardDim" }

-- silly is the full dashboard: robot, menu with icons, projects with git,
-- banners, health line and tip
local function silly(data)
  local block = {}
  local function add(lines)
    vim.list_extend(block, lines)
  end
  local blank = { {} }

  add(art(data))
  add(blank)
  add(menu(data.menu))
  add(blank)
  add(projects(data.projects))
  if #data.banners > 0 then
    add(blank)
    for _, b in ipairs(data.banners) do
      add({ { seg("  " .. (icons[b.level] or icons.tip) .. " " .. b.text, levels[b.level]) } })
    end
  end
  add(blank)
  if data.health then
    add({ { seg("  " .. data.health, "BudDashboardDim") } })
  end
  if data.tip then
    add({
      {
        seg("  " .. icons.tip .. " ", "BudDashboardIcon"),
        seg("tip: ", "BudDashboardDim"),
        seg(data.tip.lhs, "BudDashboardKey"),
        seg("  " .. data.tip.desc),
      },
    })
  end
  return block
end

-- minimal (BASE.md 5.4) keeps only the essentials: a one-line title, the keys
-- four to a row, and one bottom line with the projects and, at the far end,
-- a single symbol when doctor has something to say
local function minimal(data)
  local block =
    { { seg("  " .. M.faces[data.mood or "normal"] .. " ", "BudDashboardArt"), seg("bud", "BudDashboardTitle") }, {} }

  local per_row, cell = 4, 14
  for i = 1, #data.menu, per_row do
    local line = {}
    for j = i, math.min(i + per_row - 1, #data.menu) do
      local item = data.menu[j]
      table.insert(line, seg("  " .. item.key, "BudDashboardKey"))
      table.insert(line, seg(pad("  " .. (item.short or item.label:lower()), cell - 3)))
    end
    table.insert(block, line)
  end
  table.insert(block, {})

  local bottom = {}
  for _, p in ipairs(data.projects) do
    table.insert(bottom, seg("  " .. p.key, "BudDashboardKey"))
    table.insert(bottom, seg(" " .. p.name))
  end
  if #data.projects == 0 then
    table.insert(bottom, seg("  no projects yet", "BudDashboardDim"))
  end
  -- the worst banner's icon only, right-aligned under the menu
  local worst
  for _, b in ipairs(data.banners) do
    if b.level == "error" or not worst then
      worst = b
    end
  end
  if worst then
    local keys_width = 0
    for _, line in ipairs(block) do
      keys_width = math.max(keys_width, width(line))
    end
    table.insert(bottom, seg(string.rep(" ", math.max(2, keys_width - width(bottom) - 1))))
    table.insert(bottom, seg(icons[worst.level] or icons.warn, levels[worst.level]))
  end
  table.insert(block, bottom)
  return block
end

-- build returns the dashboard's lines (padded to center it in width x height)
-- and its highlights as { row, start_col, end_col, group }, 0-based bytes.
-- data.style picks the layout: "silly" (the default) or "minimal"
function M.build(data, win_width, win_height)
  local block = data.style == "minimal" and minimal(data) or silly(data)

  local block_width = 0
  for _, line in ipairs(block) do
    block_width = math.max(block_width, width(line))
  end
  local left = string.rep(" ", math.max(0, math.floor((win_width - block_width) / 2)))
  local top = math.max(0, math.floor((win_height - #block) / 2))

  local lines, hls = {}, {}
  for _ = 1, top do
    table.insert(lines, "")
  end
  for _, line in ipairs(block) do
    local row, text = #lines, left
    for _, s in ipairs(line) do
      if s[2] and s[1] ~= "" then
        table.insert(hls, { row, #text, #text + #s[1], s[2] })
      end
      text = text .. s[1]
    end
    table.insert(lines, (text:gsub("%s+$", "")))
  end
  return lines, hls
end

return M
