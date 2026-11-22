---
name: d111-unattended-deny
description: Suite D111 (2026-11-21, PR #94): bud-unattended-deny OpenCode plugin rejects permission asks unanswered after 10 minutes
metadata:
  type: project
---

Suite D111: `content/plugins/bud-unattended-deny/server.js` closes the D99 gap (OpenCode server with no client waits forever on an ask). It subscribes to permission.asked/replied and replies only `reject` + message after 10 min. Ships to every OpenCode profile (nil `plugins` = all). Tests: `just opencode-plugin-test` (node --test, fake ctx). Not yet run in a live OpenCode 2.3.2; npm install of @opencode/cli was blocked in cloud sessions.

**Why:** keeps attended planning asks (Linear/GitHub writes) while an unattended session refuses instead of hanging.
**How to apply:** overlay profiles that list `plugins` must include it on the planning server. Related: [[r5b-planning-build]]
