# Planning templates

## Design doc

```markdown
# <Name>: plan

> Status: <one line: what's built and what's still a plan>
> Date: <YYYY-MM-DD>, updated <YYYY-MM-DD>

## 0. The one-paragraph version

<What it is, what it's made of and how the pieces fit, in one paragraph someone
could read on its own.>

### Guiding principles

1. **<Principle>.** <One line on what it means in practice.>
2. **One source of truth per concern.** <...>

## Decisions made

| #  | Decision | Date | Notes |
| -- | -------- | ---- | ----- |
| D1 | **<The decision, in bold.>** | YYYY-MM-DD | <Why, what it rules out, where it shows up in the code.> |
| D2 | **Superseded by D5.** ~~<Old decision.>~~ | YYYY-MM-DD | <What replaced it.> |

## 1. <Section: what it is, how it works, where things live>

<Prose and tables. Say why for each key choice, citing the decision: "(D3)".>

## N. Suggested build order

1. **<Milestone>:** <what ships, small enough to test on its own>.
2. ...

## N+1. Open questions for you

<Each question with the options and your recommendation. Answered ones move
into the decision table; say so here ("dashboard extras: D37").>
```

## Issue

Title: what it should do, in the area's own words. `Dashboard f: grep the
working directory instead of recent files`. For a bug, a plain statement of
it: `bud dev link trips over dot entries like .git`.

```markdown
> <Jason's own words, quoted, when the request came from him>

## Now

<What happens today, with exact files and functions: `bud.open.file()`
(`nvim/lua/bud/open.lua`) runs ... For a bug, the error output in a code
block, then the cause.>

## Behaviour

<What it should do instead.>

## Where

<The files and functions to change, and what it builds on. Optional.>

## Open question

<Only when there's a real choice: the options as bullets, then "Pick one.">

## Done when

* <A checkable outcome.>
* <The test that proves it: which file, which case.>

## Decided

* <Answers to the open question, appended later. Never edited into the question.>
```

A bug can be shorter: the error, the cause with file references, why the fix
is right, then **Done when**.
