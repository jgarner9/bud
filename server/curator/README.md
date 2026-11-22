# server/curator/

The KB curator (Suite D9, D123, D124): `bud curator run` behind a nightly systemd timer. Each run reads the approved records in working memory through the memory service's `export` action, asks a local model which durable facts each new record states, and promotes a fact into bud-kb once two independent records agree on it (a source counts once a day). A fact that contradicts the page goes to the review queue unless two records newer than all of the page's evidence back it. Then it flags sections nothing has referenced for 6 months, and archives them at 12 months, or queues a refresh check for evergreen ones. Every change is its own commit by "Bud curator", with the evidence in the message, and the run pushes at the end. How it works is in [docs/internals/kb.md](https://github.com/jgarner9/bud/blob/main/docs/internals/kb.md).

| File | What it is |
| --- | --- |
| `bud-curator.service` | The oneshot unit. It runs `bud curator run` under `op run`, so the memory token reaches it as an environment variable. |
| `bud-curator.timer` | Starts it nightly at 03:30, catching up a missed night. |
| `curator.env.example` | The `op://` reference `op run` resolves, with a placeholder vault path. |

## What stays where

- **Local models only.** The curator calls the registry's `curator` role on providers marked `local: true`, never the router, so a record can't reach a cloud model through a fallback. With the Qwen box asleep, extraction waits for the next night; the staleness pass, which needs no model, still runs.
- **No P0 in the KB while its remote is GitHub.** The default `--tiers P1,P2` leaves P0 records out, and the curator token's policy should allow only those tiers too. Once bud-kb's remote lives on the home server, add P0 to both.
- **State outside the KB.** `~/.local/state/bud/curator.json` remembers which records were read and which evidence was used, so reverting a curator commit sticks: the fact comes back only when a new record states it.

## Answering it

The review queue is on the KB site's home page. Clear an item with `bud curator resolve <id> keep|take|confirm` on the home server (keep the page, take memory's value, or confirm an evergreen section is still right), or edit the page yourself and commit: the next run drops contradictions the edit settled. `bud curator promote <record id>` promotes one record's facts on the next run without waiting for a second record. `bud curator restore <projects|systems>/<page>#<section>` brings an archived section back, as does `git revert` of its archive commit.

## Setting it up

The memory service (`server/memory/`), the bud-kb clone and the KB site (`server/hugo/`) come first.

1. Add a curator token to the memory service's clients policy with `"actions": ["export"]` and `"tiers": ["P1", "P2"]`, and put the token in the Bud vault.
2. Put the env file at `~/.config/bud-server/curator.env`, mode 0600, with the token's `op://` path.
3. Check that the registry's `curator` role names a model on a local provider (`qwen-35b` in the public registry).
4. Run it once by hand: `op run --env-file ~/.config/bud-server/curator.env -- bud curator run`. It prints what it did; `git -C ~/.local/share/bud/kb log` shows the commits.
5. Install and start the timer with the commands at the top of `bud-curator.service`, as the user the memory service runs as. `journalctl --user -u bud-curator` shows each night's summary.
