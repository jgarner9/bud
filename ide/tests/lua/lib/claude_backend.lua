-- For the tests that still drive bud.ai.claude directly (it goes with
-- JGA-402): bud.ai.session and bud.ai.agent.backend() answer for Claude Code,
-- as they did before the IDE talked only to OpenCode (D139).
--
--   dofile("tests/lua/lib/claude_backend.lua")

require("bud.ai.session").module = function()
  return require("bud.ai.claude")
end
require("bud.ai.agent").backend = function()
  return "claude"
end
