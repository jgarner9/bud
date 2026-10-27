# Building a Claude TUI

Every AI part of bud (the chat, the side pane, inline actions, `:BudGen`) is a consumer of one module, `bud.ai.claude`, which owns one Claude Code session per project. Yours can be too. Nothing in bud starts its own Claude Code process.

```lua
local claude = require("bud.ai.claude")

claude.session()                                  -- this project's session, starting it if needed
claude.send("explain this", { context = { "lua/x.lua#L10-20" } })
claude.interrupt()                                -- stop the reply in progress
claude.status()                                   -- "starting" | "ready" | "busy" | "stopped" | "missing" | "term"

local off = claude.on("message", function(msg, session)
  -- msg.text: a block of the reply; session.root: which project
end)
-- also: "tool_use" (name, input), "done" (ok, text, cost), "exit", "status",
-- "permission_request" (tool, input)
off()                                             -- stop listening
```

Each event carries plain fields and, as `raw`, Claude Code's own stream-json event for anything more. `context` lists files (optionally `#L` ranges) that are mentioned with the message.

## What else you get

- **The editor is connected.** Claude Code sees the open buffers, the selection and diagnostics through bud's IDE bridge, so "this function" works.
- **Edits are reviewed.** When Claude wants to change a file, it opens as a diff to accept (`ga`, `:w`) or reject (`q`), and other tools ask in a small prompt. You don't handle permissions.
- **A view to start from.** `require("bud.chat.view").new({ key = ..., root = ... })` is the transcript-plus-input that the chat and the side pane use; `:show(win)` puts it in a window.

## Your own session

A TUI that shouldn't share the project's conversation can have its own: `claude.session(dir, { key = "my-tui", resume = "new" })`, then `claude.send(text, { key = "my-tui" })`. Chat (`key = "chat"`) and `:BudGen` (`key = "gen"`) work this way.

Put it in `nvim/lua/bud/` to ship it with bud, or make it a tool (see tools.md) to keep it on your machine.
