-- Bud's Neovim entry point. bud launches nvim with NVIM_APPNAME=bud, so this
-- lives at ~/.config/bud and never touches an existing ~/.config/nvim.

-- for the dashboard's health line: the first screen is timed from when bud
-- started; BUD_LAUNCHED is cleared so nothing started from here inherits it
local startup = require("bud.startup")
startup.start(vim.env.BUD_LAUNCHED)
vim.env.BUD_LAUNCHED = nil
startup.watch()

-- leader has to be set before plugins load so their keymaps pick it up
vim.g.mapleader = " "
vim.g.maplocalleader = "\\"

require("bud.core")
require("bud.pack").setup(require("bud.plugins"))
