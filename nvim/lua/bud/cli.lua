-- Calling back into the bud CLI from the editor (the dashboard's doctor run,
-- registering projects). Only the CLI writes bud's state files, so Lua asks it.

local M = {}

-- bin is the bud that launched this nvim (BUD_BIN, set by nvim.Launch), else
-- the one on PATH, else nil
function M.bin()
  local bin = vim.env.BUD_BIN
  if bin and vim.fn.executable(bin) == 1 then
    return bin
  end
  local found = vim.fn.exepath("bud")
  return found ~= "" and found or nil
end

return M
