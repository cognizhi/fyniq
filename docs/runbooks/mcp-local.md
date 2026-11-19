# Runbook: connect Claude Code to the local MCP server (DEV-06)

The MCP server is off by default: the launch shell answers every call with `MCP-OFF-01` until an operator sets
`FYNIQ_MCP_SERVER=on`. This is how to try it against the sample company on your own machine. Never point a local client at staging or
production data (DEV-08), and never set the switch in a deployment before security review #4 signs.

## 1. Start the stack with the server on

```sh
FYNIQ_MCP_SERVER=on make up      # the local images behind Caddy on localhost:8080
```

The API registers the first-party clients (`claude-code`, `claude-desktop`), the MCP service's delegation client and its trust
relationship at start. Local only: the delegation key is derived from `FYNIQ_MCP_DELEGATION_SEED`, which the API and the MCP service
both read (a default is set for the local stack).

## 2. Open it for the organisation, the app and yourself

Each step is a person's choice; the first two are an Owner's, each with a second approver (the sample company has two founders):

1. Settings → Organisation → **AI apps through MCP**: turn it on. The second Owner approves it in Approvals.
2. **AI apps that may connect**: add `claude-code`. The second Owner approves it.
3. As yourself, opt in to AI apps (`PUT /v1/me/mcp`, `{"opted_in": true}`; the Connected apps screen will do this).

## 3. Connect

```sh
claude mcp add --transport http --client-id claude-code --callback-port 3118 fyniq-local http://localhost:8080/mcp
```

Claude Code finds the protected-resource metadata, signs you in at Fyniq (after your second factor), and asks you to allow the scopes
(the default is reports). The client ID and the port matter: Hydra matches `localhost` redirects exactly, so `http://localhost:3118/callback`
is the one registered for `claude-code`.

## 4. Check it

Ask Claude Code for the company's revenue for October 2026. The answer should come as typed facts with IDs. Then check the three
stops: opt out (the next call is refused with `MCP-GATE-03`), take `claude-code` off the list (`MCP-GATE-02`), and turn the organisation's
switch off (`MCP-GATE-01`); each takes effect on the next call.

## 5. Apps that register themselves

An Owner turns on **AI apps may register themselves** (second approver). An app then finds `registration_endpoint` in the
authorisation-server metadata (`/.well-known/oauth-authorization-server/oauth`) and registers; its ID (`mcp-…`) appears as pending, and it
connects only once an Owner adds that ID to the apps that may connect. A registration nobody approves expires after 7 days.

## When it goes wrong

| Symptom | Likely cause |
|---|---|
| `MCP-OFF-01` | The switch is off: this is the launch shell |
| 401 with `WWW-Authenticate` | No valid token: connect again |
| `redirect_uri does not match` at sign-in | Claude Code was not given `--callback-port 3118`, or a different port |
| `MCP-GATE-01/02/03` | The organisation, the app or you have not allowed it (section 2) |
| `MCP-SCOPE-01` | The token lacks a scope this tool needs: connect again and allow it |
| `MCP-LIMIT-01` | Over the organisation's limits: wait the `Retry-After` |
