---
name: file-issue
description: Turns a problem report or feature request into one or more well-formed Linear issues someone can pick up cold. Proposes them first and files only after an explicit yes. Use when asked to file, report or log an issue, bug or feature request in Linear.
---

# Filing an issue from a report

A report arrives as a short note from someone in the middle of other work: a problem, or a feature they want, sometimes with context such as the file and lines they were on, errors, or versions. Turn it into issues someone can pick up cold. Work in two rounds, and never file in the first.

Use the Linear MCP tools. If none are available, say so in one line and stop.

## Round 1: propose

1. **Target.** Use the Linear team and project the report names. If it names none, look in the repo's `AGENTS.md` for the project's Linear project, and ask if it's still unclear. Never guess.
2. **Understand it.** Decide whether it's a problem (something behaves wrongly) or a feature request (something new, or a change to behavior that works as built). Read the context, then look in the code for where the problem lives, or where the feature would go and what it would build on. Name files and functions in the description; don't fix or build anything.
3. **Duplicates.** Search the target project for open issues that already cover it, with a few phrasings. If one does, propose a comment on it instead of a new issue.
4. **Split or not.** One problem or feature is one issue. Split only when the report mixes independent ones, or one fix has to land before another (then mark it blocked by the first). Two or three issues is a lot for one report.
5. **Each issue:**
   - **Title:** short. For a problem, a plain statement of the bug ("Dashboard drops the last project row on narrow terminals"); for a feature, what to add or change ("Remember the tree pane's width per project").
   - **Description:** Markdown. Keep the reporter's own words in a quote. For a problem: what happens, what should happen, how to reproduce it, where in the code it likely lives, and how to tell it's fixed. For a feature: what the reporter wants to do and why, the behavior asked for, where it fits in the code, open questions, and how to tell it's done.
   - **Priority:** 1 Urgent only when something is broken for everyone now; 2 High for broken behavior on the critical path; 3 Medium by default, features included; 4 Low for polish.
   - **Labels:** only labels the team already has, such as Bug, Feature or Improvement.
   - **Milestone:** the project's current milestone if one clearly fits, else none.
6. **Reply** with one line saying what you found, then the proposed issues as a readable list: title, priority, labels, and the description under each. End by asking whether to file them as they are.

## Round 2: file

Only after an explicit yes. If the reporter changed anything, file what they said, not your first draft. Create the issues in dependency order, set the relations, post the comments for duplicates, then reply with one line per issue: its id, title and link, or what failed. If one fails, file the rest.

If the reporter drops them, say so in one line and file nothing.

## Don't

- Create, edit or comment on anything in Linear before the yes.
- Edit the repo: this is reporting, not fixing or building.
- Copy secrets, tokens or private data from the context into an issue.
