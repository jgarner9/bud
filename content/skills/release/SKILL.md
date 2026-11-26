---
name: release
description: Cut a release of the current repository, after a written review checklist of everything since the last release and a docs check against the code. Use whenever cutting, shipping or publishing a release, bumping VERSION, tagging vX.Y.Z, or when asked to cut one.
---

# Cut a release

Every release gets a review of everything it ships, and docs that say what the
code does, before it ships. Copilot reviews each PR as it merges; this is the
second pass, over everything the release ships together, and it is a written
checklist you walk yourself (step 3). The person you work for picks the version and decides what a finding means; you do the
legwork and never cut the release past a finding they haven't seen.

## 1. Find out how this repo releases

- Read the release section of `AGENTS.md` (or `README.md`, `RELEASING.md`)
  and the release workflow in `.github/workflows/`. bud, for example, releases
  when a push to `main` changes `VERSION` (D80): the workflow checks it, runs
  the tests, pushes the `vX.Y.Z` tag and builds the release. Cloud sessions
  can't push tags, so a repo that needs a hand-pushed tag has to be released
  from the person's own machine: say so and stop.
- `git fetch origin main --tags`, then work from an up-to-date `main` with a
  clean tree. Uncommitted or unpushed work: stop and ask what goes in.
- The last release is the newest stable tag:
  `git tag --list 'v[0-9]*' --sort=-v:refname | grep -v -- - | head -1`
  (the `grep` drops prereleases like `nightly` or `-rc1`). No tag means the
  first release: everything is in it.

## 2. Say what's in it

List the commits in `<last tag>..origin/main` (`git log --oneline`) and
summarize them in a few lines, grouped by `feat`, `fix` and the rest. Nothing
since the last tag: there's nothing to release, so say so and stop.

## 3. Review it

Review the whole range, not just the last commit. Read the diff
(`git diff <last tag>..origin/main`, file by file for a large one), not the
commit messages: the messages say what the author meant, the diff says what
shipped. Walk every item below and write one line per item: what you checked
and what you found, or "nothing to check" with the reason. An item with no
line hasn't been reviewed.

1. **Correctness.** Read each changed function for wrong results, missed edge
   cases (empty input, missing file, nil or zero values), unchecked errors,
   off-by-one and ordering mistakes, and races. Follow each changed signature
   to its callers. Check that a change does what its commit message says.
2. **Tests for new behaviour.** Every new feature or fixed bug has a test that
   fails without it. Run the repo's checks (`just check`, or what CI runs) on
   the release commit and report what they printed; a check you couldn't run is
   "not verified", with why. A test that was skipped, loosened or deleted in
   the range needs a reason you can state.
3. **Docs match code.** Done in step 5; note here which pages the range
   changes behaviour for, so step 5 reads those first.
4. **Secrets and personal data.** Search the added lines (`git diff <last
   tag>..origin/main | grep '^+'`) for keys, tokens, passwords, `op://` paths,
   hostnames, tailnet names, home directories and personal names or email
   addresses. Run the repo's leak check if it has one. Anything real blocks the
   release: removing it from the tip doesn't remove it from history, so tell
   the person before doing anything else.
5. **Shell and file safety.** For each new or changed script, command
   execution and file write: arguments are quoted or passed as a list, never
   built by string concatenation from input; paths are checked before delete or
   overwrite; temp files are private and cleaned up; writes that matter are
   atomic; nothing runs with more privilege or reach than it needs; network
   calls verify what they download. Shell scripts stay shellcheck-clean.
6. **Signing and install paths.** If the range touches signing keys, checksums,
   the installer, the updater or the release workflow: the signature is
   verified before anything is installed or run, an unsigned or downgraded
   artifact is refused, archive names still match what the installer
   downloads, and the pinned keys still agree wherever they're listed. No
   change there: say so in one line.

Findings are written down, not fixed on the spot: each gets the file, the line,
what breaks and the fix, and a severity (critical, high, medium, low). Nothing
is bumped or pushed until step 4 is done. Fixes made in step 4 get the same
walk over their own commits as the range (items 1, 2, 4 and 5 are usually
enough for a small fix).

## 4. Settle the findings

Show the findings from the checklist in one list, worst first, each with file,
line, what breaks and the fix, together with the checklist's one-line results,
so the person sees what was covered as well as what was found. Then let them
choose, per finding: fix it now, ship with it (they say why), or stop the
release.

- A fix is an ordinary change: follow `code-style`, run the repo's checks
  (`just check` or what CI runs), commit, push to `main`, and walk the
  checklist over the fix's own diff before going on.
- A critical or high-severity security finding blocks the release until it's
  fixed or the person explicitly ships with it.
- No findings: say so in one line and go on.

## 5. Check the docs against the code

Docs drift when code moves under them, so check every page, not only the ones
the release touched:

- **What to read:** the README, `docs/`, plan or design docs that describe the
  current state (bud's `BASE.md`, sections 0 to 7), and `AGENTS.md`.
  Leave dated records alone: a decision log entry, a dated spec or
  plan, a changelog entry for an old release. They say what was true then.
- **How to check:** against the code and the built binary, never from memory.
  Commands and flags against each command's `-h` output, keys against the
  keymap code, config keys and defaults against the defaults in code, file
  trees against `ls`, counts ("seven languages", "three layouts") against what
  ships, and statuses or event names against the code that emits them. A doc
  that names a file, function or skill that no longer exists is wrong too.
- **What to fix:** anything that's wrong, missing for what this release adds,
  or describes something removed. Keep each page's voice and level of detail;
  don't rewrite what's accurate. Changes in behaviour the docs need but the
  code doesn't have are findings for the person, not doc edits.
- **Then:** show the person a short list of what was wrong, page by page, make the
  fixes as one `docs:` commit, run the repo's checks (some test the docs, like
  bud's `TestDocsCoverEveryKey`) and push to `main`. Nothing wrong: say so in
  one line and go on.

## 6. Pick the version

Propose the next version from the commits (semver: a breaking change bumps
major, a `feat` minor, only fixes patch), or take the one they passed, and
let them confirm it. It must be newer than the last tag.

## 7. Cut it

Do it the repo's way. For a `VERSION` file (bud): write the version, commit
`chore: release vX.Y.Z` with a body that records the review, and push `main`:

```text
chore: release v0.20.0

Reviewed v0.19.0..<sha> against the release checklist: 2 findings, both
fixed in <sha>; nothing on secrets, shell safety or signing; docs: 3 pages
fixed in <sha>.
```

A finding the person chose to ship with goes in that body too, with their reason.

## 8. Watch it land

Follow the release workflow run (GitHub's Actions tools: list the runs for
the push, then get the run) until it's done. Green: give the person the release
link. Red: show the failing step and its log, and fix it the same way as a
finding. Never delete or move a tag, or re-run with the checks skipped, to
get it green.
