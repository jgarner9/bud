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

The content release workflow (task 1.10) must publish exactly these. Content releases have to be GitHub prereleases, or one of them becomes the repo's "latest" release and `install.sh`'s `releases/latest/download` stops finding the code archives.

## What a sync does

1. Takes the device's sync lock (an `flock` on `~/.local/state/bud/sync.lock`, dropped when the process exits), so a timer and a hand run never interleave; a second run fails at once. A dry run takes no lock. Then reads the ledger, `~/.local/state/bud/sync.json`. Missing means this device has never synced.
2. Fast-forwards the overlay with `overlay.Fetch` (never forced; it stops on local changes or divergence), then finds the profile.
3. Downloads `channel.json` and checks its signature. The offered version is the profile's channel's `content`.
4. Picks the version to use. The installed one again when it's what's offered; the installed one when the offer is a version `--rollback` left; otherwise the offer, which `content.CheckNewer` must find newer than the installed one, so an old signed bundle can't roll a device back.
5. Unless that version's dir is already on disk, downloads its `content.json`, checks the signature and that it names that version, and unpacks the bundle into `~/.local/share/bud/content/<version>/` with `content.Unpack` (every file checked against the manifest; the dir is complete or absent). Deleting the dir makes the next sync fetch it again.
6. Layers the overlay over that dir with `overlay.Layer` and keeps what the profile selects. The registry is the content's `env/registry.jsonc` with the overlay's merged over it, validated; it is never treated as an env fragment.
7. Builds the active link set. `~/.local/share/bud/active` links to `active.d/<hash>`, which holds `skills/<id>` and `plugins/<id>` links into the content version or the overlay. The hash is of the links themselves, so an unchanged selection keeps the same dir.
8. If the profile runs OpenCode (`harness` or `server`), renders `opencode.jsonc` with `adapter/opencode`: skills from `active/skills`, instructions from `content/current/AGENTS.md` then the overlay's, plugins from `active/plugins/<id>`, env fragments public first and the overlay's last. Agents and commands are copied to `~/.config/opencode/{agents,commands}/<id>.md`. Provider keys that are `op://` references go in `~/.local/state/bud/opencode.env` (mode 0600, references only) for `op run --env-file`.
9. Plans each file against the disk and the ledger, builds the link set, then switches `content/current` and `active` (each a new link renamed over the old, so readers see one or the other; if `active` can't follow, `current` is put back), writes and deletes the files, saves the ledger, and removes unused `active.d` dirs.

Before switching, bud verifies that the content directory and every active link target exist and that the built link set is intact. If either link switch fails, it restores the prior links, or removes a newly created `current` link.

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

## Not done yet

- Whether OpenCode reloads its config when `current` moves is unchecked; if not, a sync will have to restart the harness.
- `bud doctor`'s drift checks against the ledger are task 1.9.
