-- Run by scripts/smoke.sh inside headless Neovim, after bud's config has loaded.
-- Quits with exit code 1 and a reason on stderr when anything went wrong.

local problems = {}

if vim.v.errmsg ~= "" then
  table.insert(problems, "error during startup: " .. vim.v.errmsg)
end

local ok, lazy = pcall(require, "lazy")
if not ok then
  table.insert(problems, "lazy.nvim didn't load: " .. tostring(lazy))
else
  for _, plugin in ipairs(lazy.plugins()) do
    if not plugin._.installed then
      table.insert(problems, "plugin not installed: " .. plugin.name)
    end
  end
end

if #problems > 0 then
  -- Neovim's own error output may not end in a newline. selene's Lua 5.1
  -- definitions leave out file methods on io.stderr, so it flags this wrongly
  -- selene: allow(incorrect_standard_library_use)
  io.stderr:write("\n" .. table.concat(problems, "\n") .. "\n")
  vim.cmd("cquit 1")
end
vim.cmd("qall!")
