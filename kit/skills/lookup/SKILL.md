---
name: lookup
description: Where answers come from, cheapest certain source first - Claude's own knowledge only for facts that can't have changed, then the Basic Memory docs cache, then Context7, then Exa - and how lookups are cached back into Basic Memory. Use for factual, theoretical and "how do I" questions, "what's the latest", and whenever a task needs a fact about a library, tool, service or anything current.
---

# Looking things up

Answer from the cheapest source that is certainly current, and save what you had to fetch so the next lookup is cheap.

## The ladder

Go down one rung at a time and stop at the first that answers.

1. **What you already know**, only for things that don't change: language semantics, algorithms, maths, protocols by RFC, history, general concepts. Anything versioned, priced, released, scheduled, or about who runs what skips this rung. The test: if you can't say why the fact couldn't have changed since your training, it isn't rung 1.
2. **Basic Memory.** `search_notes` with a few keywords (not the whole question), `page_size` 3-5, in the `cache/` folder first, and across the knowledge base when the question is about the user's own projects or setup. If the hits look off, retry once with `search_type="vector"`. `read_note` only the one or two you need.
   - Fresh (today minus `last_verified` is within `stale_after_days`): answer from it.
   - Stale: treat it as a lead, not an answer. Go to rung 3 or 4 and refresh the note.
3. **Context7** for anything about a library, framework, SDK, CLI or service: `resolve-library-id`, then `query-docs` with one concept per query.
4. **Exa** for everything else current, or when Context7 has nothing: `web_search_exa` describing the page you want, then `web_fetch_exa` on the one or two best results.

If a rung's tools aren't connected, say so in one line and go to the next.

## Caching what you fetched

After rung 3 or 4, write the answer back with `write_note` (or `edit_note` when refreshing a stale one). Search first: update a note on the same question rather than adding another.

- **Folder:** `cache/<library-or-topic>`, title `<topic>: <question gist>`, e.g. `cache/neovim` → "neovim: vim.pack lockfile format".
- **Shelf life** (`stale_after_days`): 30 for versioned library or tool docs, 7 for current events, prices and releases, 90 for standards and specs.

```markdown
---
title: <topic>: <question gist>
type: cache
source: <Context7 library id, or URL>
version: <version the answer is for, when known>
last_verified: <today, YYYY-MM-DD>
stale_after_days: <30|7|90>
---

# <topic>: <question gist>

<The answer, distilled: the facts and the snippet that answer the question. Under about 300 words. Never a docs dump.>

## Sources
- <links>

## Relations
- about [[<library or topic>]]
```

When refreshing, bump `last_verified`, and mark facts that changed instead of deleting them: `- [superseded YYYY-MM-DD] Was: X. Now: Y.`

Never cache secrets, the user's private code, or Linear contents.

## Saying where it came from

End an answer with one source line:

- `◇ known` (rung 1)
- `◇ memory cache/neovim/vim-pack-lockfile (verified 2026-10-20)`
- `◇ context7 /neovim/neovim · cached → cache/neovim/vim-pack-lockfile`
- `◇ exa <url> · cached → cache/<topic>/<slug>`

Mid-task lookups don't need the line; the task log is enough.
