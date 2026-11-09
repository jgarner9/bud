# Filing issues from the editor

Spot a problem or think of a feature while you work, press a key, say what's wrong or what you want, and Claude turns it into Linear issues you approve before anything is filed. A feature request goes through the same keys and comes back labelled as a feature.

| Key | What |
| --- | --- |
| `<leader>ai` | An issue about this project (`:BudIssue`) |
| `<leader>aB` | An issue about bud itself (`:BudIssue bud`) |
| `<leader>aw` | Have Claude fix an issue (`:BudIssue work JGA-12 [local\|cloud]`); the last one filed by default; [in the cloud](#in-the-cloud) with `ai.work.where` |
| `:BudIssues` | What you filed and what Claude is fixing; `<CR>` opens one, `w` fixes it, `d` shows the fix, `a` answers Claude |

## How it goes

1. A float opens for your report: the first line is the summary, the rest whatever detail you have. Leave insert mode and press `<CR>` to send it (`<C-s>` works too, unless a terminal multiplexer like zellij takes it), `q` closes it and keeps the draft for next time, and `<Tab>` shows what bud attaches by itself: the file and lines you were on (or your selection), diagnostics there, recent `:messages`, and the bud, Neovim and layout versions.
2. A Claude Code session of its own (never your project's conversation) runs the `bud-ide:file-issue` skill (from the `bud-ide` plugin, which `bud kit sync` installs): it reads the code around the report, checks Linear for duplicates and proposes one or more issues, or a comment on an existing one.
3. The proposal shows in a review float. `<CR>` opens an issue's description, `<Esc>` folds it again, `e` opens the proposal as JSON to edit (`:w` brings it back), `y` files it, and `n` or `q` drops it.
4. On `y`, Claude files exactly what you approved and bud tells you the ids.

Until you press `y`, Claude can read Linear but not write to it: bud refuses its writes, so nothing is filed by accident.

## From idea to merged: the whole flow

The same steps work for a bug and for a new feature; a feature just comes back labelled as one. Do it once to see it.

1. **Once per machine:** `bud install`, then `bud claude setup` so Claude Code is logged in, and `bud kit sync` so the `bud-ide` plugin is there. Linear needs to be connected in Claude Code (the claude.ai Linear connector).
2. **Once per project:** in the project's folder, run `bud issue set --team ENG --project Web` to say where its issues go (or press `<leader>ai` and let bud ask). `bud issue target` shows the answer.
3. **Open the project** with `bud` (press its number on the dashboard, or `bud launch <path>`).
4. **Say what you want.** Press `<leader>ai`, press `i`, and write the summary on the first line and the detail below. For a feature, say what you want to be able to do. Press `<Esc>`, then `<CR>` to send.
5. **Review the proposal.** After a short wait a float lists the issues Claude would file, with their type, priority and any open question. `<CR>` opens one, `e` edits the JSON, `n` drops everything. Nothing is in Linear yet.
6. **Press `y`** to file. bud tells you the ids (say JGA-12).
7. **Hand it to Claude.** Press `<leader>aw` and press `<CR>` to accept the last issue filed, or type another id. Claude gets a git worktree next to your repo (`<repo>.worktrees/jga-12`) on a branch named after the issue, moves the issue to In Progress, makes the change, runs your checks, commits with the issue id and moves it to In Review. Your own checkout is never touched.
8. **Answer its prompts.** Claude asks before running shell commands (the checks, git): `y` allows once, `a` allows for the session, `n` denies, `r` denies and lets you say why. If it has a question, the notification says so; answer with `a` in `:BudIssues`.
9. **Review the work.** Open `:BudIssues`, put the cursor on the issue and press `d` for the diff in diffview.
10. **Land it yourself.** (Cloud mode: the branch is already pushed to `origin`; open the pull request from it.) From the worktree (or the branch in your checkout): push, open the pull request, merge, then `git worktree remove <repo>.worktrees/jga-12`. Move the issue to Done in Linear. bud never pushes or merges for you.

Something wrong with bud itself while you do this: `<leader>aB` files it in bud's own tracker. Use `<leader>ai` for the project you are in.

## In the cloud

By default `<leader>aw` works in a local worktree. Set `ai.work.where` to `cloud` (`bud config set ai.work.where cloud`), or add `cloud` or `local` to one command (`:BudIssue work JGA-12 cloud`), and the work goes to a Claude Code cloud session instead. Your laptop stays free, and several issues can run at once.

What happens: bud runs `claude --cloud` in the repo with the same task. The cloud session clones the repo from GitHub, works the issue on a branch named after it (`jga-12-dashboard-drops-a-row`, or `claude/jga-12-...` where the environment only allows that prefix), runs the checks and pushes the branch. bud shows the session's link in `:BudIssues`, and when a branch with the issue id in its name appears on `origin` it marks the issue ready. `d` then fetches the branch and shows the diff in diffview. Merging stays yours. Cloud runs are saved (`cloud-runs.json` in bud's state dir), so closing bud doesn't lose them: the next start lists them again with their links and keeps watching the ones still working, and drops those older than a week.

What it needs:

- The repo's `origin` is on GitHub, and the base branch (usually `main`) is pushed. Anything else is refused with a message, and `local` still works.
- Claude Code trusts the repo's folder: run `claude` in it once and accept the prompt. bud says so if it hasn't.
- Your local Claude Code can reach Linear (the claude.ai connector). Sessions started by `claude --cloud` have no Linear of their own, so bud reads the issue (title, description, comments) with your local Claude first and puts it in the task, then moves the issue to In Progress when the session starts and to In Review when its branch appears. If it can't read the issue, nothing is started.

What differs from local: a cloud session doesn't use bud's permission float, so it runs in its own sandboxed environment (`bud-dev` for bud's own work, whichever your account uses elsewhere). A question from it isn't answered with `a`; open the link in `:BudIssues` and answer there.

## Having Claude fix one

`w` on an issue in `:BudIssues` (or `<leader>aw`) hands it to Claude in a git worktree of its own, next to your repo: `<repo>.worktrees/<id>`, on a branch named after the issue (`jga-12-dashboard-drops-a-row`) from your default branch. Your checkout and open buffers never change.

Claude reads the issue, moves it to In Progress, makes the fix, runs the project's checks, commits on that branch with the issue id and moves the issue to In Review. Its edits inside the worktree go through without asking; shell commands (the checks, git) still ask you, and so would an edit anywhere else. The statusline shows how many are in progress, and a question from Claude shows up as a notification: answer it with `a` in `:BudIssues`.

When it's done, `d` opens the diff against the base in diffview. Merging, pushing and removing the worktree (`git worktree remove <path>`) are yours. For bud's own issues, Claude needs a bud checkout: the one `<leader>aB` remembered, or `bud dev link ~/dev/bud`.

## Where issues go

```sh
bud issue target                        # where this project's issues go
bud issue set --team ENG --project Web  # this project's own target
bud issue set --global --team ENG       # every project without its own
bud issue unset                         # back to the global one
```

The first `<leader>ai` in a project with no target asks for one and offers to save it. Issues about bud go to `ai.issues.bud` (bud's own tracker), and Claude reads bud's code in your `bud dev link` checkout. Without one, the first `<leader>aB` asks for the path to a bud checkout and remembers it (`bud issue checkout <path>`; `bud issue checkout` shows it, `--unset` forgets it). It is checked every time: if the dir has been moved or removed, bud says so and asks again, and an empty answer falls back to the synced config, which has no Go code to read.

Needs the `bud-ide` plugin (`bud kit sync`) and Linear in Claude Code: the claude.ai Linear connector, or a `linear` MCP server in Claude Code's config where there's no claude.ai (Bedrock).
