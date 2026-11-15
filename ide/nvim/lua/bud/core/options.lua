local opt = vim.opt

-- no remote plugins, so no providers: checking for a Python host alone costs
-- ~60 ms the first time a Python file opens (D35)
for _, provider in ipairs({ "python3", "ruby", "perl", "node" }) do
  vim.g["loaded_" .. provider .. "_provider"] = 0
end

opt.number = true
opt.relativenumber = true
opt.signcolumn = "yes"
opt.termguicolors = true
opt.mouse = "a"
-- the system clipboard, but not over SSH (D95): there it falls back to OSC 52,
-- and many terminals refuse to answer its paste request, so every p waits for
-- a timeout. "+y and "+p still use it there when you ask
opt.clipboard = vim.env.SSH_CONNECTION and "" or "unnamedplus"

opt.expandtab = true
opt.shiftwidth = 2
opt.tabstop = 2
opt.smartindent = true

opt.ignorecase = true
opt.smartcase = true

opt.splitright = true
opt.splitbelow = true
opt.scrolloff = 8
-- one statusline for the whole screen, like an IDE (JGA-133)
opt.laststatus = 3
-- no ~ on the empty lines past the end of a buffer, in files or the tree (JGA-134)
opt.fillchars:append({ eob = " " })

opt.undofile = true
opt.swapfile = false
opt.updatetime = 250
