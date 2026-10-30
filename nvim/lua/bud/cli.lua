-- Calling back into the bud CLI from the editor (the dashboard's doctor run,
-- registering projects, :BudUpdate). Only the CLI writes bud's state files, so Lua asks it.

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

-- terminal runs a bud command in a new terminal buffer (the dashboard's d
-- and u, :BudUpdate), for ones that may ask something
function M.terminal(args)
  local bin = M.bin()
  if not bin then
    vim.notify("bud: can't find the bud binary", vim.log.levels.ERROR)
    return
  end
  vim.cmd.enew()
  vim.fn.jobstart(vim.list_extend({ bin }, args), { term = true })
  vim.cmd.startinsert()
end

return M
