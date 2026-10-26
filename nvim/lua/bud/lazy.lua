-- Bootstraps lazy.nvim and loads every spec under lua/bud/plugins/.
-- Plugin versions come from the committed lazy-lock.json; `bud install` and
-- `bud update` run `Lazy! restore` so every machine lands on the same snapshot.

local lazypath = vim.fn.stdpath("data") .. "/lazy/lazy.nvim"
if not vim.uv.fs_stat(lazypath) then
  local out = vim.fn.system({
    "git",
    "clone",
    "--filter=blob:none",
    "--branch=stable",
    "https://github.com/folke/lazy.nvim.git",
    lazypath,
  })
  if vim.v.shell_error ~= 0 then
    vim.api.nvim_echo({
      { "Failed to clone lazy.nvim:\n", "ErrorMsg" },
      { out, "WarningMsg" },
    }, true, {})
    return
  end
end
vim.opt.rtp:prepend(lazypath)

require("lazy").setup({
  spec = {
    -- keep lazy.nvim itself on tagged releases when the lockfile is bumped
    { "folke/lazy.nvim", version = "*" },
    { import = "bud.plugins" },
  },
  -- updates go through `bud update` and the lockfile, never lazy's own checker
  checker = { enabled = false },
  change_detection = { notify = false },
  rocks = { enabled = false },
  install = { missing = true },
  performance = {
    rtp = {
      disabled_plugins = {
        "gzip",
        "tarPlugin",
        "tohtml",
        "tutor",
        "zipPlugin",
      },
    },
  },
})
