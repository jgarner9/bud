-- For tests of the claude backend (bud.ai.claude and Claude Code's TUI in the
-- AI pane): ai.backend reads "claude" unless the test's config.json says
-- otherwise, whatever the default is (claude for now, D140).
--
--   dofile("tests/lua/lib/claude_backend.lua")

local config = require("bud.config")
local get = config.get
config.get = function(key, default)
  if key == "ai.backend" then
    return get(key, "claude")
  end
  return get(key, default)
end
