# The model registry

One file names every model provider, every model, and which model fills each of Bud's roles (Suite D46, D58). `bud sync` renders each runtime's own config from it: OpenCode's `opencode.jsonc` today, and Hermes, minuet and llama.cpp as those land. Swapping the model behind a role is one edit in one file. No runtime config names a model by hand.

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
    "qwen-35b": { "provider": "qwen", "name": "qwen-35b", "context": 32768, "privacy": ["P0", "P1", "P2"], "cost": "C0" },
    "kimi-k3":  { "provider": "go", "name": "kimi-k3", "privacy": ["P1", "P2"], "cost": "C1" }
  },
  "roles": {
    "manager":    { "model": "kimi-k3", "fallbacks": ["qwen-35b"] },
    "classifier": { "model": "qwen-35b" }
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
| `context` | The context size in tokens, if known. |
| `privacy` | The privacy tiers the model may see: `P0` private, `P1` internal, `P2` public (Suite D23). `P0` needs a `local` provider. |
| `cost` | `C0` free and local, `C1` included in the plan, `C2` paid top-up (Suite D24). |
| `dims` | An embedding model's vector size. Each stored vector records it, so swapping the model is a re-embed job (Suite D52). |

### Roles

Every one of the nine roles must be filled, and nothing else can be a role: `manager`, `coding`, `planning`, `classifier`, `fim`, `embedding`, `fallback`, `curator`, `vision`. Each names a `model` and optional `fallbacks`, tried in order. A model can appear only once in a role's list.

Three roles see private data, so every model they list must allow P0. The `classifier` reads a prompt before its tier is known, the `curator` writes personal KB pages, and `embedding` covers all of working memory. The `embedding` role's models also need `dims`.

## The overlay

Your overlay's `env/registry.jsonc` uses the same format, and every part of it is optional. An entry replaces the public entry with the same ID in full, so it restates every field, including `local`. An entry with a new ID is added. A provider that loses `local: true` this way also loses its P0 models, so the check fails safe. The merged registry has to pass the same check as the public one.
