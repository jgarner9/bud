# Release tooling

Bootstrap, the nightly build and release signing. The release flow itself is in CLAUDE.md.

**Bootstrap.** `bud bootstrap [--config] user@host` (`internal/cli/bootstrap.go`) runs the install.sh one-liner over `ssh -t` with `--profile` (default minimal), `--yes` and `--version` of this bud (`releaseVersion`); `--config` pipes `config.json` into the remote state dir first. Tests swap `runSSH`.

**Nightly.** `.github/workflows/nightly.yml` runs daily (and by hand): when main moved since the `nightly` tag, GoReleaser builds a snapshot named `X.Y.Z-nightly.YYYYMMDDHHMM` (one patch past the newest stable tag, via `NIGHTLY_VERSION` in `.goreleaser.yaml`'s snapshot template) and the `nightly` prerelease is deleted and recreated with the same archive names, so `releases/latest` never sees it. `.goreleaser.yaml`'s `git.ignore_tags` leaves the `nightly` tag out when GoReleaser picks the previous release, so a release's changelog starts at the last `vX.Y.Z`.

**Signing.** Rotating the key is `scripts/rotate-release-key.sh start|finish` (D64), run by hand, never by you: it creates a real key, writes to 1Password and sets the Actions secret. `just rotate-test` covers it with fakes. The release workflow runs `scripts/sign-release.sh` only when the `BUD_RELEASE_KEY` Actions secret is set; it signs `dist/checksums.txt` under namespace `bud-release`, refuses if the key isn't one of install.sh's `RELEASE_KEYS`, and uploads `checksums.txt.sig`.
