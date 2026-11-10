-- bud's statusline (JGA-133): one across all windows (laststatus=3, set in
-- core/options.lua), built from mini.statusline's sections plus bud's own.
-- Left: mode, git and diff, the file relative to the project. Right:
-- diagnostics, the buffer's language servers, bud's sections, the location.
-- Every number carries its name ("2 errors", "Ln 42, Col 7", "37% of 580").
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

-- lsp names the language servers attached to the current buffer. Until
-- something has loaded vim.lsp no server can be attached, and touching it here
-- would load it (~4 ms) on the first screen
function M.lsp()
  if not package.loaded["vim.lsp"] then
    return ""
  end
  local names = {}
  for _, client in ipairs(vim.lsp.get_clients({ bufnr = 0 })) do
    table.insert(names, client.name)
  end
  if #names == 0 then
    return ""
  end
  return require("bud.icons").lsp .. " LSP: " .. table.concat(names, " ")
end

-- diagnostics counts the current buffer's problems with a name on each number:
-- "2 errors 1 warning" when there's room, "E2 W1" when narrow. Hints and infos
-- only show when there are some, and a clean buffer shows nothing
function M.diagnostics(short)
  local counts = vim.diagnostic.count(0)
  local kinds = {
    { vim.diagnostic.severity.ERROR, "E", "error" },
    { vim.diagnostic.severity.WARN, "W", "warning" },
    { vim.diagnostic.severity.INFO, "I", "info" },
    { vim.diagnostic.severity.HINT, "H", "hint" },
  }
  local parts = {}
  for _, kind in ipairs(kinds) do
    local n = counts[kind[1]] or 0
    if n > 0 then
      if short then
        table.insert(parts, kind[2] .. n)
      else
        -- "info" is its own plural
        local plural = (n == 1 or kind[3] == "info") and "" or "s"
        table.insert(parts, ("%d %s%s"):format(n, kind[3], plural))
      end
    end
  end
  return table.concat(parts, " ")
end

-- position says where the cursor is in words, not bare numbers: "Ln 42, Col 7"
-- and "37% of 580" (lines in the file), or just "42:7" when narrow. It returns
-- a statusline format string: %l, %v, %p and %L are items the statusline fills
-- in, and %% is a literal percent sign
function M.position(short)
  if short then
    return "%l:%v"
  end
  return "Ln %l, Col %v  %p%% of %L"
end

function M.active()
  local sl = require("mini.statusline")
  local mode, mode_hl = sl.section_mode({ trunc_width = 120 })
  local git = sl.section_git({ trunc_width = 40 })
  local diff = sl.section_diff({ trunc_width = 75 })
  local diagnostics = sl.is_truncated(75) and "" or M.diagnostics(sl.is_truncated(110))
  local lsp = sl.is_truncated(100) and "" or M.lsp()
  local location = M.position(sl.is_truncated(75))

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
