---
name: r7-mail-build
description: State of the R7 iCloud mail build (bud mail-mcp, Suite D106/D107), its slices, PRs and decisions
metadata:
  type: project
  modified: 2026-11-21T06:30:28.302Z
---

R7 mail (thread "Build Bud mail over iCloud", started 2026-11-21). Spec: /mnt/project-files/architecture/r7-mail-design.md, approved by Jason 2026-11-21.

- `bud mail-mcp` (`internal/mail`), port 127.0.0.1:8796, gateway server `mail`, P0 only. Env BUD_MAIL_USER, BUD_MAIL_PASSWORD, BUD_MAIL_FROM via op run.
- Suite D107 (Jason, 2026-11-21): a send's Signal prompt shows the whole message (to, cc, subject, full body), bodies over 4,000 chars refused; reply_message takes explicit to/cc checked against the original's participants. Needs the approval prompt (internal/approval/socket.go summarize) to skip the 160-char cut for mail send/reply.
- Slices merged 2026-11-21: 1 IMAP client (PR #81), 2 read tools + command (#82), 3 move/restore/digest/action log + SMTP sends + gateway tiers (#83), 4 systemd unit + `--record` (#84). Left: the home server setup by Jason (steps in server/mail-mcp/README.md, handed over 2026-11-21), then 5 JGA-317 ingestion in Hermes.
- reply_message also takes `subject`, which must equal ReplySubject(original) ("Re: " + original), so the Signal prompt shows it (Copilot fix on #83).
- 1Password (2026-11-21): one item named `iCloud` (was "iCloud CalDAV") serves calendar and mail; username, password, and the send-as address in its `email` field (BUD_MAIL_FROM). Contacts still uses its own carddav item.
- Transcripts in testdata are hand-written until Jason records real iCloud sessions on the home server (slice 4).
- Open until the live check: does iCloud save sent mail to Sent Messages itself; Deleted Messages retention (D54); MOVE/UIDPLUS present.

Related: [[bud-suite-roadmap]], [[signal-approvals]]
