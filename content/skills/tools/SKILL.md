---
name: tools
description: Which tool to reach for and in what order. Code navigation (OpenCode's code tools — the explore subagent, Serena, or cocoindex-code; plus ripgrep, ast-grep and language servers), the lookup ladder for facts and docs (own knowledge only for things that can't change, then Context7, then Exa), notes and memory in Bud memory for Jason's own projects and setup, tasks in Linear, and compressed output. Use when you need to find or understand code, for a fact or doc you can't answer from knowledge, and before reading or writing notes. Not needed to answer a chat question you already know.
---

# Using the tools

Your tools are part of the answer, not a fallback. Check what this session actually has (MCP servers, tools, skills, subagents) and use them before guessing. When a tool you'd expect is missing or fails to connect, say so in one line instead of quietly working around it.

## Finding code

Prefer cheap, precise lookups to reading whole files:

1. **Orient.** Without a map of the repo in context, get the most-referenced files and their top-level symbols from OpenCode's code tools (the explore subagent, Serena, or cocoindex-code).
2. **Symbols:** Serena (`find_symbol`, `find_referencing_symbols`, `get_symbols_overview`) or a language-server MCP for definitions, references, callers and types. Check references before calling anything unused. Use only their read-only tools unless you're asked to edit.
3. **"Where's the code that does X":** a semantic code search (cocoindex-code) when connected.
4. **Text:** `rg -n 'pattern'`, narrowed with `-t go`, `-g '*.md'`, `-F` for literal strings. `ast-grep run -p 'PATTERN' -l LANG` when the shape matters more than the text (`fmt.Errorf($$$)`), search only, never `--rewrite`.
5. Read only the part of a file you need.

## Facts, docs and anything current: the ladder

Go down one rung at a time and stop at the first that answers. Every tool call resends the whole conversation, so a quick question gets at most one lookup, and one it can answer from knowledge gets none.

1. **What you already know**, only for things that can't change: language semantics, algorithms, maths, protocols by RFC, history, general concepts. Anything versioned, priced, released, scheduled, or about who runs what skips this rung. If you can't say why the fact couldn't have changed since your training, it isn't rung 1.
2. **Bud memory**, only when the question is about Jason's own projects, setup or past work (the section below). Don't check it for a general fact or a library question.
3. **Context7** for anything about a library, framework, SDK, CLI or service, even ones you know well: `resolve-library-id`, then `query-docs` with one concept per query. Name the version the answer is for.
4. **Exa** (or the built-in web search) for everything else current, or when Context7 has nothing: search describing the page you want, then fetch the one or two best results.

If a rung's tools aren't connected, say so in one line and go to the next.

After rung 3 or 4, don't write to Bud memory unasked. For a one-off question just answer. When the answer looks worth keeping (he's researching a topic, or he'll plainly ask again), offer in one line to cache it, and stage it only if he says yes; the format is in [cache-note.md](cache-note.md). Never cache secrets, private code, or anything from a work codebase or Linear.

End an answer that needed a lookup with one source line:

- `◇ known` (rung 1)
- `◇ memory <topic>: <question gist> (saved <YYYY-MM-DD>)`, when you did use a record
- `◇ context7 /neovim/neovim`
- `◇ exa <url>`

Add `· staged → <topic>: <question gist>` when he said yes to caching.

Lookups in the middle of a task don't need the line.

## Notes and memory (Bud memory)

- Bud memory is one shared store, the same for every runtime, so no runtime keeps a memory of its own. A record you stage waits for Jason's review and is searchable only once he approves it.
- Before answering about Jason's own projects, setup or past work (and only then), check Bud memory first: `memory_search` with a few words for the topic, not the whole question, `limit` 3 to 5. Each hit comes back with its body plus its source, tier and when it was saved; take only the one or two you need. If the hits look off, retry once with other words.
- Stage at checkpoints, not every turn: a real decision, or when he says "save that" or "wrap up". Do it at the end of the reply, after the answer.
- Search before staging, and stage what's new rather than a second copy of a record already there. There is no file tree: the old folders — `projects/`, `decisions/`, `reference/`, `learning/`, `cache/` — still name what a record is for, so put that in the body and pick the `kind` that fits (`observation` for something seen, `fact` for something to keep, `summary` for a wrap-up). Record decisions with the reason.
- Freshness: a record carries when it was saved, not a stale-by date. Use an old record but say "as of <date>" and that it may have changed, and confirm with him before acting on it. Reading never refreshes a record; only a newly approved one does.
- When a fact changes, don't restate it silently: stage "Was: X. Now: Y, because Z." so the old record isn't taken for current.

## Everything else

- **Tasks:** Linear, following the `planning` skill.
- **Compressed output:** when a result says it was compressed (Headroom) and gives a way to fetch the original, retrieve the detail you need instead of working from the summary.
- **Skills:** load one only when the task is writing code, planning or filing issues, or cutting a release (`code-style`, `planning`, `release`), or when he asks for it. A chat question doesn't need one.
