# The kit manifest

The format of `kit/kit.json` and the files beside it (D111, D114, D123), and what `kit.Load` checks. It is the one description of the format: change it and `internal/kit/manifest.go` together. What `bud kit` does with the kit is in [kit.md](kit.md).

The manifest is the desired state of the Claude setup on every surface. It is public and embedded in the binary (D5), so it holds no secret, hostname or personal detail: a value that differs by machine is a `${VAR}` placeholder, filled from `config.json` or the keychain when a server is applied.

Loading only reads and checks. Nothing in `servers`, `settings`, `rules`, `account` or `cloud` is applied to a machine yet (JGA-249, JGA-250 and later do that).

## Files

| Path in `kit/` | Holds |
| --- | --- |
| `kit.json` | Every section below |
| `rules/*.md` | The text of a rule the kit itself carries |
| `cloud/setup.sh` | The setup script a cloud environment should carry |

## Sections

Every section is optional, and a missing one is empty. A key `kit.json` doesn't define here is an error at any depth, except inside a `settings` object, which is Claude Code's format and not bud's.

### `marketplaces`, `plugins`

As before: `marketplaces` maps a name to the source `claude plugin marketplace add` takes, and `plugins` lists `name@marketplace` ids. `Load` doesn't check them (the sync does).

### `servers`

An object, one entry per MCP server, keyed by the name Claude Code knows it by.

| Field | Required | Meaning |
| --- | --- | --- |
| `description` | yes | One line, shown when choosing servers |
| `mcp` | yes | How to reach it, in the format `claude mcp add-json` takes |
| `modes` | yes | The modes it is on in by default: any of `full`, `merge`. `[]` means opt-in |
| `vars` | no | The values its `mcp` placeholders need: name → `{"prompt": "...", "secret": true\|false}`, both required |
| `requires` | no | A command that must be on `PATH` for it to be added |
| `connector` | no | The claude.ai connector that delivers the same server where there is an account: a key of `account.connectors` |
| `desktop` | no | `true` offers it to Claude Desktop on a third-party provider |
| `settings` | no | A `settings.json` fragment merged with the base settings while the server is on |

`mcp` has `type` (`stdio`, `http` or `sse`) and then, for `stdio`, `command` with optional `args` (strings) and `env` (name → string); for `http` and `sse`, `url` with optional `headers` (name → string).

A placeholder is `${NAME}` or `${NAME:?}`, in `command`, an `args` element, an `env` or `headers` value, or `url`. The plain form means the entry it is in is dropped when the value is unset; `:?` means the server can't work without it and is skipped. Loading only checks that placeholders and `vars` agree.

### `settings`

An object: the base `settings.json`, merged under a machine's own. Its contents aren't checked.

### `rules`

An object, one entry per rule, keyed by the file name it gets in `~/.claude/rules/bud/`.

| Field | Required | Meaning |
| --- | --- | --- |
| `file` | one of these two | The rule's text, as a path in `kit/` |
| `marketplace` | one of these two | The rule's text, as `marketplace/path` in a kit marketplace's checkout |
| `server` | no | The rule is installed only while this server is on: a key of `servers` |

### `account`

What the claude.ai account should hold (D112: read and guided, never written).

| Field | Meaning |
| --- | --- |
| `marketplaces` | Plugin marketplaces the account should sync, as `owner/repo` |
| `plugins.wanted` | Plugin names the account should have on |
| `plugins.unwanted` | Plugin names it should not have, shown until they are gone (D120) |
| `connectors` | name → `{"on": true\|false}`, `on` required: whether the connector is on by default. The name is the one claude.ai shows |
| `instructions` | The text pasted into personal preferences, as `marketplace/path` |

### `cloud`

`script`: the setup script, as a path in `kit/`.

## What `Load` checks

`Load(fsys)` returns the kit or the first error. Entries are checked in sorted order of name and rules in the order below, so the same bad manifest always gives the same error. Every error starts `kit.json: `, and one about an entry names it next: `kit.json: server "exa": ...`, `kit.json: rule "notes.md": ...`, `kit.json: connector "Linear": ...`, `kit.json: account: ...`, `kit.json: cloud: ...`.

1. `kit.json` is valid JSON with no unknown top-level key.
2. Each server, by name:
   1. The name matches `^[a-z0-9][a-z0-9._-]*$`.
   2. No unknown field (in the entry, its `mcp` or a `vars` entry).
   3. `description` is not empty; `mcp` and `modes` are present (`modes` may be `[]`, not absent or `null`).
   4. `mcp.type` is `stdio`, `http` or `sse`. `stdio` has a `command` and no `url` or `headers`; `http` and `sse` have a `url` and no `command`, `args` or `env`.
   5. Each mode is `full` or `merge`, with no repeat.
   6. `requires`, when set, has no whitespace and no `/`.
   7. `connector` and `desktop` are set only on an `http` or `sse` server.
   8. Each `vars` name matches `^[A-Z][A-Z0-9_]*$`, its `prompt` is not empty and `secret` is present.
   9. Every `${` in `mcp` starts a well-formed placeholder whose name is in `vars`; every name in `vars` is used by a placeholder; an `env` or `headers` name holds no `${`.
   10. `settings`, when present, is an object.
3. `settings`, when present, is an object.
4. Each connector in `account.connectors`, by name: the name is not empty and has no leading or trailing space, no unknown field, `on` is present.
5. Each server's `connector` is a key of `account.connectors`, and no two servers name the same one.
6. `account`: a `plugins.wanted` or `plugins.unwanted` name matches the server-name pattern (so no `@`), no name is in both, and each wanted name is the name part of an entry in `plugins`. `marketplaces` has no empty entry and no repeat. `instructions`, when set, is `marketplace/path` with the marketplace a key of `marketplaces` and the path valid (`fs.ValidPath`).
7. Each rule, by name: the name matches `^[a-z0-9][a-z0-9._-]*\.md$`; no unknown field; exactly one of `file` and `marketplace`; a `file` is a valid path that exists in `fsys` and is not empty; a `marketplace` is checked as `instructions` is; `server`, when set, is a key of `servers`.
8. `cloud.script`, when set, is a valid path that exists in `fsys` and is not empty.

## What the code gets

`Kit` has a field per section: `Marketplaces`, `Plugins`, `Servers` (`map[string]Server`), `Settings` (`map[string]any`), `Rules` (`map[string]Rule`), `Account`, `Cloud`. `Load` never reads the rule or script text; callers read it from the same `fsys`.

`Kit.ExpectedServers()` is what `expected_mcp` was: the sorted names of the servers that are on by default in full mode and need no command on `PATH`, which doctor's `claude setup` check looks for. What `core` was is `Account.Instructions`.

## Tests

`internal/kit/manifest_test.go`: the embedded kit loads and has every server clod had; one table test with a case per rule above, each a small `kit.json` and the error it must give; and a check that no file in the embedded `kit/` names a person, a tailnet or clod.
