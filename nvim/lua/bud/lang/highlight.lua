-- Tree-sitter highlighting for the enabled languages' filetypes (JGA-184).
-- It starts a tick after the buffer's FileType, after the first screen
-- (D35), since the first start in a session is slow. Until then nothing
-- colours the buffer: Neovim's syntaxset autocmd skips the regex syntax for a
-- buffer with b:ts_highlight, which attach sets at once, because the regex
-- syntax it would load is thrown away when tree-sitter starts and costs
-- startup (html.vim with css.vim is about 13 ms). Where tree-sitter can't
-- start (its parser isn't built yet) the regex syntax comes back. It never
-- touches a filetype it isn't given.

local M = {}

-- starts tree-sitter on a buffer; swapped in tests
M.start = vim.treesitter.start

--- attach holds back the regex syntax for buf and starts tree-sitter on it
--- on the next tick, putting the regex syntax back if that fails.
function M.attach(buf)
  vim.b[buf].ts_highlight = true
  vim.schedule(function()
    if not vim.api.nvim_buf_is_valid(buf) then
      return
    end
    if not pcall(M.start, buf) then
      vim.b[buf].ts_highlight = nil
      vim.bo[buf].syntax = "ON"
    end
  end)
end

--- setup attaches every buffer of filetypes as its FileType is set. Its
--- autocmd has to come before Neovim's syntaxset one, which `syntax on`
--- defines after init.lua, so it's set up from the config.
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
