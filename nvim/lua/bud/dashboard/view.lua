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

local icons = require("bud.icons")
local menu_width = 30

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

-- the buddy's idle moves (JGA-155): each is a list of frames, shown about
-- 150 ms apart, that change a part of the art; the mood face shows between
-- them. A frame never changes the art's width, so nothing around it moves
M.moves = {
  blink = { { face = "[-.-]" } },
  look = { { face = "[<.<]" }, { face = "[<.<]" }, { face = "[>.>]" }, { face = "[>.>]" } },
  antenna = { { top = "_*_" }, { top = "___" }, { top = "_*_" } },
  wave = { { wave = true }, {}, { wave = true }, {}, { wave = true } },
}
-- asleep, it only snores
M.snore = { { z = "Z" }, {}, { z = "Z" } }

local function art(data)
  local frame = data.frame or {}
  local face = frame.face or M.faces[data.mood] or M.faces.normal
  local sleepy = data.mood == "sleepy"
  local z = frame.z or "z"
  return {
    { seg("   " .. (frame.top or "___"), "BudDashboardArt"), seg(sleepy and "     " .. z or "", "BudDashboardDim") },
    {
      seg("  " .. face, "BudDashboardArt"),
      -- the raised arm takes the spacer's first cell
      seg(frame.wave and "/" or "", "BudDashboardArt"),
      seg(sleepy and "  " .. z .. "     " or (frame.wave and "       " or "        "), "BudDashboardDim"),
      seg("hey bud.", "BudDashboardTitle"),
    },
    {
      seg(frame.wave and "  /[_] " or "  /[_]\\", "BudDashboardArt"),
      seg("        "),
      seg('"' .. data.tagline .. '"', "BudDashboardDim"),
    },
    { seg("   ] [", "BudDashboardArt") },
  }
end

-- menu is two columns of key, icon and label; roomy puts a blank line
-- between the rows
local function menu(items, roomy)
  local lines = {}
  local half = math.ceil(#items / 2)
  for i = 1, half do
    local line = {}
    for _, item in ipairs({ items[i], items[i + half] }) do
      table.insert(line, seg("  " .. item.key, "BudDashboardKey"))
      table.insert(line, seg("   " .. (icons[item.icon] or " "), "BudDashboardIcon"))
      table.insert(line, seg(pad("  " .. item.label, menu_width - 7)))
    end
    if roomy and i > 1 then
      table.insert(lines, {})
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
  local out = { seg(icons.branch .. "  " .. git.branch .. "   ", "BudDashboardDim") }
  if git.changed > 0 then
    table.insert(out, seg(icons.changed .. "  " .. git.changed, "BudDashboardWarn"))
  else
    table.insert(out, seg(icons.clean, "BudDashboardOk"))
  end
  if git.ahead > 0 then
    table.insert(out, seg("   " .. icons.ahead .. " " .. git.ahead, "BudDashboardDim"))
  end
  if git.behind > 0 then
    table.insert(out, seg("   " .. icons.behind .. " " .. git.behind, "BudDashboardDim"))
  end
  return out
end

local function projects(list)
  local lines = {}
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
        "   " .. (p.pinned and icons.pinned or icons.unpinned) .. "  ",
        p.pinned and "BudDashboardIcon" or "BudDashboardDim"
      ),
      seg(pad(p.name, name_width + 4)),
    }
    vim.list_extend(line, git_status(p.git))
    -- optional extras (JGA-97): open PRs and review requests, TODO count
    if p.prs and (p.prs.open > 0 or p.prs.review > 0) then
      table.insert(line, seg("   " .. icons.pr .. "  " .. p.prs.open, "BudDashboardDim"))
      if p.prs.review > 0 then
        table.insert(line, seg("   " .. icons.review .. "  " .. p.prs.review, "BudDashboardWarn"))
      end
    end
    if p.todos and p.todos > 0 then
      table.insert(line, seg("   " .. icons.todo .. "  " .. p.todos, "BudDashboardDim"))
    end
    table.insert(lines, line)
  end
  return lines
end

local levels =
  { error = "BudDashboardError", warn = "BudDashboardWarn", info = "BudDashboardDim", update = "BudDashboardOk" }

