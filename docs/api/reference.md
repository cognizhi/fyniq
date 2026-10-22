# Fyniq API reference (v1)

<!-- Generated from the API's code with `make openapi`, like contracts/openapi/fyniq.v1.yaml; never edit by hand. -->

The operations of Fyniq's public API, generated from the same code as the contract ([`contracts/openapi/fyniq.v1.yaml`](../../contracts/openapi/fyniq.v1.yaml)). Every resource has a JSON Schema in [`contracts/schemas/`](../../contracts/schemas/). AI agents start with the [agent guide](agent-guide.md); [`llms.txt`](llms.txt) lists everything for a model.

## Conventions

Fyniq's public API. The web app uses only this API (FR-INT-01).

Conventions (PRD-001 §8.18.1):
- Errors are RFC 9457 problem details (application/problem+json) with a stable code, the field path,
  what is wrong and how to fix it; validation errors list every invalid field at once (FR-INT-07).
- Every POST that creates or changes state accepts an Idempotency-Key header, kept 24 hours per
  client: a repeat returns the original response; the same key with a different request returns 422
  (FR-INT-08). The installation's operations that take or return a one-time code (first-run setup,
  Super admins) take none: the code makes them one-time, and a response with a code is never stored.
- Versioned records return an ETag; commands that change one require If-Match and return 412 when
  the record changed since, 428 without it (FR-INT-09).
- Lists are cursor-paginated (page_size up to 500, an opaque next_cursor). Money is a decimal string
  with its currency, never a JSON number; dates are ISO 8601; IDs are UUIDv7 strings (FR-INT-10).
- Writes accept Prefer: return=preview: the command runs with every check its commit would, and nothing
  is saved; the answer carries Preference-Applied: return=preview (FR-INT-14). The one-time-code
  operations take no preview.
- Bulk operations take up to 250 items and answer with one result per item, in order (FR-INT-11).
- Each client may make 600 reads and 120 writes a minute in an organisation; RateLimit and
  RateLimit-Policy say what is left, and 429 comes with Retry-After (FR-INT-16).
- A deprecated operation carries Deprecation, Sunset and a Link to its migration note, at least six
  months before its sunset (FR-INT-06).
- Every operation states the right it needs (x-fyniq-permission, from PRD-001 Appendix D) and the
  error codes it returns (x-fyniq-error-codes).

## Installation

First-run setup of a new installation (FR-ADM-06).

### `GET /v1/setup`

**Get the setup status** (`getSetupStatus`)

Says whether first-run setup has been completed (FR-ADM-06). Public: it needs no credential, so the web app can show the setup wizard on a new installation. Read-only; no side effects.

- **Permission:** View on (installation setup)
- **Responses:**
  - `200` OK [`SetupStatus`](../../contracts/schemas/SetupStatus.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-UNAVAILABLE-01`

### `POST /v1/setup`

**Complete first-run setup** (`completeSetup`)

Claims a new installation with the one-time setup code created at deployment (FR-ADM-06). You become the Super admin and the Owner of the first organisation, and receive your ten recovery codes, shown once (open question 36); the installation's own audit trail is created with it. Side effects: your user and sign-in identity, the Super admin and their recovery codes, two organisations, your membership and role, audit events. Works once: a wrong or used code changes nothing. Needs a person signed in to Fyniq; an OAuth client or the AI is refused. It takes no Idempotency-Key: the code itself makes it one-time.

- **Permission:** Create on (installation setup). Needs the one-time setup code and a first-party sign-in; an OAuth client or the AI is refused.
- **Request body:** [`SetupRequest`](../../contracts/schemas/SetupRequest.schema.json)
- **Responses:**
  - `201` Created [`SetupResult`](../../contracts/schemas/SetupResult.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-SETUP-01` Setup refused.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `STORAGE-INV-05` An amount or rate is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-SETUP-01`, `API-UNAVAILABLE-01`, `STORAGE-INV-05`

### `POST /v1/super-admins`

**Become a Super admin with an invitation** (`acceptSuperAdminInvitation`)

Redeems a Super admin's invitation: you (a new user, or the one your sign-in belongs to) become a Super admin and receive your ten recovery codes, shown once (FR-ADM-06/09). Side effects: your user and sign-in identity when new, the Super admin, the recovery codes, events in the installation's trail. A wrong, used or expired invitation changes nothing. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Create on (Super admins). Needs a valid invitation and a person signed in to Fyniq; an OAuth client or the AI is refused.
- **Request body:** [`SuperAdminInvitation`](../../contracts/schemas/SuperAdminInvitation.schema.json)
- **Responses:**
  - `201` Created [`NewSuperAdmin`](../../contracts/schemas/NewSuperAdmin.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-SUPERADMIN-01` Code refused; `API-SUPERADMIN-02` Super admin change refused.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-SUPERADMIN-01`, `API-SUPERADMIN-02`, `API-UNAVAILABLE-01`

### `POST /v1/super-admins/invitations`

**Invite the second Super admin** (`createSuperAdminInvitation`)

Creates the one-time invitation that makes its redeemer the installation's second Super admin (FR-ADM-06/07; one per founder, open question 36). Side effects: the invitation (it lasts 7 days and replaces an unused one) and its event in the installation's trail. Refused when the installation has two Super admins already. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Responses:**
  - `201` Created [`OneTimeCode`](../../contracts/schemas/OneTimeCode.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-SUPERADMIN-02` Super admin change refused.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-SUPERADMIN-02`, `API-UNAVAILABLE-01`

### `POST /v1/super-admins/recovery`

**Recover your Super admin access** (`recoverSuperAdmin`)

Gives a Super admin who lost their sign-in their access back, with two proofs (SEC-35, FR-ADM-09): one of their printed recovery codes with the other Super admin's confirmation, or the deployment's two-person recovery (docs/runbooks/super-admin-recovery.md). The sign-in you use now replaces your others. Side effects: your sign-in identities, ten new recovery codes (shown once; the old ones stop working), the revocation of the tokens of the replaced sign-ins (AI-38) and events in the installation's trail. Wrong, used or expired codes change nothing. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Create on (Super admins). Needs two proofs (SEC-35): a recovery code with the other Super admin's confirmation, or the deployment's two-person recovery; and a person signed in to Fyniq, who may have lost their second factor: the two proofs stand in for it. An OAuth client or the AI is refused.
- **Request body:** [`SuperAdminRecovery`](../../contracts/schemas/SuperAdminRecovery.schema.json)
- **Responses:**
  - `200` OK [`RecoveredSuperAdmin`](../../contracts/schemas/RecoveredSuperAdmin.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-SUPERADMIN-01` Code refused.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-SUPERADMIN-01`, `API-UNAVAILABLE-01`

### `POST /v1/super-admins/recovery-codes`

**Replace your recovery codes** (`replaceSuperAdminRecoveryCodes`)

Gives you ten new recovery codes, shown once; your unused codes stop working (FR-ADM-09). Side effects: the codes and their event in the installation's trail. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Responses:**
  - `200` OK [`RecoveryCodes`](../../contracts/schemas/RecoveryCodes.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`

### `POST /v1/super-admins/{user_id}/recovery-confirmations`

**Confirm the other Super admin's recovery** (`confirmSuperAdminRecovery`)

Creates the one-time confirmation that, with one of their printed recovery codes, gives the other Super admin their access back: the second proof a recovery needs (SEC-35, FR-ADM-09). Confirm only after you have spoken to them. Side effects: the confirmation (it lasts 24 hours and replaces an unused one) and its event in the installation's trail. Nobody confirms their own recovery. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Approve on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). The other Super admin confirms; nobody confirms their own recovery.
- **Parameters:**
  - `user_id` (path, required): The Super admin whose recovery you confirm: not you.
- **Responses:**
  - `201` Created [`OneTimeCode`](../../contracts/schemas/OneTimeCode.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-SUPERADMIN-02` Super admin change refused.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-SUPERADMIN-02`, `API-UNAVAILABLE-01`

## Session

Signing in to the web app: the providers, and your own session (SEC-05, FR-ADM-08).

### `GET /v1/session`

**Get your session** (`getSession`)

Who you are signed in as: the provider, your user, the organisation your session is in, your roles there, whether you are a Super admin, until when your step-up counts and how you passed it (SEC-05, FR-ADM-07), and the organisation's feature switches (FR-ORG-06). The web app reads it after signing in; a 401 means it should show the sign-in page. Needs a person signed in; an OAuth client or the AI is refused. Read-only; no side effects.

- **Permission:** View on (your session). Needs a person signed in to Fyniq; an OAuth client or the AI is refused.
- **Responses:**
  - `200` OK [`SessionInfo`](../../contracts/schemas/SessionInfo.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`

### `GET /v1/session/devices`

**List your devices** (`listDevices`)

The browsers you are signed in on: your live sessions, newest first, with the one making this request marked (FR-ADM-03). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused. Read-only; no side effects.

- **Permission:** View on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-03).
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`DevicePage`](../../contracts/schemas/DevicePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/session/devices/sign-out-others`

**Sign out everywhere else** (`signOutOtherDevices`)

Ends every one of your sessions but the one making this request (FR-ADM-03). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused. Side effects: those sessions end, and the security log records each.

- **Permission:** Create on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-03).
- **Responses:**
  - `200` OK [`DevicesEnded`](../../contracts/schemas/DevicesEnded.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `DELETE /v1/session/devices/{device_id}`

**Sign a device out** (`signOutDevice`)

Ends one of your sessions, such as on a lost phone: its next request is signed out (FR-ADM-03). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused. Side effects: the session ends, and the security log records it.

- **Permission:** Create on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-03).
- **Parameters:**
  - `device_id` (path, required): The device's ID.
- **Responses:**
  - `200` OK [`Device`](../../contracts/schemas/Device.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `GET /v1/session/passkeys`

**List your passkeys** (`listPasskeys`)

Your passkeys that work, oldest first (FR-ADM-02). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). Read-only; no side effects.

- **Permission:** View on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01).
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`PasskeyPage`](../../contracts/schemas/PasskeyPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-PASSKEY-03` The challenge cannot be used; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-PASSKEY-01`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-PASSKEY-01`

### `POST /v1/session/passkeys`

**Register a passkey** (`registerPasskey`)

Registers the passkey your authenticator made for the registration challenge (FR-ADM-02). Fyniq checks the challenge, this site's origin and relying party ID, that you were present and verified, and the key; the challenge works once. Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). The session passed a second factor in the last 5 minutes. Side effects: the passkey and its event in the installation's audit trail.

- **Permission:** Create on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). The session passed a second factor in the last 5 minutes.
- **Request body:** [`NewPasskey`](../../contracts/schemas/NewPasskey.schema.json)
- **Responses:**
  - `201` Created [`Passkey`](../../contracts/schemas/Passkey.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-PASSKEY-03` The challenge cannot be used; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-PASSKEY-01`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-PASSKEY-01`

### `POST /v1/session/passkeys/challenge`

**Start registering a passkey** (`startPasskeyRegistration`)

A registration challenge for your session, with the options to pass to navigator.credentials.create: a passkey for this site, with your fingerprint, face or PIN required; your passkeys already registered are excluded. Answer it within 5 minutes with POST /v1/session/passkeys. Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). The session passed a second factor in the last 5 minutes. Side effects: the challenge, kept for the answer; nothing else.

- **Permission:** Create on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). The session passed a second factor in the last 5 minutes.
- **Responses:**
  - `200` OK [`PasskeyChallenge`](../../contracts/schemas/PasskeyChallenge.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-PASSKEY-03` The challenge cannot be used; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-PASSKEY-01`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-PASSKEY-01`

### `DELETE /v1/session/passkeys/{passkey_id}`

**Revoke a passkey** (`revokePasskey`)

Revokes one of your passkeys: it works no more, for step-up or anything else (FR-ADM-02). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). The session passed a second factor in the last 5 minutes. Side effects: the revocation and its event in the installation's audit trail.

- **Permission:** Create on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). The session passed a second factor in the last 5 minutes.
- **Parameters:**
  - `passkey_id` (path, required): The passkey's ID.
- **Responses:**
  - `200` OK [`Passkey`](../../contracts/schemas/Passkey.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-PASSKEY-03` The challenge cannot be used; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-PASSKEY-01`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-PASSKEY-01`

### `POST /v1/session/step-up`

**Confirm it's you with a passkey** (`stepUp`)

Checks your authenticator's answer to the step-up challenge and, when it passes, steps your session up: for 5 minutes it may do the actions that need a second factor, such as asking for or approving a change (FR-APR-04, SEC-01). Fyniq checks the challenge, this site's origin and relying party ID, that you were present and verified, the signature with one of your passkeys, and that its counter went up; the challenge works once. Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). Open to a session that passed no second factor yet: this is how it passes one. Side effects: the session's step-up and the passkey's last use.

