# Troubleshooting

Start with `bud doctor`. It checks Neovim, the tools bud needs, every enabled language, the synced config, the startup time, OpenCode and the project registry, plus what `bud sync` left on a device with a profile set, and each problem comes with the command that fixes it. `bud doctor --net` also asks whether newer content is out, whether the model endpoints answer, and whether the embedding server returns vectors of the size the registry names.

## Common problems

**bud is slow to start.** `bud doctor` times the first screen, empty and on a file of each language; the budget is 50 ms. The dashboard's health line shows the same thing for the start you just made, counted from when `bud` started. A language's tooling that's slow the first time runs in the background, so a slow start is usually a plugin or a tool you added. `bud tools disable <name>` rules out tools, one at a time.

**A language has no highlighting, LSP or formatting.** `bud lang ls` says whether it's on and whether its spec is broken. `bud update` builds parsers and installs Mason packages; `:Mason` shows what's installed. Formatting on save only happens in projects with the spec's `format_markers`, when it has them.

**A file opens without tree-sitter, its language server or completion.** It's a big file: over 1.5 MB, or with lines averaging over 1000 characters (minified code). bud opens those as filetype `bigfile` and colours them with Vim's own syntax for their language, because tree-sitter, a language server and completion would each freeze the editor on them; a notification says so when it happens. A markdown file over 250 KB shows as text rather than rendered, for the same reason.

**Pasting is slow over SSH.** It shouldn't be: over SSH (`SSH_CONNECTION` set) bud doesn't tie `p` and `y` to the system clipboard, since there that clipboard goes through your terminal (OSC 52), and many terminals never answer a paste request. `"+y` copies to your own machine's clipboard through the terminal; `"+p` pastes only where the terminal allows it.

**OpenCode isn't answering.** `bud ai status` says whether OpenCode is installed, which version and command it runs, and prints each problem with a `fix:` line, such as the command not being installed (`bud ai setup` installs it with npm, asking first). `bud doctor` warns about the same under "opencode". In the editor, `:BudAgent` shows the project's session and why it stopped; `:BudAgent restart` starts it again once fixed. The chat (`<leader>ac`) is opencode.nvim and starts or attaches to its own local `opencode` server, so a chat that works with the project's session stopped, or the reverse, points at that one's own problem. Signing in to a model is OpenCode's step, not bud's: run `opencode` and connect a provider there.

**OpenCode can't edit anything.** In the project's session, edits come as diffs to accept with `ga` or `:w`; a prompt that was closed counts as no. Allowing something for the rest of the session (`a`) only lasts until you quit. In the chat, permission requests are answered in the chat (`1`/`2`/`3`) or with `<leader>ay` and `<leader>an`.

**An update went wrong.** bud checks itself after an update and goes back to the previous version if a check that passed before fails, saying why. `~/.cache/bud/update.log` has the background updates. `bud update` (or `:BudUpdate` in the editor) runs one by hand, then runs the full `bud doctor`, listing anything that isn't right with its fix. A failing check makes `bud update` exit 1, as `bud doctor` does (warnings don't); when a release went in, the error starts "updated to X.Y.Z, but", and the new release stays. `bud update --no-doctor` skips the check.

**The command line or messages look wrong.** Typing a command (`;`), a search or an input opens a float in the middle of the screen, and it goes back to the bottom line when you're done; Neovim's own ui2 draws both, and it's still experimental in Neovim 0.12. If the float sits in the wrong place or completion opens away from it, `bud config set ui.cmdline_float false` keeps the command line at the bottom; if messages misbehave too, `bud config set ui.messages false` turns ui2 off. Both take effect when bud next starts. `g<` shows a message that was cut short.

**The app doesn't open.** `bud bundle` builds it again with your current `PATH` and bud; `bud launch` from a shell shows any error.

**Bud.app is out of date with kitty.** On macOS with Kitty, Bud.app is a copy of kitty.app, so it doesn't follow kitty's updates. `bud update` copies it again by itself; between updates, `bud doctor` warns (`Bud app`) when kitty has moved on, and `bud bundle` copies it again.

**`bud doctor` says a synced file changed by hand.** `bud sync` never overwrites a file you edited, so the edit stays and doctor keeps naming it. To keep the change, put it in your overlay (an env fragment, or your own skill or agent), then delete the file and run `bud sync` to get bud's version back. When "synced config" warns that a sync would write something, the content, the overlay or bud itself changed since the last sync, and `bud sync` brings the files up to date.

## Starting over

`bud uninstall` lists everything bud put on the machine and removes it after asking (`--keep-config` keeps `config.json` and your tools). Then reinstall with the one-liner from the README.
