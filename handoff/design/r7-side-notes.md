# R7 mail: side notes for Jason (2026-11-21)

Things to talk over when you're back. Nothing here is blocking dev; where a choice was needed I went with the recommendation and say so.

## Done while you were out

- PR #85 (merged): the calendar and mail env examples and READMEs point at the one 1Password item `iCloud` (`username`, `password`, and `email` as the send-as address; the examples spell it `op://vault/iCloud/...` with its exact case). The overlay has no iCloud references, so it needed no change. Your real env files on the home server hold the real paths, so `~/.config/bud-server/calendar-mcp.env` needs the new item name too (see the setup steps in the thread).

## For discussion

1. **Contacts still has its own item.** `server/contacts-mcp/contacts-mcp.env.example` still says `icloud-carddav`. I left it alone because you only renamed the CalDAV item. If contacts uses the same app-specific password, I'll point it at `iCloud` too. Recommendation: share the item, since one app password per Apple ID is simpler and revoking it cuts off all three at once, which is what you'd want if it leaked.
2. **One app password for calendar and mail.** This is the effect of the rename. The spec said "made for Bud alone so it can be revoked on its own", and that still holds, because it's one password for all of Bud rather than one per service. No action needed unless you'd rather keep mail separate.
3. **signal-cli and long prompts.** A mail send's prompt can reach about 6,000 characters. Signal's own limit is generous, but signal-cli may split or refuse long text. The long test email in step 7 settles it. If it splits, the fix is in the bud-signal plugin: send it as one attachment-free long message, or refuse over the limit (fail closed).
4. **Slice 5 (JGA-317, isolated ingestion) waits on the live run.** The spec puts it after slice 4 runs on the home server, because its design depends on what the recording shows (UIDVALIDITY, how iCloud reports new mail). My draft shape, for you to confirm before I build it:
   - A deterministic `bud mail-ingest` on a systemd timer, with no model. It fetches mail newer than the last seen UID per folder and stores the UIDVALIDITY and UID high-water mark in `~/.local/state/bud/`, so a restart neither repeats nor drops mail. It hands each message to the summarizer as data.
   - A Hermes profile for the summarizer with no tools at all (no MCP servers, no terminal, no memory writes) and P0 models only. Its output is a summary with provenance (folder, uid, Message-ID) that is marked untrusted wherever it goes next.
   - Adversarial fixtures (a forged "yes 1234", injected instructions, encoded headers, links) asserting no tool calls and no cloud egress.
   - Open question: whether the summarizer should be a Hermes profile or a direct call to the router with no tools in the request. Recommendation: the direct router call, since a request with no tool definitions can't call a tool whatever the mail says, and it's simpler to test. Hermes then reads the summaries as untrusted data.