- **Permission:** Create on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). Open to a session that passed no second factor yet: this is how it passes one.
- **Request body:** [`StepUpAnswer`](../../contracts/schemas/StepUpAnswer.schema.json)
- **Responses:**
  - `200` OK [`SteppedUp`](../../contracts/schemas/SteppedUp.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-PASSKEY-03` The challenge cannot be used; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-PASSKEY-01`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-PASSKEY-01`

### `POST /v1/session/step-up/challenge`

**Start confirming it's you with a passkey** (`startStepUp`)

A step-up challenge for your session, with the options to pass to navigator.credentials.get: any of your passkeys, with your fingerprint, face or PIN required (FR-APR-04, SEC-01). Answer it within 5 minutes with POST /v1/session/step-up. Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). Open to a session that passed no second factor yet: this is how it passes one. Side effects: the challenge, kept for the answer; nothing else.

- **Permission:** Create on (your session). Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01). Open to a session that passed no second factor yet: this is how it passes one.
- **Responses:**
  - `200` OK [`PasskeyChallenge`](../../contracts/schemas/PasskeyChallenge.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-PASSKEY-02` You have no passkey; `API-PASSKEY-03` The challenge cannot be used; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-PASSKEY-01`, `API-PASSKEY-02`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-PASSKEY-01`

### `GET /v1/sign-in-providers`

**List the sign-in providers** (`listSignInProviders`)

The providers people may sign in to the web app with (FR-ADM-08): Google and Zoho through Identity Platform, or the local sign-in page in local mode, a page at a time (cursor pagination). Public: the sign-in page needs it before anyone is signed in. Read-only; no side effects.

- **Permission:** View on (your session)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`SignInProviders`](../../contracts/schemas/SignInProviders.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`

## Organisation

The organisation your session is in.

### `GET /v1/organisation`

**Get your organisation** (`getOrganisation`)

Returns the organisation your session is in: its name, base currency and team mode, which every screen shows. Read-only; no side effects. Every member of the organisation may call it.

- **Permission:** View on (membership)
- **Responses:**
  - `200` OK [`Organisation`](../../contracts/schemas/Organisation.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/organisation/accounting`

**Get a year's accounting framework and policies** (`getAccounting`)

The accounting framework and edition, and each accounting policy, in force for the financial year that holds the day asked for (FR-ORG-10; PRD-001 rev 0.62), with who recorded, confirmed and approved each version and when. The year's statements are titled "Management statements" and name no framework until its framework is confirmed and every policy is approved by a member who holds the Accountant role, other than its writer; then "Financial statements" with the basis they are prepared under (G4-017). checks lists what is missing. Read-only; no side effects.

- **Permission:** View on Accounting framework and policies (rev 0.62)
- **Parameters:**
  - `on` (query): The day to read the values for; today, in the organisation's time zone, when left out.
- **Responses:**
  - `200` OK [`AccountingYear`](../../contracts/schemas/AccountingYear.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `PERIOD-01` The financial years are not set.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `PERIOD-01`

### `POST /v1/organisation/accounting/framework`

**Record the accounting framework** (`recordAccountingFramework`)

Records the next version of the accounting framework and its edition, from the first day of a financial year until a later version replaces it (FR-ORG-10; PRD-001 rev 0.62). A version never changes: a correction is a new version, which the accountant confirms again. Side effects: the version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Accounting framework and policies (rev 0.62)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`AccountingFrameworkInput`](../../contracts/schemas/AccountingFrameworkInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`AccountingFramework`](../../contracts/schemas/AccountingFramework.schema.json)
  - `201` Created [`AccountingFramework`](../../contracts/schemas/AccountingFramework.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `ACCOUNTING-02` You wrote it, so another accountant signs it; `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `ACCOUNTING-03` Signed already, or replaced; `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `PERIOD-01` The financial years are not set; `STORAGE-ACCOUNTING-01` A framework or policy version never changes.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `ACCOUNTING-01` Not a framework or policy version that can be recorded; `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `ACCOUNTING-01`, `ACCOUNTING-02`, `ACCOUNTING-03`, `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `PERIOD-01`, `STORAGE-ACCOUNTING-01`

### `POST /v1/organisation/accounting/framework/{version}/confirm`

**Confirm the accounting framework** (`confirmAccountingFramework`)

Records the caller's confirmation, as the accountant, that the company may use a framework version (FR-ORG-10; PRD-001 rev 0.62). Needs the Accountant role; never its writer, never twice, and never a version a later one from the same year replaces (the founders' decision of 21 Oct 2026; the database refuses the writer too). While the accounting.signing_step_up setting is on (the default), it needs step-up in the last 5 minutes. Side effects: the confirmation and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Approve on Accounting framework and policies (rev 0.62)
- **Parameters:**
  - `version` (path, required): The framework version.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`AccountingFramework`](../../contracts/schemas/AccountingFramework.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `ACCOUNTING-02` You wrote it, so another accountant signs it; `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `ACCOUNTING-03` Signed already, or replaced; `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `PERIOD-01` The financial years are not set; `STORAGE-ACCOUNTING-01` A framework or policy version never changes.
  - `422` Unprocessable Entity: `ACCOUNTING-01` Not a framework or policy version that can be recorded; `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `ACCOUNTING-01`, `ACCOUNTING-02`, `ACCOUNTING-03`, `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `PERIOD-01`, `STORAGE-ACCOUNTING-01`

### `POST /v1/organisation/accounting/policies/{area}`

**Record an accounting policy** (`recordAccountingPolicy`)

Records the next version of one accounting policy's words, from the first day of a financial year until a later version replaces it (FR-ORG-10; PRD-001 rev 0.62). Fyniq never writes a policy: the words are the accountant's. A version never changes: a correction is a new version, which the accountant approves again. Side effects: the version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Accounting framework and policies (rev 0.62)
- **Parameters:**
  - `area` (path, required): The policy's area.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`AccountingPolicyInput`](../../contracts/schemas/AccountingPolicyInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`AccountingPolicy`](../../contracts/schemas/AccountingPolicy.schema.json)
  - `201` Created [`AccountingPolicy`](../../contracts/schemas/AccountingPolicy.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `ACCOUNTING-02` You wrote it, so another accountant signs it; `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `ACCOUNTING-03` Signed already, or replaced; `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `PERIOD-01` The financial years are not set; `STORAGE-ACCOUNTING-01` A framework or policy version never changes.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `ACCOUNTING-01` Not a framework or policy version that can be recorded; `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `ACCOUNTING-01`, `ACCOUNTING-02`, `ACCOUNTING-03`, `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `PERIOD-01`, `STORAGE-ACCOUNTING-01`

### `POST /v1/organisation/accounting/policies/{area}/{version}/approve`

**Approve an accounting policy** (`approveAccountingPolicy`)

Records the caller's approval, as the accountant, of a version of one accounting policy (FR-ORG-10; PRD-001 rev 0.62). Needs the Accountant role; never its writer, never twice, and never a version a later one from the same year replaces (the founders' decision of 21 Oct 2026; the database refuses the writer too). While the accounting.signing_step_up setting is on (the default), it needs step-up in the last 5 minutes. Side effects: the approval and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Approve on Accounting framework and policies (rev 0.62)
- **Parameters:**
  - `area` (path, required): The policy's area.
  - `version` (path, required): The framework version.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`AccountingPolicy`](../../contracts/schemas/AccountingPolicy.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `ACCOUNTING-02` You wrote it, so another accountant signs it; `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `ACCOUNTING-03` Signed already, or replaced; `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `PERIOD-01` The financial years are not set; `STORAGE-ACCOUNTING-01` A framework or policy version never changes.
  - `422` Unprocessable Entity: `ACCOUNTING-01` Not a framework or policy version that can be recorded; `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `ACCOUNTING-01`, `ACCOUNTING-02`, `ACCOUNTING-03`, `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `PERIOD-01`, `STORAGE-ACCOUNTING-01`

### `GET /v1/organisation/email/messages/{message_id}`

**Get an email message's outcome** (`getEmailMessage`)

The record of an outbound message: its recipient, subject, attempts and outcome, never its body (FR-INT-21). Read-only; no side effects.

- **Permission:** View on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `message_id` (path, required): The message's ID.
- **Responses:**
  - `200` OK [`EmailMessage`](../../contracts/schemas/EmailMessage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `POST /v1/organisation/email/test-messages`

**Send a test email** (`sendTestEmail`)

Queues a test message to the organisation's own email address, from its profile, to check outbound email (FR-INT-21, FR-NTF-05). It goes only to that address, so this cannot send mail anywhere else. Side effects: the message's record, its audit event and its send in the outbox; the worker sends it once, and the record shows the outcome. Idempotent with Idempotency-Key; Prefer: return=preview checks it and queues nothing.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`EmailMessage`](../../contracts/schemas/EmailMessage.schema.json)
  - `201` Created [`EmailMessage`](../../contracts/schemas/EmailMessage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `EML-01` The organisation has no email address; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-RATE-01`, `EML-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/gst-registrations`

**List the GST registrations** (`listGSTRegistrations`)

The organisation's GST registrations with their effective dates, by the date each starts, a page at a time (FR-ORG-03). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`GSTRegistrationPage`](../../contracts/schemas/GSTRegistrationPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/organisation/gst-registrations`

**Record a GST registration** (`createGSTRegistration`)

Records that the organisation is GST-registered from a date, with its registration number, return frequency and quarters, and its accounting basis (FR-ORG-03). A registration may not overlap another (409). Side effects: the registration and its audit event. Approval policy: the Owner approves once the approval engine arrives (P1). Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation profile, financial year, defaults (rev 0.29). The Owner approves GST registration changes; the approval itself arrives with the approval engine (P1).
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`GSTRegistrationDraft`](../../contracts/schemas/GSTRegistrationDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`GSTRegistration`](../../contracts/schemas/GSTRegistration.schema.json)
  - `201` Created [`GSTRegistration`](../../contracts/schemas/GSTRegistration.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DATES-01` The dates overlap another version; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `GST-01` The registration's dates do not fit; `GST-02` The GST registration number is not valid; `GST-03` Not a return frequency; `GST-04` Not a quarters' start month; `GST-05` An accounting basis Fyniq does not offer; `STORAGE-ORG-01` The organisation's details are not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `GST-01`, `GST-02`, `GST-03`, `GST-04`, `GST-05`, `STORAGE-DATES-01`, `STORAGE-LOCK-01`, `STORAGE-ORG-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/gst-registrations/{registration_id}`

**Get a GST registration** (`getGSTRegistration`)

Returns one GST registration with its ETag (FR-ORG-03). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `registration_id` (path, required): The registration's ID.
- **Responses:**
  - `200` OK [`GSTRegistration`](../../contracts/schemas/GSTRegistration.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PATCH /v1/organisation/gst-registrations/{registration_id}`

**Change a GST registration** (`updateGSTRegistration`)

Changes the fields sent and keeps the rest; "" clears the deregistration date. Deregistering is recording deregistered_from (FR-ORG-03). A change may not make it overlap another registration (409). Side effects: the next version and its audit event. Needs If-Match with the registration's ETag. Approval policy: the Owner approves once the approval engine arrives (P1). Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation profile, financial year, defaults (rev 0.29). The Owner approves GST registration changes; the approval itself arrives with the approval engine (P1).
- **Parameters:**
  - `registration_id` (path, required): The registration's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`GSTRegistrationChanges`](../../contracts/schemas/GSTRegistrationChanges.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`GSTRegistration`](../../contracts/schemas/GSTRegistration.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-DATES-01` The dates overlap another version; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `GST-01` The registration's dates do not fit; `GST-02` The GST registration number is not valid; `GST-03` Not a return frequency; `GST-04` Not a quarters' start month; `GST-05` An accounting basis Fyniq does not offer; `STORAGE-ORG-01` The organisation's details are not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `GST-01`, `GST-02`, `GST-03`, `GST-04`, `GST-05`, `STORAGE-DATES-01`, `STORAGE-LOCK-01`, `STORAGE-ORG-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/organisation/gst-status`

**Get the GST status on a date** (`getGSTStatus`)

Whether the organisation is GST-registered on a date, under which registration, and the return period that contains the date, from the registrations' effective dates, return frequency and quarters (FR-ORG-03). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `date` (query, required): The date to ask about, YYYY-MM-DD.
- **Responses:**
  - `200` OK [`GSTStatus`](../../contracts/schemas/GSTStatus.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/organisation/profile`

**Get the organisation's profile** (`getOrganisationProfile`)

What the organisation's documents print about it (legal name, UEN, addresses, contact details, the bank and PayNow details customers pay to, the footer) and how its financial years run (the start month, the books' start, the first year's end) and its time zone (FR-ORG-01, FR-ORG-02), with the organisation's ETag. Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Responses:**
  - `200` OK [`OrganisationProfile`](../../contracts/schemas/OrganisationProfile.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `PATCH /v1/organisation/profile`

**Change the organisation's profile** (`updateOrganisationProfile`)

Changes the fields sent and keeps the rest: a field left out keeps its value, and "" clears it. Every rule that fails is reported at once (UEN, email, time zone, a first financial year that ends after the books start, on the day before a year starts). Side effects: the organisation's next version and an audit event with the changes, bank and PayNow numbers masked. Needs If-Match with the organisation's ETag. Approval policy: the Owner approves profile changes once the approval engine arrives (P1). Once a period exists, the books start, the start month and the first year end no longer change (PERIOD-04). Prefer: return=preview checks the change and saves nothing.

- **Permission:** Create on Organisation profile, financial year, defaults (rev 0.29). The Owner approves changes to the profile; the approval itself arrives with the approval engine (P1).
- **Parameters:**
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ProfileChanges`](../../contracts/schemas/ProfileChanges.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`OrganisationProfile`](../../contracts/schemas/OrganisationProfile.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `PERIOD-04` The financial years are fixed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `ORG-01` A text is blank or too long; `ORG-02` Not a UEN; `ORG-03` Not an email address; `ORG-04` Not a time zone; `ORG-05` Not a month; `ORG-06` The first financial year does not fit; `ORG-07` Not a bank account number; `STORAGE-ORG-01` The organisation's details are not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `ORG-01`, `ORG-02`, `ORG-03`, `ORG-04`, `ORG-05`, `ORG-06`, `ORG-07`, `PERIOD-04`, `STORAGE-LOCK-01`, `STORAGE-ORG-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/organisation/sequences`

**List the document number sequences** (`listSequences`)

One number sequence per document type that is numbered, as set up or with its defaults (FR-ORG-04). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`SequencePage`](../../contracts/schemas/SequencePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/organisation/sequences/{document_type}`

**Get a document number sequence** (`getSequence`)

A document type's number sequence with its ETag; "0" while it still has its defaults (FR-ORG-04). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `document_type` (path, required): The document type.
- **Responses:**
  - `200` OK [`Sequence`](../../contracts/schemas/Sequence.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `PATCH /v1/organisation/sequences/{document_type}`

**Change a document number sequence** (`updateSequence`)

Sets up or changes a document type's number sequence: its prefix, next number (forward only, so a number is never issued twice), padding and whether it is gap-free (FR-ORG-04). Numbers already given never change. Side effects: the sequence's next version and its audit event. Needs If-Match with its ETag ("0" while it has its defaults). Approval policy: the Owner approves once the approval engine arrives (P1). Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation profile, financial year, defaults (rev 0.29). The Owner approves changes to the defaults; the approval itself arrives with the approval engine (P1).
- **Parameters:**
  - `document_type` (path, required): The document type.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SequenceChanges`](../../contracts/schemas/SequenceChanges.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Sequence`](../../contracts/schemas/Sequence.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress; `STORAGE-SEQUENCE-01` The number sequence cannot be set that way.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `ORG-08` Not a prefix; `ORG-09` The next number does not fit; `ORG-10` Not a number of digits.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `ORG-08`, `ORG-09`, `ORG-10`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-SEQUENCE-01`, `STORAGE-VERSION-01`

### `GET /v1/organisation/settings`

**List the organisation settings** (`listSettings`)

Every organisation setting by key, with its value on a day (today when on is left out), its recommended value and whether it holds it, why, its guide section, whether it needs a second approver or is set by the platform, and the changes scheduled after that day (FR-ORG-04, FR-ORG-06, FR-ORG-07, FR-ORG-08). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `on` (query): The day to read the values for; today, in the organisation's time zone, when left out.
- **Responses:**
  - `200` OK [`SettingPage`](../../contracts/schemas/SettingPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/organisation/settings/{key}`

**Get an organisation setting** (`getSetting`)

One setting with its value on a day (today when on is left out), every recorded change and its ETag (FR-ORG-08). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `key` (path, required): The setting's key.
  - `on` (query): The day to read the values for; today, in the organisation's time zone, when left out.
- **Responses:**
  - `200` OK [`Setting`](../../contracts/schemas/Setting.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PATCH /v1/organisation/settings/{key}`

**Change an organisation setting** (`updateSetting`)

Records a new value for a setting, or with reset its recommended value, from a day: today in the organisation's time zone or later, never earlier (FR-ORG-08). Earlier values are kept with their dates. A setting set by the platform never changes. One under dual control (FM-1: thresholds, retention, AI) is not changed at once: the answer is 202 with an approval request, which another member who holds the approval right (an Owner) approves; the change is made then, if nothing it depends on changed (rev 0.36). Asking needs a person signed in who passed step-up in the last 5 minutes. Side effects: the setting's next version and its audit event, with the value before and after; or the approval request and its audit event. Needs If-Match with its ETag ("0" before any change). Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation profile, financial year, defaults (rev 0.29). A setting under dual control (FM-1) waits for another member who holds A to approve it: the request needs step-up (rev 0.36).
- **Parameters:**
  - `key` (path, required): The setting's key.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SettingUpdate`](../../contracts/schemas/SettingUpdate.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Setting`](../../contracts/schemas/Setting.schema.json)
  - `202` Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. The body is the approval request (rev 0.36). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `APR-06` A change to it is waiting for approval already; `SET-04` Set by the platform; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `SET-01` Not a value for this setting; `SET-02` A setting never changes in the past; `SET-03` The setting can only lengthen; `SET-06` Send a value or a reset.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `SET-01`, `SET-02`, `SET-03`, `SET-04`, `SET-06`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

## Chart of accounts

Accounts: what the ledger posts to (PRD-001 §8.2).

### `GET /v1/accounts`

**List the chart of accounts** (`listAccounts`)

Returns the organisation's accounts by code, a page at a time (cursor pagination). Read-only; no side effects.

- **Permission:** View on Chart of accounts, dimensions
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`AccountPage`](../../contracts/schemas/AccountPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/accounts`

**Add an account** (`createAccount`)

Adds an account to the chart (FR-COA-01, FR-COA-02), as a leaf or as a group under which others sit (FR-COA-05). Its code, type and role never change. Side effects: the account and its audit event. Approval policy: the Owner and the Controller approve chart changes once the approval engine arrives (P1). Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Chart of accounts, dimensions. The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1).
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`AccountDraft`](../../contracts/schemas/AccountDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Account`](../../contracts/schemas/Account.schema.json)
  - `201` Created [`Account`](../../contracts/schemas/Account.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-ACCOUNT-01` The account cannot change that way; `STORAGE-ACCOUNT-02` The account cannot sit there; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `COA-01` Not an account code; `COA-02` A text is blank or too long; `COA-03` Not an account type; `COA-04` Not a function; `COA-05` Not a GST code; `COA-06` Not a currency; `COA-07` Not a system role; `COA-08` Changes only with an approval; `COA-09` A system account stays active; `COA-10` The account is not revalued at month end.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `COA-01`, `COA-02`, `COA-03`, `COA-04`, `COA-05`, `COA-06`, `COA-07`, `COA-08`, `COA-09`, `COA-10`, `STORAGE-ACCOUNT-01`, `STORAGE-ACCOUNT-02`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/accounts/{account_id}`

**Get an account** (`getAccount`)

Returns one account with its ETag. Read-only; no side effects.

- **Permission:** View on Chart of accounts, dimensions
- **Parameters:**
  - `account_id` (path, required): The account's ID.
- **Responses:**
  - `200` OK [`Account`](../../contracts/schemas/Account.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PATCH /v1/accounts/{account_id}`

**Change an account** (`updateAccount`)

Changes the fields sent and keeps the rest: the name, the group it sits under, whether it is a group, whether it is active (FR-COA-04: an inactive account takes no new postings; a system account stays active, FR-COA-03), and the FR-COA-02 attributes. Its code, type and role never change. The control and payroll-cost flags change only with an approval, which arrives with the approval engine: refused until then. Side effects: the next version and its audit event. Needs If-Match with the account's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Chart of accounts, dimensions. The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1).
- **Parameters:**
  - `account_id` (path, required): The account's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`AccountChanges`](../../contracts/schemas/AccountChanges.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Account`](../../contracts/schemas/Account.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-ACCOUNT-01` The account cannot change that way; `STORAGE-ACCOUNT-02` The account cannot sit there; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `COA-01` Not an account code; `COA-02` A text is blank or too long; `COA-03` Not an account type; `COA-04` Not a function; `COA-05` Not a GST code; `COA-06` Not a currency; `COA-07` Not a system role; `COA-08` Changes only with an approval; `COA-09` A system account stays active; `COA-10` The account is not revalued at month end.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `COA-01`, `COA-02`, `COA-03`, `COA-04`, `COA-05`, `COA-06`, `COA-07`, `COA-08`, `COA-09`, `COA-10`, `STORAGE-ACCOUNT-01`, `STORAGE-ACCOUNT-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/accounts:load-standard-chart`

**Load the standard chart of accounts** (`loadStandardChart`)

Loads the standard Singapore start-up chart into an organisation that has no account yet: 86 accounts with their report groups, cash-flow lines, functions, GST codes, flags and guidance (FR-COA-01, FR-COA-02; PRD-001 rev 0.31, provisional until the accountant's review). Refused once the organisation has any account. Side effects: the accounts and an audit event for each. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Chart of accounts, dimensions. The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1).
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`ChartLoaded`](../../contracts/schemas/ChartLoaded.schema.json)
  - `201` Created [`ChartLoaded`](../../contracts/schemas/ChartLoaded.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-CHART-01` The organisation has accounts already; `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CHART-01`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

## Journals

Manual journals: drafted, posted and reversed; posted history never changes (FR-GL-12).

### `GET /v1/journals`

**List the journals** (`listJournals`)

The organisation's manual journals at their current version, newest date first (a draft without a date last), a page at a time, narrowed by status, dates and text in the memo or number. Each has its debit totals per currency and who prepared and posted it (FR-GL-01), and its statuses in the workbook's priority order (checks; ENG-20, ENG-21). Read-only; no side effects.

- **Permission:** View on Journals
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only journals with this status.
  - `from` (query): Only journals dated on or after this day.
  - `to` (query): Only journals dated on or before this day.
  - `q` (query): Only journals whose memo or number contains this text, ignoring case.
- **Responses:**
  - `200` OK [`JournalPage`](../../contracts/schemas/JournalPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/journals`

**Save a draft journal** (`createJournal`)

Saves a manual journal as a draft (✎) at version 1. Side effects: a draft document and its audit event; nothing posts until postJournal. A draft is checked only for its amounts and currencies. Idempotent with Idempotency-Key.

- **Permission:** Create on Journals
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`JournalDraft`](../../contracts/schemas/JournalDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Journal`](../../contracts/schemas/Journal.schema.json)
  - `201` Created [`Journal`](../../contracts/schemas/Journal.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/journals/waiting-reversals`

**List the automatic reversals that wait** (`listWaitingReversals`)

The posted journals whose automatic reversal waits, earliest reversal first: no period that is not closed covers its date, so the scheduler cannot post it until that period is opened or reopened (FR-GL-02, FR-AUT-02; PRD-001 rev 0.58), a page at a time. Each is a journal summary with its statuses, the first of which says why it waits. Read-only; no side effects.

- **Permission:** View on Journals
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`JournalPage`](../../contracts/schemas/JournalPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/journals/{journal_id}`

**Get a journal** (`getJournal`)

Returns a manual journal's current version and status, with its ETag, the pending request to post or reverse it, if one waits (approval_asked), and its statuses in the workbook's priority order (checks; ENG-20, ENG-21). Read-only; no side effects.

- **Permission:** View on Journals
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
- **Responses:**
  - `200` OK [`Journal`](../../contracts/schemas/Journal.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `JOURNAL-04` The saved journal cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`, `JOURNAL-04`

### `PUT /v1/journals/{journal_id}`

**Change a draft journal** (`replaceJournal`)

Saves the draft's new content as its next version, recording who saved it (FR-GL-01); the earlier versions are kept. Only a draft changes: a posted journal is corrected by reversal (FR-GL-12). Like createJournal, a draft is checked only for its amounts and currencies. Side effects: the new version and its audit event. Needs If-Match with the draft's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Journals
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`JournalDraft`](../../contracts/schemas/JournalDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Journal`](../../contracts/schemas/Journal.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `JOURNAL-02` The journal is not a draft; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `JOURNAL-04` The saved journal cannot be read; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-04`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/journals/{journal_id}/edit`

**Edit a posted journal** (`editJournal`)

Corrects a posted journal in an open period (FR-GL-12; PRD-001 rev 0.54, provisional): saves the content sent as its next version, reverses the entry it had on that entry's own date, and posts the new version's entry on its date, in one transaction; the journal stays posted, keeps its number, and records who posted the edit, and the database refuses an edit without both postings. Side effects: a new version, two entries, a new ledger revision, balances, audit events with the reason. Needs If-Match with the journal's ETag. An edit needs the approval a void needs: in Founder mode it is refused (JOURNAL-10), and in any mode a new version whose debits are above the self-approval threshold is refused (JOURNAL-07); requestJournalEdit asks for the approval. Idempotent with Idempotency-Key; Prefer: return=preview shows the two entries and saves nothing.

- **Permission:** Post on Journals
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`JournalEdit`](../../contracts/schemas/JournalEdit.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`JournalEdited`](../../contracts/schemas/JournalEdited.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-03` The journal is not posted; `JOURNAL-07` The journal needs another person's approval; `JOURNAL-10` Editing needs a second person; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DOC-04` An edit posts its reversal and its new version together; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-03`, `JOURNAL-04`, `JOURNAL-07`, `JOURNAL-10`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DOC-04`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/journals/{journal_id}/edit-approval`

**Ask for approval to edit a posted journal** (`requestJournalEdit`)

Asks for another person's approval to edit a posted journal with the content sent (FR-GL-12, FM-1, FM-2; PRD-001 rev 0.54, provisional): what editJournal asks for in Founder mode (JOURNAL-10) or above the self-approval threshold (JOURNAL-07). The content must pass the posting rules. The answer is 202 with an approval request whose changes list the date, the description, the debits and each line that changes; it keeps the content, and its envelope holds the content's SHA-256 with the journal's version, status and debits, so any change to the journal voids it. A person who holds A on Journals and did not ask approves it, stepped up; on the approval the edit posts as the approver, with the requester as the one who prepared the new version. Asking needs a person signed in who passed step-up, and If-Match with the journal's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Journals
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`JournalEdit`](../../contracts/schemas/JournalEdit.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `STORAGE-APPROVAL-01` The approval cannot be made that way.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-APPROVAL-01`

### `POST /v1/journals/{journal_id}/post`

**Post a draft journal** (`postJournal`)

Posts a draft: the posting engine turns its current version into a balanced entry, written with the journal's change to posted in one transaction (ENG-02). Side effects: a journal entry, a new ledger revision, balances, audit events. Needs If-Match with the draft's ETag. Approval (PRD-001 rev 0.53): a journal whose debits in the base currency are above the self-approval threshold (S$2,000 by default) is refused (JOURNAL-07): ask for approval with requestJournalPost; at or below it, a Founder-mode post is logged as self-approved for the weekly review (FM-2, FM-3). Idempotent with Idempotency-Key; the same version never posts twice.

- **Permission:** Post on Journals. The Accountant's amount limit and SOD-3 arrive with the approval engine (P1).
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`JournalPosting`](../../contracts/schemas/JournalPosting.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-02` The journal is not a draft; `JOURNAL-07` The journal needs another person's approval; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-04`, `JOURNAL-07`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/journals/{journal_id}/post-approval`

**Ask for approval to post a journal** (`requestJournalPost`)

Asks for another person's approval to post a draft journal at the version read (FR-GL-01, FM-2, SOD-3; PRD-001 rev 0.53, provisional): what postJournal asks for when the journal's debits are above the self-approval threshold (JOURNAL-07). The answer is 202 with an approval request, which a person who holds A on Journals (the Controller), did not ask and did not prepare the journal approves, stepped up; on the approval the journal posts, as the approver. The request holds the journal's version and status and its debits, so a change to the draft voids it. Asking needs a person signed in who passed step-up, and If-Match with the journal's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Journals
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `JOURNAL-02` The journal is not a draft; `JOURNAL-03` The journal is not posted; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-03`, `JOURNAL-04`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/journals/{journal_id}/reverse`

**Reverse a posted journal** (`reverseJournal`)

Voids a posted journal with a reversing entry on the date given (FR-GL-12); posted history never changes. Side effects: the reversing entry, a new ledger revision, balances, audit events with the reason. Needs If-Match with the journal's ETag. In Founder mode voiding needs a second person (FM-1): it is refused (JOURNAL-08), and requestJournalReversal asks for the approval. Idempotent with Idempotency-Key.

- **Permission:** Post on Journals. A reversal voids a posted journal; FM-1's second person arrives with the approval engine (P1).
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`JournalPosting`](../../contracts/schemas/JournalPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-03` The journal is not posted; `JOURNAL-08` Reversing needs a second person; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-03`, `JOURNAL-04`, `JOURNAL-08`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/journals/{journal_id}/reverse-approval`

**Ask for approval to reverse a journal** (`requestJournalReversal`)

Asks for another person's approval to void a posted journal with a reversing entry on the date given, with a reason (FR-GL-12, FM-1; PRD-001 rev 0.53, provisional): what reverseJournal asks for in Founder mode (JOURNAL-08). The answer is 202 with an approval request, which a person who holds A on Journals and did not ask approves, stepped up; on the approval the reversal posts. Asking needs a person signed in who passed step-up, and If-Match with the journal's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Journals
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `JOURNAL-02` The journal is not a draft; `JOURNAL-03` The journal is not posted; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-03`, `JOURNAL-04`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/journals:bulk`

**Save up to 250 draft journals** (`bulkCreateJournals`)

Saves each draft on its own, as createJournal does, and answers with one result per item in the order sent (FR-INT-11): 201 with the draft's ID and ETag, or the item's problem details. An item that is refused takes no other item with it, and is never left out. A request whose items break the schema is refused whole, with every invalid field listed. Side effects: a draft document and its audit event per saved item. Idempotent with Idempotency-Key, which covers the whole request; with Prefer: return=preview every item is checked and none is saved.

- **Permission:** Create on Journals
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`JournalDrafts`](../../contracts/schemas/JournalDrafts.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BulkResults`](../../contracts/schemas/BulkResults.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/opening-balances`

**Get the opening balances** (`getOpeningBalances`)

The organisation's opening balances at the books start (FR-GL-06): none until first saved, then a draft until posted, with who prepared and posted them and their ETag ("0-none" while none is saved). Read-only; no side effects.

- **Permission:** View on Journals
- **Responses:**
  - `200` OK [`OpeningBalances`](../../contracts/schemas/OpeningBalances.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `JOURNAL-05` Not the opening balances.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `JOURNAL-04` The saved journal cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `JOURNAL-04`, `JOURNAL-05`

### `PUT /v1/opening-balances`

**Save the opening balances as a draft** (`replaceOpeningBalances`)

Saves the opening balances as a draft dated at the books start (FR-GL-06; PRD-001 rev 0.49, provisional): the first save creates them, each later one their next version, recording who saved it. Each line is an account and its balance in the base currency, a debit balance positive and a credit balance negative; share capital may take a line (INV-13). Like a journal's draft, they are checked only for their amounts; posting checks the rest. Only a draft changes. Side effects: the document or its new version, and the audit event. Needs If-Match with their ETag ("0-none" before the first save). Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Journals
- **Parameters:**
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`OpeningDraft`](../../contracts/schemas/OpeningDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`OpeningBalances`](../../contracts/schemas/OpeningBalances.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `JOURNAL-05` Not the opening balances.
  - `409` Conflict: `JOURNAL-02` The journal is not a draft; `JOURNAL-06` The books start is not set; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `JOURNAL-04` The saved journal cannot be read; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `JOURNAL-02`, `JOURNAL-04`, `JOURNAL-05`, `JOURNAL-06`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/opening-balances/edit-approval`

**Ask for approval to change the posted opening balances** (`requestOpeningBalancesEdit`)

Asks for another person's approval to change posted opening balances to the content sent (FR-GL-06, FR-GL-12, FM-1; PRD-001 rev 0.66, provisional): FM-1 lists editing opening balances among the high-risk actions, so in every team mode they never change at once. The content must pass the posting rules as posting does (every line in the base currency, netting to zero; share capital allowed, INV-13). The answer is 202 with an approval request whose changes list the memo, the debits and each line that changes; it keeps the content, and its envelope holds the content's SHA-256 with the balances' version, status and debits, so any change to them voids it. A person who holds A on Journals and did not ask approves it, stepped up; on the approval, in one transaction, their next version is saved, the entry they had is reversed on its date and the new version's entry posts at the books start, so the month the books start in must be open; the approver posts it, with the requester as the one who prepared it. Asking needs a person signed in who passed step-up, and If-Match with their ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Journals. A person stepped up in the last 5 minutes; the approval engine has another person who holds A approve it (FM-1).
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`OpeningEdit`](../../contracts/schemas/OpeningEdit.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `JOURNAL-03` The journal is not posted; `JOURNAL-06` The books start is not set; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-INV-03` The period is closed.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `JOURNAL-03`, `JOURNAL-04`, `JOURNAL-06`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `STORAGE-APPROVAL-01`, `STORAGE-INV-03`

### `POST /v1/opening-balances/post`

**Post the opening balances** (`postOpeningBalances`)

Posts the opening balances' current version as entered at the books start, once they net to zero (FR-GL-06, posting rule "Opening balances"): every line in the base currency, share capital allowed (INV-13), the rest of ENG-24 as for a journal. Side effects: the entry, a new ledger revision, balances, audit events. Needs If-Match with their ETag. Posted, they change only with another person's approval (requestOpeningBalancesEdit; FM-1). Idempotent with Idempotency-Key; Prefer: return=preview shows the entry and saves nothing.

- **Permission:** Post on Journals
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`OpeningPosting`](../../contracts/schemas/OpeningPosting.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal; `JOURNAL-05` Not the opening balances.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-02` The journal is not a draft; `JOURNAL-06` The books start is not set; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-04`, `JOURNAL-05`, `JOURNAL-06`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `GET /v1/recurring-journals`

**List the recurring journals** (`listRecurringJournals`)

The organisation's recurring journals by name, a page at a time (FR-GL-02; PRD-001 rev 0.60): each one's schedule, next occurrence, content and statuses. Read-only; no side effects.

- **Permission:** View on Journals
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`RecurringJournalPage`](../../contracts/schemas/RecurringJournalPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/recurring-journals`

**Add a recurring journal** (`createRecurringJournal`)

Adds a recurring journal, active: a journal's memo and lines, whether each draft reverses on the first day of the next month, and a schedule (every 1, 3, 6 or 12 months on a day of the month, from a start, until an optional end; FR-GL-02, FR-AR-07; PRD-001 rev 0.60). The scheduler makes the draft journal of each occurrence, dated on it, which a person posts under the usual approvals: nothing recurring posts by itself, and whoever wrote the content does not approve its drafts (SOD-3). Its content may be incomplete: its statuses say what the posting rules would refuse. Side effects: the recurring journal, its first content version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Journals
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`RecurringJournalInput`](../../contracts/schemas/RecurringJournalInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringJournal`](../../contracts/schemas/RecurringJournal.schema.json)
  - `201` Created [`RecurringJournal`](../../contracts/schemas/RecurringJournal.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `RECURRING-02` The recurring journal is not in that state; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/recurring-journals/{recurring_journal_id}`

**Get a recurring journal** (`getRecurringJournal`)

Returns a recurring journal with its statuses and its ETag (FR-GL-02; PRD-001 rev 0.60). Read-only; no side effects.

- **Permission:** View on Journals
- **Parameters:**
  - `recurring_journal_id` (path, required): The recurring journal's ID.
- **Responses:**
  - `200` OK [`RecurringJournal`](../../contracts/schemas/RecurringJournal.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PUT /v1/recurring-journals/{recurring_journal_id}`

**Change a recurring journal** (`replaceRecurringJournal`)

Replaces a recurring journal's name, schedule and content (FR-GL-02; PRD-001 rev 0.60). A changed memo, lines or reversal is a new content version, written by the caller; drafts already made keep theirs. A changed schedule takes effect after the last occurrence made. Side effects: the next version and its audit event. Needs If-Match with the recurring journal's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Journals
- **Parameters:**
  - `recurring_journal_id` (path, required): The recurring journal's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`RecurringJournalInput`](../../contracts/schemas/RecurringJournalInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringJournal`](../../contracts/schemas/RecurringJournal.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `RECURRING-02` The recurring journal is not in that state; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/recurring-journals/{recurring_journal_id}/pause`

**Pause a recurring journal** (`pauseRecurringJournal`)

Stops an active recurring journal making drafts until it is resumed (FR-GL-02; PRD-001 rev 0.60). Drafts already made stay. Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Journals
- **Parameters:**
  - `recurring_journal_id` (path, required): The recurring journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringJournal`](../../contracts/schemas/RecurringJournal.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `RECURRING-02` The recurring journal is not in that state; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/recurring-journals/{recurring_journal_id}/resume`

**Resume a recurring journal** (`resumeRecurringJournal`)

Starts a paused recurring journal again (FR-GL-02; PRD-001 rev 0.60): its next draft is for its first occurrence on or after today, so the occurrences it missed while paused are skipped, not made late. Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Journals
- **Parameters:**
  - `recurring_journal_id` (path, required): The recurring journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringJournal`](../../contracts/schemas/RecurringJournal.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `RECURRING-02` The recurring journal is not in that state; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

## Periods

Accounting periods and financial years: a posting's period is the month of its date (FR-ORG-02, FR-GL-08).

### `GET /v1/close-checklist-template`

**Get the close checklist template** (`getCloseChecklistTemplate`)

The tasks every month's close checklist has (FR-GL-07; PRD-001 rev 0.68, provisional), in order, each with the role that usually does it and the days after the month's end it is due by: the organisation's latest version, or the platform's recommended template (version 0) while it has saved none. With its ETag. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Responses:**
  - `200` OK [`ChecklistTemplate`](../../contracts/schemas/ChecklistTemplate.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `PUT /v1/close-checklist-template`

**Change the close checklist template** (`replaceCloseChecklistTemplate`)

Saves the close checklist template's next version: every task, in order (FR-GL-07; the founders' decision of 22 Oct 2026: the Accountant or the Controller changes it). A task keeps its key so the months already marked keep their marks; a new task leaves it out and gets one from its title; a task left out is no longer on any open month's checklist. One to 40 tasks, each with a title of up to 200 characters, a detail of up to 500, an owner among the roles that work on the close, and a due day from 0 to 60 after the month's end; every problem is listed at once (CLOSE-11). A version never changes. Side effects: the new version and its audit event. Needs If-Match with the template's ETag ("0" for the recommended template). Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ChecklistTemplateRequest`](../../contracts/schemas/ChecklistTemplateRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`ChecklistTemplate`](../../contracts/schemas/ChecklistTemplate.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `CLOSE-12` No such task on the checklist; `STORAGE-CLOSE-03` The checklist does not change that way; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `CLOSE-11` The checklist template is not valid; `CLOSE-13` Skipping a task needs a reason.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `CLOSE-11`, `CLOSE-12`, `CLOSE-13`, `STORAGE-CLOSE-03`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/periods`

**List the periods** (`listPeriods`)

The organisation's accounting periods, earliest first, a page at a time, each with its status and financial year (FR-ORG-02, FR-GL-08), and the financial year that opens next. A posting's period is the month of its posting date (Appendix B); a date no period covers cannot be posted. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`PeriodPage`](../../contracts/schemas/PeriodPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/periods/{period_id}`

**Get a period** (`getPeriod`)

A period with its status and its ETag. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
- **Responses:**
  - `200` OK [`Period`](../../contracts/schemas/Period.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `GET /v1/periods/{period_id}/checklist`

**Get a month's close checklist** (`getPeriodChecklist`)

A month's close checklist (FR-GL-07, FM-4; PRD-001 rev 0.68, provisional): the current template's tasks, in order, each due the given days after the month's end, with its latest mark: done, skipped for the month with a reason, or open, and who marked it when. The close checks refuse the close while a task is open (Appendix C.2 #58). Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
- **Responses:**
  - `200` OK [`PeriodChecklist`](../../contracts/schemas/PeriodChecklist.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `POST /v1/periods/{period_id}/checklist/{task_key}`

**Mark a task of a month's close checklist** (`markChecklistTask`)

Marks a task of an open month's close checklist done, skipped for the month with a reason (CLOSE-13), or open again (FR-GL-07; PRD-001 rev 0.68, provisional), by the members who run the close checks. Each mark is kept, append-only: the latest is the task's state, and the database refuses one in a month that is not open (STORAGE-CLOSE-03). Run the close checks again afterwards: they record what they found when they ran. Side effects: the mark and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and records nothing.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
  - `task_key` (path, required): The task's key.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ChecklistMark`](../../contracts/schemas/ChecklistMark.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PeriodChecklist`](../../contracts/schemas/PeriodChecklist.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CLOSE-01` The period is not open; `CLOSE-12` No such task on the checklist; `STORAGE-CLOSE-03` The checklist does not change that way; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CLOSE-11` The checklist template is not valid; `CLOSE-13` Skipping a task needs a reason.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CLOSE-01`, `CLOSE-11`, `CLOSE-12`, `CLOSE-13`, `STORAGE-CLOSE-03`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/periods/{period_id}/close`

**Get a period's close** (`getPeriodClose`)

A period's close (FR-GL-07, FM-4): while it is open, the latest run of its close checks, with who ran them, the ledger revision they saw and whether an entry dated in or before the period posted since (stale); once closed, who attested it, the revision at the close and the SHA-256 of its trial-balance snapshot (AUD-04). With the period's ETag. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
- **Responses:**
  - `200` OK [`PeriodCloseState`](../../contracts/schemas/PeriodCloseState.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `POST /v1/periods/{period_id}/close`

**Attest and close a period** (`closePeriod`)

Attests a period's close on its latest checks and closes it, in one transaction (FR-GL-07, FR-GL-08, FM-4; PRD-001 rev 0.50, provisional). The attester is a person who passed step-up in the last 5 minutes and did not run the checks, in every team mode; the database refuses otherwise (FY153). The checks must be clean and of this period, every earlier period closed, and, under the ledger lock, no entry dated on or before the period end may have posted after the checks ran: a back-dated posting and the close never both win (ENG-47, G4-053). Side effects: the period's hard lock, which the database enforces on every posting (INV-03); the close record with the trial balance at the period end as canonical JSON and its SHA-256 (AUD-04); audit events. Needs If-Match with the period's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and closes nothing.

- **Permission:** Approve on Period close / reopen. A person stepped up in the last 5 minutes, who did not run the checks (FM-4).
- **Parameters:**
  - `period_id` (path, required): The period's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CloseRequest`](../../contracts/schemas/CloseRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PeriodCloseState`](../../contracts/schemas/PeriodCloseState.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required; `CLOSE-05` Another founder attests the close.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CLOSE-01` The period is not open; `CLOSE-02` The checks are stale; `CLOSE-03` An earlier period is open; `CLOSE-04` The checks refused the close; `CLOSE-06` Not the checks of this period; `CLOSE-07` The period is not closed; `CLOSE-08` A later period is closed; `STORAGE-CLOSE-01` The close is not attested that way; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-03` The period is already closed, or not closed.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CLOSE-01`, `CLOSE-02`, `CLOSE-03`, `CLOSE-04`, `CLOSE-05`, `CLOSE-06`, `CLOSE-07`, `CLOSE-08`, `STORAGE-CLOSE-01`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-03`, `STORAGE-RETRY-01`

### `POST /v1/periods/{period_id}/close-checks`

**Run a period's close checks** (`runCloseChecks`)

Runs the close checks of an open period and records them with the ledger revision they saw and who ran them: the close's preparer (FR-GL-07; PRD-001 rev 0.50, provisional). Until the checks catalogue arrives they are: every earlier period closed (✖), the balances agreeing with the ledger (✖), the trial balance at the period end netting to zero (✖), draft journals dated in the period (⚠), automatic reversals not posted (⚠), recurring journals without their drafts (⚠, or ✖ when the scheduler failed) or with content the posting rules refuse (⚠), variances against the previous month above both thresholds with no explanation of their totals as they are (✖, FR-GL-14; listPeriodVariances), checklist tasks neither done nor skipped with a reason (✖, FR-GL-07; getPeriodChecklist) and, in the period the books start in, opening balances not posted (⚠). Side effects: the record of the run and its audit event. Needs If-Match with the period's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs them and records nothing.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PeriodCloseState`](../../contracts/schemas/PeriodCloseState.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `CLOSE-05` Another founder attests the close.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CLOSE-01` The period is not open; `CLOSE-02` The checks are stale; `CLOSE-03` An earlier period is open; `CLOSE-04` The checks refused the close; `CLOSE-06` Not the checks of this period; `CLOSE-07` The period is not closed; `CLOSE-08` A later period is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `CLOSE-01`, `CLOSE-02`, `CLOSE-03`, `CLOSE-04`, `CLOSE-05`, `CLOSE-06`, `CLOSE-07`, `CLOSE-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/periods/{period_id}/reopen`

**Ask to reopen a closed period** (`requestPeriodReopen`)

Asks to reopen the latest closed period, with a reason (FR-GL-08, FM-1; PRD-001 rev 0.51, provisional). It never reopens at once: the answer is 202 with an approval request, which a person who holds A on "Period close / reopen" (the Owner or the Controller) and did not ask approves, stepped up. On the approval, under the ledger lock, the period still has to be the latest closed one; it reopens, and the reopening is recorded with the reason, the approval, and the snapshots before (the close's) and after (the trial balance at the period end), whose SHA-256 the database recomputes. Asking needs a person signed in who passed step-up. Needs If-Match with the period's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Create on Period close / reopen. A person stepped up in the last 5 minutes; the approval engine has another person who holds A approve it (FM-1).
- **Parameters:**
  - `period_id` (path, required): The period's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ReopenRequest`](../../contracts/schemas/ReopenRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required; `CLOSE-05` Another founder attests the close.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `CLOSE-01` The period is not open; `CLOSE-02` The checks are stale; `CLOSE-03` An earlier period is open; `CLOSE-04` The checks refused the close; `CLOSE-06` Not the checks of this period; `CLOSE-07` The period is not closed; `CLOSE-08` A later period is closed; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `CLOSE-01`, `CLOSE-02`, `CLOSE-03`, `CLOSE-04`, `CLOSE-05`, `CLOSE-06`, `CLOSE-07`, `CLOSE-08`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/periods/{period_id}/variances`

**List a period's variances** (`listPeriodVariances`)

Flux analysis for the close (FR-GL-14; PRD-001 rev 0.65, provisional): each income or expense account's total for the period against its total for the previous period (the one ending the day before it starts), in the base currency from the balance service, by account code, a page at a time. A variance needs an explanation when its size is more than both the amount and the percentage of the previous total in the organisation's settings (close.variance_amount, close.variance_percent; when the previous total is zero the amount alone decides); each shows its latest explanation and whether that explanation is still of the totals as they are. unexplained counts, over every page, those the close checks refuse the close for (Appendix C.2 #57). Balance-sheet accounts rely on the support the close requires; budget variances arrive with budgets. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`VariancePage`](../../contracts/schemas/VariancePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `POST /v1/periods/{period_id}/variances/{account_id}/explanation`

**Explain a variance** (`explainVariance`)

Records why an income or expense account's total for an open period moved against the previous period (FR-GL-14; PRD-001 rev 0.65, provisional). The request names the two totals as listPeriodVariances showed them: the explanation is of those totals, and is refused when the account's totals are not those now (CLOSE-10), so read the variances again. It replaces the account's earlier explanation for the period; explanations are never changed or removed, and once either total changes the explanation is stale and the close asks for a new one. The database refuses one in a period that is not open or for a balance-sheet account (STORAGE-CLOSE-02). Run the close checks again afterwards: they record what they found when they ran. Side effects: the explanation and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and records nothing.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
  - `account_id` (path, required): The account whose variance is explained.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ExplainVarianceRequest`](../../contracts/schemas/ExplainVarianceRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Variance`](../../contracts/schemas/Variance.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CLOSE-01` The period is not open; `CLOSE-09` The account has no variance to explain; `CLOSE-10` The totals changed since they were read; `STORAGE-CLOSE-02` The variance is not explained that way; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CLOSE-01`, `CLOSE-09`, `CLOSE-10`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `STORAGE-CLOSE-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/periods/{period_id}/year-end`

**Get the year end of a period's financial year** (`getYearEnd`)

The year end of the financial year that holds the period (FR-GL-10; PRD-001 rev 0.69, provisional): how many of its periods are closed, its income, expenses and result so far from the balance service, and the retained earnings it carries forward. Year end is computed, never posted: on the next year's first day revenue and expense accounts start from nothing and the account with the retained-earnings role opens with every earlier year's result, so there is no closing entry and nothing to carry forward; the optional closing entry for statutory presentation waits for the accountant. Amounts are in the base currency as in the trial balance, debits positive. Refused while the financial years are not set (PERIOD-01). Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
- **Responses:**
  - `200` OK [`YearEnd`](../../contracts/schemas/YearEnd.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `PERIOD-01` The financial years are not set.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `PERIOD-01`

### `POST /v1/periods:open-year`

**Open the next financial year** (`openFinancialYear`)

Creates the periods of the next financial year: the first year runs from the books start to the first year end, each later one the twelve months after; its periods are calendar months, the first one short when the books start mid-month (FR-ORG-02; PRD-001 rev 0.47, provisional). The body names the year's first day, as listPeriods' next_year gives it, and any other year is refused, as is a year that starts more than a year after the one that holds today. Once a period exists, when the books start and how the years run no longer change. Side effects: the periods and an audit event for each. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`OpenYearRequest`](../../contracts/schemas/OpenYearRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`FinancialYearOpened`](../../contracts/schemas/FinancialYearOpened.schema.json)
  - `201` Created [`FinancialYearOpened`](../../contracts/schemas/FinancialYearOpened.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `PERIOD-01` The financial years are not set; `PERIOD-02` The year starts too far ahead; `PERIOD-03` Not the next year to open; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-02` A period overlaps another.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `PERIOD-01`, `PERIOD-02`, `PERIOD-03`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-02`, `STORAGE-RETRY-01`

## Exchange rates

Rates into the base currency: the daily feed, the organisation's own rates for a day and its monthly table, and the cascade a posting takes its rate from (FR-GL-05, ENG-40).

### `GET /v1/fx/feed`

**List the daily feed's rates** (`listFeedRates`)

The daily rate feed's rates into the organisation's base currency between two dates, at most 366 days apart (FR-GL-05; PRD-001 rev 0.70): the same for every organisation, each as published, with its RND-RATE rate. The feed publishes business days only. Locally the feed is a directory of rates files; MAS's rates arrive at the cloud gate. A page at a time. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `currency` (query): Only this currency's rates.
  - `from` (query, required): The first day.
  - `to` (query, required): The last day, at most 366 days after the first.
- **Responses:**
  - `200` OK [`FeedRatePage`](../../contracts/schemas/FeedRatePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/fx/money-accounts`

**List the money accounts' balances in other currencies** (`listForeignMoneyAccounts`)

Each bank, card, cash or wallet account's balance in each currency other than the base, with what it is in the books in the base currency, from the balance service (ENG-43; PRD-001 rev 0.74), at the end of the period that holds today, or of the last period when none does. Reconciliation is in the account's currency, with bank import. A page at a time. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`ForeignMoneyAccountPage`](../../contracts/schemas/ForeignMoneyAccountPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/fx/rate`

**Find the rate a posting takes** (`lookupFxRate`)

The rate the cascade finds for a currency and a date (ENG-40; PRD-001 rev 0.70, provisional), as a posting on that date takes it when its document has no rate of its own: the organisation's rate for the day; else the daily feed's for the day, or its latest up to 7 days before (weekends and public holidays); else the monthly table for the month, the average of its rates when there are several. source is what a converted amount records. Refused with FX-01 when there is no rate anywhere. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `currency` (query, required): The foreign currency.
  - `date` (query, required): The date of the posting.
- **Responses:**
  - `200` OK [`FXRateLookup`](../../contracts/schemas/FXRateLookup.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `FX-01`

### `GET /v1/fx/rates`

**List the organisation's exchange rates** (`listFxRates`)

The organisation's own rates (FR-GL-05; PRD-001 rev 0.70, provisional): rates for a day, which override the daily feed's on that date, and the monthly table, used for a date with neither (ENG-40). Every rate is base currency per one unit of the currency, converted from its quote under RND-RATE, with the quote kept. Current rates only, unless withdrawn is true. duplicates lists the months of the monthly table with more than one current rate of a currency (⚠ until all but one are withdrawn). A page at a time. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `currency` (query): Only this currency's rates.
  - `kind` (query): Only rates for a day, or only the monthly table.
  - `from` (query): Only rates dated on or after this day.
  - `to` (query): Only rates dated on or before this day.
  - `withdrawn` (query): Include withdrawn rates.
- **Responses:**
  - `200` OK [`FXRatePage`](../../contracts/schemas/FXRatePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/fx/rates`

**Enter an exchange rate** (`createFxRate`)

Enters a rate for a day or for the monthly table (FR-GL-05, ENG-40; PRD-001 rev 0.70, provisional). The quote is stored as base currency per one unit under RND-RATE, with the quote and its form kept. A day's rate replaces the organisation's current rate for that currency and day, which is withdrawn with the reason "Replaced by a new rate for the same day"; the monthly table keeps every rate, and a second one for a month makes the cascade average them (⚠). A rate is never changed: withdraw it and enter another. Refused for the base currency, or a monthly rate not dated on its month's first day (FX-02). Postings already made keep the rate they took (ENG-14). Side effects: the rate, any rate it replaces, and their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`FXRateEntry`](../../contracts/schemas/FXRateEntry.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`FXRate`](../../contracts/schemas/FXRate.schema.json)
  - `201` Created [`FXRate`](../../contracts/schemas/FXRate.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `FX-03` The rate is withdrawn already; `STORAGE-DUPLICATE-01` Already done; `STORAGE-FX-01` The exchange rate does not change that way; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-02` The exchange rate is not valid; `FX-04` Withdrawing a rate needs a reason.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `FX-02`, `FX-03`, `FX-04`, `STORAGE-DUPLICATE-01`, `STORAGE-FX-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/fx/rates/{rate_id}`

**Get an exchange rate** (`getFxRate`)

One of the organisation's rates, withdrawn or not (FR-GL-05). Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `rate_id` (path, required): The rate's ID.
- **Responses:**
  - `200` OK [`FXRate`](../../contracts/schemas/FXRate.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `POST /v1/fx/rates/{rate_id}/withdraw`

**Withdraw an exchange rate** (`withdrawFxRate`)

Withdraws one of the organisation's rates with a reason (FR-GL-05): the cascade no longer finds it, and it stays listed with who withdrew it, when and why. Needs If-Match with the rate's ETag. A rate never changes otherwise. Postings that took it keep it (ENG-14). Refused when it is withdrawn already (FX-03). Side effects: the withdrawal and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `rate_id` (path, required): The rate's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`WithdrawFXRateRequest`](../../contracts/schemas/WithdrawFXRateRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`FXRate`](../../contracts/schemas/FXRate.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `FX-03` The rate is withdrawn already; `STORAGE-DUPLICATE-01` Already done; `STORAGE-FX-01` The exchange rate does not change that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-02` The exchange rate is not valid; `FX-04` Withdrawing a rate needs a reason.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-02`, `FX-03`, `FX-04`, `STORAGE-DUPLICATE-01`, `STORAGE-FX-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/fx/rates:bulk`

**Enter up to 250 exchange rates** (`bulkCreateFxRates`)

Enters each rate on its own, as createFxRate does, such as the rows of a rates file, and answers with one result per item in the order sent (FR-INT-11): 201 with the rate's ID, or the item's problem details. An item that is refused takes no other item with it, and is never left out. Items run in order, so a later rate for the same day replaces an earlier one. Side effects: each rate, any rate it replaces, and their audit events. Idempotent with Idempotency-Key, which covers the whole request; with Prefer: return=preview every item is checked and none is saved.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`FXRateEntries`](../../contracts/schemas/FXRateEntries.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BulkResults`](../../contracts/schemas/BulkResults.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/periods/{period_id}/fx-revaluation`

**Preview a period's FX revaluation** (`getFxRevaluation`)

The month-end revaluation of the period (FR-GL-04; PRD-001 rev 0.73, provisional): every account marked revalue with a balance in a foreign currency at the period end, from the balance service, at the rate the cascade finds for the period's last day (ENG-40), with its change, RND-FX(open × rate) − carrying; the currencies with no rate; the foreign-currency balances on accounts not marked revalue, which stay as recorded; what a run would post now; and the latest run, with whether a posting dated in or before the period came after it. Read-only; no side effects.

- **Permission:** View on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
- **Responses:**
  - `200` OK [`FXRevaluation`](../../contracts/schemas/FXRevaluation.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `POST /v1/periods/{period_id}/fx-revaluation`

**Revalue a period's foreign-currency balances** (`postFxRevaluation`)

Runs the period's month-end revaluation (FR-GL-04; PRD-001 rev 0.73, provisional) under the ledger lock: records what it read as a revaluation document and posts its entry, dated the period's last day: a base-only line per account and currency that changes, and the losses and gains on the unrealised-FX account (fx_revaluation@1). The entry reverses on the next period's first day, at once when that period is open, else when it opens (the scheduler's fx.reversals). A run with nothing to change posts no entry and still records the revaluation. Run again after a posting dated in or before the period: the new run posts what changed since. The scheduler also runs it by itself (fx.revalue, daily; rev 0.75) once the period has ended and every currency has a rate for its last day, as scheduler:fx.revalue, so a person's run is needed only for a rate entered later. Refused while the period is not open (FX-05), before its last day (FX-06), without an unrealised-FX account (FX-07), or with a currency that has no rate (FX-01). Needs If-Match with the period's ETag, as getFxRevaluation or getPeriod gives it. Answers the revaluation as it then stands. Side effects: the document, its entry, its reversal, and their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and posts nothing.

- **Permission:** Create on Period close / reopen
- **Parameters:**
  - `period_id` (path, required): The period's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`FXRevaluation`](../../contracts/schemas/FXRevaluation.schema.json)
  - `201` Created [`FXRevaluation`](../../contracts/schemas/FXRevaluation.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `FX-05` The period is not open; `FX-06` The period has not ended; `FX-07` No account takes unrealised FX; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `FX-05`, `FX-06`, `FX-07`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

## Reports

Reports read from the ledger at one revision: the general ledger (FR-GL-11), and the statements as they arrive.

### `GET /v1/reports/general-ledger`

**Get the general ledger report** (`getGeneralLedger`)

The general ledger of one to 20 accounts over a range of dates, in the base currency (FR-GL-11; PRD-001 rev 0.52, provisional): each account's balance before the range, its lines in posting order (date, revision, line) with the running balance and each line's source to open it, its debits and credits, and its closing balance; all read at one ledger revision. At most 5,000 lines per account: a wider range is refused (REPORT-01) until the asynchronous export. Year-end (FR-GL-10; rev 0.56, provisional): revenue and expense accounts start each financial year from nothing, and the retained-earnings account's opening includes the profit or loss of every earlier year, computed, never posted; so a range lies within one financial year, and one that crosses a year's start is refused (REPORT-02). Needs an unqualified view of the reports: a view limited to sales, purchases, payroll, a department or the board pack does not reach every line. Read-only; no side effects.

- **Permission:** View on Reports, dashboards. A view of the reports limited to sales, purchases, payroll, a department or the board pack is refused.
- **Parameters:**
  - `account_id` (query): The accounts, one to 20, each as its own account_id parameter.
  - `from` (query, required): The first day.
  - `to` (query, required): The last day.
- **Responses:**
  - `200` OK [`GeneralLedgerReport`](../../contracts/schemas/GeneralLedgerReport.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `REPORT-01` Too many lines for one report; `REPORT-02` The range crosses a financial year.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `REPORT-01`, `REPORT-02`

## Contacts

Customers and vendors (FR-CT-01).

### `GET /v1/contacts`

**List the contacts** (`listContacts`)

The organisation's customers and vendors, in the order they were added, a page at a time; role narrows the list to customers or vendors (FR-CT-01). Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `role` (query): Only customers, or only vendors.
- **Responses:**
  - `200` OK [`ContactPage`](../../contracts/schemas/ContactPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/contacts`

**Add a contact** (`createContact`)

Adds a customer, a vendor or both (FR-CT-01), and lists the existing contacts it may duplicate on name, UEN or tax ID, or email (FR-CT-04); a possible duplicate is flagged, not refused. Side effects: the contact and its audit event. Vendor bank details have their own dual-controlled flow (FR-CT-03). Idempotent with Idempotency-Key; Prefer: return=preview checks it, lists the possible duplicates and saves nothing.

- **Permission:** Create on Contacts and items (rev 0.33)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ContactDraft`](../../contracts/schemas/ContactDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Contact`](../../contracts/schemas/Contact.schema.json)
  - `201` Created [`Contact`](../../contracts/schemas/Contact.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CT-01` The legal name is not valid; `CT-02` Neither a customer nor a vendor; `CT-03` Not an email address; `CT-04` Not a country code; `CT-05` Not a currency; `CT-06` Payment terms out of range; `CT-07` Not a GST code; `CT-08` Not a Peppol participant ID; `CT-09` A text is too long; `CT-10` Not an account that takes postings.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `CT-01`, `CT-02`, `CT-03`, `CT-04`, `CT-05`, `CT-06`, `CT-07`, `CT-08`, `CT-09`, `CT-10`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/contacts/{contact_id}`

**Get a contact** (`getContact`)

Returns a contact with its ETag and the contacts it may duplicate (FR-CT-01, FR-CT-04). Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `contact_id` (path, required): The contact's ID.
- **Responses:**
  - `200` OK [`Contact`](../../contracts/schemas/Contact.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PATCH /v1/contacts/{contact_id}`

**Change a contact** (`updateContact`)

Changes the fields sent and keeps the rest; "" clears a text, -1 the payment terms. active false keeps the contact for its history and offers it for nothing new; a contact is never deleted. Lists the contacts it may then duplicate (FR-CT-04). Side effects: the next version and its audit event. Needs If-Match with the contact's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Contacts and items (rev 0.33)
- **Parameters:**
  - `contact_id` (path, required): The contact's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ContactChanges`](../../contracts/schemas/ContactChanges.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Contact`](../../contracts/schemas/Contact.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `CT-01` The legal name is not valid; `CT-02` Neither a customer nor a vendor; `CT-03` Not an email address; `CT-04` Not a country code; `CT-05` Not a currency; `CT-06` Payment terms out of range; `CT-07` Not a GST code; `CT-08` Not a Peppol participant ID; `CT-09` A text is too long; `CT-10` Not an account that takes postings.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CT-01`, `CT-02`, `CT-03`, `CT-04`, `CT-05`, `CT-06`, `CT-07`, `CT-08`, `CT-09`, `CT-10`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

## Members

Members of the organisation, their roles, and invitations to join it (FR-ADM-01).

### `GET /v1/members`

**List the members** (`listMembers`)

The organisation's active members, in the order they joined, with their names and live roles (FR-ADM-01). Read-only; no side effects.

- **Permission:** View on Users, roles, team mode
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`MemberPage`](../../contracts/schemas/MemberPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/members`

**Join an organisation with an invitation** (`acceptMemberInvitation`)

Redeems an invitation: you (a new user, or the one your sign-in belongs to) become a member of the organisation that made it, with its roles; your session is in that organisation from your next request (FR-ADM-01). Needs a person signed in. Side effects: your user and sign-in identity when new, the membership, the roles and their audit events. A wrong, used, withdrawn or expired code, or an organisation you belong to already, changes nothing. It takes no Idempotency-Key and no preview: its code makes it one-time (FR-INT-08, rev 0.35).

- **Permission:** Create on (joining by invitation). The invitation's code decides; a person signed in, never the AI or an OAuth client.
- **Request body:** [`JoinRequest`](../../contracts/schemas/JoinRequest.schema.json)
- **Responses:**
  - `201` Created [`Joined`](../../contracts/schemas/Joined.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-SUPERADMIN-01` Code refused.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-SUPERADMIN-01`, `API-UNAVAILABLE-01`

### `GET /v1/members/invitations`

**List the pending invitations** (`listMemberInvitations`)

The invitations neither accepted, withdrawn nor expired, oldest first, without their codes (FR-ADM-01). Read-only; no side effects.

- **Permission:** View on Users, roles, team mode
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`MemberInvitationPage`](../../contracts/schemas/MemberInvitationPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/members/invitations`

**Invite someone to the organisation** (`createMemberInvitation`)

Makes a one-time invitation that gives its redeemer the roles named, valid 7 days (FR-ADM-01). Give the code in person or on a call; invitation emails arrive later. A role change falls under FM-1: until the organisation has a second member its Owner acts alone (the founder bootstrap, rev 0.35); after that the answer is 202 with an approval request and the code, which works once another Owner approves the invitation (rev 0.36); asking needs a person signed in who passed step-up in the last 5 minutes. An invitation made before keeps its roles. Side effects: the invitation, or the approval request, and its audit event. It takes no Idempotency-Key and no preview: its code makes it one-time (FR-INT-08, rev 0.35).

- **Permission:** Create on Users, roles, team mode. Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they wait for another Owner to approve them, and the request needs step-up (rev 0.36).
- **Request body:** [`NewMemberInvitation`](../../contracts/schemas/NewMemberInvitation.schema.json)
- **Responses:**
  - `201` Created [`MemberInvitationCode`](../../contracts/schemas/MemberInvitationCode.schema.json)
  - `202` Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. The body is the approval request (rev 0.36). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `APR-06` A change to it is waiting for approval already; `MEM-06` The last Owner keeps the role; `MEM-07` The member holds that role already, or does not hold it; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-MEMBER-01` The membership or role cannot change that way; `TEAM-02` The roles would conflict.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `MEM-01` Not a role a member can hold; `MEM-02` The name is not valid; `MEM-03` No role, or a role named twice; `MEM-04` Not an expiry for these roles.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `APR-06`, `MEM-01`, `MEM-02`, `MEM-03`, `MEM-04`, `MEM-06`, `MEM-07`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-MEMBER-01`, `STORAGE-RETRY-01`, `TEAM-02`

### `DELETE /v1/members/invitations/{invitation_id}`

**Withdraw an invitation** (`withdrawMemberInvitation`)

Ends an unused invitation: its code stops working (FR-ADM-01). Withdrawing grants nothing, so it is never held back for a second approver. Side effects: the withdrawal and its audit event. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Users, roles, team mode
- **Parameters:**
  - `invitation_id` (path, required): The invitation's ID.
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`MemberInvitation`](../../contracts/schemas/MemberInvitation.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/members/{member_id}/roles`

**Give a member a role** (`assignRole`)

Gives a member a role, theirs until it is revoked or, for an external user, until it expires (FR-ADM-01). A role change falls under FM-1: until the organisation has a second member its Owner acts alone, their own roles included (the founder bootstrap, rev 0.35); after that the answer is 202 with an approval request, and another Owner's approval makes the change, if the member's roles did not change meanwhile (rev 0.36); asking needs a person signed in who passed step-up in the last 5 minutes. Side effects: the role and its audit event, or the approval request and its audit event; the member's next request holds the role, and their OAuth tokens are revoked (AI-38). Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Users, roles, team mode. Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they wait for another Owner to approve them, and the request needs step-up (rev 0.36).
- **Parameters:**
  - `member_id` (path, required): The membership's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`RoleGrant`](../../contracts/schemas/RoleGrant.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Member`](../../contracts/schemas/Member.schema.json)
  - `201` Created [`Member`](../../contracts/schemas/Member.schema.json)
  - `202` Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. The body is the approval request (rev 0.36). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `MEM-06` The last Owner keeps the role; `MEM-07` The member holds that role already, or does not hold it; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-MEMBER-01` The membership or role cannot change that way; `TEAM-02` The roles would conflict.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MEM-01` Not a role a member can hold; `MEM-02` The name is not valid; `MEM-03` No role, or a role named twice; `MEM-04` Not an expiry for these roles; `TEAM-06` The override is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `MEM-01`, `MEM-02`, `MEM-03`, `MEM-04`, `MEM-06`, `MEM-07`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-MEMBER-01`, `STORAGE-RETRY-01`, `TEAM-02`, `TEAM-06`

### `DELETE /v1/members/{member_id}/roles/{role}`

**End a member's role** (`revokeRole`)

Ends a member's role; their next request no longer holds it (FR-ADM-01). The organisation always keeps an Owner. A role change falls under FM-1: until the organisation has a second member its Owner acts alone (the founder bootstrap, rev 0.35); after that the answer is 202 with an approval request, and another Owner's approval makes the change, if the member's roles did not change meanwhile (rev 0.36); asking needs a person signed in who passed step-up in the last 5 minutes. Side effects: the revocation and its audit event, or the approval request and its audit event, and the member's OAuth tokens are revoked (AI-38). Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Users, roles, team mode. Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they wait for another Owner to approve them, and the request needs step-up (rev 0.36).
- **Parameters:**
  - `member_id` (path, required): The membership's ID.
  - `role` (path, required): The role to end.
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Member`](../../contracts/schemas/Member.schema.json)
  - `202` Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. The body is the approval request (rev 0.36). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `APR-06` A change to it is waiting for approval already; `MEM-06` The last Owner keeps the role; `MEM-07` The member holds that role already, or does not hold it; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-MEMBER-01` The membership or role cannot change that way.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `MEM-01` Not a role a member can hold; `MEM-02` The name is not valid; `MEM-03` No role, or a role named twice; `MEM-04` Not an expiry for these roles.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `MEM-01`, `MEM-02`, `MEM-03`, `MEM-04`, `MEM-06`, `MEM-07`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-MEMBER-01`, `STORAGE-RETRY-01`

### `GET /v1/team-mode`

**Get the team mode** (`getTeamMode`)

The organisation's team mode (PRD-001 §6.4), the conflicts of §6.3 its members' roles and pending invitations hold now, the overrides that let members keep them in a mode that blocks conflicts, and every version. Founder mode until the first switch. Its ETag is the version. Read-only; no side effects.

- **Permission:** View on Users, roles, team mode
- **Responses:**
  - `200` OK [`TeamMode`](../../contracts/schemas/TeamMode.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `PATCH /v1/team-mode`

**Ask to switch the team mode** (`switchTeamMode`)

Asks for a switch of the team mode (PRD-001 §6.4, FR-ADM-04). The switch is never made at once: the answer is 202 with an approval request, which a member who holds A on "Users, roles, team mode" (an Owner) and is not the requester approves (dual control). A switch to a mode that blocks conflicts (Standard) lists every member whose roles conflict and waits until each is resolved or overridden: send an override with a reason for each conflict that stays (TEAM-01); a pending invitation whose roles conflict is withdrawn first. Enterprise mode is not available yet (TEAM-03). The approval records the overrides, which are reported with the team mode, and every pending approval request of the organisation becomes void, since each was asked for under the old mode. Asking needs a person signed in who passed step-up in the last 5 minutes and If-Match with the team mode's ETag. Side effects: the approval request and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Users, roles, team mode. Always waits for approval by a member who holds A on the row (an Owner), never the requester (§6.4, FR-ADM-04); needs a person signed in who passed step-up in the last 5 minutes.
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`TeamModeSwitch`](../../contracts/schemas/TeamModeSwitch.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress; `TEAM-01` Conflicts of duties block the switch; `TEAM-02` The roles would conflict; `TEAM-05` The organisation is in that mode already.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `TEAM-03` That team mode is not available; `TEAM-06` The override is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `APR-06`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`, `TEAM-01`, `TEAM-02`, `TEAM-03`, `TEAM-05`, `TEAM-06`

## Items

Products and services the organisation sells, with a price per currency (FR-ITM-01).

### `GET /v1/items`

**List the items** (`listItems`)

The organisation's products and services by code, with their prices, a page at a time (FR-ITM-01). Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`ItemPage`](../../contracts/schemas/ItemPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/items`

**Add an item** (`createItem`)

Adds a product or service with its code, name, printed description, unit, revenue account, GST code and a unit price per currency (FR-ITM-01). Side effects: the item and its audit event. Its quantity and sales for the financial year come with the sales documents. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Contacts and items (rev 0.33)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ItemDraft`](../../contracts/schemas/ItemDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Item`](../../contracts/schemas/Item.schema.json)
  - `201` Created [`Item`](../../contracts/schemas/Item.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `ITM-01` Not an item code; `ITM-02` The item's name is not valid; `ITM-03` Neither a product nor a service; `ITM-04` A text is too long; `ITM-05` Not a currency, or priced twice; `ITM-06` Not a unit price; `ITM-07` Not a GST code; `ITM-08` Not a revenue account that takes postings.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `ITM-01`, `ITM-02`, `ITM-03`, `ITM-04`, `ITM-05`, `ITM-06`, `ITM-07`, `ITM-08`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/items/{item_id}`

**Get an item** (`getItem`)

Returns an item with its prices and its ETag (FR-ITM-01). Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `item_id` (path, required): The item's ID.
- **Responses:**
  - `200` OK [`Item`](../../contracts/schemas/Item.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PATCH /v1/items/{item_id}`

**Change an item** (`updateItem`)

Changes the fields sent and keeps the rest; "" clears a text, and prices, when sent, replace every price. The code never changes. active false keeps the item for its history and offers it for nothing new; an item is never deleted. Side effects: the next version and its audit event, with the prices before and after. Needs If-Match with the item's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Contacts and items (rev 0.33)
- **Parameters:**
  - `item_id` (path, required): The item's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ItemChanges`](../../contracts/schemas/ItemChanges.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Item`](../../contracts/schemas/Item.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `ITM-01` Not an item code; `ITM-02` The item's name is not valid; `ITM-03` Neither a product nor a service; `ITM-04` A text is too long; `ITM-05` Not a currency, or priced twice; `ITM-06` Not a unit price; `ITM-07` Not a GST code; `ITM-08` Not a revenue account that takes postings.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `ITM-01`, `ITM-02`, `ITM-03`, `ITM-04`, `ITM-05`, `ITM-06`, `ITM-07`, `ITM-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

## Approvals

Changes that wait for a second approver (FM-1): the envelope each approver signs, their decisions, and the policies that say who approves (FR-APR-01…05, FR-APR-09, FR-APR-10).

### `GET /v1/approval-policies`

**List the approval policies** (`listApprovalPolicies`)

The approval policy in force for each kind of change the approval engine covers: who signs a request, as ordered steps with a quorum of people who hold the approval right (A) on the object's Appendix D row, narrowed to roles or named members (FR-APR-01). The built-in policy, one step with a quorum of one, applies until the organisation changes it. Read-only; no side effects.

- **Permission:** View on Approval policies, AI catalogue
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`ApprovalPolicyPage`](../../contracts/schemas/ApprovalPolicyPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/approval-policies/{object_type}`

**Get an approval policy** (`getApprovalPolicy`)

The approval policy in force for one kind of change, with every version the organisation has had of it and the approved request that made each (FR-APR-01, FR-APR-09). Its ETag is the policy's number. Read-only; no side effects.

- **Permission:** View on Approval policies, AI catalogue
- **Parameters:**
  - `object_type` (path, required): The kind of change the policy covers.
- **Responses:**
  - `200` OK [`ApprovalPolicyDetail`](../../contracts/schemas/ApprovalPolicyDetail.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PATCH /v1/approval-policies/{object_type}`

**Ask to change an approval policy** (`changeApprovalPolicy`)

Asks for the policy's next version: its ordered steps, each a quorum of 1 to 5 people who hold the approval right (A) on the object's Appendix D row, narrowed to roles or to named members, never both (FR-APR-01). A policy never adds anyone Appendix D does not, and its steps must be ones the members can sign (APR-08). The change is never made at once: the answer is 202 with an approval request, which a member who holds A on "Approval policies, AI catalogue" (the Owner or the Controller) and is not the requester approves (FR-APR-09, SOD-8). On its approval the version is added, and every pending request of that kind restarts: the database voids them (FR-APR-10). The policy for approval policies is fixed (APR-09). Asking needs a person signed in who passed step-up in the last 5 minutes and If-Match with the policy's ETag. Side effects: the approval request and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Approval policies, AI catalogue. Always waits for approval by a member who holds A on the row (the Owner or the Controller), never the requester (FR-APR-09, SOD-8); needs a person signed in who passed step-up in the last 5 minutes.
- **Parameters:**
  - `object_type` (path, required): The kind of change the policy covers.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ApprovalPolicyChange`](../../contracts/schemas/ApprovalPolicyChange.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `APR-09` This approval policy is fixed; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `APR-08` The policy's steps cannot be signed.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `APR-08`, `APR-09`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/approvals`

**List the approval requests** (`listApprovals`)

The approval requests you asked for or hold a right on the object of, oldest first: pending ones by default, each saying whether you may approve it now (FR-APR-01, FM-1). Read-only; no side effects.

- **Permission:** View on (approvals). Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6). Lists the requests the caller made or holds a right on the object of.
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): The requests to list: pending (the default), one other status, or all.
- **Responses:**
  - `200` OK [`ApprovalPage`](../../contracts/schemas/ApprovalPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/approvals/{approval_id}`

**Get an approval request** (`getApproval`)

One approval request with exactly what an approver signs: the envelope (RFC 8785 canonical JSON) and its SHA-256, what it changes before and after, and its signatures (FR-APR-02, FR-APR-03, FR-APR-10). Shown to its requester and to whoever holds a right on its object. Read-only; no side effects.

- **Permission:** View on (approvals). Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6). Shows a request to its requester and to whoever holds a right on its object.
- **Parameters:**
  - `approval_id` (path, required): The request's ID.
- **Responses:**
  - `200` OK [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `POST /v1/approvals/{approval_id}/approve`

**Approve a request** (`approveApproval`)

Signs the request's current step and, once every step of its policy has its quorum, carries out its change in the same transaction (FR-APR-01, FR-APR-03, FR-APR-10, FM-1). Needs a person signed in who passed step-up in the last 5 minutes, holds the approval right (A) on the object's Appendix D row, is one the current step names (APR-11), did not ask for it and has not signed it; and envelope_hash must be the request's, and If-Match its ETag. With passkey, your passkey's answer to the request's challenge signs the envelope's hash itself instead of your session's step-up, and the signature keeps the assertion (FR-APR-03, rev 0.44). The envelope is rebuilt from current data with its rows locked: if the record, any dependency or the approval policy changed since the request, nothing executes, the request becomes void and APR-04 says so (FR-APR-05); if an earlier approver no longer holds the right their step needs, APR-10 does. Side effects: the signed approval and its audit event; on the last step, the request approved, the change itself and their audit events, which name the approval. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Approve on (approvals). Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6). Needs A on the object's row, a person signed in who passed step-up in the last 5 minutes, and someone other than the requester (FM-1).
- **Parameters:**
  - `approval_id` (path, required): The request's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ApprovalDecision`](../../contracts/schemas/ApprovalDecision.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required; `APR-01` You asked for it, so another person approves it; `APR-05` You hold no approval right on it; `APR-11` The request's current step needs someone else; `JOURNAL-09` You prepared it, so another person approves it; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `API-PASSKEY-03` The challenge cannot be used; `APR-02` The request was decided already; `APR-03` The request expired; `APR-04` The envelope differs: nothing was changed; `APR-06` A change to it is waiting for approval already; `APR-09` This approval policy is fixed; `APR-10` An earlier approver no longer holds the right: nothing was changed; `JOURNAL-02` The journal is not a draft; `JOURNAL-03` The journal is not posted; `MEM-06` The last Owner keeps the role; `MEM-07` The member holds that role already, or does not hold it; `PACK-07` This release cannot read that pack version; `PACK-09` A pinned statutory test fails; `PACK-10` Activating it needs a second approver; `PACK-13` That pack version is activated already; `SET-04` Set by the platform; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DOC-04` An edit posts its reversal and its new version together; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-MEMBER-01` The membership or role cannot change that way; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused; `APR-07` A rejection needs its reason; `APR-08` The policy's steps cannot be signed; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `MEM-01` Not a role a member can hold; `MEM-02` The name is not valid; `MEM-03` No role, or a role named twice; `MEM-04` Not an expiry for these roles; `SET-01` Not a value for this setting; `SET-02` A setting never changes in the past; `SET-03` The setting can only lengthen; `SET-06` Send a value or a reset; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-PASSKEY-01`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `APR-01`, `APR-02`, `APR-03`, `APR-04`, `APR-05`, `APR-06`, `APR-07`, `APR-08`, `APR-09`, `APR-10`, `APR-11`, `FX-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-03`, `JOURNAL-04`, `JOURNAL-09`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `MEM-01`, `MEM-02`, `MEM-03`, `MEM-04`, `MEM-06`, `MEM-07`, `PACK-07`, `PACK-09`, `PACK-10`, `PACK-13`, `SET-01`, `SET-02`, `SET-03`, `SET-04`, `SET-06`, `STORAGE-APPROVAL-01`, `STORAGE-DOC-02`, `STORAGE-DOC-04`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-MEMBER-01`, `STORAGE-PASSKEY-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`, `TEAM-04`

### `POST /v1/approvals/{approval_id}/challenge`

**Start signing a request with a passkey** (`challengeApproval`)

A challenge for signing the request with your passkey: 16 random bytes followed by the SHA-256 of the request's envelope, so your authenticator signs exactly what you approve or reject (FR-APR-03). Pass the options to navigator.credentials.get, then send the answer with approve or reject, within 5 minutes; it works once, for this request, organisation and envelope only (G4-056). Needs a person signed in to Fyniq in the browser who may decide the request now, and If-Match with the request's ETag, so the challenge is over the version you were shown. Side effects: the challenge, kept for the answer; nothing else.

- **Permission:** Approve on (approvals). Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6). A challenge for signing the request with a passkey: needs A on the object's row and a person signed in to Fyniq in the browser (FR-APR-03).
- **Parameters:**
  - `approval_id` (path, required): The request's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
- **Responses:**
  - `200` OK [`PasskeyChallenge`](../../contracts/schemas/PasskeyChallenge.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required; `APR-01` You asked for it, so another person approves it; `APR-05` You hold no approval right on it; `APR-11` The request's current step needs someone else.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-PASSKEY-02` You have no passkey; `API-PASSKEY-03` The challenge cannot be used; `APR-02` The request was decided already; `APR-03` The request expired; `APR-04` The envelope differs: nothing was changed; `APR-06` A change to it is waiting for approval already; `APR-10` An earlier approver no longer holds the right: nothing was changed; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused; `APR-07` A rejection needs its reason.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-PASSKEY-01`, `API-PASSKEY-02`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `APR-01`, `APR-02`, `APR-03`, `APR-04`, `APR-05`, `APR-06`, `APR-07`, `APR-10`, `APR-11`, `STORAGE-APPROVAL-01`, `STORAGE-PASSKEY-01`

### `POST /v1/approvals/{approval_id}/reject`

**Reject a request** (`rejectApproval`)

Signs a rejection of the request's envelope at its current step, with your reason; the change never executes (FR-APR-03, FM-1). Needs a person signed in who passed step-up in the last 5 minutes, holds the approval right (A) on the object's Appendix D row, is one the current step names, did not ask for it and has not signed it; and envelope_hash must be the request's, and If-Match its ETag. With passkey, your passkey signs the envelope's hash itself (FR-APR-03, rev 0.44). Side effects: the signed rejection, the request rejected and their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Approve on (approvals). Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6). Needs A on the object's row, a person signed in who passed step-up in the last 5 minutes, and someone other than the requester.
- **Parameters:**
  - `approval_id` (path, required): The request's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ApprovalDecision`](../../contracts/schemas/ApprovalDecision.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required; `APR-01` You asked for it, so another person approves it; `APR-05` You hold no approval right on it; `APR-11` The request's current step needs someone else.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `API-PASSKEY-03` The challenge cannot be used; `APR-02` The request was decided already; `APR-03` The request expired; `APR-04` The envelope differs: nothing was changed; `APR-06` A change to it is waiting for approval already; `APR-10` An earlier approver no longer holds the right: nothing was changed; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused; `APR-07` A rejection needs its reason.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-PASSKEY-01`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `APR-01`, `APR-02`, `APR-03`, `APR-04`, `APR-05`, `APR-06`, `APR-07`, `APR-10`, `APR-11`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-PASSKEY-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/approvals/{approval_id}/withdraw`

**Withdraw your request** (`withdrawApproval`)

Withdraws a pending request you asked for: it becomes void with the reason "withdrawn by its requester", and any signatures it has are voided; the change never executes (rev 0.40). Needs the person who asked for it, signed in to Fyniq, and If-Match with its ETag. Side effects: the request void and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** View on (approvals). Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6). Only the person who asked for the change withdraws it, signed in to Fyniq (rev 0.40).
- **Parameters:**
  - `approval_id` (path, required): The request's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required; `APR-01` You asked for it, so another person approves it; `APR-05` You hold no approval right on it; `APR-11` The request's current step needs someone else; `APR-12` Only the person who asked for it withdraws it.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-02` The request was decided already; `APR-03` The request expired; `APR-04` The envelope differs: nothing was changed; `APR-06` A change to it is waiting for approval already; `APR-10` An earlier approver no longer holds the right: nothing was changed; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `APR-07` A rejection needs its reason.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-01`, `APR-02`, `APR-03`, `APR-04`, `APR-05`, `APR-06`, `APR-07`, `APR-10`, `APR-11`, `APR-12`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/review-log`

**List the review log** (`listReviewLog`)

The high-risk actions that executed, oldest first: each approved change (FM-1) and each role change or invitation the Owner made alone before a second member joined (FR-ADM-01), with its review once another founder reviewed it (FM-3). In Founder mode an entry still unreviewed 14 days after it executed is overdue. Shown to whoever holds a right on what each changed, and to the one who acted; never to the AI. Read-only; no side effects.

- **Permission:** View on (approvals). Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6). Lists the high-risk actions the caller may see, by the row of what each changed (FM-3).
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): The entries to list: unreviewed (the default), reviewed, or all.
- **Responses:**
  - `200` OK [`ReviewLogPage`](../../contracts/schemas/ReviewLogPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/review-log/reviews`

**Review high-risk actions** (`reviewLogEntries`)

Records your review of entries of the review log (FM-3): each unreviewed, none you acted on, and each one whose object you hold the approval right (A) on, such as an Owner. Every entry is checked first, and nothing is recorded unless all pass. Needs a person signed in to Fyniq; never the AI. Side effects: a review per entry and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Approve on (approvals). Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6). Needs A on the row of what each entry changed, a person signed in, and never the one who acted (FM-3).
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ReviewLogReview`](../../contracts/schemas/ReviewLogReview.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`ReviewLogReviewed`](../../contracts/schemas/ReviewLogReviewed.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required; `APR-05` You hold no approval right on it; `APR-13` Another person reviews what you did.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-14` Reviewed already; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-05`, `APR-13`, `APR-14`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

## Statutory packs

Statutory parameters as versioned packs from official sources, with pinned tests, and the organisation's activation of each version (ENG-30, ENG-32).

### `GET /v1/statutory-packs`

**List the statutory pack versions** (`listStatutoryPacks`)

Every statutory pack version the release loaded (GST rates and codes, and later CPF, SDL and SHG), by ID: its effective dates, review date and digest, how many of its pinned statutory tests pass, and the organisation's activation of it (ENG-30, ENG-32, FR-ORG-05). Read-only; no side effects.

- **Permission:** View on Statutory parameters
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`StatutoryPackPage`](../../contracts/schemas/StatutoryPackPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/statutory-packs/{version_id}`

**Get a statutory pack version** (`getStatutoryPack`)

One loaded version with its official sources, its verifier and conditions, each pinned statutory test and its result, and the organisation's activation of it (ENG-30, ENG-32). Read-only; no side effects.

- **Permission:** View on Statutory parameters
- **Parameters:**
  - `version_id` (path, required): The version: <pack>@<year>.<n>.
- **Responses:**
  - `200` OK [`StatutoryPackDetail`](../../contracts/schemas/StatutoryPackDetail.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `POST /v1/statutory-packs/{version_id}/activate`

**Ask to activate a statutory pack version** (`activateStatutoryPack`)

Asks for the organisation to adopt a loaded version (ENG-32). Its pinned statutory tests must pass (PACK-09), this release must carry it as it was loaded (PACK-07), and the organisation must not have activated it already (PACK-13). It is never activated at once: the answer is 202 with an approval request, which a member who holds A on "Statutory parameters" (the Owner or the Controller) and is not the proposer approves (SOD-10, FM-1). On the approval the pinned tests run again and the activation is recorded, naming the proposer, the approver and the request. Asking needs a person signed in who passed step-up in the last 5 minutes, and If-Match with the version's ETag ("0" before it is activated). Side effects: the approval request and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Statutory parameters. Always waits for approval by a member who holds A on the row (the Owner or the Controller), never the proposer (ENG-32, SOD-10); needs a person signed in who passed step-up in the last 5 minutes.
- **Parameters:**
  - `version_id` (path, required): The version: <pack>@<year>.<n>.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `PACK-07` This release cannot read that pack version; `PACK-09` A pinned statutory test fails; `PACK-10` Activating it needs a second approver; `PACK-13` That pack version is activated already; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `PACK-07`, `PACK-09`, `PACK-10`, `PACK-13`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

## Evidence

Files that prove what happened, attached to records: scanned, hashed, and append-only once the record is final (FR-DOC-05, FR-DOC-07).

### `GET /v1/journals/{journal_id}/evidence`

**List a journal's evidence** (`listJournalEvidence`)

The files attached to a manual journal, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.

- **Permission:** View on Evidence: add, supersede (rev 0.3). Needs the right to view the journal as well.
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`EvidencePage`](../../contracts/schemas/EvidencePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`

### `POST /v1/journals/{journal_id}/evidence`

**Attach evidence to a journal** (`addJournalEvidence`)

Attaches a file to a manual journal (FR-DOC-05, FR-DOC-11), as multipart/form-data. The file's type is decided from its content; it is scanned for malware and its image metadata, such as location, is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its audit event with the SHA-256 of the file as received and as stored, the uploader, the source and the journal (FR-DOC-07). On a draft the file can be removed; on a posted or reversed journal it is committed to the retention-locked store and is append-only (FR-DOC-13). Needs If-Match with the journal's ETag, so the file goes on the journal as you read it. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and saves nothing. AI and MCP clients cannot upload: they propose doc.attach.

- **Permission:** Create on Evidence: add, supersede (rev 0.3). Needs the right to view the journal as well. AI and MCP clients only propose attaching a document (doc.attach), which a person approves; the evidence rules per activity (FR-DOC-06) arrive later in P1.
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Evidence`](../../contracts/schemas/Evidence.schema.json)
  - `201` Created [`Evidence`](../../contracts/schemas/Evidence.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `DOC-01` Not a file type Fyniq accepts; `DOC-02` The file is empty or larger than 25 MB; `DOC-03` The malware scan flagged the file; `DOC-04` The evidence type is blank, or a text is too long; `DOC-05` Not a data class this record can hold; `DOC-06` Not how a file arrives here; `DOC-07` The picture cannot be read; `DOC-08` Office files with macros are refused; `DOC-09` The office file's archive is not safe to open.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `DOC-01`, `DOC-02`, `DOC-03`, `DOC-04`, `DOC-05`, `DOC-06`, `DOC-07`, `DOC-08`, `DOC-09`, `JOURNAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/journals/{journal_id}/evidence/{evidence_id}`

**Get a journal's evidence file** (`getJournalEvidence`)

Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.

- **Permission:** View on Evidence: add, supersede (rev 0.3). Needs the right to view the journal as well.
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `evidence_id` (path, required): The file's ID.
- **Responses:**
  - `200` OK [`Evidence`](../../contracts/schemas/Evidence.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`

### `DELETE /v1/journals/{journal_id}/evidence/{evidence_id}`

**Remove evidence from a draft journal** (`removeJournalEvidence`)

Takes a file off a draft journal (FR-DOC-07). Once the journal is posted its evidence is append-only, and the database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by the next sweep. Side effects: the removal and its audit event. Needs If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Evidence: add, supersede (rev 0.3). Needs the right to view the journal as well; only while the journal is a draft (FR-DOC-07).
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `evidence_id` (path, required): The file's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Evidence`](../../contracts/schemas/Evidence.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `STORAGE-EVIDENCE-01` Evidence is append-only once its record is final; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`, `STORAGE-EVIDENCE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`
