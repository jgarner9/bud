# bud sync

`internal/syncer` is `bud sync` (build plan task 1.8; Suite D46, D57, D59, D60, D61). It brings one device's content and runtime config up to the channel its device profile follows. `internal/cli/sync.go` only reads the config and prints the report.

## Where things come from

| Input | From |
| --- | --- |
| The device profile | config `device`, looked up in the overlay's `profiles/` first, then bud's examples |
| The overlay | `BUD_OVERLAY`, else config `overlay.repo`; empty means none |
| Content releases | config `content.mirror`, else `https://github.com/jgarner9/bud/releases/download` |

A release host serves two kinds of path, so a mirror is a plain file server laid out the same way:

```
<base>/content-channel/channel.json        newest content and code version per channel
<base>/content-channel/channel.json.sig    signed under bud-channel
<base>/content-<v>/content-<v>.tar.gz      the bundle
<base>/content-<v>/content.json            the manifest
<base>/content-<v>/content.json.sig        signed under bud-content
```

The content release workflow (`.github/workflows/content.yml`, described in [release.md](release.md)) publishes exactly these. Content releases are GitHub prereleases, or one of them would become the repo's "latest" release and `install.sh`'s `releases/latest/download` would stop finding the code archives.

## What a sync does

1. Takes the device's sync lock (an `flock` on `~/.local/state/bud/sync.lock`, dropped when the process exits), so a timer and a hand run never interleave; a second run fails at once. A dry run takes no lock. Then reads the ledger, `~/.local/state/bud/sync.json`. Missing means this device has never synced.
2. Fast-forwards the overlay with `overlay.Fetch` (never forced; it stops on local changes or divergence), then finds the profile.
3. Downloads `channel.json` and checks its signature. The offered version is the profile's channel's `content`.
4. Picks the version to use. The installed one again when it's what's offered; the installed one when the offer is a version `--rollback` left; otherwise the offer, which `content.CheckNewer` must find newer than the installed one, so an old signed bundle can't roll a device back.
5. Unless that version's dir is already on disk, downloads its `content.json`, checks the signature and that it names that version, and unpacks the bundle into `~/.local/share/bud/content/<version>/` with `content.Unpack` (every file checked against the manifest; the dir is complete or absent). Deleting the dir makes the next sync fetch it again.
6. Layers the overlay over that dir with `overlay.Layer` and keeps what the profile selects. The registry is the content's `env/registry.jsonc` with the overlay's merged over it, validated; it is never treated as an env fragment.
7. Builds the active link set. `~/.local/share/bud/active` links to `active.d/<hash>`, which holds `skills/<id>` and `plugins/<id>` links into the content version or the overlay. The hash is of the links themselves, so an unchanged selection keeps the same dir.
8. If the profile runs OpenCode (`harness` or `server`), renders `opencode.jsonc` with `adapter/opencode`: skills from `active/skills`, instructions from `content/current/AGENTS.md` then the overlay's, plugins from `active/plugins/<id>`, env fragments public first and the overlay's last. After every fragment's rules come `external_directory` allows for the content versions and the overlay (`opencode.Input.Reachable`, from the resolved data dir), so a fragment that asks before leaving the project still lets skills and instructions load. Agents and commands are copied to `~/.config/opencode/{agents,commands}/<id>.md`. Provider keys that are `op://` references go in `~/.local/state/bud/opencode.env` (mode 0600, references only) for `op run --env-file`. If the profile runs `server`, it also renders Hermes's `~/.hermes/config.yaml` and the tier gate's `~/.local/state/bud/hermes-tiers.json` (both mode 0600) with `adapter/hermes` from the registry and the same fragments (see "Hermes" below), and after the switch locks every synced skill in Hermes. The fragments' bud block becomes both runtimes' MCP servers (see "Tools and MCP servers" below), and each server left out is named in the report's notes.
9. Plans each file against the disk and the ledger, builds the link set, then switches `content/current` and `active` (each a new link renamed over the old, so readers see one or the other; if `active` can't follow, `current` is put back), writes and deletes the files, saves the ledger, and removes unused `active.d` dirs.

Before switching, bud verifies that the content directory and every active link target exist and that the built link set is intact. If either link switch fails, it restores the prior links, or removes a newly created `current` link.

## Tools and MCP servers

The tool map (Suite D72-D79, D81, D85-D87, JGA-358) reaches each runtime through its env fragments. A fragment's top level is OpenCode's own config, so the built-in tool settings sit there as OpenCode reads them: `websearch` off on dev machines and on Exa on the server, permission rules for each MCP server's tools, and so on. The `bud` key is bud's, never passed through to a runtime. `internal/adapter/env` takes it out of the merged fragments and resolves it, and each adapter renders it in its runtime's shape.

```jsonc
{
  "bud": {
    "gateway": "http://<tailnet IP>:8790",   // the MCP gateway; the overlay sets it
    "privacy": "P1",                          // the tier this device's sessions send it
    "mcp": {
      "linear": { "gateway": true, "privacy": ["P1", "P2"] },
      "context7": { "url": "https://mcp.context7.com/mcp", "privacy": ["P1", "P2"] },
      "serena": { "command": ["uvx", "..."], "privacy": ["P0", "P1", "P2"] },
      "memory": { "privacy": ["P0", "P1", "P2"] }  // url from the overlay
    },
    "hermes": { "web": { "keyless_fallback": false } }  // laid over config.yaml as it is
  }
}
```

| Key | What it is |
| --- | --- |
| `gateway` | The MCP gateway's base URL. Its host must be a loopback or tailnet IP address with a port, since the gateway refuses any `Host` that isn't one (its DNS-rebinding guard), so a MagicDNS name would never get through. Empty leaves every gateway server out. |
| `privacy` | The tier sent in `X-Bud-Privacy` to gateway servers. Empty sends no header, which the gateway takes as P0, so only P0-safe servers would answer. |
| `mcp.<name>` | One server. Names are lowercase letters, digits and `-`; OpenCode names its tools `<name>_<tool>`, which the permission rules match. |
| `.gateway` | `true` reaches it at `<gateway>/<name>/mcp` with the header. It takes no headers or env, since the gateway adds the key. |
| `.url`, `.headers` | A remote server reached directly. |
| `.command`, `.env` | A local server over stdio, its arguments after it. |
| `.privacy` | The session tiers that may use it. The private agent is denied every server that leaves out `P0`. |
| `.disabled` | Leaves it out, such as to turn off a public server from the overlay. |
| `hermes` | Laid over Hermes's config as it is: `platform_toolsets`, `web`, `secrets.onepassword.env`, and extras for a server's entry under `mcp_servers.<name>` (such as `trust: untrusted`). It may not set what bud renders for the registry and the D48 changes (see "Hermes" below). |
| `tiers` | Hermes's tools by action tier for the tier gate: a tool name or pattern (MCP tools are `mcp__<server>__<tool>`) to `allow`, `ask` or `deny`, with `*` for the rest. A later fragment sets a key over an earlier one's. Needs content format 3. |

A server needs exactly one of `gateway`, `url` and `command`, and one with none is left out until a later fragment sets one; that's how the public fragments list Bud memory while only the overlay knows its address. Fragments merge before the block is read, key by key, so the overlay's `env/personal.jsonc` fills in `gateway`, `mcp.memory.url` or a whole server of its own. A server's `privacy` and `command` are replaced by a later fragment rather than appended to, so the overlay can narrow a server's tiers (taking P0 away) or swap its executable. Values a runtime fills from its environment are `{env:VAR}`, which the Hermes adapter writes as Hermes's `${VAR}`; a `url` may hold them, and the rest of it is still checked. A fragment's own `mcp.servers.<name>` (OpenCode) or `hermes.mcp_servers.<name>` adds fields to a server the block declares (such as `codemode` or `trust`), and is dropped when the server was left out. It may not name a server the block doesn't declare, since that server would have no tiers for the private agent to deny, nor set the transport (`type`, `url`, `headers`, `command`, `environment`; for Hermes `url`, `headers`, `command`, `args`, `env`, `transport`), which comes from the block. A fragment that sets `agents.private` is refused too, so nothing changes the private agent's model or adds a rule after its denies.

The P0 tool rule (Suite D81) is the OpenCode adapter's `private` agent: a primary agent on the first model of the coding role (then the fallback role) allowed P0, whose rules deny `webfetch`, `websearch`, `experimental_image_generate`, `subagent` (a subagent runs under its own rules, not its parent's) and `<name>_*` for every server not allowed P0. OpenCode applies agent rules after every global one, so no fragment's allow outranks them. With no P0 model there's no agent, and a note says so.

