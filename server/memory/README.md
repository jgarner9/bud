# Bud working memory service

The memory service runs on the home server as a separate local process. Bud
clients connect to its Unix socket. The process owns the SQLite database,
FTS5 and sqlite-vec. The policy gives each client an explicit set of privacy
tiers and actions.

The service uses distinct credentials for agent and reviewer identities. A
reviewer credential can approve staged records, so it must be held outside
the agent's shell and process environment. Socket access and OS user
permissions must also keep the reviewer credential inaccessible to the agent.
The example unit creates a private socket directory owned by the service
account. A client running as a different user needs an explicitly configured
access mechanism.

## Build sqlite-vec

Run the build script on the target architecture with a C compiler, Git,
Python 3 and curl available.

```sh
sh scripts/build-memory-vec.sh ./memory-vec-build
sha256sum ./memory-vec-build/vec0.so
```

The script verifies the pinned upstream source blobs before compiling.
Install the resulting extension in a service-owned, non-writable directory,
and record its absolute path and SHA-256 in the private environment file.
The launcher checks that digest before loading the extension.

## Configuration

Copy the Python files in this directory into
`~/.local/lib/bud/memory/`, and the user unit into the systemd user unit
directory. Copy `memory.env.example` into
`~/.config/bud/memory.env`. Set the extension path and digest, and make
`BUD_MEMORY_CLIENTS` a 1Password reference to a JSON object mapping each
high-entropy bearer token to its server-owned policy. For example, the
resolved value has this shape, with real tokens held only in 1Password.

```json
{
  "agent-token-at-least-32-characters": {
    "tiers": ["P0"],
    "actions": ["stage", "search"]
  },
  "reviewer-token-at-least-32-characters": {
    "tiers": ["P0", "P1", "P2"],
    "actions": ["search", "approve"]
  }
}
```

The unit reads `OP_SERVICE_ACCOUNT_TOKEN` from
`~/.config/bud-server/op-service-account.env` and uses `op run --env-file` to
resolve the reference at startup. Create that file with mode 0600. Run the
unit as a dedicated service account with access limited to the required
1Password item. Use separate credentials and least-privilege policies for
each client. The current service socket is mode 0600.

## Packaging decision

Use the CPython launcher under systemd. The service owns SQLite, FTS5,
sqlite-vec, migrations and the database. Clients use its Unix-socket HTTP API.
The Bud Go module stays standard-library-only.

| Option | Consequences |
| --- | --- |
| Pinned distro CPython and SQLite packages | Recommended for the Linux host if its package source can retain the tested builds. Systemd keeps socket and state ownership explicit. |
| Container pinned by image digest | Pins the runtime and its SQLite together. The service still needs a persistent database and a Unix socket accessible to the right host identity. |
| Separately built runtime | Can pin CPython and SQLite independently of the distro. Bud would own the builds and security updates. |

A Python virtual environment inherits its interpreter and SQLite library.
For the production package, record the exact CPython and SQLite package builds
and repository snapshot, the interpreter path used by the unit, and the
sqlite-vec artifact SHA-256 for the target architecture. The build script
checks sqlite-vec v0.1.6 source and uses SQLite 3.46.1 headers when compiling.
The linked runtime can be a different SQLite version. Check its actual
capabilities with the commands below. The launcher verifies the extension
artifact digest before loading it. JGA-368 tracks adopting the production pins,
verifying socket/credential access on the host, and checking service recovery
after an interrupted startup or hard kill.

## Tests

```sh
vec_build=$(mktemp -d)
sh scripts/build-memory-vec.sh "$vec_build"
python3 -c 'import platform, sqlite3; print("CPython", platform.python_version(), "SQLite", sqlite3.sqlite_version)'
VEC_EXTENSION="$vec_build/vec0.so" \
BUD_TEST_SQLITE_VEC="$vec_build/vec0.so" \
python3 -m unittest discover -s server/memory -v
```

The fixtures use synthetic data. They check parameter binding and transaction
isolation, an online SQLite backup during an uncommitted write, and full-text
and vector queries after restoring a portable backup into a separate running
instance. This exercises SQLite's backup capability in WAL mode. The service
recovery API uses its portable snapshot format. End-to-end recall is tracked in
JGA-310, and the runtime clients in JGA-313.

## Hybrid retrieval

Give a client the `retrieve` action to search approved records by text and a supplied embedding. `POST /retrieve` takes `query`, `model` and `values`, with optional `source`, `since`, `until` (inclusive YYYY-MM-DD) and `limit` (1–100). Allowed privacy tiers come from the client's server-side policy. Text and vector ranks are combined, and each result includes its source, privacy, creation time and matching methods. Embedding generation and the MCP integration are separate work (JGA-310).

Fusion considers up to 1,000 candidates from each ranked source before applying
the requested result limit. Results are approximate beyond that window.
