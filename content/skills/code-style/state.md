# Changing state and printing

## Anything that changes state

- Only touch what the tool created. Track managed files in a manifest: update the ones that are unchanged, delete the ones a release dropped, and leave everything else alone. When a managed file was edited by hand, skip it and say so.
- Write files that get rewritten atomically (temp file, then rename).
- Before a destructive step, list exactly what will happen and confirm on the terminal. Without a terminal, refuse and name the `--yes` flag. Add `--dry-run` where it's cheap.
- git: fast-forward only. Never force-push, never rewrite shared history, never merge on Jason's behalf. On divergence, stop and report.
- Never install or upgrade system packages silently. Report what's missing and the exact command to fix it.
- Secrets come from the environment or the OS keychain, never from the repo, logs or command-line arguments.
- Network work never blocks startup: run checks in the background and apply the result next time.

## Terminal output

- Color only when writing to a terminal, `TERM` isn't `dumb` and `NO_COLOR` is unset, so pipes and tests stay plain. Pad text before coloring it.
- Plain Unicode symbols, never emoji.
- Prompts read from the terminal (`/dev/tty` when stdin is a pipe, as under `curl | sh`). With `--yes` or no terminal, take the defaults.
