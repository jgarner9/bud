# server/router/

The home server's model router (Suite D70, D71): `bud router` behind a systemd user unit. It is one OpenAI-compatible endpoint over every provider in the model registry, so a device points its client at the router and names a model (or `bud/<role>`) without holding any provider key. It enforces privacy tiers per request (the `X-Bud-Privacy` header, P0 when missing). The header guards against accidental leaks; it isn't authentication, since any tailnet caller can set it; spend caps and ranking are not built. How it works is in [docs/internals/router.md](https://github.com/jgarner9/bud/blob/main/docs/internals/router.md).

| File | What it is |
| --- | --- |
| `bud-router.service` | The user unit. It runs the router under `op run`, so provider keys reach it as environment variables. |
| `router.env.example` | The variables `op run` fills, with placeholder names. |

## Setting it up

The real env file comes from the overlay, since it names the Bud vault.

1. Run `bud sync` on the server, so it has the registry (or pass `--registry` in the unit).
2. Put the env file at `~/.config/bud-server/router.env`, with one `op://` line per keyed provider, and the service account's `OP_SERVICE_ACCOUNT_TOKEN=...` at `~/.config/bud-server/op-service-account.env`, both mode 0600. The router refuses to start when a keyed provider's variable is empty, and the error names it.
3. Set `--listen` in the unit to the server's tailnet IP (`tailscale ip -4`); the router refuses any other address than loopback or the tailnet (Suite D72).
4. Install and start the unit with the commands at the top of `bud-router.service`, then `loginctl enable-linger`.
5. Check it: `curl -s http://<listen>/v1/models` lists the models, and `curl -s http://<listen>/v1/chat/completions -H 'Content-Type: application/json' -H 'X-Bud-Privacy: P1' -d '{"model":"bud/coding","messages":[{"role":"user","content":"hi"}]}'` answers from the coding role's first model allowed P1 (without the header the request is P0, so only a local model can answer).
