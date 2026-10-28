# Filing issues from the editor

Spot a problem or think of a feature while you work, press a key, say what's wrong or what you want, and Claude turns it into Linear issues you approve before anything is filed. A feature request goes through the same keys and comes back labelled as a feature.

| Key | What |
| --- | --- |
| `<leader>ai` | An issue about this project (`:BudIssue`) |
| `<leader>aB` | An issue about bud itself (`:BudIssue bud`) |
| `<leader>aw` | Have Claude fix an issue (`:BudIssue work JGA-12`); the last one filed by default |
| `:BudIssues` | What you filed and what Claude is fixing; `<CR>` opens one, `w` fixes it, `d` shows the fix, `a` answers Claude |

## How it goes

1. A float opens for your report: the first line is the summary, the rest whatever detail you have. Leave insert mode and press `<CR>` to send it (`<C-s>` works too, unless a terminal multiplexer like zellij takes it), `q` closes it and keeps the draft for next time, and `<Tab>` shows what bud attaches by itself: the file and lines you were on (or your selection), diagnostics there, recent `:messages`, and the bud, Neovim and layout versions.
2. A Claude Code session of its own (never your project's conversation) runs the kit's `file-issue` skill: it reads the code around the report, checks Linear for duplicates and proposes one or more issues, or a comment on an existing one.
3. The proposal shows in a review float. `<CR>` opens an issue's description, `<Esc>` folds it again, `e` opens the proposal as JSON to edit (`:w` brings it back), `y` files it, and `n` or `q` drops it.
4. On `y`, Claude files exactly what you approved and bud tells you the ids.

Until you press `y`, Claude can read Linear but not write to it: bud refuses its writes, so nothing is filed by accident.

## Having Claude fix one

`w` on an issue in `:BudIssues` (or `<leader>aw`) hands it to Claude in a git worktree of its own, next to your repo: `<repo>.worktrees/<id>`, on a branch named after the issue (`jga-12-dashboard-drops-a-row`) from your default branch. Your checkout and open buffers never change.

Claude reads the issue, moves it to In Progress, makes the fix, runs the project's checks, commits on that branch with the issue id and moves the issue to In Review. Its edits inside the worktree go through without asking; shell commands (the checks, git) still ask you, and so would an edit anywhere else. The statusline shows how many are in progress, and a question from Claude shows up as a notification: answer it with `a` in `:BudIssues`.

When it's done, `d` opens the diff against the base in diffview. Merging, pushing and removing the worktree (`git worktree remove <path>`) are yours. For bud's own issues, Claude needs a checkout: `bud dev link ~/dev/bud`.

## Where issues go

```sh
bud issue target                        # where this project's issues go
bud issue set --team ENG --project Web  # this project's own target
bud issue set --global --team ENG       # every project without its own
bud issue unset                         # back to the global one
```

The first `<leader>ai` in a project with no target asks for one and offers to save it. Issues about bud go to `ai.issues.bud` (bud's own tracker), and Claude reads bud's code in your `bud dev link` checkout, or the synced config without one.

Needs the kit's `file-issue` skill (`bud kit sync`) and Linear in Claude Code (the claude.ai Linear connector).
