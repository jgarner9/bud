# Caching a lookup in Bud memory

After rung 3 or 4 of the ladder, stage the answer with `memory_stage` so Jason can approve it. Search first with `memory_search`: if a record on the same question is already there, stage the correction rather than a second record.

- **Topic:** `cache/<library-or-topic>` still names what the record is for, but there is no folder — Bud memory is one shared store, so the topic goes in the body, not a path. For example "neovim: vim.pack lockfile format", not a `cache/neovim` path.
- **Shelf life:** Bud memory has no stale-by date, so put the version and the date you checked in the body, and re-check once the fact is old — 30 days for versioned library or tool docs, 7 for current events, prices and releases, 90 for standards and specs.

Stage it as a `fact` (or `summary` for a longer read), with the body:

```markdown
<topic>: <question gist>

<The answer, distilled: the facts and the snippet that answer the question.
Under about 300 words. Never a docs dump.>

Source: <Context7 library id, or URL>
Version: <version the answer is for, when known>
Checked: <today, YYYY-MM-DD>
```

When the answer changes, stage the change rather than leaving the old record as current: "Was: X. Now: Y, because Z."
