-- Tree-sitter highlighting for the enabled languages' filetypes (JGA-184).
-- It starts a tick after the buffer's FileType, after the first screen
-- (D35), since the first start in a session is slow. Until then nothing
-- colours the buffer: Neovim's syntaxset autocmd skips the regex syntax for a
-- buffer with b:ts_highlight, which attach sets at once, because the regex
-- syntax it would load is thrown away when tree-sitter starts and costs
-- startup (html.vim with css.vim is about 13 ms). Where tree-sitter can't
-- start (its parser isn't built yet), or the buffer's filetype changed before
-- the tick, the regex syntax comes back the way Neovim sets it, so `:syntax
-- off`/`manual` and a syntax a modeline picked are kept. It never touches a
-- filetype it isn't given.
--
-- The saving needs bud's FileType autocmd to run before Neovim's syntaxset
-- one, which `syntax on` defines after init.lua. Nothing tests that order on
-- a real start: if something turns syntax on earlier, or nvim-treesitter's
-- spec becomes lazy, the regex syntax loads again, and bud doctor's
-- per-language startup timing is what shows it.

local M = {}

-- starts tree-sitter on a buffer; swapped in tests
M.start = vim.treesitter.start

-- filetypes whose parser couldn't start this session: their buffers keep the
-- regex syntax rather than failing again on every FileType
M.failed = {}

-- regex hands buf back to Neovim's regex syntax, as its tree-sitter
-- highlighter does when it stops: through the syntaxset autocmd, which sets
-- nothing under `:syntax off` or `manual`, and only when no syntax is set
local function regex(buf)
  vim.b[buf].ts_highlight = nil
  if vim.bo[buf].syntax ~= "" or vim.g.syntax_on ~= 1 then
    return
  end
  vim.api.nvim_buf_call(buf, function()
    pcall(vim.api.nvim_exec_autocmds, "FileType", { group = "syntaxset", buffer = buf, modeline = false })
  end)
end

--- attach holds back the regex syntax for buf and starts tree-sitter on it
--- on the next tick, handing it back to the regex syntax if that fails.
function M.attach(buf)
  local ft = vim.bo[buf].filetype
  -- already highlighted (`:e`, `:set ft=`), or its parser failed before
  if vim.treesitter.highlighter.active[buf] or M.failed[ft] then
    return
  end
  vim.b[buf].ts_highlight = true
  vim.b[buf].bud_highlight = ft
  vim.schedule(function()
    -- gone, or unloaded (start would :edit it back in; unloading also
    -- clears bud_highlight below, so this is a second guard)
    if not vim.api.nvim_buf_is_valid(buf) or not vim.api.nvim_buf_is_loaded(buf) then
      return
    end
    -- a later attach owns the buffer now
    if vim.b[buf].bud_highlight ~= ft then
      return
    end
    vim.b[buf].bud_highlight = nil
    if vim.bo[buf].filetype ~= ft then
      return regex(buf)
    end
    if not pcall(M.start, buf) then
      M.failed[ft] = true
      regex(buf)
    end
  end)
end

--- setup attaches every buffer of filetypes as its FileType is set. Its
--- autocmd has to come before Neovim's syntaxset one (see the top), so it's
--- set up from the config.
function M.setup(filetypes)
  if #filetypes == 0 then
    return
  end
  vim.api.nvim_create_autocmd("FileType", {
    group = vim.api.nvim_create_augroup("bud.lang.treesitter", {}),
    pattern = filetypes,
    desc = "start tree-sitter highlighting",
    callback = function(ev)
      M.attach(ev.buf)
    end,
  })
end

return M
