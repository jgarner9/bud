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

## Dialogs, menus and panels

`bud.ui` (on nui.nvim) is what bud's own screens use, so yours look the same: rounded borders, a title with bud's accent, the keys in the footer, `q` or `<Esc>` to close.

```lua
local ui = require("bud.ui")

ui.dialog({
  title = "deploy",
  lines = { "", "  Push main to production?", "" },
  actions = {
    { key = "y", label = "push", fn = function() ... end },
    { key = { "n", "q", "<Esc>" }, label = "cancel" },
  },
  cancel = function() end,                -- closed any other way
})

ui.menu({ "explain", "refactor", "tests" }, { title = "ask Claude" }, function(choice, i) end)
ui.input({ prompt = "Message" }, function(text) end)   -- nil when cancelled

local p = ui.panel({                      -- several boxes in one float
  dir = "col",
  boxes = {
    { name = "transcript", size = "75%", title = "chat", modifiable = false },
    { name = "prompt", size = "25%", title = "you", enter = true },
  },
})
p.boxes.prompt.map("<CR>", function() ... end)        -- <Tab> moves between boxes
```

`ui.float` is the plain version a dialog is built on. `vim.ui.select` and `vim.ui.input` go through `ui.menu` and `ui.input` too, so any picker you open with them matches.

## Your own session

A TUI that shouldn't share the project's conversation can have its own: `claude.session(dir, { key = "my-tui", resume = "new" })`, then `claude.send(text, { key = "my-tui" })`. Chat (`key = "chat"`) and `:BudGen` (`key = "gen"`) work this way.

Put it in `nvim/lua/bud/` to ship it with bud, or make it a tool (see tools.md) to keep it on your machine.

## Answering permission requests yourself

A module that owns some tool calls can answer them without a prompt: add a function to `require("bud.ai.permissions").policies` that takes the request (`tool`, `input`, `root`) and returns `"allow"`, `"deny"` and a message for Claude, or `nil` to let the usual prompt ask. `bud.ai.issue` uses one so Linear writes only happen after you approve the issues.

