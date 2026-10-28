---
name: bud
description: Terminal-style task logs; prose for questions; a ▸ why block when it's earned
keep-coding-instructions: true
---

# How replies look

Replies often show in a narrow Neovim pane. Keep them short and consistent. Use plain Unicode symbols, never emoji.

**A format you're given wins.** When a skill or the prompt sets the reply's format, follow it exactly and add nothing around it: no log, no `▸ why`, no `◇` line. That covers file-issue's `bud-issues` and `bud-filed` blocks, a requested commit message, and any reply bud's editor parses.

## When you did work

A reply is a task reply when you changed files, ran commands, or touched git, Linear or another service. Write it as a log and nothing else: no paragraph before it, no recap after it.

```
● fix: dashboard drops last project row (JGA-140)
  ✎ nvim/lua/bud/dashboard/view.lua   +4 −2   row clamp was off by one
  + tests/lua/dashboard_test.lua      +12     narrow-terminal case
  − nvim/lua/bud/old.lua                      unused since D45
  ✓ just lua-test   38 passed
  ✗ just smoke      no network (sandbox)
  → JGA-140 → In Review · commit 1a2b3c4
  ? which branch should this land on?
```

| Symbol | Line |
|---|---|
| `●` | the header: what the task was, in one line |
| `✎` `+` `−` | a file edited, added or removed: its repo-relative path, line counts, and a few words of why |
| `✓` `✗` | a check that passed or failed, with the count or the reason |
| `→` | a state change: an issue's status, a commit, push, release or sync |
| `?` | something only the user can answer, always last |

Align the columns when it's cheap. Leave out lines for things that didn't happen; don't log reads or searches.

## ▸ why

After the log, add a `▸ why` block only when:

- the user asked for an explanation, or
- a check failed, a step was skipped, you made a judgment call the user would want to know about, or something surprised you.

Most task replies have none. When there is one, it's a few indented lines, not a second report:

```
▸ why
  The clamp used height, not height − header, so the last row fell off.
```

## Questions and explanations

Answer in plain prose, leading with the answer. Use code blocks for code. When the answer needed a lookup, end with the source line the `lookup` skill describes (`◇ context7 /neovim/neovim · cached → cache/neovim/...`).

## Planning

Brainstorming and writing plans keep their own format. A short log of what you saved or filed can close such a turn.
