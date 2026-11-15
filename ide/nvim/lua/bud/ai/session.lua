-- The configured backend's session (D131): bud.ai.agent over ACP, or, for
-- ai.backend = "claude" until 1.1 task 6 moves it onto claude-agent-acp,
-- bud.ai.claude. Both have the same calls and events (session, send, on,
-- interrupt, status, stop, restart, drop, get, list), so a consumer requires
-- this and doesn't care which runs. Each call goes to the backend configured
-- at the time of the call.
--
--   local ai = require("bud.ai.session")
--   ai.on("done", function(d, s) ... end)
--   ai.send("draft a commit message", { key = "commit" })

local M = {}

-- module is the module the configured backend runs on
function M.module()
  local agent = require("bud.ai.agent")
  if agent.backend() == "claude" then
    return require("bud.ai.claude")
  end
  return agent
end

return setmetatable(M, {
  __index = function(_, key)
    return M.module()[key]
  end,
})