What each example profile gets, from `content/env/coding.jsonc` and `content/env/server.jsonc`:

| Profile | OpenCode | Hermes |
| --- | --- | --- |
| `dev-machine` | `websearch` off; Linear, Exa and the Bud ops MCP through the gateway; Context7, Bud memory, Serena (its read-only symbol tools only), cocoindex-code and Playwright (opening a URL asks, since a rule for an MCP tool can't see the URL); Linear reads run alone and writes ask, the ops tools run alone | none |
| `server` | `websearch` on Exa; Linear through the gateway, GitHub (GitHub's hosted server at `api.githubcopilot.com/mcp/`, with `GITHUB_PERSONAL_ACCESS_TOKEN` from `op run` as the bearer token, so nothing is installed for it; reads and opening PRs run alone, the rest asks), Context7, Bud memory and the Bud ops MCP on loopback | the same servers, with Linear and GitHub `trust: untrusted`; the tool map's toolsets on `cli`, `signal`, `api_server` and `cron` (no `clarify` where nobody can answer); `web` searching through SearXNG and extracting through Exa, with `keyless_fallback: false` |
| `model-box` | none | none |

Golden files for each profile, synced with the shipped content and an overlay's personal fragment, are in `internal/syncer/testdata/profiles/`; `go test ./internal/syncer -run Golden -update` rewrites them.

Not done yet: a session's tier is fixed per device (the header can't follow the session until the router, phase 3, knows it), so the gateway's P0 refusal guards a client that wasn't configured, and the private agent's rules guard P0 sessions. One exception: Hermes sends `P0` instead of the device's tier to a gateway server that the device's tier may not use and P0 may (the calendar, Suite D95). That holds because Hermes's config lists only providers and models allowed P0, so neither its own picks nor `/model` can reach a cloud model; the planning OpenCode keeps the device's tier, so the gateway still refuses it. The planning OpenCode's edit scope (D49) comes with the planning OpenCode's install, and `SEARXNG_URL` with SearXNG (JGA-301); `EXA_API_KEY` and `GITHUB_PERSONAL_ACCESS_TOKEN` reach Hermes through the overlay's `bud.hermes.secrets.onepassword.env`. With the token unset, Hermes doesn't connect GitHub and its log names the variable. The planning OpenCode doesn't get either yet: `opencode.env` holds only the registry's provider keys, so routing fragment secrets into it comes with the planning OpenCode's install. Content needs format 2 for the bud block, since an older bud would pass it into `opencode.jsonc`, and format 3 for `tiers`.

## Hermes

`adapter/hermes` renders Hermes's config for Hermes v0.23 (Suite D46, D48), checked against Hermes's own `hermes_cli/config_defaults.py` and docs at tag `v0.23.2`. YAML comes from a small emitter in the package (D1): strings double-quoted, keys sorted, lists of scalars or maps.

**Two profiles (Suite D95).** Hermes runs as two profiles, each a Hermes home of its own with its own memory, sessions and skills. The default profile, `~/.hermes/config.yaml`, is private: it owns Signal, the calendar and reminders, and its config lists only models allowed P0, so no chat in it can reach a cloud model, by fallback or by `/model`. The cloud profile, `~/.hermes/profiles/cloud/config.yaml`, which Jason reaches with `hermes -p cloud`, lists every chat model and none of the servers only private sessions may use: those P0 may use and neither P1 nor P2 may, whatever the device's own tier. It sets `gateway.standalone: true`, since Hermes otherwise serves every profile from the default profile's gateway process, and the cloud profile has no bot to run. Hermes's hook context doesn't say which model a turn uses before a tool runs, and its `pre_llm_call` hook fails open, so tracking which chats have read P0 data couldn't be made safe; a profile that has no cloud model at all is safe by construction. Both profiles get the same D48 settings, gate and synced skills.

**Models.** In the private profile, each registry provider with a chat model allowed P0 becomes `providers.<id>` (`api` is the endpoint, `transport: chat_completions`, `key_env`, and `models.<name>.context_length`), listing only those models. A provider with none is left out whole, and so is its key, and a note names the models left out. `model` is the manager role's first model allowed P0, and `fallback_providers` its other P0 models, then the fallback role's. A registry whose manager and fallback roles have no P0 model is refused. A fragment that loads a left-out provider's key through `bud.hermes.secrets.onepassword.env` is refused. Hermes's built-in providers read their keys from its environment (`OPENCODE_GO_API_KEY` among them) whatever the config lists, so bud sync also refuses, before writing anything, a `~/.hermes/.env` that sets a left-out provider's key variable or any of the built-in providers' key variables (`hermes.BuiltinKeys`, taken from Hermes v0.23.2's provider plugins and checked again with each pin) that no listed provider uses, byte order mark or not, and names `~/.hermes/profiles/cloud/.env` as its place. The cloud profile lists every provider with a chat model and all their chat models; its `model` and fallbacks are the manager role's members in order, then the fallback role's, whatever their tier. Naming the provider also stops Hermes's auxiliary tasks (compression, titles, vision) from guessing another logged-in provider: they use the main model. A key that is `{env:VAR}` becomes `key_env: VAR`, which Hermes reads from its `.env`; an `op://` key becomes `BUD_<PROVIDER>_API_KEY` in `secrets.onepassword.env`, which Hermes resolves with `op read` at start, so no key is in the file.

**The D48 changes**, which no fragment may set:

| Setting | Value | Why |
| --- | --- | --- |
| `approvals` | `mode: manual`, `timeout: 3600`, `cron_mode`, `single_query_mode` and `unattended_mode: deny`, `approvers: []`, `deny`: every shell rule whose effect is `deny` | No model approves for Jason (Hermes's default, `smart`, lets one), an approval waits an hour and is then denied (D42), and only the requester answers. The never tier is refused by Hermes itself as well, even under `--yolo`. |
| `command_allowlist` | `[]` | "Always approve" answers would become standing approvals; pre-approval only covers the named action (D40). The next sync clears any. |
| `hooks.pre_tool_call` | `<bin>/bud hermes-gate ~/.local/state/bud/hermes-tiers.json`, `timeout: 10`, `fail_closed: true`; `hooks_auto_accept: true` | The tier gate, below. Fail-closed, so a missing or crashing gate blocks every tool. The gateway can't answer Hermes's first-use consent prompt, and bud owns `hooks`, so the only hook auto-accepted is the gate. |
| `skills.write_approval`, `memory.write_approval` | `true` | Every skill and memory write the agent makes is staged (`/skills pending`, `/memory pending`) for Jason's review. |
| `skills.external_dirs` | `[]` | `hermes curator lock` takes only a skill in Hermes's own dir, so synced skills are copied there (see Locked skills) rather than loaded from bud's active set. |
| `auxiliary.background_review.enabled`, `curator.consolidate` | `false` | No post-turn self-review or LLM consolidation writes on its own. |
| `terminal.backend`, `terminal.docker_mount_cwd_to_workspace` | `docker`, `false` | The agent's commands and file tools run in a container, so they can't read Hermes's `.env`, the 1Password token or bud's files. |
| `updates.check` | `false` | Bud bumps Hermes's pin (D12); Hermes doesn't offer its own updates. |
| `plugins.enabled` | `[bud-signal]` | No plugin code runs but bud's: the one that lets Signal reactions and codes answer approvals (D30). |
| `platforms.signal` | `enabled: true`, `extra.http_url: http://127.0.0.1:8793`, `unauthorized_dm_behavior: ignore` | Signal on signal-cli's loopback daemon; a stranger gets no reply and no pairing code (D30). The numbers come from `.env` or the overlay. |
| `_config_version` | `53` | The schema version of the pinned Hermes (v0.23.2). Without it Hermes reads the file as v0 and asks to migrate it; raise it with the pin. |

**The tier gate.** Both profiles run the same gate on the same tiers file. `bud hermes-gate <tiers file>` reads one call from stdin and answers in Hermes's hook protocol: `{}` runs it alone, `{"action": "approve"}` sends it to Hermes's approval gate (Jason answers in the chat it came from; on Signal by reaction or code, see [signal.md](signal.md)), and `{"action": "block"}` refuses it. It never sends a `rule_key`, so an approval covers one call. A `terminal` command is split at `;`, `&`, `|`, `&&`, `||` and new lines outside quotes, each part is matched against the fragments' `shell` rules (OpenCode's globs, last match wins, no match asks), and the strictest part wins; a line with `$( )`, backticks, `( )`, `<` or `>` asks at least, since the parts don't show what it runs. Hermes's manual mode still asks about any command it deems dangerous, so the gate only ever adds approvals. Every other tool is tiered by the bud block's `tiers`: its exact name, then the longest pattern that matches (the strictest of equally long ones), then `*`, and with none of those it asks. An unreadable tiers file or call is refused, saying to run `bud sync`. Before any of that, a tool on a server only private sessions may use (the tiers file's `private` patterns, `mcp__calendar__*`) is refused unless the call's `profile` is `default`, the private profile; the cloud profile has no such server, so this is a second guard (Suite D95). `content/env/server.jsonc` holds the picks: reads and sandboxed tools run alone, memory and skill writes run alone because Hermes stages them, cron jobs and the browser ask (one tool both reads and writes each), the browser vault and computer use are refused, and MCP tools run alone because Hermes asks before any tool on an untrusted server that isn't marked read-only.

**Locked skills.** On a `server` device, bud sync copies each synced skill's regular files (not its links) into `~/.hermes/skills/<id>/` (`paths.HermesSkills`, kind `hermes-skill` in the ledger), and the same into the cloud profile's `~/.hermes/profiles/cloud/skills/<id>/`, so the manifest rule applies: a skill the profile drops is removed and a hand-edited copy is skipped and named. Whether bud may write a skill is decided for the whole skill (`hermesSkill`): a skill dir holding files bud doesn't track (Hermes's own, or Jason's) and a path through a link (`~/.hermes/skills`, the skill dir or a subdir) each leave the skill alone, unlocked, with a note, while an empty dir left by a removed skill is reused. Copies keep their source's permission bits. After the files are written, bud runs `hermes curator lock <id>` (and `hermes -p cloud curator lock <id>`) for every skill it copied (idempotent, so new skills are covered too). Hermes keeps the lock in its own `skills/.usage.json`; while it holds, `skill_manage`, `write_file`, `patch` and `terminal` refuse to change the skill. Without `hermes` on `PATH`, or when a lock fails, the report's notes say so.

**Signal and bud-signal.** On a `server` device bud sync also installs `bud-signal`, the Hermes plugin that lets a Signal reaction or code answer an approval, from the binary into `~/.hermes/plugins/bud-signal/` (`hermesPlugin`, kind `hermes-plugin`, layer `binary`; decided for the whole plugin the way a skill is), and renders `plugins.enabled: [bud-signal]` and `platforms.signal` (signal-cli's loopback URL, unknown senders ignored). Both keys are bud's. A plugin dir bud can't write leaves the plugin out of `plugins.enabled`, so Hermes's own Signal adapter, with typed `/approve`, is used. The details are in [signal.md](signal.md). Only the private profile gets them: the cloud profile renders no `platforms.signal` and enables no plugin (Suite D95).

Not built here: memory shared between the profiles. Each has Hermes's built-in memory today, so nothing crosses; when Bud memory (D51, phase 5) serves both, it has to keep what the private profile writes away from the cloud one. Cron jobs run only in the private profile, since the cloud profile has no gateway process to tick them. Also not built: the approval service (D28, JGA-348), which would send every approval to the Tauri app first and Signal after 5 minutes; Hermes's memory provider over Bud's SQLite and its 300 ms recall check (D51, phase 5), so Hermes uses its built-in memory with writes staged until then, which is D51's fallback; the sandbox image is Hermes's default `nousresearch/hermes-sandbox:desktop`, a moving tag, until a digest is pinned.

## The manifest rule

| On disk | Ledger | What happens |
| --- | --- | --- |
| missing | any | written |
| what bud would write | has it | left, recorded |
| what bud would write | doesn't | left, never recorded, so a later release can't overwrite or delete it |
| what bud last wrote | has it | rewritten |
| anything else | has it | skipped and named as a hand edit; the ledger keeps bud's hash, so `bud doctor` keeps flagging it |
| anything else | doesn't | skipped and named as not bud's; never recorded |

A file the ledger has that the sync no longer wants is deleted when it's still what bud wrote, and otherwise left, named and dropped from the ledger. A step that fails leaves the file's old ledger entry in place, and the ledger is saved either way, so it always matches what's on disk.

## The ledger

`format`, `device`, `channel`, `content` (the version `current` points at), `rolled_back` (versions a rollback left), `overlay` (`repo` and `commit`), `active` (the `active.d` dir) and `files` (each written file's `path`, `sha256`, `kind` and `layer`: `public`, `overlay`, or `rendered` for one built from both). It holds no times, so a sync that changes nothing writes nothing.

## Rollback

`bud sync --rollback` takes the newest version dir on disk older than the ledger's, and runs steps 6 to 9 on it with the overlay as it is on disk (it isn't fetched). The version it left goes into `rolled_back`. A later sync stays put while the channel offers anything no newer than a rolled-back version, so going .1 to .3, rolling back, then being offered .2 and .3 again never brings .3 back; an offer newer than all of them is taken and clears the list. Old version dirs aren't pruned yet.

## Dry run

`--dry-run` does steps 1 to 8 without changing anything: the overlay isn't fetched (the clone on disk is used, or none), and a new bundle is downloaded, verified and unpacked into a temp dir that's thrown away. It reports the same changes a real sync would make.

## What bud doctor checks

`internal/doctor/sync.go` (build plan task 1.9, Suite D46) adds these checks once config `device` is set; a device with none gets none of them. They only read, and of them only the `--net` ones go online. They hold the sync lock while they read (`syncer.TryLock`), because a sync switches the links before it saves the ledger; while a sync runs they are skipped with one warning, so a sync in progress never looks broken (which could roll back a `bud update`).

| Check | OK | Warn | Fail |
| --- | --- | --- | --- |
| device profile | found and valid (overlay's `profiles/` first, `syncer.ProfileDirs`) | | not found or invalid |
| sync ledger | `current` points at the ledger's version, its dir is there, every file matches its hash (`Ledger.Drift`) | never synced; a file missing or changed by hand | unreadable ledger; `current` gone or pointing elsewhere; the version dir gone |
| registry | the content's, with the overlay's over it, validates (`syncer.LoadRegistry`) | | doesn't validate |
| content layers | names every item the profile takes and its layer: the overlay's (marking each that replaces a public one), the public ones, and the files the sync rendered (`overlay.Layer` over the overlay on disk; a device with no OpenCode lists only skills and plugins) | the overlay is set but not cloned, or the content can't be layered | |
| synced config | `syncer.Check` finds nothing to change (that includes Hermes's `config.yaml` on a `server` device, so a hand edit that turns a tool setting back on, like Hermes's keyless search fallback, is named) | a sync would write or delete something; a file bud would write isn't bud's | rendering fails; a skill or plugin link in the active set is gone or points elsewhere |
| content channel (`--net`) | the newest on the channel, or held there by a rollback | newer content is out; nothing offered; the channel offers an older version; the host can't be reached | |
| model endpoints (`--net`) | every provider a role uses answers `GET <endpoint>/models` | one doesn't answer, or its model list lacks a name the registry uses | |

The ledger check stops there when it fails or the device hasn't synced, the layers check needs a profile, and the render check needs a profile and registry that validate. `syncer.Check` is a dry run of steps 6 to 8 on the version in use with the overlay as it is on disk, so it catches what a hand edit can't: an overlay pull that changed what's rendered, or a `bud update` whose adapter renders differently. It leaves out files the ledger check already named. Drift only warns, since `bud update` rolls back on a check that newly fails, and a hand edit is never the new release's fault; a broken render does fail, since a new release can cause that.

Endpoints get a key only when theirs is an `{env:VAR}` that's set; an `op://` key is never read (Suite D7), so those providers show only that they answer (any HTTP status counts), and a model list is checked only when one comes back (an empty list lacks every name). Down only warns, since the Qwen box sleeps when idle (Suite D26). Tests swap `syncCheck`, `fetchChannel` and `netClient`.

## Not done yet

- Whether OpenCode reloads its config when `current` moves is unchecked; if not, a sync will have to restart the harness.
- Doctor doesn't check OpenCode's version yet: the pinned OpenCode install (the rest of task 1.7) isn't built, so there's no pin to check against.
