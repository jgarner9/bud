---
name: plan-to-linear
description: Turns an approved Superpowers plan into prioritized Linear issues and keeps the two in step while the work is done. Use after superpowers writing-plans produces a plan the user has approved, when the user asks to track a plan or spec in Linear, or when working through tasks that have Linear ids in a plan file.
---

# From plan to Linear, and back

Superpowers and Linear each do one job here:

- **Superpowers builds the thinking, in the repo.** `brainstorming` works out the concept with the user and saves the spec; `writing-plans` turns the approved spec into a plan of small tasks. Both stay as files in the repo, next to the code they describe, and they're the source of truth for *what* and *why*.
- **Linear holds the work.** Each task becomes an issue with a priority and its dependencies, so it can be scheduled, picked up and tracked. Linear is the source of truth for *status*.

Use the Linear MCP tools (the claude.ai Linear connector) for every Linear step. If they aren't available, say so and stop; don't track the plan anywhere else instead.

## 1. Before filing: find where it goes

1. Find the Linear team and project. Look in the repo's `CLAUDE.md`, the plan or spec, and memory first; ask the user only if it's still unclear. Never guess a team.
2. List the project's milestones. If the plan has phases that match milestones, use them; if it has phases with no milestone, ask before creating milestones.
3. Search the project for issues that already cover a task (by title and description). Link to an existing issue instead of filing a duplicate.

## 2. File one issue per task

Only once the user has approved the plan. Show the list you're about to file (title, priority, milestone, blocked-by) and file it after they say yes.

- **Title:** the task's own title, short and imperative.
- **Description:** what to do and how to tell it's done, taken from the plan (files, acceptance checks), plus the plan's path in the repo and the task's heading, so the issue leads back to the full context. Don't paste the whole plan.
- **Priority:** Urgent only for what blocks everything else or is broken now. High for the critical path (tasks others depend on, the core of the feature). Medium for the rest of the feature. Low for polish and nice-to-haves the plan marks optional. If the plan states priorities, use those.
- **Dependencies:** a task that needs another task's result is *blocked by* it. Set the relation in Linear, and order the issues so the unblocked ones come first.
- **Milestone and labels:** from step 1; match the project's existing labels (Feature, Bug, ...) rather than inventing new ones.
- A large plan can have a parent issue for the feature, with each task as a sub-issue. Ask if unsure.

## 3. Write the ids back

Edit the plan file: put each issue's identifier next to its task, e.g. `### Task 3: Parse the manifest (JGA-123)`, and a line at the top naming the Linear project. Commit the plan with the code, as the repo's workflow says; the plan and Linear now point at each other.

## 4. Working through it

- Take the next task in plan order whose blockers are done. Move its issue to In Progress when you start.
- When it's done (its checks pass), tick it in the plan and move the issue on, following the repo's own workflow (for example In Review after the commit, Done after a release). If the repo says nothing, use Done.
- Something new turns up mid-task: add it to the plan *and* file it, rather than doing it silently. A task that turns out to be unnecessary: say why in the issue and cancel it, and strike it in the plan.
- Report filing and progress as a log: `+ JGA-123 <title>` for each issue filed, `→ JGA-123 → In Review` for each state change.
- If the plan changes (the user changes their mind, a design doesn't work out), update the plan first, then bring Linear in line: retitle, reprioritize or cancel issues. Don't leave the two disagreeing.

## Don't

- File issues for a plan the user hasn't approved.
- Copy secrets, credentials or private data into issues.
- Create projects, teams or milestones without asking.
