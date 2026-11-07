-- Big files (D94): generated code, logs, lockfiles, minified bundles. One over
-- 1.5 MB, or whose lines average over 1000 characters, opens with filetype
-- "bigfile", so nothing that hangs off its real filetype starts: tree-sitter,
-- language servers, linting, formatting, markdown rendering. Completion is off
-- for it too. Vim's regex syntax for the real filetype still colours it, and
-- everything else (search, editing, git signs, which cap themselves) works.
-- The same rule and limits as LazyVim's (snacks.nvim's bigfile).

local M = {}

-- swappable in tests
M.size = 1.5 * 1024 * 1024 -- bytes
M.line_length = 1000 -- average, which catches minified files

-- Lockfiles Neovim detects by name, in a table it checks before any pattern,
-- with the filetype it gives each (a test keeps these equal to Neovim's)
M.named = {
  ["go.sum"] = "gosum",
  ["go.work.sum"] = "gosum",
  ["Cargo.lock"] = "toml",
  ["yarn.lock"] = "yaml",
  ["composer.lock"] = "json",
  ["flake.lock"] = "json",
  ["Pipfile.lock"] = "json",
  ["mix.lock"] = "elixir",
  ["uv.lock"] = "toml",
  ["bun.lock"] = "jsonc",
  ["deno.lock"] = "json",
}

-- true while finding a big file's real filetype, so detection gives it
local detecting = false

-- big says whether path, loaded in buf, is a big file
function M.big(path, buf)
  local size = vim.fn.getfsize(path)
  if size <= 0 then
    return false
  end
  if size > M.size then
    return true
  end
  local lines = vim.api.nvim_buf_line_count(buf)
  if lines == 0 then
    return false
  end
  return (size - lines) / lines > M.line_length
end

-- filetype is buf's real filetype: what a big file would have been, else its own
function M.filetype(buf)
  if buf == 0 then
    buf = vim.api.nvim_get_current_buf()
  end
  if vim.bo[buf].filetype == "bigfile" and vim.b[buf].bud_bigfile then
    return vim.b[buf].bud_bigfile
  end
  return vim.bo[buf].filetype
end

-- check is the detection rule: "bigfile" for the file loaded in buf when it's
-- big, nil otherwise (and while finding a big file's real filetype)
local function check(path, buf)
  if detecting or not path or not buf or buf <= 0 or not vim.api.nvim_buf_is_loaded(buf) then
    return nil
  end
  -- only for the file loaded in buf, not a name matched on its own
  if path ~= vim.fs.normalize(vim.api.nvim_buf_get_name(buf)) then
    return nil
  end
  return M.big(path, buf) and "bigfile" or nil
end

-- real is the filetype buf's file would have without this module
local function real(buf)
  detecting = true
  local ok, ft = pcall(vim.filetype.match, { buf = buf })
  detecting = false
  return ok and ft or ""
end

-- start sets buf up as a big file, saying so the first time
local function start(buf)
  local ft = real(buf)
  local noted = vim.b[buf].bud_bigfile ~= nil
  vim.b[buf].bud_bigfile = ft
  vim.b[buf].completion = false
  -- after the first screen: regex syntax on a big file isn't free either
  vim.schedule(function()
    if vim.api.nvim_buf_is_valid(buf) then
      vim.bo[buf].syntax = ft
    end
  end)
  if not noted then
    vim.notify(
      ("bud: %s is a big file, so tree-sitter, language servers and completion are off for it"):format(
        vim.fn.fnamemodify(vim.api.nvim_buf_get_name(buf), ":t")
      ),
      vim.log.levels.INFO
    )
  end
end

function M.setup()
  local filename = {}
  for name, ft in pairs(M.named) do
    filename[name] = function(path, buf)
      return check(path, buf) or ft
    end
  end
  vim.filetype.add({
    filename = filename,
    -- the default priority runs before extensions, so this sees every other file
    pattern = { [".*"] = check },
  })
  vim.api.nvim_create_autocmd("FileType", {
    group = vim.api.nvim_create_augroup("bud.bigfile", { clear = true }),
    pattern = "bigfile",
    desc = "Set a big file up without the per-filetype features",
    callback = function(ev)
      if vim.api.nvim_buf_get_name(ev.buf) ~= "" then
        start(ev.buf)
      end
    end,
  })
end

return M
