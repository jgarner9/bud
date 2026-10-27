-- bud's statusline: mini.statusline's sections plus bud's own. Other modules add
-- a section with add(), and it shows up just before the cursor location; the
-- Claude Code session (BASE.md 4.2) will use this for its status.

local M = {}

local extra = {}

-- add registers fn, which returns the text to show (or "" for nothing). Keep it
-- cheap: it runs on every redraw
function M.add(fn)
  table.insert(extra, fn)
end

local function extras()
  local parts = {}
  for _, fn in ipairs(extra) do
    local ok, text = pcall(fn)
    if ok and text and text ~= "" then
      table.insert(parts, text)
    end
  end
  return table.concat(parts, " ")
end

function M.active()
  local sl = require("mini.statusline")
  local mode, mode_hl = sl.section_mode({ trunc_width = 120 })
  local git = sl.section_git({ trunc_width = 40 })
  local diff = sl.section_diff({ trunc_width = 75 })
  local diagnostics = sl.section_diagnostics({ trunc_width = 75 })
  local filename = sl.section_filename({ trunc_width = 140 })
  local fileinfo = sl.section_fileinfo({ trunc_width = 120 })
  local location = sl.section_location({ trunc_width = 75 })

  return sl.combine_groups({
    { hl = mode_hl, strings = { mode } },
    { hl = "MiniStatuslineDevinfo", strings = { git, diff, diagnostics } },
    "%<",
    { hl = "MiniStatuslineFilename", strings = { filename } },
    "%=",
    { hl = "MiniStatuslineFileinfo", strings = { fileinfo } },
    { hl = "MiniStatuslineDevinfo", strings = { extras() } },
    { hl = mode_hl, strings = { location } },
  })
end

return M
