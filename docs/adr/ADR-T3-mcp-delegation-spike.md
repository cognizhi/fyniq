# ADR-T3 spike: how the MCP server gets an API token (open question 40, option (b))

**For:** the security reviewer (review pull request #4, AI and MCP) and the founders, who choose between
options (a), (b) and (c) before P1's MCP work (PRD-001 open question 40, decided rev 0.20: "the P0
identity spike proves option (b); an Enterprise Licence quote is requested at the same time").
**Status:** spike built and measured on 15 Oct 2026 against the pinned Ory Hydra Apache-2.0 v26.2.0
(`docs/adr/ADR-T3-identity-and-oauth.md`). **The Enterprise Licence quote:** requested by the founders,
not received yet; it belongs beside this record when it arrives.

## The problem

An MCP client (Claude Desktop, Claude Code) holds an **MCP-audience** token: the API refuses it (AI-36,
no passthrough). To answer a tool call, the MCP server needs an **API-audience** token for the same person.
The clean way is RFC 8693 token exchange, which Hydra's Apache-2.0 build does not have.

| Option | What | Cost and risk |
|---|---|---|
| (a) | Ory Enterprise Licence: token exchange (RFC 8693) | A licence fee (quote pending) |
| **(b)** | **RFC 7523 JWT-bearer grant** in the Apache-2.0 build, from the MCP server's own key, only for the subject of an MCP token it has just validated, at most 5 minutes, scopes intersected by the API | **This spike** |
| (c) | Another Apache-2.0 server with token exchange, e.g. Keycloak 26.2+ (internal-to-internal only, no `resource`) | A second OAuth server to run and secure |

## What was built (option (b))

- **`internal/mcpdelegation.Delegator`** (used only by `cmd/mcp`, so choosing (a) or (c) replaces this
  package alone):
  1. It introspects the MCP token at the MCP audience; only an active agent-class token with a subject goes on (AI-34).
  2. It signs an assertion with the MCP server's key (ES256): issuer `urn:fyniq:mcp`, subject the person's
     user ID, audience Hydra's token endpoint, valid one minute, with a `jti`.
  3. It asks Hydra's token endpoint (`grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer`) as its own
     confidential **delegation client** for the API audience and the MCP token's scopes (`openid` and
     `offline_access` dropped).
  4. It refuses an answer whose token lives longer than 5 minutes.
- **Hydra setup** (`internal/adapters/hydra`):
  - A **trust relationship** for the MCP server's public key, with `allow_any_subject: true` and the
    scopes MCP may ever have.
  - A **delegation client**: the JWT-bearer grant only, the API audience only, agent class, and
    `jwt_bearer_grant_access_token_lifespan: 5m`.
- **The API** (`authn.Delegation` on the introspector) treats a token from the delegation client this way:
  - **Always agent class**, whatever its claims say. No consent stamped it, so nothing in it but the subject is trusted.
  - The person's **organisation and roles are read when the token is used**.
  - It refuses a token that lives longer than 5 minutes (one second's grace for whole-second `iat` and
    `exp`), and a subject that is not a user.
  - The AI's rights are then the lesser of the person's and the AI column's (`authz.Decide`): that is the intersection.

## Measured (`internal/api/delegation_test.go`, local Docker, in-memory Hydra)

| What | Result |
|---|---|
| Delegation, end to end (introspect, sign, grant) | about **18–21 ms** per call |
| Delegated token's life | **300 s** (`exp − iat` reads 300 or 301 s: whole seconds) |
| Delegated token's audience | the API's, **plus Hydra's token URL** copied from the assertion. Ory's docs say `oauth2.grant.jwt.omit_assertion_audience` defaults to omitting it; in v26.2.0 the extra audience appears, and setting `OAUTH2_GRANT_JWT_OMIT_ASSERTION_AUDIENCE=true` did not remove it. Harmless to the API, which needs its own audience among them; the MCP audience is never there |
| Scopes | at most the requested ones; a scope outside the trust relationship is refused (`400 invalid_scope`) |
| The MCP token at the API | refused (`ErrWrongAudience`): no passthrough |
| An assertion used twice (same `jti`) | refused |
| The grant without the delegation client's credentials | refused (`400 invalid_request`): Hydra requires client authentication for this grant |
| An invalid, made-up or local dev token at the delegator | refused before any assertion is made |
| A token for a person with no membership | issued by Hydra, **refused by the API** (no organisation, no roles) |
| Revoking the person's consents (`RevokeSubject`, AI-38) | the delegated token **stays active**: it has no consent session. Only its 5-minute life ends it; a role change still applies at once, because the API reads roles on every use (within its 30-second introspection cache) |

## The limits, plainly

1. **Whoever holds the MCP server's key and its delegation client's credentials can get a token for
   any person**, within the agent class and that person's current rights. That is the known weakness
   of option (b) (open question 40). Hydra needs both: the key alone is refused. Mitigations: the key
   lives in Cloud KMS (EC_SIGN_P256_SHA256), so it never leaves it, and only the MCP service account may
   sign. The client authenticates with `private_key_jwt` in the cloud (SEC-07), not the secret the spike
   uses. Every delegated token is introspected, logged and alerted on like any other (AI-45).
2. **A delegated token cannot be revoked early.** Five minutes is the window after a person's access is
   cut. Their roles stop applying at once (AI-38's 60 seconds holds for rights); the token itself
   outlives the revocation by at most 5 minutes.
3. **The AI never acts as the person beyond the agent class.** It is proposal-only (AI-34): writes go
   through the proposal executor, which is P1. The spike shows the API refusing a direct journal write
   for a delegated token.
4. **Option (a) or (c) removes limit 1:** with token exchange the MCP token itself is the proof, and no
   key can mint for an arbitrary subject.

## Recommendation for the decision before P1 MCP work

- **Option (b) is workable for P1**, given limits 1 and 2 and the mitigations above (KMS key,
  `private_key_jwt`, alerts). Run it through the MCP red-team suite (QA-MCP-01) before any MCP client
  is enabled.
- **Prefer option (a) if the Enterprise Licence quote is affordable.** It removes the any-subject
  weakness without running a second OAuth server.
- **Option (c)** only if (a) is too expensive and (b) is rejected.

## Not done in the spike

- No MCP tool calls the delegator yet; the MCP server's tools arrive in P1.
- The KMS signer, `private_key_jwt` for the delegation client and the cloud setup of the trust
  relationship come with p0-infra-opentofu and P1.
- The red-team suite (QA-MCP-01) is P1.
