# Issues and tracking

## 4. Issues

Use the linear MCP server. If it isn't connected, say so and stop. Don't track the plan anywhere else instead. The issue template is in [templates.md](templates.md).

- **Where:** find the team and project in the repo's `AGENTS.md`, the plan, or his notes. If it's still unclear, ask. Never guess a team.
- **Before filing:** search the project for issues that already cover it (a few phrasings). Comment on an existing one instead of filing a duplicate.
- **Splitting:** one problem or feature is one issue. Split only when the parts are independent, or one has to land first (then it's `blocked by`).
- **Priority:** Urgent only when something is broken for everyone now. High for broken behavior and the critical path. Medium by default, features included. Low for polish and nice-to-haves.
- **Labels and milestones:** only ones the project already has (Bug, Feature, Improvement). Ask before creating projects, milestones or labels.
- **Approval:** show the list you'd file (title, priority, milestone, blocked-by) and file only after he says yes.
- **After filing:** write each id back into the plan, as in `### Task 3: Parse the manifest (JGA-123)`, so the plan and Linear point at each other.

## 5. While the work happens

- Take the next task whose blockers are done. Move its issue to In Progress when you start, In Review when it's committed, and Done when a release ships it (or whatever the repo's workflow says).
- If the plan changes, update the doc first, then bring Linear in line: retitle, reprioritize or cancel, with the reason in the issue.
- Report filing and progress as a log: `+ JGA-123 <title>` for each issue filed, `→ JGA-123 → In Review` for each move.
