# Device profiles

A device profile says what one machine runs, which update channel it follows, and which synced content it takes (Suite D18, D57, D61). `bud install --device <name>` reads it to pick the components to set up and remembers the name as the `device` setting. `bud sync` will read it to pick the content once it's built (planned, task 1.8). Without a device, `bud install` sets up the IDE alone, as it always has. The device profile is separate from `--profile full|minimal`, which is only how much of the IDE to install (Suite D62).

The public repo ships three examples in `profiles/`: `dev-machine`, `server` and `model-box`. Your real profiles (laptop, home server, Qwen box) live in your private overlay's `profiles/`. An overlay profile replaces the example with the same name in full.

## Format

JSON, one file per profile, named after it (`profiles/<name>.json`). A field bud doesn't know is an error.

```json
{
  "name": "dev-machine",
  "components": ["ide", "harness"],
  "channel": "edge",
  "content": {
    "skills": ["*"],
    "env": ["base", "coding"]
  }
}
```

| Field | Meaning |
| --- | --- |
| `name` | Lowercase letters, digits and `-`, and the same as the file's name. |
| `components` | What the device runs, at least one: `ide` (the Neovim IDE), `harness` (OpenCode, the local coding harness), `app` (the chat app), `server` (the home-server units: Hermes, the planning OpenCode, the router, the curator and the KB site), `qwen` (llama.cpp and the local models). |
| `channel` | `stable` or `edge`. Edge gets every content version and nightly code, while stable gets tagged releases and the content you promote. The laptop runs edge and the home server runs stable, so you try a change before the 24/7 manager gets it. |
| `content` | One list of globs per kind: `skills`, `agents`, `commands`, `plugins` and `env`. A glob matches IDs, like `"*"`, `"plan-*"` or `"learn"`. A kind left out takes everything, because skills cost only a name and a description until they're used. `env` is the exception: env fragments are per device, so leaving it out takes only `base`. An empty list takes nothing. |

The `model-box` example has no `content`, since nothing on it runs OpenCode or Hermes.
