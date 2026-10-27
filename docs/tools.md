# Writing a tool

A tool is a small Neovim extension in a directory:

```text
my-tool/
├── manifest.json   # name, description, the commands and keymaps it adds
└── init.lua        # returns { setup = function(manifest) ... end }
```

```json
{
  "name": "my-tool",
  "description": "What it does, in one line",
  "commands": ["BudMyTool"],
  "keymaps": []
}
```

```lua
-- my-tool: what it does
local M = {}

function M.setup()
  vim.api.nvim_create_user_command("BudMyTool", function()
    vim.notify("hi from my-tool")
  end, { desc = "Say hi" })
end

return M
```

Yours go in `~/.local/share/bud/user-tools/<name>/` and load the next time bud starts, after the first screen, so they never slow startup. One of yours with the same name as one of bud's wins. bud ships one, `tools/test-file/` (`:BudTestFile`), which is a good one to copy.

## Letting Claude write it

`:BudGen "a command that opens the test file for the current file"` (or `bud tools gen "..."` from the shell) has Claude write the tool. Each file it writes comes up as a diff you accept or reject, then the tool is loaded in a headless Neovim first: it's turned on only if that works, and turned off with the error if not. Ask for changes in the same split.

## Managing them

```sh
bud tools ls                     # yours and bud's, on or off, and why a broken one is off
bud tools disable my-tool
bud tools enable my-tool
bud tools rm my-tool             # asks first
bud tools promote my-tool        # moves it into a bud checkout's tools/, commits and pushes, after asking
```

A tool that fails to load is turned off with a notification, so a broken one never breaks the editor.
