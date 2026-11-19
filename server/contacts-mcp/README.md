# Private Hermes contacts

The home-server worker reads iCloud contacts over CardDAV (Suite D88).
Hermes can use the name, nicknames, email and phone fields to prepare a
private draft. Sending still follows the existing approval policy.

1. Create an iCloud app-specific password for the worker and a separate
   random bearer token of at least 32 characters. Store both in 1Password.
2. Copy the unit to the systemd user directory and adapt the env example
   in the private overlay. Leave only 1Password references in that file.
   The systemd worker uses `op run` to fill its environment.
3. Resolve the same `BUD_CONTACTS_MCP_TOKEN` reference in the environment
   used to launch the private Hermes gateway. Wrap that launch in
   `op run --env-file` with a reference-only file. Hermes needs only the
   bearer token. Its cloud profile and OpenCode do not need any of these
   variables.
4. Add the fragment below to the server overlay, enable the unit, run
   `bud sync`, and restart the private Hermes gateway. Keep the service out
   of the shared MCP gateway.
5. Ask the private profile to find an existing contact for a draft. Verify
   the cloud profile and planning OpenCode have no contacts entry. Use a
   draft to validate lookup without sending anything.

```json
{
  "bud": {
    "mcp": {
      "contacts": {
        "hermes_only": true,
        "url": "http://127.0.0.1:8794/mcp",
        "privacy": ["P0"],
        "headers": {"Authorization": "Bearer {env:BUD_CONTACTS_MCP_TOKEN}"}
      }
    },
    "hermes": {"mcp_servers": {"contacts": {"trust": "untrusted"}}}
  }
}
```

Update Bud to a version that understands `hermes_only` before adding the
fragment.

The single MCP tool, `find_contact`, requires a name of 2–120 characters
and returns at most ten matches. It never returns raw vCards or writes to
iCloud. Its upstream only sends read-only `PROPFIND` and `REPORT` requests
to the iCloud CardDAV endpoint or an iCloud contacts shard. It applies
name matching again locally and returns only the fields needed for a
draft. The endpoint requires loopback and the bearer token for every
request. Keep the existing private Hermes Docker boundary in place.
