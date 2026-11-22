# The model registry

One file names every model provider, every model with what it can do, and the pool of models each of Bud's roles picks from (Suite D46, D58, D64). `bud sync` renders each runtime's own config from it: OpenCode's `opencode.jsonc` and, on the home server, Hermes's `config.yaml` today, and minuet and llama.cpp as those land. Changing a role's models is one edit in one file. No runtime config names a model by hand, and routing names roles, never models.

The public file is `content/env/registry.jsonc`. It is generic: its local hosts are placeholders under `.invalid`, a domain that never resolves. Your private overlay's `env/registry.jsonc` is merged over it (Suite D59). Check either one with:

```sh
bud registry check content/env/registry.jsonc [overlay/env/registry.jsonc]
```

It prints every problem at once, each with what to fix, and exits 1 when there is one.

## Format

JSONC: JSON plus `//` and `/* */` comments and trailing commas. A field bud doesn't know is an error, so a typo can't drop a setting without anyone noticing.

```jsonc
{
  "version": 1,
  "providers": {
    "go":   { "api": "openai", "endpoint": "https://.../v1", "key": "{env:OPENCODE_GO_API_KEY}" },
    "qwen": { "api": "openai", "endpoint": "http://qwen.invalid:8080/v1", "local": true }
  },
  "models": {
    "qwen-35b": { "provider": "qwen", "name": "qwen-35b", "privacy": ["P0", "P1", "P2"], "cost": "C0",
      "grade": "mid", "inputs": ["text", "image"], "caps": ["tools", "reasoning", "json"], "context": 32768 },
    "glm-5.3":  { "provider": "go", "name": "glm-5.3", "privacy": ["P1", "P2"], "cost": "C1",
      "grade": "top", "allowance": 120, "retention": 0, "inputs": ["text"], "caps": ["tools", "reasoning", "json"],
      "context": 1000000, "output": 131072, "price": { "in": 1.4, "out": 4.4 } }
  },
  "roles": {
    "coding":     { "needs": ["tools"], "pool": ["glm-5.3", "qwen-35b"] },
    "classifier": { "needs": ["json"], "pool": ["qwen-35b"] }
    // ... all nine roles
  }
}
```

### Providers

| Field | Meaning |
| --- | --- |
| `api` | The wire format. `openai` (the OpenAI-compatible API) is the only one so far. |
| `endpoint` | The base URL, `http` or `https`. |
| `key` | How the runtime gets the API key: an `op://vault/item/field` reference (Suite D7), `{env:VAR}` for a variable the runtime is started with, or left out for none. Never the key itself; a value that's neither form fails the check. |
| `local` | `true` when the endpoint is inside the house. Only models on a local provider may be given P0 data (Suite D23). |
| `region` | Optional, for providers that serve from more than one region. |

### Models

| Field | Meaning |
| --- | --- |
| `provider` | A provider's ID. |
| `name` | The model's id as its provider spells it. On a local llama.cpp provider it's the `--alias` bud gives the server, so the weights can change without this file changing. |
| `privacy` | The privacy tiers the model may see: `P0` private, `P1` internal, `P2` public (Suite D23). `P0` needs a `local` provider. |
| `cost` | `C0` free and local, `C1` included in the plan, `C2` paid top-up (Suite D24). |
| `dims` | An embedding model's vector size. Each stored vector records it, so swapping the model is a re-embed job (Suite D52). |
| `inputs` | What it can be given: `text`, `image`, `video`, `audio`, `pdf`. A fact. |
| `caps` | What it can do beyond chat: `tools` (tool calling), `reasoning`, `json` (answers that follow a JSON schema), `fim` (fill-in-the-middle), `embed`. The first three are facts; `fim` and `embed` are declared by hand. |
| `context`, `output` | Its context window and longest answer, in tokens. Facts. |
| `price` | `{ "in": ..., "out": ... }`, dollars per million tokens. A fact. |
| `grade` | `top`, `mid` or `light`: how hard a task it is worth spending on. The router matches it to a request's difficulty. Kept by hand. |
| `allowance` | The plan's included spend on it, in dollars a month (Go Plus sets one per model). Kept by hand. |
| `retention` | Days the provider keeps requests, `0` for none. Kept by hand. A model kept longer than 0 days may see only `P2` (Suite D65). |
| `trains` | `true` when the provider trains on requests. Such a model may see only `P2` (Suite D65). |

Facts are what the provider's own model list reports, so `bud registry refresh` keeps them true (below). The fields marked "kept by hand" come from the provider's docs or your judgement, and a refresh never touches them.

### Roles

Every one of the nine roles must be filled, and nothing else can be a role: `manager`, `coding`, `planning`, `classifier`, `fim`, `embedding`, `fallback`, `curator`, `vision`.

| Field | Meaning |
| --- | --- |
| `pool` | The models the role may use, in preference order. The router drops the ones a request's privacy tier, needs, size, provider health or remaining allowance rule out, then ranks the rest by grade, allowance left and this order (Suite D64). A model can appear only once. |
| `needs` | Capabilities or inputs every member must have, such as `tools` for coding or `image` for vision. The check fails for a member without one. |
| `pin` | Optional: one member the router always uses. A runtime that takes one model per role, like OpenCode's default model, gets the pin, or else the first member. |
| `model`, `fallbacks` | The older form, read as the pool `[model, fallbacks...]`. A role uses one form or the other. |

