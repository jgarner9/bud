---
name: add-provider
description: Add a model provider to Bud's model pool (OpenRouter, Nous Portal, an OpenAI or Anthropic API key, another local llama-server), or write the adapter for a provider API bud doesn't speak yet. Use when asked to add, connect, swap or drop a provider or its models.
---

# Adding a provider to the model pool

Providers and models live in one file, the model registry (Suite D46), and every role picks from a pool of models in it (D64). Routing names roles, never models, so a new provider's models join the pool without touching any runtime config (D70, D71). Read `docs/registry.md` in the bud repo first: it is the format's one reference.

## 1. Settle the facts before editing

Find these in the provider's own docs, and ask Jason for anything you can't:

- **Endpoint and API style.** Is it OpenAI-compatible (`/v1/chat/completions`, `/v1/models`)? Most are: OpenCode Go, OpenRouter, Nous Portal, llama-server, OpenAI itself.
- **Key.** A `{env:VAR}` the runtime starts with, or an `op://vault/item/field` reference (Suite D7). Never the key itself. A real `op://` path or a private host goes in the overlay's `env/registry.jsonc`, never the public file (`just leak-check` fails on them).
- **Privacy.** Does it keep requests, and for how long? Does it train on them? Only a `local` provider may see P0; only a zero-retention model may see P1; a model that keeps requests is P2 at most, and one that trains is P2 at most and usually left out (D23, D65). Record `retention` and `trains`.
- **Billing.** Included in a plan (`cost: "C1"`, with a dollar `allowance` per model if the plan sets one) or pay per token (`C2`, under the spending cap, D24).
- **A subscription is not API access.** A ChatGPT, Claude or Copilot subscription covers its own apps and agents, not a gateway calling it. Check the provider's terms before wiring one in; if they don't clearly allow it, say so to Jason and stop.

## 2. Pick the path

- **The registry already speaks its API** (`APIs` in `internal/registry/registry.go`; `openai` today): it is a registry edit only. Go to 3.
- **It speaks something else** (Anthropic Messages, OpenAI Responses, Gemini), **or its model list has a shape bud can't read**: do 4, then 3.

## 3. Registry edit

1. Add the provider under `providers`: `api`, `endpoint`, `key`, and `local: true` only if it runs in the house.
2. Add each model you want under `models` with the hand-kept fields: `provider`, `name` (as the provider spells it), `privacy`, `cost`, `grade` (`top`, `mid`, `light`), `allowance`, `retention`, `trains`. The same model through two providers gets two IDs, such as `openrouter-glm-5.3` beside `glm-5.3`.
3. Fill the facts from the provider's list: `bud registry refresh --write content/env/registry.jsonc <provider>`. It writes `inputs`, `caps`, `context`, `output` and `price` and keeps everything else. A provider that lists only IDs gives no facts: write those fields by hand from its docs. It also lists models you didn't add; mention the promising ones to Jason rather than adding them.
4. Put the models in role pools, in preference order. Each member must meet the role's `needs`. The classifier, curator and embedding pools take only P0 (local) models.
5. Check: `bud registry check content/env/registry.jsonc [overlay env/registry.jsonc]`, then `just test`. Run the refresh once more; it should say "every fact matches".
6. Commit `feat: add <provider> to the model pool`. A change under `content/` ships as a content release when it merges.

## 4. A new API style or model-list shape (code)

Keep each step to one place; nothing outside these files should change.

1. **Model list.** `internal/registry/catalog.go` reads OpenCode Go's list (facts included) and ID-only lists. For another shape, map its fields onto `Facts` in one function and test it with a trimmed copy of the provider's real answer, the way `catalog_test.go` uses `goList`. Fetch the real answer first rather than trusting docs. OpenRouter, for example, puts inputs under `architecture.input_modalities`, the context in `context_length`, prices as strings per token (multiply by 1,000,000) and capabilities in `supported_parameters`.
2. **Wire format.** Add the value to `APIs` in `internal/registry/registry.go` and document it in `docs/registry.md`. Each runtime adapter that renders providers (`internal/adapter/opencode` today) maps it to that runtime's provider package.
3. **Gateway adapter.** Once phase 3's gateway exists (D70), each API style is one adapter there, behind one Go interface: list models, send a chat request (streaming), and read usage from the response. Add a file for the new style with tests against an `httptest` server. Until the gateway lands, this step doesn't apply.
4. Run `just check`, then do section 3 for the provider itself.

## Don't

- Put a key, a private host or a real `op://` path in the public repo.
- Give a model P0 unless its provider is local, or P1 unless it keeps nothing.
- Add every model a provider lists. Jason picks what joins the pool.
