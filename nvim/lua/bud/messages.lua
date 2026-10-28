-- The command line and messages in floats (JGA-137): Neovim's own ui2, which
-- does what noice.nvim did, built in. It's experimental in 0.12, so
-- ui.messages = false in config.json turns it off. Loaded from bud.core, since
-- it has to be on before the first message.

local M = {}

-- the module's name moved during 0.12; nil when this Neovim has neither
function M.module()
  for _, name in ipairs({ "vim._core.ui2", "vim._extui" }) do
    local ok, mod = pcall(require, name)
    if ok and type(mod) == "table" and mod.enable then
      return mod
    end
  end
end

function M.setup()
  if require("bud.config").get("ui.messages", true) == false then
    return false
  end
  local ui2 = M.module()
  if not ui2 then
    return false
  end
  -- the defaults: messages in the cmdline area like before, but long ones
  -- collapse to a [+N] marker instead of the hit-enter prompt (g< shows them)
  return (pcall(ui2.enable, {}))
end

return M
