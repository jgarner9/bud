-- Puts a plugin on the runtimepath for tests (just lua-test runs nvim --clean,
-- with no plugins): bud's installed copy, else a clone at the revision
-- nvim-pack-lock.json pins, kept in the cache. probe is a file the plugin has.
--
--   dofile("tests/lua/lib/plugin.lua")("oil.nvim", "lua/oil/init.lua")

return function(name, probe, env)
  local lock = vim.json.decode(table.concat(vim.fn.readfile("nvim/nvim-pack-lock.json"), "\n"))
  local pin = lock.plugins[name]

  local candidates = { vim.fn.expand("~/.local/share/bud/site/pack/core/opt/" .. name) }
  if env and vim.env[env] and vim.env[env] ~= "" then
    table.insert(candidates, 1, vim.env[env])
  end
  for _, dir in ipairs(candidates) do
    if vim.uv.fs_stat(dir .. "/" .. probe) then
      vim.opt.rtp:prepend(dir)
      return
    end
  end

  local dir = vim.fn.stdpath("cache") .. "/bud-tests/" .. name
  if not vim.uv.fs_stat(dir .. "/" .. probe) then
    vim.fn.mkdir(vim.fs.dirname(dir), "p")
    local out = vim.fn.system({ "git", "clone", "--quiet", pin.src, dir })
    assert(vim.v.shell_error == 0, "cloning " .. name .. ": " .. out)
  end
  vim.fn.system({ "git", "-C", dir, "checkout", "--quiet", pin.rev })
  vim.opt.rtp:prepend(dir)
end
