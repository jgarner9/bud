# Keys

Leader is <kbd>Space</kbd>. Press it and wait, and a panel lists the keys that follow, grouped: `a` AI, `c` code, `g` git, `h` hunks, `q` quit, `u` interface. Language servers use Neovim's own keys.

## Everywhere

| Keys | Does |
| --- | --- |
| `;` | The command line, without Shift. `:` repeats the last `f`/`t` jump instead. |
| `jk` | Leave Insert mode (so does Esc). In the terminal and OpenCode's chat, go to Normal mode; Esc there goes to the program, so it can interrupt OpenCode. |
| `<leader>w` | Window commands, as `<C-w>`: `h` `j` `k` `l` to move, `+` `-` `<` `>` to resize. |
| `<leader>d` | The dashboard. |
| `<leader>,` | Settings. |
| `<leader>P` · `<leader>M` | The plugin list · Mason. |
| `<leader>un` | Notification history. |
| `<leader>ub` | The buddy in the file window's bottom right corner, on or off (`ui.buddy` starts him with the editor). |
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
| `<leader>ac` | Show or hide the chat, opencode.nvim (D150). |
| `<leader>gs` | Show or hide the git pane (changed files, neo-tree). |
| `<leader>aP` | Home planning: OpenCode's own TUI attached to the home server's planning OpenCode (`ai.planning.url`), in a pane apart from local coding (D152). bud checks the server first and says why when it's unavailable; hiding the pane keeps it attached, and the server keeps running when it closes. `:BudPlan` does the same. |
| `<leader>av` | bud's own chat with the AI: the conversation drawn in Neovim, a prompt under it (`<CR>` sends, `<C-c>` stops the reply, `<Up>`/`<Down>` recall your earlier messages, `<C-x><C-o>` after a `/` completes the agent's commands). Pane or full screen by `ai.agent.chat_layout`; `ai.agent.chat_ui = "bud"` puts it in the layout's chat pane too. |
| `<leader>aF` | The AI pane full screen, in a tab of its own, or back to the pane. `ai.agent.chat_layout = "full"` makes the layout's chat pane open that way; `<leader>ac` is always opencode.nvim. |

The panes are locked: only these keys open and close them. `:q`, `:close` or `:only` puts a pane straight back with an error naming its key, and a file opened while a pane has the cursor (`:e`, a picker, a buffer switch) opens in the editor window instead, with the same error. `:qa` and the `<leader>q` keys still quit bud, and `:q` in the last file window takes the panes with it, so it quits as it would without them.

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

Markdown files open rendered: headings, lists, code blocks, tables and checkboxes are drawn in place. In Normal mode the line under the cursor shows as written, and Insert mode shows the whole file as text, so small edits don't need the toggle. A diff (an edit OpenCode proposes, or `<leader>gd`) always shows the text, and so does a file over 250 KB, since rendering one that size freezes the editor for a moment; there `<leader>um` isn't mapped. `bud config set ui.markdown false` opens markdown as text instead; `<leader>um` still renders one.

## OpenCode

OpenCode is the only AI bud runs, and its keys sit under `<leader>a`, plus `<leader>ch` for the standalone chat. Three things are involved. The chat is opencode.nvim (D150): bud keeps its own `<leader>o` keys off and maps the few it needs here. The project's OpenCode terminal pane is OpenCode's own TUI, which the inline actions type into and the layouts' `chat` pane shows. Commit messages, issues, working an issue and `:BudGen` run on headless sessions (`opencode acp`) that bud starts without a window: the project's own for commit messages, and one of their own for each issue filed or worked.

| Keys | Does |
| --- | --- |
| `<leader>ac` | Show or hide the chat. |
| `<leader>as` | Add the selection (or this line) to the chat's context. |
| `<leader>ad` | Review the session's changes: changed files on the left, before and after on the right. `r` picks a range of prompts, `c` leaves a comment the next prompt carries, `q` closes. |
| `<leader>ap` · `<leader>aS` | Pick the model · pick a session. |
| `<leader>ay` · `<leader>an` | Allow the waiting request once · deny it, from any window. In the chat, `1`/`2`/`3` (or `j`/`k` and `<CR>`) also answer it, with allow always as the third choice. |
| `<leader>ch` | Chat in OpenCode's own TUI, in its own tab, outside any project (`bud chat` from the shell). |
| `<leader>ae` · `<leader>af` · `<leader>at` | Explain · refactor · write tests for the selection: typed into the project's OpenCode terminal pane, which opens beside the code if it isn't showing. |
| `<leader>am` | In a commit message buffer: draft the message from the staged diff, on the project's headless session. |
| `<leader>ar` | Restart this project's headless session. |
| `<leader>ai` · `<leader>aB` · `<leader>aw` | File an issue about this project · about bud · have OpenCode fix one. |
| `ga` (or `:w`, `:wq`, `:x`, `ZZ`) · `q` | In an edit a headless session proposes: accept · reject (closing either side any other way rejects it too). |
| `y` · `a` · `n` · `r` | In a permission prompt from a headless session: allow · allow for this session · deny · deny and say why. |

`:Opencode` has every other action in the chat, such as `:Opencode revert all prompt` to undo the last prompt's edits. In the chat, edits are written when OpenCode allows them and reviewed afterwards with `<leader>ad`, so commit first if you want git to hold the way back.

## Debugging

| Keys | Does |
| --- | --- |
| `F5` | Start or continue. |
| `F9` · `<leader>B` | Toggle a breakpoint. |
| `F10` · `F11` · `Shift-F11` | Step over · into · out. |
| `<leader>D` | The debugger panel. |

## Dashboard

`p` open a project, `f` open a file, `r` recent files, `n` new file, `c` chat, `x` scratchpad (when on), `P` plugins, `M` Mason, `s` settings, `d` doctor, `u` update, `t` switch between the silly and minimal views, `q` quit.
