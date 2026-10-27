# Layouts

A layout says which panes open around the editor when a project opens. bud ships three in `layouts/`:

- `default`: file tree on the left, chat on the right, the `<leader>t` terminal along the bottom.
- `focus`: the editor alone.
- `debug`: the file tree and the debugger panel along the bottom; F5 starts debugging.
- `review`: the changed files on the left, and the working tree's diff in its own tab (`:DiffviewClose` gets back).

```sh
bud layout ls                    # the layouts, marking the default and this project's
bud layout set-default focus     # what projects open in
bud layout set review ~/src/app  # one project's own
bud layout unset ~/src/app
bud --layout focus ~/src/app     # just this once
```

In the editor, `:BudLayout name` switches, and `:BudLayout reload` re-reads the files (handy with `bud dev link` while you edit one).

## Writing one

```lua
-- layouts/mine.lua
return {
  description = "Tree and chat, a taller terminal",
  panes = {
    { kind = "tree", side = "left", size = 30 },
    { kind = "chat", side = "right", size = 60 },
  },
  terminal = { height = 0.4 },
}
```

`kind` is `tree`, `git`, `chat` or `debug`; `side` is `left`, `right` or `bottom`; `size` is columns (rows for bottom), or a share of the screen under 1. A kind that isn't available is skipped, such as `git` outside a repo or `chat` without Claude Code. `diff = true` opens the working tree's diff in its own tab. A Lua module can add its own kind with `require("bud.layout").register(kind, { available = fn, open = fn })`.
