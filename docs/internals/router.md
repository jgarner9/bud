# The router

`bud router` (`internal/router`, `internal/cli/router.go`) is the home server's one OpenAI-compatible endpoint over the model registry's providers (Suite D70, D71, D98). Its unit is in `server/router/`. It routes, proxies and enforces privacy tiers. Not built yet: spend caps, allowance tracking, the classifier, ranking, and Lemonade behind it (Suite D98).

## Endpoints

- `GET /v1/models` lists the registry model ids the request's tier allows (`owned_by` is the provider) and one `bud/<role>` entry per role with an eligible member (`owned_by` is `bud`).
- `POST /v1/chat/completions`, `/v1/completions` and `/v1/embeddings` forward to the model's provider.

## Resolving a request

The body is read up to 10 MiB (413 past it) and decoded into `map[string]json.RawMessage`, only to find `model`; every other field is re-encoded as it came, without HTML escaping. `model` is a registry model id, or `bud/<role>`, which is the pin if it allows the request's tier, else the first `Role.Members()` entry that does. It's replaced by the Model's `name`, and the request goes to the provider's `endpoint` with the same path after `/v1` (`<endpoint>/chat/completions`). An unknown model is a 404 that names it and points at `GET /v1/models`; a body that isn't a JSON object or has no model is a 400.

## Privacy tiers

The tier comes from `X-Bud-Privacy` (`registry.RequestTier`, shared with the MCP gateway): P0, P1 or P2, and a missing header is P0, so an unconfigured client fails closed (Suite D23, D81). Any other value is a 400 naming the valid ones. A model serves a request only if the tier is in its `privacy` list (`Model.Allows`); otherwise an explicit model id, or a role with no eligible member, is a 403 that points at `GET /v1/models` with that header. The header is stripped before forwarding, and every proxied response carries `X-Bud-Model: <registry model id>`, replacing any upstream header of that name.

## Keys and headers

A provider's key comes from the environment variable the OpenCode adapter names for it (`registry.KeyVar`: the `{env:VAR}` it names, or `BUD_<PROVIDER>_API_KEY` for an `op://` reference). `New` fails when a keyed provider's variable is empty, so a unit started outside `op run` stops at once. The key goes out as `Authorization: Bearer`; the client's `Authorization` and `Cookie` are dropped, as are hop-by-hop headers. Key values are never logged or returned.

## Proxying and errors

`httputil.ReverseProxy` with `FlushInterval: -1` passes SSE through as it arrives. Errors are OpenAI-shaped (`{"error":{"message":...,"type":...}}`). An unreachable provider is a 502 with a generic message; the transport error is not echoed, since it names the upstream's address (as in the MCP gateway).

## Listening

`--listen` (default `127.0.0.1:8795`) must be a loopback or tailnet IP, checked by `internal/tailnet.Inside`, which the MCP gateway uses too (Suite D72); `localhost` is refused. `--registry` reads one file; without it the router loads the synced content's registry with the overlay's merged over it, as `bud doctor` does.
