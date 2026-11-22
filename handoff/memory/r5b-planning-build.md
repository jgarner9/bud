---
name: r5b-planning-build
description: R5b built 2026-11-21 (PRs #92, #93): planning OpenCode as system user bud-planning and the IDE's <leader>aP home planning pane
metadata:
  type: project
---

R5b merged 2026-11-21 overnight. Suite D109: planning OpenCode runs as system user `bud-planning` (home /var/lib/bud-planning, since leak-check flags /home/<user> paths), system unit `server/planning-opencode/bud-planning-opencode.service` (ProtectHome=yes, /run/user hidden), port 8797, Basic auth OPENCODE_PASSWORD; profile `server-planning`; fragment secrets via `bud.opencode_secrets` (overlay, op:// refs into opencode.env). Suite D110 / BASE D152: `<leader>aP` / `:BudPlan` run `opencode --server <ai.planning.url>` in its own pane after a curl check of /api/info (password from ai.planning.password_file on stdin); statusline plan:home/plan:off. BASE D151 is PR #91's opencode.nvim keys.

**Why:** the planning server must not reach Jason's approval socket/review secret (r6b Q1).
**How to apply:** the home server install is Jason's (steps in notes/side-notes-2026-11-21.md). Known gap: Linear/GitHub write asks block an unattended planning server (D99). Related: [[bud-suite-roadmap]]
