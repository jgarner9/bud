-- Bud's Neovim entry point. bud launches nvim with NVIM_APPNAME=bud, so this
-- lives at ~/.config/bud and never touches an existing ~/.config/nvim.

-- leader has to be set before plugins load so their keymaps pick it up
vim.g.mapleader = " "
vim.g.maplocalleader = "\\"

require("bud.core")
require("bud.pack").setup(require("bud.plugins"))
