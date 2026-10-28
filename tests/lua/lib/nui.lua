-- Puts nui.nvim on the runtimepath for tests of bud.ui and what's built on it
-- (just lua-test runs nvim --clean, with no plugins): bud's installed copy,
-- else a clone at the revision nvim-pack-lock.json pins, kept in the cache.

local lock = vim.json.decode(table.concat(vim.fn.readfile("nvim/nvim-pack-lock.json"), "\n"))
local rev = lock.plugins["nui.nvim"].rev

local candidates = { vim.fn.expand("~/.local/share/bud/site/pack/core/opt/nui.nvim") }
if vim.env.BUD_TEST_NUI and vim.env.BUD_TEST_NUI ~= "" then
  table.insert(candidates, 1, vim.env.BUD_TEST_NUI)
end
for _, dir in ipairs(candidates) do
  if vim.uv.fs_stat(dir .. "/lua/nui/popup/init.lua") then
    vim.opt.rtp:prepend(dir)
    return
  end
end

local dir = vim.fn.stdpath("cache") .. "/bud-tests/nui.nvim"
if not vim.uv.fs_stat(dir .. "/lua/nui") then
  vim.fn.mkdir(vim.fs.dirname(dir), "p")
  local out = vim.fn.system({ "git", "clone", "--quiet", "https://github.com/MunifTanjim/nui.nvim", dir })
  assert(vim.v.shell_error == 0, "cloning nui.nvim: " .. out)
end
vim.fn.system({ "git", "-C", dir, "checkout", "--quiet", rev })
vim.opt.rtp:prepend(dir)
