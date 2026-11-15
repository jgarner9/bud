# content/

What syncs to every device that runs OpenCode (Suite D17, D57): generic instructions, skills, agents, commands, plugins and the environment. CI packs this directory into a signed content bundle on each push to main that changes it (Suite D60), and `bud sync` unpacks it on each device and layers the private overlay on top (Suite D59).

| Path | What goes there | Lands in |
| --- | --- | --- |
| `AGENTS.md` | Global instructions, generic only; the persona lives in the overlay | build plan task 1.11 |
| `skills/<id>/SKILL.md` | Skills, one directory each | task 1.11 |
| `agents/*.md` | Agents as Markdown with front matter | task 1.11 |
| `commands/*.md` | Commands, with fixed, trusted command lines only | phase 4 |
| `plugins/<name>/` | OpenCode TypeScript plugins | phase 7 |
| `env/registry.jsonc` | Providers, models and the nine roles (Suite D46, D58) | task 1.2 |
| `env/*.jsonc` | Config fragments: MCP servers, permissions, routes | tasks 1.7, 1.11 |

Nothing personal goes here: no hostnames, tailnet names, home paths or real `op://` paths.
