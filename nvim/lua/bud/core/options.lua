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
opt.clipboard = "unnamedplus"

opt.expandtab = true
opt.shiftwidth = 2
opt.tabstop = 2
opt.smartindent = true

opt.ignorecase = true
opt.smartcase = true

opt.splitright = true
opt.splitbelow = true
opt.scrolloff = 8

opt.undofile = true
opt.swapfile = false
opt.updatetime = 250
