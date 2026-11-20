# OpenCode 2.3.2 approval baseline probes

Run `scripts/opencode-approval-probe.sh /path/to/opencode` with Bun 1.4.4 and the pinned checkout's dependencies installed. The runner requires commit `c0ad18b402eb2827fc1ee71c171b1177b3fc02d1` and copies upstream test fixtures into uniquely named temporary files within that checkout; it removes them on exit. It does not fetch dependencies. Use a trusted, disposable pinned checkout with trusted dependencies: the runner refuses tracked modifications, but untracked files and installed dependencies can still shadow source and are not authenticated.

These are **negative baseline probes**, not proof that approval is secure. On the stock runtime, the four tests pass by observing behaviors an integration must address:

- A `tool.execute.before` rewrite is followed by schema validation that can transform the input again.
- A permission `always` reply can save a rule and allow a subsequent request without review.
- `shell.create.before` can replace the command, working directory and environment after `tool.execute.before`.
- A synthetic plugin tool can invoke the real plugin host's permission-reply API. The permission service in this test is a stand-in, so this does not establish a complete exploit against a running session or an authenticated ACP connection.

The runner uses OpenCode's own `packages/core/script/test.ts`, started with an empty environment except for PATH; it creates an isolated temporary HOME and XDG directories. The tests use upstream's offline test layer, which rejects non-loopback requests through its fetch client. This is not an operating-system network sandbox; use an isolated machine or network namespace when stronger isolation is required. The shell probe executes only a fixed local `printf` command in a temporary directory. The upstream test runner cleans its test directory. These probes must be replaced or inverted as part of a secured runtime implementation, and supplemented with actual authenticated connection, isolation, cancellation, replay and native-guardrail tests.
