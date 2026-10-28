-- bud's statusline (JGA-133): one across all windows (laststatus=3, set in
-- core/options.lua), built from mini.statusline's sections plus bud's own.
-- Left: mode, git and diff, the file relative to the project. Right:
-- diagnostics, the buffer's language servers, bud's sections, the location.
-- Other modules add a section with add(); it shows up in its own block just
-- before the location.

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

-- file is the current buffer's name relative to the project (just the tail when
-- short is set), with markers for modified and read-only; special buffers
-- (the tree, help, terminals) show what they are instead of their name
function M.file(short)
  local icons = require("bud.icons")
  local bt = vim.bo.buftype
  if bt == "terminal" then
    return "terminal"
  elseif bt ~= "" then
    return vim.bo.filetype ~= "" and vim.bo.filetype or bt
  end
  local name = vim.fn.expand(short and "%:t" or "%:~:.")
  if name == "" then
    name = "[No Name]"
  end
  -- a % in a file name would start a statusline item
  name = name:gsub("%%", "%%%%")
  if vim.bo.modified then
    name = name .. " " .. icons.modified
  end
  if vim.bo.readonly or not vim.bo.modifiable then
    name = name .. " " .. icons.locked
  end
  return name
end

-- lsp names the language servers attached to the current buffer
function M.lsp()
  local names = {}
  for _, client in ipairs(vim.lsp.get_clients({ bufnr = 0 })) do
    table.insert(names, client.name)
  end
  if #names == 0 then
    return ""
  end
  return require("bud.icons").lsp .. " " .. table.concat(names, " ")
end

function M.active()
  local sl = require("mini.statusline")
  local mode, mode_hl = sl.section_mode({ trunc_width = 120 })
  local git = sl.section_git({ trunc_width = 40 })
  local diff = sl.section_diff({ trunc_width = 75 })
  local diagnostics = sl.section_diagnostics({ trunc_width = 75 })
  local lsp = sl.is_truncated(100) and "" or M.lsp()
  local location = sl.section_location({ trunc_width = 75 })

  return sl.combine_groups({
    { hl = mode_hl, strings = { mode } },
    { hl = "MiniStatuslineDevinfo", strings = { git, diff } },
    "%<",
    { hl = "MiniStatuslineFilename", strings = { M.file(sl.is_truncated(140)) } },
    "%=",
    { hl = "MiniStatuslineFilename", strings = { diagnostics } },
    { hl = "MiniStatuslineFileinfo", strings = { lsp } },
    { hl = "BudStatuslineAI", strings = { extras() } },
    { hl = mode_hl, strings = { location } },
  })
end

function M.setup()
  -- Claude and bud's other sections get their own colour; a colorscheme can
  -- set BudStatuslineAI to change it
  vim.api.nvim_set_hl(0, "BudStatuslineAI", { link = "MiniStatuslineModeCommand", default = true })
  require("mini.statusline").setup({ content = { active = M.active } })
end

return M
