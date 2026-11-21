# server/embed/

The home server's embedding server (JGA-294, Suite D52): llama.cpp's `llama-server` running Qwen3-Embedding-0.6B on the CPU. It is the registry's `embed` provider (port 8082, model `qwen3-embedding-0.6b`, 1,024 dims), so working memory embeds records and queries on the always-on server and never waits for the Qwen box. Memory text is P0, so the embedding role holds local models only and has no cloud fallback: the registry refuses a model in that role that isn't allowed P0, and a P0 model must sit on a local provider.

| File | What it is |
| --- | --- |
| `bud-embed.service` | The user unit. It checks the binary's and the model's SHA-256 before starting, and serves `POST /v1/embeddings` and `GET /v1/models`. |
| `embed.env.example` | The digests the unit checks, the tailnet address it listens on and the thread count. |
| `../../scripts/build-embed-server.sh` | Builds `llama-server` at the pinned llama.cpp tag (CPU only, static, no web UI) and fetches the pinned GGUF, checking both. |

## Pins

| What | Pin |
| --- | --- |
| llama.cpp | tag `b12200`, commit `c19043f67e52f1f1e00d797246e19643bb2f168e` (2026-11-07) |
| Model | `Qwen/Qwen3-Embedding-0.6B-GGUF` at revision `370f27d7550e0def9b39c1f16d3fbaa13aa67728`, file `Qwen3-Embedding-0.6B-Q8_0.gguf`, SHA-256 `06507c7b42688469c4e7298b0a1e16deff06caf291cf0a5b278c308249c3e439` (639 MB) |

The build script checks that the tag still points at the pinned commit and that the download matches the pinned digest. It builds from source rather than taking a release archive, because the source pin is a git commit and the binary then matches the server's CPU (`GGML_NATIVE=ON`). The binary's own digest depends on the machine that built it, so the script prints it for the env file.

## Setting it up

1. Install the build tools: git, cmake, a C++ compiler and curl.
2. Build into the unit's directory: `sh scripts/build-embed-server.sh ~/.local/lib/bud/embed`. It takes a few minutes and prints two `BUD_EMBED_*_SHA256=` lines.
3. Copy `embed.env.example` to `~/.config/bud-server/embed.env` (mode 0600), paste the two printed lines over the placeholders, and set `BUD_EMBED_THREADS` to the server's physical core count.
4. Set `BUD_EMBED_HOST` in the same file to the server's tailnet IP (`tailscale ip -4`). The server has no key of its own; the tailnet is the access control, as with the router (Suite D100).
5. Install and start the unit with the commands at the top of `bud-embed.service`, then `loginctl enable-linger` if the server doesn't already have it.
6. Point the overlay's registry at it: its `embed` provider's endpoint is `http://<tailnet IP>:8082/v1`. Then `bud sync`.
7. Check it: `bud doctor --net` shows `embeddings: qwen3-embedding-0.6b (1024 dims) answered`, and
   `curl -s http://<host>:8082/v1/embeddings -H 'Content-Type: application/json' -d '{"model":"qwen3-embedding-0.6b","input":"hello"}'` returns one vector of 1,024 numbers.

## Changing the model

Every stored vector records the model that made it, and vectors from different models are never compared (each model gets its own index in the memory service, and `POST /vector_pending` lists what a model still has to embed; see `server/memory/README.md`). A new model, a new quantization or new dims is therefore a re-embed: change the pins here, give the unit's `--alias` and the registry's model `name` a new value together (so new vectors land in a new index rather than next to old ones), set `dims`, and re-embed memory. `bud doctor --net` warns when the server returns a different vector size from the registry's `dims`, the sign that the server and the registry disagree; weights with the same size and the same alias can't be told apart, which is why the alias has to change.

## Measured

Measured with the unit's flags and 4 threads on the cloud build machine (4 vCPUs, no GPU), not on the home server, so re-measure there and update this table.

| Case | Result |
| --- | --- |
| Resident memory | 2.1 GB, idle or under load. Most of it is the 4,096-token micro-batch's compute buffer; one slot of 4,096 is 1.7 GB, and two slots of 8,192 are 3.0 GB |
| One short query (10 tokens) | 29 ms median of 10 |
| A batch of 32 memory records (104 tokens each) | 7.2 s, about 225 ms a record |
| An input over 4,096 tokens | refused with a 400 that names the limit |
| Quality spot check | vectors are unit length; "The cat sat on the mat." against a paraphrase scores 0.94, against "Quarterly tax filing deadline" 0.27 |

So a query costs well under the time a model call takes, and re-embedding 10,000 records takes about 40 minutes on four cores.
