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

-- Files Neovim detects by name, in a table it checks before any pattern, that
-- grow big (lockfiles, ctags' tags), with the filetype it gives each (a test
-- keeps these equal to Neovim's)
M.named = {
  ["go.sum"] = "gosum",
  ["go.work.sum"] = "gosum",
  ["Gopkg.lock"] = "toml",
  ["Cargo.lock"] = "toml",
  ["uv.lock"] = "toml",
  ["yarn.lock"] = "yaml",
  ["pixi.lock"] = "yaml",
  ["composer.lock"] = "json",
  ["symfony.lock"] = "json",
  ["flake.lock"] = "json",
  ["Pipfile.lock"] = "json",
  ["deno.lock"] = "json",
  ["bun.lock"] = "jsonc",
  ["mix.lock"] = "elixir",
  ["opam.locked"] = "opam",
  ["tags"] = "tags",
}

-- true while finding a big file's real filetype, so detection gives it
local detecting = false

-- big says whether path, loaded in buf (so it has at least one line), is a big file
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

-- same says whether path and name are one file. Neovim can name a buffer by its
-- resolved path while detection gets the path as typed (macOS's /var is
-- /private/var), so a mismatch is checked again with symlinks resolved.
function M.same(path, name)
  if path == vim.fs.normalize(name) then
    return true
  end
  local a, b = vim.uv.fs_realpath(path), vim.uv.fs_realpath(name)
  return a ~= nil and a == b
end

-- is says whether buf was opened as a big file
function M.is(buf)
  if buf == 0 then
    buf = vim.api.nvim_get_current_buf()
  end
  return vim.bo[buf].filetype == "bigfile" and vim.b[buf].bud_bigfile ~= nil
end

-- filetype is buf's real filetype: what a big file would have been, else its own
function M.filetype(buf)
  if buf == 0 then
    buf = vim.api.nvim_get_current_buf()
  end
  return M.is(buf) and vim.b[buf].bud_bigfile or vim.bo[buf].filetype
end

-- check is the detection rule: "bigfile" for the file loaded in buf when it's
-- big, with its real filetype kept in b:bud_bigfile, else nil (and nil while
-- finding that real filetype)
local function check(path, buf)
  if detecting or not path or not buf or buf <= 0 or not vim.api.nvim_buf_is_loaded(buf) then
    return nil
  end
  -- only for the file loaded in buf, not a name matched on its own
  if not M.same(path, vim.api.nvim_buf_get_name(buf)) then
    return nil
  end
  if not M.big(path, buf) then
    -- a file that shrank is itself again, and is announced if it grows back
    vim.b[buf].bud_bigfile = nil
    return nil
  end
  -- found here, during detection: Neovim's own detectors for shell scripts,
  -- conf files and the like give nothing once a FileType event has fired
  detecting = true
  local ok, ft, on_detect = pcall(vim.filetype.match, { buf = buf })
  detecting = false
  if vim.b[buf].bud_bigfile == nil then
    vim.b[buf].bud_bigfile_new = true
  end
  vim.b[buf].bud_bigfile = ok and ft or ""
  -- what detection sets up for the real filetype (make's dialect, say), for its syntax
  if ok and on_detect then
    pcall(on_detect, buf)
  end
  return "bigfile"
end

-- start sets buf up as a big file in the current window, saying so the first time
local function start(buf)
  vim.b[buf].completion = false
  -- this window's own values, not its default: one that showed a rendered
  -- markdown file or tree-sitter folds would otherwise pass them on
  vim.wo[0][0].foldmethod = "manual"
  vim.wo[0][0].conceallevel = 0
  -- after the first screen: regex syntax on a big file isn't free either
  vim.schedule(function()
    if vim.api.nvim_buf_is_valid(buf) and M.is(buf) then
      vim.bo[buf].syntax = vim.b[buf].bud_bigfile
    end
  end)
  if vim.b[buf].bud_bigfile_new then
    vim.b[buf].bud_bigfile_new = nil
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
    -- first of all the patterns, which Neovim checks before extensions
    pattern = { [".*"] = { check, { priority = math.huge } } },
  })
  vim.api.nvim_create_autocmd("FileType", {
    group = vim.api.nvim_create_augroup("bud.bigfile", { clear = true }),
    pattern = "bigfile",
    desc = "Set a big file up without the per-filetype features",
    callback = function(ev)
      if M.is(ev.buf) then
        vim.api.nvim_buf_call(ev.buf, function()
          start(ev.buf)
        end)
      end
    end,
  })
end

return M
