# Local memory service API draft

The service binds an HTTP/JSON API to a Unix socket created with mode 0600.
Client identities have separately provisioned high-entropy bearer tokens and
server-owned action and tier policies. The caller cannot choose its search
tiers. Agent identities may stage and search, while an independent reviewer
identity may approve. Configuration that gives one identity both stage and
approve is rejected. Credentials must come from an authenticated service
manager and never be exposed to the agent; the socket's Unix owner must be
the dedicated service account. Any process able to read the reviewer's token
can approve its allowed tiers. Socket permissions and secret isolation must
enforce the separation in deployment.

The launcher accepts policy through its environment and verifies the extension digest. Deployment
must supply credentials through 1Password, arrange a restricted socket
directory and database permissions, provision unique reviewer and agent tokens,
and clean up only the socket created by the same process. The current socket
mode 0600 permits a same-UID client or a process explicitly passed the socket.
Cross-UID operation needs an intentional owner/group deployment policy.

Each service is single-threaded and supports bounded JSON requests and
responses from the storage API. Store startup and migration happen in the
server thread. Deployment must handle availability, retries, database backup,
and safe privilege separation before connecting a runtime. The API currently
covers stage, literal FTS search, and reviewer approval. It does not implement
vector search, export, references, or an audit log.
