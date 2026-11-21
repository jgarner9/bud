# The router

`bud router` (`internal/router`, `internal/cli/router.go`) is the home server's one OpenAI-compatible endpoint over the model registry's providers (Suite D70, D71, D98). Its unit is in `server/router/`. It routes, proxies, enforces privacy tiers, tracks spend and caps the month's top-up. Not built yet: the classifier, ranking, and Lemonade behind it (Suite D98).

## Endpoints

- `GET /v1/models` lists the registry model ids the request's tier allows (`owned_by` is the provider) and one `bud/<role>` entry per role with an eligible member (`owned_by` is `bud`).
- `POST /v1/chat/completions`, `/v1/completions` and `/v1/embeddings` forward to the model's provider.

## Resolving a request

The body is read up to 10 MiB (413 past it) and decoded into `map[string]json.RawMessage`, only to find `model`; every other field is re-encoded as it came, without HTML escaping. `model` is a registry model id, or `bud/<role>`, which is the pin if it allows the request's tier, else the first `Role.Members()` entry that does. It's replaced by the Model's `name`, and the request goes to the provider's `endpoint` with the same path after `/v1` (`<endpoint>/chat/completions`). An unknown model is a 404 that names it and points at `GET /v1/models`; a body that isn't a JSON object or has no model is a 400.

## Privacy tiers

The tier comes from `X-Bud-Privacy` (`registry.RequestTier`, shared with the MCP gateway): P0, P1 or P2, and a missing header is P0, so an unconfigured client fails closed (Suite D23, D81). Any other value is a 400 naming the valid ones. A model serves a request only if the tier is in its `privacy` list (`Model.Allows`); otherwise an explicit model id, or a role with no eligible member, is a 403 that points at `GET /v1/models` with that header. The header is stripped before forwarding, and every proxied response carries `X-Bud-Model: <registry model id>`, replacing any upstream header of that name. As at the gateway, the header is a guard against private text leaking by accident, not authentication: a caller on the tailnet can send any tier, and the tailnet is the authentication (Suite D72). Devices get their header from `bud sync`.

## Keys and headers

A provider's key comes from the environment variable the OpenCode adapter names for it (`registry.KeyVar`: the `{env:VAR}` it names, or `BUD_<PROVIDER>_API_KEY` for an `op://` reference). `New` fails when a keyed provider's variable is empty, so a unit started outside `op run` stops at once. The key goes out as `Authorization: Bearer`; the client's `Authorization` and `Cookie` are dropped, as are hop-by-hop headers and `Accept-Encoding`. Without the client's `Accept-Encoding` the transport asks for gzip itself and decodes the body, so the spend meter always reads plain JSON. Key values are never logged or returned.

## Proxying and errors

`httputil.ReverseProxy` with `FlushInterval: -1` passes SSE through as it arrives. Errors are OpenAI-shaped (`{"error":{"message":...,"type":...}}`). An unreachable provider is a 502 with a generic message; the transport error is not echoed, since it names the upstream's address (as in the MCP gateway).

## Listening

`--listen` (default `127.0.0.1:8795`) must be a loopback or tailnet IP, checked by `internal/tailnet.Inside`, which the MCP gateway uses too (Suite D72); `localhost` is refused. `--registry` reads one file; without it the router loads the synced content's registry with the overlay's merged over it, as `bud doctor` does.

## Spend and the top-up cap

The router keeps a spend ledger (`internal/router/spend.go`, Suite D24, D64): for the current UTC calendar month, the dollars spent on each registry model and the month's top-up total. It is a JSON file, `router-spend.json` in bud's state dir unless `--spend` names another, written atomically with mode 0600 after every charge. A missing file is an empty ledger. A corrupt one stops the router at start with an error naming the file, which you move aside. When the month changes, the old month's numbers stop counting and only the new month is kept.

A request's cost comes from the `usage` object in the response: prompt tokens times the model's input price plus completion tokens times its output price, per million tokens. C0 models are never charged. The part of a charge that goes past a C1 model's monthly allowance, and all of a C2 model's charge, is added to the top-up total. A model with no price, or a response with no usage, is charged nothing, and the router logs one line per model per process saying so. Nothing about it reaches the client.

The router reads the response body as it passes through, without delaying or changing it. A JSON body is buffered up to 10 MiB and charged when it ends; past that, only its last 64 KiB is kept, and the last `usage` object in it is charged. An event stream is scanned for a `data:` chunk with a non-null `usage`, and charged at the end. So that a stream carries usage, the router adds `"stream_options":{"include_usage":true}` to a `POST /v1/chat/completions` body that has `"stream": true` and no `stream_options` of its own; a caller's own `stream_options` is left alone. The client then sees one extra final chunk with empty `choices`, which the OpenAI spec defines.

For a `bud/<role>` request, the router takes the first eligible member (the pin, then the pool order) that spends no top-up, meaning a C0 model or a C1 model with allowance left. Only if there is none does it take the first eligible member that would spend top-up, and only while the month's top-up is under `--topup-cap`. An explicit model id is served when its tier allows it, unless serving it would spend top-up and the cap is reached. At the cap the router answers 402 with an `insufficient_quota` error saying that spending past the cap needs approval, which isn't built yet, and that C0 and in-allowance models still work. A cap of 0 means no top-up ever. `--topup-cap` must be a finite amount of 0 or more.

Top-up requests run one at a time: a request that would spend top-up waits until the one before it has been charged, then is checked against the cap again, so requests admitted together can't all land past it. The cap can still be passed by the one request that crosses it. If a ledger write fails, the charge is kept in memory but no top-up is admitted until a later write works, since a charge that isn't on disk would be lost on restart; those requests get a 503 saying the ledger couldn't be saved. C0 and in-allowance requests keep working. `GET /v1/models` keeps listing every model the tier allows, so nothing disappears silently.

Every proxied response carries `X-Bud-Spend`, such as `topup=1.23/10.00`: the month's top-up so far, before this request is charged, over the cap. An upstream header of that name is replaced.
