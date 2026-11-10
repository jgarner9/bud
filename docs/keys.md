# Keys

Leader is <kbd>Space</kbd>. Press it and wait, and a panel lists the keys that follow, grouped: `a` Claude, `c` code, `g` git, `h` hunks, `q` quit, `u` interface. Language servers use Neovim's own keys.

## Everywhere

| Keys | Does |
| --- | --- |
| `;` | The command line, without Shift. `:` repeats the last `f`/`t` jump instead. |
| `jk` | Leave Insert mode (so does Esc). In the terminal, Claude Code's pane and chat, go to Normal mode; Esc there goes to the program, so it can interrupt Claude Code. |
| `<leader>w` | Window commands, as `<C-w>`: `h` `j` `k` `l` to move, `+` `-` `<` `>` to resize. |
| `<leader>d` | The dashboard. |
| `<leader>,` | Settings. |
| `<leader>P` · `<leader>M` | The plugin list · Mason. |
| `<leader>un` | Notification history. |
| `<leader>ub` | The buddy in the bottom right corner, on or off (`ui.buddy` starts him with the editor). |
| `<leader>qq` · `<leader>qw` · `<leader>qQ` | Quit (asks about unsaved files) · save all and quit · quit without saving. |

## Finding things

| Keys | Does |
| --- | --- |
| `<leader>p` | Open a project. |
| `<leader>f` | Find a file in this project, or type a full path. |
| `<leader>/` | Grep the project. |
| `<leader>b` · `<leader>r` | Buffers · recent files. |
| `gw` · `s` | Jump to any word on screen · jump by character. |
| `<leader>o` · `<leader>S` | Symbols outline · workspace symbols. |
| `<leader>;` · `[;` · `];` | Pick a breadcrumb · start of the current context · next context. |
| `<leader>m` | Pin or unpin this file (four per project). |
| `<leader>1`–`<leader>4` · `<leader>0` | Jump to a pinned file · unpin one from a menu. |

## Panes

| Keys | Does |
| --- | --- |
| `<leader>e` · `<leader>E` | Go to the explorer pane, or back from it · close it. |
| `-` | Edit the parent directory as a buffer (oil). |
| `<leader>t` | Show or hide the terminal along the bottom. |
| `<leader>ac` | Show or hide Claude Code's pane. |

## Code

| Keys | Does |
| --- | --- |
| `K` | Hover docs. |
| `grn` · `gra` · `grr` · `gri` | Rename · code action · references · implementation. |
| `[d` · `]d` | Previous · next diagnostic. |
| `<leader>x` | Diagnostics for the workspace. |
| `<leader>cf` | Format the buffer, or the selection. |

## Git

| Keys | Does |
| --- | --- |
| `]h` · `[h` | Next · previous hunk. |
| `<leader>hs` · `<leader>hr` · `<leader>hp` · `<leader>hb` | Stage · reset · preview the hunk · blame the line. |
| `<leader>gd` | Diff the working tree (`:DiffviewClose` gets back). |
| `<leader>gh` · `<leader>gH` | This file's history · the repo's history. |

## Markdown

| Keys | Does |
| --- | --- |
| `<leader>um` | Switch this markdown file between rendered and plain text. |

Markdown files open rendered: headings, lists, code blocks, tables and checkboxes are drawn in place. In Normal mode the line under the cursor shows as written, and Insert mode shows the whole file as text, so small edits don't need the toggle. A diff (Claude's edit review, `<leader>gd`) always shows the text, and so does a file over 250 KB, since rendering one that size freezes the editor for a moment; there `<leader>um` isn't mapped. `bud config set ui.markdown false` opens markdown as text instead; `<leader>um` still renders one.

## Claude Code

| Keys | Does |
| --- | --- |
| `<leader>ac` | Claude Code's own TUI, beside the code, on this project's session. |
| `<leader>as` | Share the selection (or this line) with Claude Code. |
| `<leader>ae` · `<leader>af` · `<leader>at` | Explain · refactor · write tests for the selection. |
| `<leader>am` | In a commit message buffer: draft the message from the staged diff. |
| `<leader>ar` | Restart this project's session. |
| `<leader>ch` | Chat: Claude Code in chat mode, in its own tab. |
| `<leader>ai` · `<leader>aB` · `<leader>aw` | File an issue about this project · about bud · have Claude fix one. |
| `ga` (or `:w`, `:wq`, `:x`, `ZZ`) · `q` | In a proposed edit: accept · reject (closing either side any other way rejects it too). |
| `y` · `a` · `n` · `r` | In a permission prompt: allow · allow for this session · deny · deny and say why. |

## Debugging

| Keys | Does |
| --- | --- |
| `F5` | Start or continue. |
| `F9` · `<leader>B` | Toggle a breakpoint. |
| `F10` · `F11` · `Shift-F11` | Step over · into · out. |
| `<leader>D` | The debugger panel. |

## Dashboard

`p` open a project, `f` open a file, `r` recent files, `n` new file, `c` chat, `x` scratchpad (when on), `P` plugins, `M` Mason, `s` settings, `d` doctor, `u` update, `t` switch between the silly and minimal views, `q` quit.
