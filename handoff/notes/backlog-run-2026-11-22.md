# Backlog run 2026-11-22 (thread "Work remaining Bud backlog tasks")

What this thread built, and what each needs from Jason. Revisit items it settles are named.

## PR #104: memory review on Signal (Suite D113, on Claude's recommendation)

Settles revisit item 17 (ops `reviews`) and the gap in item 3 (nothing approved staged memory records).

- `bud memory-review` asks on Signal about each staged record, showing the whole record. A 👍 or `yes <code>` approves it. His 👎 or `no <code>` deletes it. With no answer the record stays staged and is asked about again after a day.
- The ops `reviews` tool reads the counts it writes. It does this instead of using item 17's `pending_count` token: no new token, and ops stays off `op run`.

Once #104 is merged and the home server runs a main build that includes it, set it up on the home server (`server/memory-review/README.md`):

```sh
# 1. Laptop, your own op sign-in: add "staged" and "discard" to the reviewer's actions in the "Bud Memory" item's clients field
#    (the reviewer entry becomes {"tiers":["P0","P1","P2"],"actions":["search","staged","approve","discard"]}),
#    then on the home server: systemctl --user restart bud-memory
# 2. the home server:
printf 'BUD_MEMORY_TOKEN_REVIEWER=op://vault/Bud Memory/reviewer\n' > ~/.config/bud-server/memory-review.env && chmod 600 ~/.config/bud-server/memory-review.env
cp server/memory-review/bud-memory-review.service ~/.config/systemd/user/   # from the bud checkout
systemctl --user daemon-reload && systemctl --user enable --now bud-memory-review
# within a minute, record 1 (the smoke test) arrives on Signal as "Save this to Bud memory?"
```

Part F (approving by hand) is then only needed for a record longer than 6,000 characters.

## PR #105: start lines without --verbose (merged)

The gateway, router and memory-review now print their start line to the journal even without `--verbose`. For the gateway and the router, that line includes the privacy-rule count. Once the home server builds main, `journalctl --user -u bud-mcp-gateway` shows "N privacy rules", so item 15's check no longer needs a throwaway gateway.

## Left alone

- Item 16 (router over-cap approval) waits on the approvals audit in the thread "Loosen Signal approvals and privacy routing", since Jason wants fewer Signal prompts.
- Memory review adds one Signal prompt per staged record. That belongs in the same audit, for example auto-approving low-risk records or batching them once a day.
- Everything else open in Bud 2.0 needs the home server, the Qwen box, Herdr or a design session with Jason.
