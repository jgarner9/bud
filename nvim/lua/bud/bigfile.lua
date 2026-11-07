-- Big files (D94): generated code, logs, minified bundles. One over 1.5 MB, or
-- whose lines average over 1000 characters, opens with filetype "bigfile", so
-- nothing that hangs off its real filetype starts: tree-sitter, language
-- servers, linting, formatting, markdown rendering. Completion is off for it
-- too. Vim's regex syntax for the real filetype still colours it, and
-- everything else (search, editing, git signs, which cap themselves) works.
-- The same rule and limits as LazyVim's (snacks.nvim's bigfile).

local M = {}

-- swappable in tests
M.size = 1.5 * 1024 * 1024 -- bytes
M.line_length = 1000 -- average, which catches minified files

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
  return (size - lines) / lines > M.line_length
end

-- start sets buf up as a big file of filetype ft, in the current window
local function start(buf, ft)
  vim.b[buf].completion = false
  vim.wo.foldmethod = "manual"
  vim.wo.conceallevel = 0
  -- after the first screen: regex syntax on a big file isn't free either
  vim.schedule(function()
    if vim.api.nvim_buf_is_valid(buf) then
      vim.bo[buf].syntax = ft
    end
  end)
  vim.notify(
    ("bud: %s is a big file, so tree-sitter, language servers and completion are off for it"):format(
      vim.fn.fnamemodify(vim.api.nvim_buf_get_name(buf), ":t")
    ),
    vim.log.levels.INFO
  )
end

function M.setup()
  vim.filetype.add({
    pattern = {
      -- the default priority runs before extensions, so this sees every file
      [".*"] = function(path, buf)
        -- detecting the real filetype below must get past this
        if not path or not buf or vim.bo[buf].filetype == "bigfile" then
          return
        end
        -- only for the file loaded in buf, not a name matched on its own
        if path ~= vim.fs.normalize(vim.api.nvim_buf_get_name(buf)) then
          return
        end
        return M.big(path, buf) and "bigfile" or nil
      end,
    },
  })
  vim.api.nvim_create_autocmd("FileType", {
    group = vim.api.nvim_create_augroup("bud.bigfile", { clear = true }),
    pattern = "bigfile",
    desc = "Set a big file up without the per-filetype features",
    callback = function(ev)
      vim.api.nvim_buf_call(ev.buf, function()
        start(ev.buf, vim.filetype.match({ buf = ev.buf }) or "")
      end)
    end,
  })
end

return M
