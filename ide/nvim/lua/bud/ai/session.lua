-- The session every consumer uses (D131, D139): bud.ai.agent, OpenCode over
-- ACP. Consumers require this and call session, send, on, interrupt, status,
-- stop, restart, drop, get and list on it.
--
--   local ai = require("bud.ai.session")
--   ai.on("done", function(d, s) ... end)
--   ai.send("draft a commit message", { key = "commit" })

local M = {}

-- module is the module the session runs on (a test can swap it)
function M.module()
  return require("bud.ai.agent")
end

return setmetatable(M, {
  __index = function(_, key)
    return M.module()[key]
  end,
})