-- box outlines a section (JGA-155): its title in the top border, its lines
-- padded to inner cells between the sides. joined makes the top a divider
-- under the box before it, whose bottom the caller drops
local function box(title, lines, inner, joined)
  local border = "BudDashboardBorder"
  local top = { seg(" "), seg(joined and "├─ " or "╭─ ", border) }
  if title.icon then
    table.insert(top, seg(title.icon .. " ", "BudDashboardIcon"))
  end
  table.insert(top, seg(title.text, "BudDashboardHeader"))
  local used = (title.icon and 2 or 0) + vim.fn.strdisplaywidth(title.text) + 3
  table.insert(top, seg(" " .. string.rep("─", inner - used) .. (joined and "┤" or "╮"), border))
  local out = { top }
  for _, line in ipairs(lines) do
    -- the padding stays out of the border's highlight, so a border with its
    -- own background only colours the sides
    local row = { seg(" "), seg("│", border) }
    vim.list_extend(row, line)
    table.insert(row, seg(string.rep(" ", inner - width(line))))
    table.insert(row, seg("│", border))
    table.insert(out, row)
  end
  table.insert(out, { seg(" "), seg("╰" .. string.rep("─", inner) .. "╯", border) })
  return out
end

-- silly is the full dashboard: robot, then boxes for the menu, projects with
-- git, recent chats and the status (banners, health line and tip)
-- roomy spaces it out (a gap between menu rows and between boxes); build
-- falls back to the tight version, the boxes joined, when that won't fit
local function silly(data, roomy)
  local block = art(data)
  if data.clock then
    table.insert(block, { seg("  " .. icons.clock .. "  " .. data.clock, "BudDashboardDim") })
  end

  local sections = {
    { title = { text = "Menu" }, lines = menu(data.menu, roomy) },
    { title = { icon = icons.projects, text = "Projects" }, lines = projects(data.projects) },
  }
  if data.chats and #data.chats > 0 then
    local chats = {}
    for _, c in ipairs(data.chats) do
      table.insert(chats, { seg("  " .. c.key, "BudDashboardKey"), seg("     " .. c.title) })
    end
    table.insert(sections, { title = { icon = icons.chat, text = "Recent chats" }, lines = chats })
  end
  local status = {}
  for _, b in ipairs(data.banners) do
    table.insert(status, { seg("  " .. (icons[b.level] or icons.tip) .. "  " .. b.text, levels[b.level]) })
  end
  if data.health then
    table.insert(status, { seg("  " .. data.health, "BudDashboardDim") })
  end
  if data.tip then
    table.insert(status, {
      seg("  " .. icons.tip .. "  ", "BudDashboardIcon"),
      seg("tip: ", "BudDashboardDim"),
      seg(data.tip.lhs, "BudDashboardKey"),
      seg("  " .. data.tip.desc),
    })
  end
  if #status > 0 then
    table.insert(sections, { title = { text = "Status" }, lines = status })
  end

  -- every box as wide as the widest section, with two cells to spare
  local inner = 0
  for _, sec in ipairs(sections) do
    inner = math.max(inner, vim.fn.strdisplaywidth(sec.title.text) + 8)
    for _, line in ipairs(sec.lines) do
      inner = math.max(inner, width(line) + 2)
    end
  end
  -- the robot (and clock) centered over the boxes
  local top_width = 0
  for _, line in ipairs(block) do
    top_width = math.max(top_width, width(line))
  end
  local indent = string.rep(" ", math.max(0, math.floor((inner + 3 - top_width) / 2)))
  for _, line in ipairs(block) do
    table.insert(line, 1, seg(indent))
  end
  -- roomy: separate boxes a line apart; tight: one box with titled dividers
  table.insert(block, {})
  for i, sec in ipairs(sections) do
    local joined = i > 1 and not roomy
    if joined then
      table.remove(block)
    elseif i > 1 then
      table.insert(block, {})
    end
    vim.list_extend(block, box(sec.title, sec.lines, inner, joined))
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
  local block
  if data.style == "minimal" then
    block = minimal(data)
  else
    -- roomy when it fits with a line to spare above and below
    block = silly(data, true)
    if #block > win_height - 2 then
      block = silly(data, false)
    end
  end

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
