# Content bundles

`internal/content` builds and checks the signed bundle that carries `content/` to every device running OpenCode (Suite D57, D60, D61). `bud sync` (build plan task 1.8) is its caller on a device, and CI's content release (task 1.10) is its caller at the other end.

## What a content release is

Three release assets plus two signatures:

| File | What it is |
| --- | --- |
| `content-<version>.tar.gz` | The files of `content/`, at their paths under it. `README.md` is left out. Anything at the top level other than `AGENTS.md`, `skills/`, `agents/`, `commands/`, `plugins/` or `env/` fails the pack, so a stray file gets noticed instead of shipped. |
| `content.json` and `content.json.sig` | The manifest: the version, and each file's path, kind, size and sha256, sorted by path. It's signed under the `bud-content` namespace. |
| `channel.json` and `channel.json.sig` | The newest content and code version on each channel (`stable`, `edge`). It's signed under `bud-channel`. |
| `channel-recovery.tar.gz` | The exact signed channel pair, kept as one release asset so a retry can restore either conventional channel asset after an interrupted upload. |

One key signs everything Bud ships: the release key, `updater.ReleaseKeys`. The SSH signature namespace keeps the uses apart. A signature made for a code release (`bud-release`), a manifest or a channel file verifies only as that kind, so a valid signature on one can never be replayed as another.

## The archive is deterministic

`Pack` walks `content/` in lexical order and writes PAX tar entries with mode 0644 and no times or owners, so the same tree packs to the same bytes every time. Only regular files ship. A symlink or device fails the pack.

## How a device checks a bundle

1. `VerifyManifest` checks `content.json`'s signature against the trusted keys first, then parses it strictly. Unknown fields fail, and so does a version that isn't in the canonical `YYYY.MM.DD.N` form.
   The manifest also carries the content format the bundle needs, and a bud refuses one newer than its own `content.Format`, saying to run `bud update` first, before anything is fetched or unpacked. See "The content format" below.
2. `CheckNewer` refuses a version that isn't newer than the installed one, so an old signed bundle can't be replayed to roll a device back. Going back is `bud sync --rollback`, which uses a version still on disk.
3. `Unpack` extracts into a temp dir beside the target, then renames it into place, so the version dir is either complete and verified or absent. It refuses anything that isn't a regular file, any path that isn't plain and relative (`..`, absolute or backslashed), any entry the manifest doesn't list, a duplicate, a size or sha256 mismatch, and any listed file the bundle is missing. It never unpacks over an existing version dir. The manifest is written into the dir as the record of what it holds.

## The content format

`content.Format` is the newest content format this bud reads, and `Pack` writes it into `content.json` as `format`. Bump it in the same PR as content that needs new code to read it, such as a new field in `env/registry.jsonc` that an older bud's strict decoder would reject. A device whose bud is older then stops at the manifest with "run bud update" and keeps the content it has, instead of failing partway through on the file it can't parse.

Format 1 is written as no field at all (`omitempty`, and `Pack` sets it only above 1), so manifests stay byte-identical, and readable by buds built before the field existed, until the first bump. Those older buds parse strictly, so once the format is 2 they refuse the manifest too, with an unknown-field error rather than the update message. Either way nothing changes on the device.

## Versions

Content versions are `YYYY.MM.DD.N`: the day CI cut it, and which cut of that day it was, starting at 1. Each version has exactly one spelling, with month and day zero-padded and N not, so two strings never name the same version. Versions compare part by part as numbers.

## Building one by hand

```sh
bud content pack --version 2026.11.15.1 --out dist content
```

This writes `dist/content-2026.11.15.1.tar.gz` and `dist/content.json`. Signing and publishing them is the content workflow's job, through `scripts/content-release.sh` (see [release.md](release.md)), which also uses `bud content channel` to write `channel.json` from the release tags (`content.NewChannel`; stable is never ahead of edge) and `bud content offered` to read what a published one offers once its signature verifies. Stable promotion is restricted to what the current signed edge channel offers. Where the release assets have to live for `bud sync` to find them is in [sync.md](sync.md).
