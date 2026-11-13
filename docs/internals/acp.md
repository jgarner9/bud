# The agent over ACP

How bud talks to its AI backend in 1.1 (D131-D138, BASE.md 4.4): the Agent Client Protocol, the backends, and what each one leaves out. Checked against the ACP v1 schema (`@agentclientprotocol/sdk` 2.6.0, `schema/v1/schema.json`) and OpenClaw 2026.11.3's docs and packages on 2026-11-13.

**The protocol.** ACP is JSON-RPC 2.0 over the agent's stdin and stdout, one message per line, with requests going both ways. bud speaks v1 (`initialize` answers with the version it speaks; every backend below speaks 1). `bud.ai.acp` (`nvim/lua/bud/ai/acp.lua`) is the protocol and nothing else: `connect(argv, opts)` starts the agent with `vim.fn.jobstart` and returns a connection with `request(method, params, cb)`, `notify(method, params)`, `running()` and `close()`; `opts.on_notification` gets `session/update`, `opts.on_request(method, params, respond)` answers the agent's own requests, and `opts.on_exit(code, stderr)` runs when it stops, after every waiting request has failed with "the agent stopped (exit N)". Messages decode with nulls as `nil`; anything that must go out as a JSON object while empty is `vim.empty_dict()` (an empty Lua table encodes as `[]`, which is what `mcpServers` wants).

**A session's calls**, in order:

| Call | Direction | Shape |
| --- | --- | --- |
| `initialize` | bud to agent | `{ protocolVersion = 1, clientCapabilities = { fs = { readTextFile, writeTextFile }, terminal = false }, clientInfo }`; the answer has `agentCapabilities` (`loadSession`, `promptCapabilities.embeddedContext`, ...) and `authMethods` |
| `session/new` | bud to agent | `{ cwd, mcpServers = {} }` returns `{ sessionId, modes?, configOptions? }` |
| `session/load` | bud to agent | `{ sessionId, cwd, mcpServers }`, only when `loadSession`; the history replays as `session/update`s before the answer |
| `session/prompt` | bud to agent | `{ sessionId, prompt = { ContentBlock... } }`; answers when the turn ends with `{ stopReason }`: `end_turn`, `max_tokens`, `max_turn_requests`, `refusal`, `cancelled` |
| `session/cancel` | bud to agent, notification | `{ sessionId }`; the open prompt then answers `cancelled`, and bud answers any open permission request `cancelled` |
| `session/update` | agent to bud, notification | `{ sessionId, update }`, `update.sessionUpdate` one of `agent_message_chunk`, `agent_thought_chunk`, `user_message_chunk`, `tool_call`, `tool_call_update`, `plan`, `available_commands_update`, `current_mode_update`, `config_option_update`, `session_info_update`, `usage_update` |
| `session/request_permission` | agent to bud | `{ sessionId, toolCall, options = { { optionId, name, kind } } }`, `kind` one of `allow_once`, `allow_always`, `reject_once`, `reject_always`; answered `{ outcome = { outcome = "selected", optionId } }` or `{ outcome = { outcome = "cancelled" } }` |
| `fs/read_text_file` | agent to bud | `{ sessionId, path, line?, limit? }` returns `{ content }`; bud reads the buffer when the file is open, so the agent sees unsaved changes |
| `fs/write_text_file` | agent to bud | `{ sessionId, path, content }` returns null; bud writes through the buffer when it's open |

A prompt's blocks: `text`, `resource_link` (`{ uri, name }`, a file the agent can read itself) and `resource` (`{ resource = { uri, text, mimeType } }`, embedded text such as a selection, only when `promptCapabilities.embeddedContext`). A tool call is `{ toolCallId, title, kind, status, content, locations, rawInput, rawOutput }`; `kind` is `read`, `edit`, `delete`, `move`, `search`, `execute`, `think`, `fetch`, `switch_mode` or `other`, and its `content` items are `content` (a block), `diff` (`{ path, oldText, newText }`) or `terminal`. `acp.text(content)` and `acp.diffs(tool_call)` pull those out.

**The backends** (`ai.backend`, D131):

| Name | Command bud runs | Signs in with | Leaves out |
| --- | --- | --- | --- |
| `openclaw` | `openclaw acp [--url <ai.openclaw.url>] [--token-file <ai.openclaw.token_file>] --session-label bud:<project>` | The Gateway token; the models' own logins live on the Gateway host | Diffs, `fs/*`, terminals, model choice, per-session MCP servers (it errors on any), `ask_user` questions; permission requests only for exec approvals during a turn; `session/load` replays only bridge-made sessions |
| `claude` | `claude-agent-acp` (npm `@agentclientprotocol/claude-agent-acp`) | Claude Code's own login and `~/.claude` | Nothing bud uses |
| `codex` | `codex-acp` (npm `@zed-industries/codex-acp`) | Codex's own login (ChatGPT Plus) | Not checked yet |
| `opencode` | `opencode acp` | OpenCode's providers | Not checked yet |
| `custom` | `ai.custom.command` | Its own | Whatever it does |

**OpenClaw** (D132). The Gateway runs on the home server (port 18789 by default, a WebSocket protocol of its own, v4, with Ed25519 device identity and pairing for full clients). bud never speaks that protocol: `openclaw acp` is OpenClaw's own bridge from ACP on stdio to the Gateway, so the CLI handles the token, device identity and reconnects. `--url` doesn't reuse the CLI's implicit credentials, so give `--token-file` with it. Plain `ws://` is accepted only for loopback, private, `.local` and `.ts.net` hosts; use Tailscale or `wss://`. Each ACP session maps to one Gateway session key; bud asks for one per project with `--session-label`. Claude Code on the Gateway is the `claude-cli` runtime (set per model: `agentRuntime: { id: "claude-cli" }`), and ChatGPT Plus is the Codex OAuth profile (`openclaw onboard --auth-choice openai-codex`). Anthropic's terms for third-party use of a Claude subscription have moved before; check them when setting up.

**Tests.** `tests/lua/lib/fake_acp.lua` is a fake ACP agent run as `nvim --clean --headless -l`, so tests need nothing installed. The prompt's first word picks a scripted turn (`hello`, `echo`, `edit <path>`, `read <path>`, `slow`, `fail`, `crash`; see its header), and it logs every message it receives to `$FAKE_ACP_LOG`. `tests/lua/acp_test.lua` drives the protocol through it.
