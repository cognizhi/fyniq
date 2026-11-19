# ADR-T3 (continued): the MCP server for first-party clients

Status: built locally; the deployment switch is not set until security review #4 signs. Decided by the owner on 14 and 19 Nov 2026.

- **One image, a deployment switch.** `cmd/mcp` serves the launch shell (`internal/mcpshell`) unless `FYNIQ_MCP_SERVER=on`; any other value
  stops the service. The switch is the operator's; `features.mcp` is an Owner's, per organisation.
- **Protocol: MCP revision 2026-07-28 only.** It has no protocol sessions and no GET stream, so AI-39's session binding is superseded:
  an `Mcp-Session-Id` is ignored and never minted, the token is checked on every request, and the protocol headers must match the
  body. An earlier revision's handshake is added only if a real client needs it (Claude Code 2.1.327 speaks the modern shape first).
- **Delegation: open question 40's option (b)**, RFC 7523 signed by the MCP service's key, for `<organisation>:<person>`, at most five
  minutes, the person's rights read by the API when used. Conditions the founders set: the key in Cloud KMS and `private_key_jwt` in the cloud
  (not wired; a release build refuses to start the server), an audit row per call, and per-call gates in the API.
- **All gates are the API's.** The MCP service holds no database and no write credential. One internal operation runs a read tool for a person
  after the scope (read from the tool and its input), the organisation's switch, the allowlist and the opt-in, then the limits.
- **Registration: Fyniq's own endpoint over Hydra's admin API (AI-90)**, not Hydra's own registration and not a fetched client metadata
  document (P2, AI-46): closed until an Owner opens it with a second approver, policy-checked, limited by network, narrowest scope, pending
  until an Owner lists it, expiring and audited.
- **First-party redirects are exact.** Claude Code at `http://localhost:3118/callback` (Hydra matches `localhost` exactly; only the IP
  loopback addresses match on any port); Claude Desktop at Anthropic's hosted callback, `[live-verify]`.

Evidence: `compliance/qa-mcp-01.json`, `docs/security/threat-models/mcp-server.md`, the change `openspec/changes/p1-mcp-first-party`.