Three roles see private data, so every model they list must allow P0. The `classifier` reads a prompt before its tier is known, the `curator` writes personal KB pages, and `embedding` covers all of working memory. The `embedding` role's models also need `dims`.

### Privacy rules

Privacy rules raise a request's privacy tier when its text matches (Suite D23, D112). The router applies them to every request body before it picks a model, so a message that starts with `#private` stays on a local model even when the client sent `P1` or `P2`. The MCP gateway applies the same rules to every tool call's arguments, so a pasted private key can't be sent to a cloud tool such as Linear from a `P1` session. They only raise: a request already more private stays as it is, and a rule can never lower a tier.

```jsonc
"privacy_rules": {
  "marker": { "match": "(?i)^\\s*#private\\b", "tier": "P0" }
}
```

| Field | Meaning |
| --- | --- |
| `match` | A regular expression in Go's RE2 syntax, matched against each message's text on its own, not the whole conversation. Use `(?i)` for any case. |
| `tier` | `P0` or `P1`: what a matching request becomes. `P2` is refused, since it can't raise anything. |
| `entropy` | Optional: the Shannon entropy, in bits per character, the matched secret must be over (strictly, as in gitleaks' rules), so a placeholder such as `AKIAAAAAAAAAAAAAAAAA` doesn't count. The secret is the group named `secret`, else the first group that took part and isn't empty, else the whole match. |
| `validate` | Optional: a check the matched text must also pass. `luhn` is a card number (13 to 19 digits passing the Luhn checksum, not all one digit), `iban` an IBAN (country, check digits, mod-97 remainder 1), `ssn` a US Social Security number in the ranges ever issued (not area 000, 666 or 900 and up, group 00 or serial 0000, nor the well-known sample numbers), and `phone` a phone number (`+` and 8 to 15 digits, or a North American number whose area code and exchange start 2 to 9). |
| `keywords` | Optional, lowercase: one must be in the text, in any case, before the pattern is tried. A substring check is far cheaper than a regular expression, and it is what keeps the whole pack at about 1.5 ms for a 40-message chat (`BenchmarkTheShippedRulesOverAChat`), under the router's 5 ms budget. |

A rule's name is an ID like a provider's. These ship (Suite D118):

- `marker` keeps a conversation local when a message starts with `#private`, and `private-key` does the same for a pasted private key.
- **Personal data:** `pii-card`, `pii-iban`, `pii-ssn` and `pii-phone`, each a broad pattern with a strict validator, so a timestamp or a build number doesn't keep a chat local but a card number that passes its checksum does. Cards are matched only when they start with 3 to 6, as the networks' numbers do, so a millisecond timestamp (starting with 1) never counts.
- **Secrets:** the `secret-*` rules, a curated subset of [gitleaks](https://github.com/gitleaks/gitleaks)' default rules (MIT), each a token with a fixed prefix or shape: cloud and AI provider keys, GitHub, GitLab, Slack and Stripe tokens, JWTs, 1Password service account tokens (`ops_`) and secret keys, and others. Rules that need a keyword nearby, such as gitleaks' `generic-api-key`, are left out, since a chat says "key" and "token" all the time. They're generated, not hand-written: `scripts/gen-secret-rules` holds the list, and `go run ./scripts/gen-secret-rules <gitleaks.toml> <commit> content/env/registry.jsonc` rewrites the lines between the `BEGIN secret rules` and `END secret rules` markers from a pinned gitleaks commit, which the block names. Only gitleaks' rule data is used, never its code (D1). Its allowlists are dropped: a false positive only keeps a chat local.

Every rule has a sample it must match and one it mustn't in `internal/registry`'s `packSamples`, and the test fails for a shipped rule without them. Personal rules (names, places, accounts, health and money terms you want kept local) go in the overlay's `registry.jsonc`, as a deny-list, which merges them by name like the rest, so an overlay rule of the same name replaces the public one whole. The router and the gateway name the rule that fired in the `X-Bud-Privacy-Raised` response header.

## Keeping it true

```sh
bud registry refresh content/env/registry.jsonc go          # report
bud registry refresh --write content/env/registry.jsonc go  # and rewrite the drifted facts
```

`refresh` fetches the provider's `GET /models` and compares each of the provider's models in the file with it (Suite D69). It reports facts that drifted, models the provider no longer lists (take them out of their pools) and models the registry doesn't have (add one by hand if you want it, with its grade, privacy and pools). `--write` rewrites only the drifted facts, in place, so comments and layout stay and the diff shows just what changed; it writes nothing if the result would fail the check. OpenCode Go's list carries every fact and needs no key; a provider that lists only IDs gets the gone and new report and nothing else. `bud doctor --net` warns when a synced registry has drifted the same way.

## The overlay

Your overlay's `env/registry.jsonc` uses the same format, and every part of it is optional. An entry replaces the public entry with the same ID in full, so it restates every field, including `local`. An entry with a new ID is added. A provider that loses `local: true` this way also loses its P0 models, so the check fails safe. The merged registry has to pass the same check as the public one.

## Adding a provider

The `add-provider` skill in `content/skills/` walks through it: the facts to settle first (key, privacy, billing), the registry edit for a provider whose API bud already speaks, and the code for one it doesn't (Suite D71).
