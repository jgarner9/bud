# UI test checklist

A hands-on pass over bud's editor UI, for a release that changes it (written for v0.14.0). Setup: `bud update`, `bud kit sync`, then open a real project with `bud ~/dev/<something>`. Anything that breaks: note it here or file it from bud with `<leader>aB`.

## 1. Startup and dashboard

- [ ] Plain `bud` opens the dashboard. Check the robot, spacing and the update banner, if there is one.
- [ ] Every dashboard key works: `p` `f` `r` `n` `c` `x` `s` `d` `u` `t` `q`.
- [ ] The Claude banner shows if you're logged out, and `c` runs the fix.
- [ ] `bud doctor` says startup is under 50 ms, both empty and when opening a file.

## 2. Command line and messages (Neovim's built-in UI, JGA-137)

- [ ] `:` opens the command line as a float, not on the bottom line.
- [ ] Long output (`:messages`, `:ls`) is readable and closes cleanly.
- [ ] Turn off `ui.messages` in settings (`<leader>,`), restart, and the classic command line is back.

## 3. Notifications (JGA-138)

- [ ] `:lua vim.notify("hi")` shows a float in the corner. Fire several and they stack, then fade.
- [ ] Language server progress shows while a Go or Python project loads.
- [ ] `<leader>un` opens the notification history.

## 4. Dialogs and menus (`bud.ui`, JGA-136)

- [ ] Anything that calls `vim.ui.select` opens bud's menu (e.g. a code action, or `:BudLayout` with no argument). Arrows, `j`/`k`, Enter, `q` and `<Esc>` all work.
- [ ] Rename a symbol: the `vim.ui.input` float takes the text and Enter submits it.
- [ ] Permission prompt: in the chat pane, ask Claude to run a shell command. The dialog appears, and `y`/`a`/`n`/`r` each do the right thing. `a` is remembered for that command next time.
- [ ] Edit permission: ask for a file edit. The diff review opens, `ga` or `:w` accepts it and `q` rejects it.
- [ ] Issue float: `<leader>ai`, type a report, then check the review screen looks right.
- [ ] Borders and titles are consistent everywhere, with no emoji.

## 5. Key hints (mini.clue, JGA-140)

- [ ] Press `<leader>` and wait: the hint window lists the named groups (`a` AI, `g` git, `h` hunks, ...).
- [ ] `<leader>a`, `<leader>g` and `<leader>h` each show a sensible submenu with no "unnamed" entries.
- [ ] `<C-w>`, `g` and `z` show their hints too.

## 6. Breadcrumbs (dropbar, JGA-139)

- [ ] A code file shows path > symbol in the bar across the top of the window.
- [ ] `<leader>;` picks a breadcrumb, menus open, and the keyboard and mouse both navigate.
- [ ] No bar on the tree, terminal, chat pane, dashboard or floats.
- [ ] Settings: `ui.breadcrumbs` off removes it.

## 7. Status line (JGA-133)

- [ ] There's one global status line: mode, git branch and diff, file path relative to the project, `[+]` when modified and a read-only marker.
- [ ] Right side: diagnostics count, the buffer's language servers, the Claude section, and line/column.
- [ ] Special buffers (tree, terminal, chat, help) show what they are, not an odd path.
- [ ] A split doesn't create a second status line.

## 8. Empty-line `~` markers (JGA-134)

- [ ] No `~` past the end of a file, in the tree or in the explore views.

## 9. Layout and panes

- [ ] `bud <dir>` opens the layout. `:BudLayout <name>` switches and `:BudLayout reload` works.
- [ ] `<leader>e` tree, `<leader>t` terminal, `<leader>o` outline, `<leader>b` buffers, `<leader>f`/`<leader>p` pickers, `<leader>/` grep.
- [ ] `<leader>m` pin a file, `<leader>0` unpin a file, `<leader>gd` diff, `<leader>gh`/`<leader>gH` history.
- [ ] Quit and reopen the project: the session restores.

## 10. Claude in the editor

- [ ] The status line's Claude section moves through starting, then ready. `:BudClaude status|restart|stop` all work.
- [ ] `<leader>ac` opens the TUI pane, and closing it hands back to the headless session.
- [ ] `<leader>ch` opens chat: send a message, then check that `@file`, `@selection` and `@diff` get filled in and the history panel appears.
- [ ] `<leader>as` shares a selection. `<leader>ae`/`af`/`at` on a selection open the chat pane with a reply.
- [ ] `<leader>am` in a commit buffer writes a commit message.

## 11. Kit plugins (JGA-168 to JGA-179)

- [ ] `bud kit sync --dry-run` lists the `bud-plugins` marketplace, `bud`, `bud-ide` and `superpowers`, and on a machine synced by an older bud, the MCP servers, files and `CLAUDE.md` block it takes out. On the Bedrock Mac, `bud@bud-plugins` and the marketplace show as skipped (clod's).
- [ ] After `bud kit sync`, `claude plugin list` has `bud-ide@bud-plugins`, and `~/.claude/settings.json` has no `outputStyle` bud set (one you picked stays).
- [ ] In `bud agent`, `/context` (or the first reply) shows the "When you're running inside bud" notes; in a plain `claude` outside bud they're absent.
- [ ] Ask something version-specific: the reply ends with a `◇` source line (`bud:tools`), and a `cache/` note appears in Basic Memory.
- [ ] Ask about the project in plain words, in `bud agent` and the chat pane: it answers conversationally, not as a task log.

## 12. Settings and config (JGA-135, JGA-132)

- [ ] `<leader>,`: every row toggles and persists, and `bud config ls --changed` shows the change.
- [ ] `bud claude status` shows `~/.claude` (bud shares it by default, D77), and the kit is synced there; with `ai.claude.config = bud` it shows bud's own folder instead.

## 13. Resizing and edge cases

- [ ] Shrink the terminal very narrow and very short. The dashboard, floats, status line and breadcrumbs shouldn't break or overlap.
- [ ] Light colour theme or `NO_COLOR`, if you use either.
