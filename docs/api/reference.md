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

### `GET /v1/installation/announcements`

**List the announcements to everyone** (`listInstallationAnnouncements`)

Every banner a Super admin published to everyone in the installation, newest first, with its status (FR-NTF-03, FR-ADM-07). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). Read-only; no side effects.

- **Permission:** View on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`AnnouncementList`](../../contracts/schemas/AnnouncementList.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/installation/announcements`

**Publish a banner to everyone** (`publishInstallationAnnouncement`)

Publishes a banner to every organisation's members, or those holding the roles named, such as a maintenance window or an incident (FR-NTF-03, FR-ADM-07), with the same fields and rules as an organisation's. Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). Side effects: the announcement and its event in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and publishes nothing.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`AnnouncementRequest`](../../contracts/schemas/AnnouncementRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Announcement`](../../contracts/schemas/Announcement.schema.json)
  - `201` Created [`Announcement`](../../contracts/schemas/Announcement.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/installation/announcements/{announcement_id}/withdraw`

**Withdraw an announcement to everyone** (`withdrawInstallationAnnouncement`)

Ends one of the installation's banners now, or cancels a scheduled one (FR-NTF-03); NTF-06 when it ended already. Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). Side effects: the withdrawal and its event in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Parameters:**
  - `announcement_id` (path, required): The banner's or announcement's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Announcement`](../../contracts/schemas/Announcement.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `NTF-06` The announcement has ended; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `NTF-06`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

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

## Connectors

Outside services as connectors: the catalogue, the installation's and the organisation's, their lifecycle, keys and tests (FR-INT-20, FR-INT-25…27).

### `GET /v1/connector-catalogue`

**List the connectors of this release** (`listConnectorCatalogue`)

Every connector compiled into this release (FR-INT-25): what it implements and who may make it act, the data classes it sends out and where, its destinations, the secrets it needs (never their values), its webhooks, health check, test action and local substitute, and the JSON Schema of its configuration. Grouped by capability. Read-only; no side effects.

- **Permission:** View on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`CataloguePage`](../../contracts/schemas/CataloguePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/installation/changes`

**List the changes waiting for a Super admin** (`listInstallationChanges`)

Every change to an installation connector, newest first: pending ones wait for the other Super admin, who reads the envelope and signs its SHA-256 (FR-ADM-07, FR-INT-26). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). Read-only; no side effects.

- **Permission:** View on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`InstallationChangePage`](../../contracts/schemas/InstallationChangePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/installation/changes/{change_id}/approve`

**Approve a change to an installation connector** (`approveInstallationChange`)

The other Super admin approves a pending change by signing the SHA-256 of its envelope, and it applies in the same transaction (FR-ADM-07, FR-INT-26); never the person who asked. Refused once it expired, was decided, or the connector changed since it was asked for (CONNECTOR-08). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). The other Super admin approves; nobody approves their own change. Side effects: the decision, the connector's change and their events in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and decides nothing.

- **Permission:** Approve on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). The other Super admin approves; nobody approves their own change.
- **Parameters:**
  - `change_id` (path, required): The change's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`DecideChangeRequest`](../../contracts/schemas/DecideChangeRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-CONNECTOR-02` The other Super admin's approval is needed; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-CONNECTOR-01`, `STORAGE-CONNECTOR-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/installation/changes/{change_id}/reject`

**Reject a change to an installation connector** (`rejectInstallationChange`)

The other Super admin rejects a pending change, signing the SHA-256 of its envelope, with a reason (FR-ADM-07); a new secret value it carried is destroyed. Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). The other Super admin rejects; nobody decides their own change. Side effects: the decision and its event in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and decides nothing.

- **Permission:** Approve on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). The other Super admin rejects; nobody decides their own change.
- **Parameters:**
  - `change_id` (path, required): The change's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`DecideChangeRequest`](../../contracts/schemas/DecideChangeRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`InstallationChange`](../../contracts/schemas/InstallationChange.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-CONNECTOR-02` The other Super admin's approval is needed; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-CONNECTOR-01`, `STORAGE-CONNECTOR-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/installation/connectors`

**List the installation's connectors** (`listInstallationConnectors`)

The connectors a Super admin installed for everyone (FR-INT-20, FR-ADM-07), live and uninstalled, in position order: each one's state, health, position, configuration, which secrets are set (never their values), last test, and whether the release's manifest asks more than it consented to. Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). Read-only; no side effects.

- **Permission:** View on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`ConnectorPage`](../../contracts/schemas/ConnectorPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/installation/connectors`

**Install a connector for everyone** (`installInstallationConnector`)

Installs a connector of the catalogue whose scope is the installation, at the end of its kind's order, consenting to its manifest: the data classes it sends out, its scopes, destinations and webhooks (FR-INT-26). Needs an active Super admin who passed step-up in the last 5 minutes. With two Super admins the answer is 202 with the change the other one approves (FR-ADM-07); with one it applies at once and is marked so in the installation's trail. Side effects: the connector's change, or the change request, and its event in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). With two Super admins every step but pause and test waits for the other's approval.
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`InstallConnectorRequest`](../../contracts/schemas/InstallConnectorRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `201` Created [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `202` Accepted: with two Super admins, the step waits for the other one's approval (FR-ADM-07); nothing changed yet. The body is the change. [`InstallationChange`](../../contracts/schemas/InstallationChange.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-CONNECTOR-02` The other Super admin's approval is needed; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-CONNECTOR-01`, `STORAGE-CONNECTOR-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/installation/connectors/{connector_id}`

**Read one of the installation's connectors** (`getInstallationConnector`)

One installed connector with its ETag (FR-INT-20): as listed, and the manifest diff when the release's asks more. Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). Read-only; no side effects.

- **Permission:** View on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
- **Responses:**
  - `200` OK [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-07`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/installation/connectors/{connector_id}/configuration`

**Configure one of the installation's connectors** (`configureInstallationConnector`)

Replaces the connector's whole configuration, checked against its manifest's JSON Schema (CONNECTOR-03 names the field); never a secret. A connector not yet enabled is tested again before it is enabled; an enabled one keeps running (FR-INT-26). Needs an active Super admin who passed step-up in the last 5 minutes. With two Super admins the answer is 202 with the change the other one approves (FR-ADM-07); with one it applies at once and is marked so in the installation's trail. Side effects: the connector's change, or the change request, and its event in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing. Needs If-Match with its ETag.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). With two Super admins every step but pause and test waits for the other's approval.
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ConfigureConnectorRequest`](../../contracts/schemas/ConfigureConnectorRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `202` Accepted: with two Super admins, the step waits for the other one's approval (FR-ADM-07); nothing changed yet. The body is the change. [`InstallationChange`](../../contracts/schemas/InstallationChange.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-CONNECTOR-02` The other Super admin's approval is needed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-CONNECTOR-01`, `STORAGE-CONNECTOR-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `PUT /v1/installation/connectors/{connector_id}/secrets/{secret}`

**Set a secret of one of the installation's connectors** (`setInstallationConnectorSecret`)

Stores a new value of one of the connector's secrets in Secret Manager at once, as a new version, and records only its reference (FR-INT-20); the value is never shown or kept anywhere else. When it applies, the value it replaces stays valid for the manifest's overlap, then is destroyed; a change that is rejected or expires destroys the new value (FR-INT-26). Needs an active Super admin who passed step-up in the last 5 minutes. With two Super admins the answer is 202 with the change the other one approves (FR-ADM-07); with one it applies at once and is marked so in the installation's trail. Side effects: the connector's change, or the change request, and its event in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing. Needs If-Match with its ETag.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). With two Super admins every step but pause and test waits for the other's approval.
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
  - `secret` (path, required): The secret's name in the manifest.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SetConnectorSecretRequest`](../../contracts/schemas/SetConnectorSecretRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `202` Accepted: with two Super admins, the step waits for the other one's approval (FR-ADM-07); nothing changed yet. The body is the change. [`InstallationChange`](../../contracts/schemas/InstallationChange.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-CONNECTOR-02` The other Super admin's approval is needed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-CONNECTOR-01`, `STORAGE-CONNECTOR-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/installation/connectors/{connector_id}/steps`

**Take a step of an installation connector's lifecycle** (`stepInstallationConnector`)

enable, pause, resume, disable, uninstall, consent (to the release's manifest: an upgrade) or order (its position) (FR-INT-26). Pause takes effect at once for one Super admin with step-up, even with two (the owner's decision of 25 Oct 2026): it stops new work and holds the queue. Disable cancels the queued work its manifest marks cancellable; uninstall also lists the webhooks to remove by hand, and its secrets are destroyed. Needs an active Super admin who passed step-up in the last 5 minutes. With two Super admins the answer is 202 with the change the other one approves (FR-ADM-07); with one it applies at once and is marked so in the installation's trail. Side effects: the connector's change, or the change request, and its event in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing. Needs If-Match with its ETag.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). With two Super admins every step but pause and test waits for the other's approval.
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ConnectorStepRequest`](../../contracts/schemas/ConnectorStepRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `202` Accepted: with two Super admins, the step waits for the other one's approval (FR-ADM-07); nothing changed yet. The body is the change. [`InstallationChange`](../../contracts/schemas/InstallationChange.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-CONNECTOR-02` The other Super admin's approval is needed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-CONNECTOR-01`, `STORAGE-CONNECTOR-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/installation/connectors/{connector_id}/test`

**Test one of the installation's connectors** (`testInstallationConnector`)

Runs the connector's test action through the egress guard (SEC-36): for email, one test message to the organisation's own email address, from its profile, so it reaches nobody else. The result is pass or fail with Fyniq's reason, never what the provider answered, and is recorded on the connector: a passing test of a configured connector lets it be enabled. No second person is needed. Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). Side effects: the test message, the result and its event in the installation's trail. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `EML-01` The organisation has no email address; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `EML-01`, `STORAGE-CONNECTOR-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/organisation/connectors`

**List the organisation's connectors** (`listOrganisationConnectors`)

The connectors the organisation installed, such as its bank feeds (FR-INT-20), live and uninstalled, in position order, as the installation's are listed. Read-only; no side effects.

- **Permission:** View on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`ConnectorPage`](../../contracts/schemas/ConnectorPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/organisation/connectors`

**Install a connector for the organisation** (`installOrganisationConnector`)

Asks to install a connector of the catalogue whose scope is the organisation, consenting to its manifest (FR-INT-26). Asking needs a person signed in who passed step-up in the last 5 minutes: the answer is 202 with the approval request, which another member who holds the approval right (an Owner) approves under FM-1, and the step is taken then. Side effects: the approval request and its event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`InstallConnectorRequest`](../../contracts/schemas/InstallConnectorRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `202` Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. The body is the approval request (rev 0.36). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `APR-06`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-APPROVAL-01`, `STORAGE-CONNECTOR-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/connectors/{connector_id}`

**Read one of the organisation's connectors** (`getOrganisationConnector`)

One of the organisation's installed connectors with its ETag (FR-INT-20). Read-only; no side effects.

- **Permission:** View on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
- **Responses:**
  - `200` OK [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-07`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/organisation/connectors/{connector_id}/configuration`

**Configure one of the organisation's connectors** (`configureOrganisationConnector`)

Asks to replace the connector's whole configuration, checked against its manifest's JSON Schema now (CONNECTOR-03) and again when approved (FR-INT-26). Asking needs a person signed in who passed step-up in the last 5 minutes: the answer is 202 with the approval request, which another member who holds the approval right (an Owner) approves under FM-1, and the step is taken then. Side effects: the approval request and its event. Needs If-Match with its ETag.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ConfigureConnectorRequest`](../../contracts/schemas/ConfigureConnectorRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `202` Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. The body is the approval request (rev 0.36). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `APR-06`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-APPROVAL-01`, `STORAGE-CONNECTOR-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `PUT /v1/organisation/connectors/{connector_id}/secrets/{secret}`

**Set a secret of one of the organisation's connectors** (`setOrganisationConnectorSecret`)

Stores the new value in Secret Manager at once, as a new version never shown again, and asks to make it current (FR-INT-20, FR-INT-26): approved, the value it replaces stays valid for the manifest's overlap; rejected or expired, the new value is destroyed. Asking needs a person signed in who passed step-up in the last 5 minutes: the answer is 202 with the approval request, which another member who holds the approval right (an Owner) approves under FM-1, and the step is taken then. Side effects: the approval request and its event. Needs If-Match with its ETag.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
  - `secret` (path, required): The secret's name in the manifest.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SetConnectorSecretRequest`](../../contracts/schemas/SetConnectorSecretRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `202` Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. The body is the approval request (rev 0.36). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `APR-06`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-APPROVAL-01`, `STORAGE-CONNECTOR-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/organisation/connectors/{connector_id}/steps`

**Take a step of an organisation connector's lifecycle** (`stepOrganisationConnector`)

enable, pause, resume, disable, uninstall, consent or order (FR-INT-26). Pause takes effect at once for one person with step-up and answers 200 (the owner's decision of 25 Oct 2026); every other step is asked for. Asking needs a person signed in who passed step-up in the last 5 minutes: the answer is 202 with the approval request, which another member who holds the approval right (an Owner) approves under FM-1, and the step is taken then. Side effects: the approval request and its event. Needs If-Match with its ETag.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ConnectorStepRequest`](../../contracts/schemas/ConnectorStepRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `202` Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. The body is the approval request (rev 0.36). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `APR-06`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `STORAGE-APPROVAL-01`, `STORAGE-CONNECTOR-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/organisation/connectors/{connector_id}/test`

**Test one of the organisation's connectors** (`testOrganisationConnector`)

Runs the connector's test action through the egress guard (SEC-36) and records the result: pass or fail with Fyniq's reason, never what the provider answered. No second person is needed. Side effects: the result and its event. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `connector_id` (path, required): The installed connector's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Connector`](../../contracts/schemas/Connector.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `CONNECTOR-01` No such connector in this release; `CONNECTOR-07` No such installed connector.
  - `409` Conflict: `CONNECTOR-04` A secret the connector needs is not set; `CONNECTOR-06` The connector cannot take that step now; `CONNECTOR-08` The change cannot be decided or applied now; `EML-01` The organisation has no email address; `STORAGE-CONNECTOR-01` A connector moves only along its lifecycle; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `CONNECTOR-02` The connector is installed at the other scope; `CONNECTOR-03` The configuration does not match the connector; `CONNECTOR-05` Not one of the connector's secrets.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`, `CONNECTOR-01`, `CONNECTOR-02`, `CONNECTOR-03`, `CONNECTOR-04`, `CONNECTOR-05`, `CONNECTOR-06`, `CONNECTOR-07`, `CONNECTOR-08`, `EML-01`, `STORAGE-CONNECTOR-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

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

### `GET /v1/organisation/email`

**Get the email settings** (`getEmailSettings`)

The organisation's outbound email (FR-INT-21; the owner's decisions of 23 and 25 Oct 2026): the enabled email connectors in order, the first primary and the next its failover, the platform's sender and the domains a sender may be chosen on, which the deployment's operators set, with each domain's DNS records and DMARC policy as checked within the hour; and the organisation's sender of each purpose, or the platform's. No provider key is ever shown. Read-only; no side effects.

- **Permission:** View on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Responses:**
  - `200` OK [`EmailSettings`](../../contracts/schemas/EmailSettings.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/email/capture-addresses`

**Get the email-in addresses** (`getCaptureAddresses`)

The organisation's email-in address and the caller's own, on the deployment's capture domain (FR-DOC-12, FR-INT-22; the owner's decisions of 23 Oct 2026): bills and receipts sent or forwarded to one are captured as documents for someone to confirm. Mail from the organisation's members and its contacts' billing and remittance emails is accepted; other senders wait in quarantine. Read-only; no side effects.

- **Permission:** View on Own notifications and preferences (rev 0.6)
- **Responses:**
  - `200` OK [`CaptureAddresses`](../../contracts/schemas/CaptureAddresses.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/organisation/email/capture-addresses/mine`

**Name your own email-in address** (`setMyCaptureAddress`)

Makes the caller's own email-in address in the organisation, such as mei.ling, replacing the one they have, or retires it when the name is left out (FR-INT-22): mail from their own sign-in email to it is captured for them. Refused for a caller who is not a person with a membership (EML-08). The name rules are the organisation address's (EML-05, EML-06, EML-07). Side effects: the address and its audit events. Needs If-Match with mine_etag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CaptureAddressChange`](../../contracts/schemas/CaptureAddressChange.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CaptureAddresses`](../../contracts/schemas/CaptureAddresses.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `EML-07` The name belongs to another organisation; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-05` Not an address's name; `EML-06` A reserved name; `EML-08` Not a person with an address of their own.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `EML-05`, `EML-06`, `EML-07`, `EML-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/organisation/email/capture-addresses/organisation`

**Name the organisation's email-in address** (`setOrganisationCaptureAddress`)

Makes the organisation's email-in address with a readable name, such as bills, replacing the one it has, or retires it when the name is left out (FR-INT-22). A name is 3 to 40 lower-case letters, digits, dots and hyphens (EML-05), not one kept for the mail system (EML-06), and stays with its organisation for good, so another's is refused (EML-07); mail to a replaced address is no longer captured. Side effects: the address and its audit events. Needs If-Match with organisation_etag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CaptureAddressChange`](../../contracts/schemas/CaptureAddressChange.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CaptureAddresses`](../../contracts/schemas/CaptureAddresses.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `EML-07` The name belongs to another organisation; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-05` Not an address's name; `EML-06` A reserved name.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-UNAVAILABLE-01`, `EML-05`, `EML-06`, `EML-07`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/organisation/email/capture-senders`

**Accept or reject a sender at the organisation's email-in address** (`decideCaptureSender`)

Accepts or rejects a sender's mail at the organisation's email-in address (FR-INT-22; the owner's decisions of 23 Oct 2026), the latest decision in force: accepted mail is captured, provided its sender's domain vouches for it (DMARC, or SPF or DKIM without DMARC); rejected mail is refused. The sender's quarantined mail there is accepted or rejected with it, and a rejected message's files deleted. Side effects: the decision, each settled message and their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SenderDecision`](../../contracts/schemas/SenderDecision.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`SenderDecisionResult`](../../contracts/schemas/SenderDecisionResult.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-09` Not one email address.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `EML-09`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/organisation/email/capture-senders/mine`

**Accept or reject a sender at your email-in address** (`decideMyCaptureSender`)

Accepts or rejects a sender's mail at the caller's own email-in address (FR-INT-22), as decideCaptureSender does for the organisation's, and settles their quarantined mail there. Refused for a caller who is not a person with a membership (EML-08). Side effects: the decision, each settled message and their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SenderDecision`](../../contracts/schemas/SenderDecision.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`SenderDecisionResult`](../../contracts/schemas/SenderDecisionResult.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own; `EML-09` Not one email address.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `EML-08`, `EML-09`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/email/inbound-messages`

**List mail received at the email-in addresses** (`listInboundMessages`)

Mail received at the organisation's email-in addresses and its members', newest first, with the status the owner's rules gave it (FR-INT-22): accepted from members and contacts, quarantined from anyone else until someone decides or its days pass, rejected from rejected senders. Its subject is untrusted data, and its text and attachments are listed, never shown here. Read-only; no side effects.

- **Permission:** View on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only messages with this status.
- **Responses:**
  - `200` OK [`InboundMessagePage`](../../contracts/schemas/InboundMessagePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/email/inbound-messages/mine`

**List mail received at your email-in address** (`listMyInboundMessages`)

Mail received at the caller's own email-in address, newest first (FR-INT-22): accepted from their own verified sign-in email, quarantined from anyone else until they decide or its days pass. Empty while they have no address. Read-only; no side effects.

- **Permission:** View on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only messages with this status.
- **Responses:**
  - `200` OK [`InboundMessagePage`](../../contracts/schemas/InboundMessagePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/email/messages`

**List the email delivery log** (`listEmailMessages`)

Every email the organisation queued, newest first: test messages, notifications and digests, each with its recipient, subject, attempts, the provider and the outcome, never its body (FR-NTF-05, FR-INT-21). A refused attempt is retried by the outbox until the last, and a message whose outcome is unknown is never sent again on a guess. Read one for what the provider reported after. Read-only; no side effects.

- **Permission:** View on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only messages with this outcome.
  - `purpose` (query): Only messages of this purpose.
- **Responses:**
  - `200` OK [`EmailMessagePage`](../../contracts/schemas/EmailMessagePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

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

### `PUT /v1/organisation/email/senders/{purpose}`

**Choose a sender** (`setEmailSender`)

Chooses the address a purpose's email is sent from (FR-INT-21; the owner's decision of 23 Oct 2026): an address on one of the deployment's sending domains (EML-03), with a display name, or neither, so the platform's sender is used. Side effects: the choice and its audit event; mail queued afterwards is sent from it. Needs If-Match with the sender's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `purpose` (path, required): The purpose.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`EmailSenderChange`](../../contracts/schemas/EmailSenderChange.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`EmailSender`](../../contracts/schemas/EmailSender.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-02` Not a sender address; `EML-03` Not a domain email is sent from.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `EML-02`, `EML-03`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/email/suppressions`

**List suppressed addresses** (`listEmailSuppressions`)

The addresses no mail is sent to, newest first (FR-INT-21): each bounced hard, complained, or was suppressed by the provider, as its signed webhook reported; a message to one fails without being sent. Released ones too with all=true. Read-only; no side effects.

- **Permission:** View on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `all` (query): true lists released suppressions too.
- **Responses:**
  - `200` OK [`EmailSuppressionPage`](../../contracts/schemas/EmailSuppressionPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/organisation/email/suppressions/{suppression_id}/release`

**Release a suppressed address** (`releaseEmailSuppression`)

Lets mail go to a suppressed address again (FR-INT-21), once its owner has fixed the mailbox or asked for mail: the next message is sent, and a later hard bounce or complaint suppresses it again. Side effects: the release and its audit event. Refused when it was released already. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)
- **Parameters:**
  - `suppression_id` (path, required): The suppression's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`EmailSuppression`](../../contracts/schemas/EmailSuppression.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `EML-04` Released already; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `EML-04`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

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

Loads the standard Singapore start-up chart into an organisation that has no account yet: 87 accounts with their report groups, cash-flow lines, functions, GST codes, flags and guidance (FR-COA-01, FR-COA-02; PRD-001 rev 0.31, provisional until the accountant's review). Refused once the organisation has any account. Side effects: the accounts and an audit event for each. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

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
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-03` The journal is not posted; `JOURNAL-07` The journal needs another person's approval; `JOURNAL-10` Editing needs a second person; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DOC-04` An edit posts its reversal and its new version together; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-03`, `JOURNAL-04`, `JOURNAL-07`, `JOURNAL-10`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DOC-04`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

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
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-02` The journal is not a draft; `JOURNAL-07` The journal needs another person's approval; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-04`, `JOURNAL-07`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

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
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-03` The journal is not posted; `JOURNAL-08` Reversing needs a second person; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-03`, `JOURNAL-04`, `JOURNAL-08`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

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
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-02` The journal is not a draft; `JOURNAL-06` The books start is not set; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-04`, `JOURNAL-05`, `JOURNAL-06`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

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

## Purchases

Bills from suppliers: drafted, approved, posted and voided; posted history never changes (FR-AP-02, FR-GL-12).

### `GET /v1/bill-payments`

**List the payments of bills** (`listBillPayments`)

The payments of suppliers' bills recorded by hand, newest date first, a page at a time, narrowed by status, dates and text in the number (FR-AP-02). Each has its supplier, money account, total and what it settled of each bill. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only payments with this status.
  - `from` (query): Only payments dated on or after this day.
  - `to` (query): Only payments dated on or before this day.
  - `q` (query): Only payments whose number contains this text, ignoring case.
- **Responses:**
  - `200` OK [`BillPaymentPage`](../../contracts/schemas/BillPaymentPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILLPAY-01` Not a payment of bills.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid; `BILLPAY-02` The saved payment cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILLPAY-01`, `BILLPAY-02`

### `POST /v1/bill-payments`

**Record a payment of bills** (`recordBillPayment`)

Records a payment the company made to a supplier, by hand, and posts it at once under the rule bill_payment@1 (FR-AP-02; ENG-41): Dr 2010 with what it settles of each bill at that bill's stored rate, Cr the money account with what left it (at the payment's rate, or what a base-currency account paid for bills in another currency), and the realised FX to 7030; the part that settles what is left of a bill clears its carrying amount, the cent of RND-FX a part leaves on a base-only line. It settles at most what each bill has open (BILLPAY-05); what it paid beyond the bills is kept as a credit with the supplier. The money has left already, so no approval is asked. Side effects: the payment, its number, the entry, an allocation per bill, a new ledger revision, balances, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`BillPaymentDraft`](../../contracts/schemas/BillPaymentDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BillPaymentPosting`](../../contracts/schemas/BillPaymentPosting.schema.json)
  - `201` Created [`BillPaymentPosting`](../../contracts/schemas/BillPaymentPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `BILLPAY-11` A bill waits for its withholding tax decision; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILLPAY-03` The payment's supplier is not a vendor; `BILLPAY-04` Not a posted bill of the supplier in the payment's currency; `BILLPAY-05` More than the bill has open; `BILLPAY-08` A text of the payment is too long; `BILLPAY-09` The payment settles too many bills; `BILLPAY-12` What is left of the bill is above the settlement tolerance; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-24` A line is in another currency; `LEDGER-25` Not an active money account; `LEDGER-26` A payment's amount is not positive or not in its currency; `LEDGER-27` What a base-currency account paid is missing or not wanted; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILLPAY-03`, `BILLPAY-04`, `BILLPAY-05`, `BILLPAY-08`, `BILLPAY-09`, `BILLPAY-11`, `BILLPAY-12`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-24`, `LEDGER-25`, `LEDGER-26`, `LEDGER-27`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `GET /v1/bill-payments/{payment_id}`

**Get a payment of bills** (`getBillPayment`)

Returns a payment recorded by hand with its ETag: its supplier, money account, rate, what it settled of each bill with the carrying amount cleared and the realised FX (ENG-41), the lines its entry wrote (ledger_impact), and the pending request to void it. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `payment_id` (path, required): The payment's ID.
- **Responses:**
  - `200` OK [`BillPayment`](../../contracts/schemas/BillPayment.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILLPAY-01` Not a payment of bills.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `BILLPAY-02` The saved payment cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILLPAY-01`, `BILLPAY-02`

### `POST /v1/bill-payments/{payment_id}/void`

**Void a payment of bills** (`voidBillPayment`)

Voids a recorded payment with a reversing entry on the date given, with a reason, and a reversing allocation for each bill it settled, which reopens the bills (FR-GL-12, FR-AR-18); posted history never changes. The date is not before the payment's (BILLPAY-07). In Founder mode voiding needs a second person (FM-1): it is refused (BILLPAY-10), and requestBillPaymentVoid asks for the approval. Side effects: the reversing entry and allocations, a new ledger revision, balances, audit events with the reason. Needs If-Match with the payment's ETag. Idempotent with Idempotency-Key; Prefer: return=preview voids nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `payment_id` (path, required): The payment's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BillPaymentPosting`](../../contracts/schemas/BillPaymentPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILLPAY-01` Not a payment of bills; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `BILLPAY-06` The payment is not posted; `BILLPAY-10` Voiding needs a second person; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILLPAY-02` The saved payment cannot be read; `BILLPAY-07` The void is dated before the payment; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILLPAY-01`, `BILLPAY-02`, `BILLPAY-06`, `BILLPAY-07`, `BILLPAY-10`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/bill-payments/{payment_id}/void-approval`

**Ask for approval to void a payment of bills** (`requestBillPaymentVoid`)

Asks for another person's approval to void a recorded payment with a reversing entry on the date given, with a reason (FR-GL-12, FM-1): what voidBillPayment asks for in Founder mode (BILLPAY-10). The answer is 202 with an approval request, which a person who holds A on "Bills, vendor credits" and did not ask approves, stepped up; on the approval the void posts and the bills reopen. Asking needs a person signed in who passed step-up, and If-Match with the payment's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `payment_id` (path, required): The payment's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILLPAY-01` Not a payment of bills.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `BILLPAY-06` The payment is not posted; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILLPAY-02` The saved payment cannot be read; `BILLPAY-07` The void is dated before the payment.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `BILLPAY-01`, `BILLPAY-02`, `BILLPAY-06`, `BILLPAY-07`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/bill-reminders`

**List the bills to pay soon** (`listBillReminders`)

The posted bills with something left to pay that are overdue, or fall due or are planned to be paid within the organisation's reminder days (the setting purchases.reminder_days, 3 by default), as at its today, earliest first, a page at a time (FR-AP-13). The Inbox shows them to the members who record bill payments. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`BillReminderPage`](../../contracts/schemas/BillReminderPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/bills`

**List the bills** (`listBills`)

The organisation's bills at their current version, newest date first (a draft without a date last), a page at a time, narrowed by status, dates and text in the number (FR-AP-02). Each has its supplier, total, due date and states. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `kind` (query): Bills, the default, or vendor credits.
  - `status` (query): Only bills with this status.
  - `from` (query): Only bills dated on or after this day.
  - `to` (query): Only bills dated on or before this day.
  - `q` (query): Only bills whose number contains this text, ignoring case.
- **Responses:**
  - `200` OK [`BillPage`](../../contracts/schemas/BillPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`, `BILL-04`

### `POST /v1/bills`

**Save a draft bill** (`createBill`)

Saves a supplier's bill as a draft (✎) at version 1 (FR-AP-02). A draft is checked for its form only; getBill shows what posting it would do, and postBill checks it all. Side effects: a draft document and its audit event; nothing posts until postBill. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`BillDraft`](../../contracts/schemas/BillDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Bill`](../../contracts/schemas/Bill.schema.json)
  - `201` Created [`Bill`](../../contracts/schemas/Bill.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-08` A text of the bill is too long; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `BILL-08`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/bills/{bill_id}`

**Get a bill** (`getBill`)

Returns a bill's current version with its ETag: its supplier, lines and total, its approval, ledger, payment and due states, what stops a draft posting (✖) and the warnings it posts with (⚠: the stated GST against RND-GST-EXCL, a duplicate, a foreign line with no SGD GST stated), the lines posting it writes or wrote (ledger_impact), and the pending request to post or void it. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
- **Responses:**
  - `200` OK [`Bill`](../../contracts/schemas/Bill.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`, `BILL-04`

### `PUT /v1/bills/{bill_id}`

**Change a draft bill** (`replaceBill`)

Saves the draft's new content as its next version, recording who saved it; the earlier versions are kept. Only a draft changes this way. Side effects: the new version and its audit event. Needs If-Match with the draft's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`BillDraft`](../../contracts/schemas/BillDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Bill`](../../contracts/schemas/Bill.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `409` Conflict: `BILL-02` The bill is not a draft; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read; `BILL-08` A text of the bill is too long; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`, `BILL-02`, `BILL-04`, `BILL-08`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/bills/{bill_id}/edit`

**Edit a posted bill** (`editBill`)

Corrects a posted bill in an open period (FR-GL-12): saves the content sent as its next version, reverses the entry it had on that entry's own date, and posts the new version's entry, in one transaction; the bill stays posted, keeps its number, and records who posted the edit. An edit needs the approval a void needs: in Founder mode it is refused (BILL-12), and in any mode a new version whose total is above the self-approval threshold is refused (BILL-09); requestBillEdit asks for the approval. Side effects: a new version, two entries, a new ledger revision, balances, audit events with the reason. Needs If-Match with the bill's ETag. Idempotent with Idempotency-Key; Prefer: return=preview shows the two entries and saves nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`BillEdit`](../../contracts/schemas/BillEdit.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BillEdited`](../../contracts/schemas/BillEdited.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `BILL-02` The bill is not a draft; `BILL-03` The bill is not posted; `BILL-09` The bill needs another person's approval; `BILL-10` Voiding needs a second person; `BILL-12` Editing needs a second person; `BILL-13` Payments settle the bill; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DOC-04` An edit posts its reversal and its new version together; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read; `BILL-05` The bill's supplier is not an active vendor; `BILL-06` Not a GST code for purchases; `BILL-07` The due date is before the bill's date; `BILL-08` A text of the bill is too long; `BILL-14` An input-tax fact of the bill is not one Fyniq reads; `BILL-15` A line's business share is not a percentage from 0 to 100; `BILL-16` The bill does not say what the non-resident supplier's payment is for; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-21` A bill's total is more than zero; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-29` A share of a line's GST is not a percentage from 0 to 100; `LEDGER-30` The bill's withholding tax cannot be worked out; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`, `BILL-02`, `BILL-03`, `BILL-04`, `BILL-05`, `BILL-06`, `BILL-07`, `BILL-08`, `BILL-09`, `BILL-10`, `BILL-12`, `BILL-13`, `BILL-14`, `BILL-15`, `BILL-16`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-21`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-29`, `LEDGER-30`, `STORAGE-DOC-02`, `STORAGE-DOC-04`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/bills/{bill_id}/edit-approval`

**Ask for approval to edit a posted bill** (`requestBillEdit`)

Asks for another person's approval to edit a posted bill with the content sent (FR-GL-12, FM-1, FM-2): what editBill asks for in Founder mode (BILL-12) or above the self-approval threshold (BILL-09). The content must pass the posting rules. The answer is 202 with an approval request whose changes list each field and line that changes and the total payable before and after; it keeps the content, and its envelope holds the content's SHA-256 with the bill's version, status and total, so any change to the bill voids it. A person who holds A on "Bills, vendor credits" and did not ask approves it, stepped up; on the approval the edit posts as the approver, with the requester as the one who prepared the new version. Asking needs a person signed in who passed step-up, and If-Match with the bill's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`BillEdit`](../../contracts/schemas/BillEdit.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `BILL-02` The bill is not a draft; `BILL-03` The bill is not posted; `BILL-13` Payments settle the bill; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read; `BILL-05` The bill's supplier is not an active vendor; `BILL-06` Not a GST code for purchases; `BILL-07` The due date is before the bill's date; `BILL-08` A text of the bill is too long; `BILL-14` An input-tax fact of the bill is not one Fyniq reads; `BILL-15` A line's business share is not a percentage from 0 to 100; `BILL-16` The bill does not say what the non-resident supplier's payment is for; `FX-01` No exchange rate for this currency; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-21` A bill's total is more than zero; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-29` A share of a line's GST is not a percentage from 0 to 100; `LEDGER-30` The bill's withholding tax cannot be worked out.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `BILL-01`, `BILL-02`, `BILL-03`, `BILL-04`, `BILL-05`, `BILL-06`, `BILL-07`, `BILL-08`, `BILL-13`, `BILL-14`, `BILL-15`, `BILL-16`, `FX-01`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-21`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-29`, `LEDGER-30`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/bills/{bill_id}/payment-plan`

**Plan a bill's payment date** (`planBillPayment`)

Plans the date a posted bill is to be paid (FR-AP-13), or unschedules it when planned_on is left out. A plan is kept beside the bill: it posts nothing and changes no version of the bill, and a later plan replaces it (FY175). The bill must have something open, and the date be on or after the bill's (BILL-18). The bills board and the reminders read it; recording the payment settles the bill. Side effects: the plan and an audit event on the bill. Needs If-Match with the bill's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits. A plan posts nothing; paying needs P.
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`BillPlanDraft`](../../contracts/schemas/BillPlanDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Bill`](../../contracts/schemas/Bill.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `BILL-03` The bill is not posted; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-18` The payment date cannot be planned.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-03`, `BILL-18`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/bills/{bill_id}/post`

**Post a draft bill** (`postBill`)

Posts a draft bill under the rule bill@1 (FR-AP-02): Dr each line's account with its net (with its GST when the input tax is not claimable), Dr 2200 with the claimable GST as stated, Cr 2010 with the total, and for a bill in another currency the difference to 7030, written with the bill's change to posted and its number in one transaction (ENG-02). Every problem comes back together. Approval as for journals: a bill whose total in the base currency is above the self-approval threshold is refused (BILL-09): ask with requestBillPost; at or below it, a Founder-mode post is logged as self-approved for the weekly review (FM-2, FM-3). Side effects: the entry, a new ledger revision, balances, audit events. Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BillPosting`](../../contracts/schemas/BillPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `BILL-02` The bill is not a draft; `BILL-03` The bill is not posted; `BILL-09` The bill needs another person's approval; `BILL-10` Voiding needs a second person; `BILL-13` Payments settle the bill; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read; `BILL-05` The bill's supplier is not an active vendor; `BILL-06` Not a GST code for purchases; `BILL-07` The due date is before the bill's date; `BILL-08` A text of the bill is too long; `BILL-14` An input-tax fact of the bill is not one Fyniq reads; `BILL-15` A line's business share is not a percentage from 0 to 100; `BILL-16` The bill does not say what the non-resident supplier's payment is for; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-21` A bill's total is more than zero; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-29` A share of a line's GST is not a percentage from 0 to 100; `LEDGER-30` The bill's withholding tax cannot be worked out; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`, `BILL-02`, `BILL-03`, `BILL-04`, `BILL-05`, `BILL-06`, `BILL-07`, `BILL-08`, `BILL-09`, `BILL-10`, `BILL-13`, `BILL-14`, `BILL-15`, `BILL-16`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-21`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-29`, `LEDGER-30`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/bills/{bill_id}/post-approval`

**Ask for approval to post a bill** (`requestBillPost`)

Asks for another person's approval to post a draft bill at the version read (FM-2, SOD-3): what postBill asks for when the bill's total is above the self-approval threshold (BILL-09). The answer is 202 with an approval request, which a person who holds A on "Bills, vendor credits", did not ask and did not prepare the bill approves, stepped up; on the approval the bill posts, as the approver. The request holds the bill's version, status and total, so a change to the draft voids it. Asking needs a person signed in who passed step-up, and If-Match with the bill's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `BILL-02` The bill is not a draft; `BILL-03` The bill is not posted; `BILL-13` Payments settle the bill; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read; `BILL-05` The bill's supplier is not an active vendor; `BILL-06` Not a GST code for purchases; `BILL-07` The due date is before the bill's date; `BILL-08` A text of the bill is too long; `BILL-14` An input-tax fact of the bill is not one Fyniq reads; `BILL-15` A line's business share is not a percentage from 0 to 100; `BILL-16` The bill does not say what the non-resident supplier's payment is for; `FX-01` No exchange rate for this currency; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-21` A bill's total is more than zero; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-29` A share of a line's GST is not a percentage from 0 to 100; `LEDGER-30` The bill's withholding tax cannot be worked out.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `BILL-01`, `BILL-02`, `BILL-03`, `BILL-04`, `BILL-05`, `BILL-06`, `BILL-07`, `BILL-08`, `BILL-13`, `BILL-14`, `BILL-15`, `BILL-16`, `FX-01`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-21`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-29`, `LEDGER-30`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/bills/{bill_id}/void`

**Void a posted bill** (`voidBill`)

Voids a posted bill with a reversing entry on the date given, with a reason (FR-GL-12); posted history never changes. In Founder mode voiding needs a second person (FM-1): it is refused (BILL-10), and requestBillVoid asks for the approval. Side effects: the reversing entry, a new ledger revision, balances, audit events with the reason. Needs If-Match with the bill's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BillPosting`](../../contracts/schemas/BillPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `BILL-02` The bill is not a draft; `BILL-03` The bill is not posted; `BILL-09` The bill needs another person's approval; `BILL-10` Voiding needs a second person; `BILL-13` Payments settle the bill; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read; `BILL-05` The bill's supplier is not an active vendor; `BILL-06` Not a GST code for purchases; `BILL-07` The due date is before the bill's date; `BILL-08` A text of the bill is too long; `BILL-14` An input-tax fact of the bill is not one Fyniq reads; `BILL-15` A line's business share is not a percentage from 0 to 100; `BILL-16` The bill does not say what the non-resident supplier's payment is for; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-21` A bill's total is more than zero; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-29` A share of a line's GST is not a percentage from 0 to 100; `LEDGER-30` The bill's withholding tax cannot be worked out; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`, `BILL-02`, `BILL-03`, `BILL-04`, `BILL-05`, `BILL-06`, `BILL-07`, `BILL-08`, `BILL-09`, `BILL-10`, `BILL-13`, `BILL-14`, `BILL-15`, `BILL-16`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-21`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-29`, `LEDGER-30`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/bills/{bill_id}/void-approval`

**Ask for approval to void a bill** (`requestBillVoid`)

Asks for another person's approval to void a posted bill with a reversing entry on the date given, with a reason (FR-GL-12, FM-1): what voidBill asks for in Founder mode (BILL-10). The answer is 202 with an approval request, which a person who holds A on "Bills, vendor credits" and did not ask approves, stepped up; on the approval the reversal posts. Asking needs a person signed in who passed step-up, and If-Match with the bill's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `BILL-02` The bill is not a draft; `BILL-03` The bill is not posted; `BILL-13` Payments settle the bill; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-04` The saved bill cannot be read; `BILL-05` The bill's supplier is not an active vendor; `BILL-06` Not a GST code for purchases; `BILL-07` The due date is before the bill's date; `BILL-08` A text of the bill is too long; `BILL-14` An input-tax fact of the bill is not one Fyniq reads; `BILL-15` A line's business share is not a percentage from 0 to 100; `BILL-16` The bill does not say what the non-resident supplier's payment is for; `FX-01` No exchange rate for this currency; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-21` A bill's total is more than zero; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-29` A share of a line's GST is not a percentage from 0 to 100; `LEDGER-30` The bill's withholding tax cannot be worked out.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `BILL-01`, `BILL-02`, `BILL-03`, `BILL-04`, `BILL-05`, `BILL-06`, `BILL-07`, `BILL-08`, `BILL-13`, `BILL-14`, `BILL-15`, `BILL-16`, `FX-01`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-21`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-29`, `LEDGER-30`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/contacts/{contact_id}/withholding-decisions`

**List a supplier's withholding decisions** (`listWithholdingDecisions`)

The tax agent's decisions on whether tax is withheld from a non-resident supplier's payments, one per nature of payment in force at a time, newest first, a page at a time (FR-AP-19). A bill of the supplier posts with the decision in force for its nature when it posts. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `contact_id` (path, required): The contact's ID.
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`WithholdingDecisionPage`](../../contracts/schemas/WithholdingDecisionPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/contacts/{contact_id}/withholding-decisions`

**Record the tax agent's withholding decision** (`recordWithholdingDecision`)

Records the tax agent's decision for a supplier resident outside Singapore and a nature of payment (FR-AP-19): not applicable, withhold at the rate they advised, or a reduced treaty rate with a certificate of residence. It replaces the supplier's earlier decision for the nature from now: bills posted before keep theirs, and a posted bill whose rate it changes waits for an edit before it is paid (BILLPAY-11). Attach the tax agent's advice to the bill named. A decision is never changed or removed (FY174). If-Match is the supplier's ETag, so the decision is recorded for the tax residence that was read. Side effects: the decision and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits. Names the tax agent who gave it; their advice is attached to the bill as evidence. AI and MCP clients never record one (§9.6).
- **Parameters:**
  - `contact_id` (path, required): The contact's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`WithholdingDecisionDraft`](../../contracts/schemas/WithholdingDecisionDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`WithholdingDecision`](../../contracts/schemas/WithholdingDecision.schema.json)
  - `201` Created [`WithholdingDecision`](../../contracts/schemas/WithholdingDecision.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `BILL-17` The withholding decision cannot be recorded.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-17`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/credit-applications`

**Apply a supplier credit to a bill** (`applyCredit`)

Applies a credit with a supplier (a posted vendor credit, or what a payment of bills left unapplied) to one of the supplier's posted bills in its currency (FR-AP-03, FR-AR-18): it settles both by the amount, at most what the bill has open and what is left of the credit (CREDIT-05), and posts under credit_application@1 only what their stored rates make differ, the realised FX to 7030 (ENG-41); in the base currency it posts nothing. Side effects: the application, two allocations, the entry when one posts, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview applies nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CreditApplicationDraft`](../../contracts/schemas/CreditApplicationDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CreditApplicationPosting`](../../contracts/schemas/CreditApplicationPosting.schema.json)
  - `201` Created [`CreditApplicationPosting`](../../contracts/schemas/CreditApplicationPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CREDIT-03` Not a posted vendor credit or a payment with an amount unapplied; `CREDIT-04` Not a posted bill of the credit's supplier in its currency; `CREDIT-05` More than the bill has open or the credit has left; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-26` A payment's amount is not positive or not in its currency; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CREDIT-03`, `CREDIT-04`, `CREDIT-05`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-26`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `GET /v1/credit-applications/{application_id}`

**Get a credit applied to a bill** (`getCreditApplication`)

Returns an application of a supplier credit to a bill with its ETag: the credit and the bill, the amount, the carrying amounts it cleared, the realised FX (ENG-41), the lines its entry wrote if any, and the pending request to void it. Read-only.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `application_id` (path, required): The application's ID.
- **Responses:**
  - `200` OK [`CreditApplication`](../../contracts/schemas/CreditApplication.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `CREDIT-01` Not a credit application.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `CREDIT-02` The saved application cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `CREDIT-01`, `CREDIT-02`

### `POST /v1/credit-applications/{application_id}/void`

**Void a credit applied to a bill** (`voidCreditApplication`)

Voids an application on the date given, with a reason (FR-GL-12): its entry, if it posted one, is reversed, and each of its allocations is reversed on that date, which reopens the bill and the credit by the amount applied. The date is not before the application's (CREDIT-07). In Founder mode voiding needs a second person (FM-1): it is refused (CREDIT-09), and requestCreditApplicationVoid asks for the approval. Needs If-Match with the application's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `application_id` (path, required): The application's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CreditApplicationPosting`](../../contracts/schemas/CreditApplicationPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `CREDIT-01` Not a credit application; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CREDIT-06` The application is not posted; `CREDIT-09` Voiding needs a second person; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CREDIT-02` The saved application cannot be read; `CREDIT-07` The void is dated before the application; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CREDIT-01`, `CREDIT-02`, `CREDIT-06`, `CREDIT-07`, `CREDIT-09`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/credit-applications/{application_id}/void-approval`

**Ask for approval to void a credit applied to a bill** (`requestCreditApplicationVoid`)

Asks for another person's approval to void an application on the date given, with a reason (FR-GL-12, FM-1): what voidCreditApplication asks for in Founder mode (CREDIT-09). The answer is 202 with an approval request, which a person who holds A on "Bills, vendor credits" and did not ask approves, stepped up; on the approval the void posts and the bill and the credit reopen. Needs a person signed in who passed step-up, and If-Match with the application's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `application_id` (path, required): The application's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `CREDIT-01` Not a credit application.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `CREDIT-06` The application is not posted; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CREDIT-02` The saved application cannot be read; `CREDIT-07` The void is dated before the application.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `CREDIT-01`, `CREDIT-02`, `CREDIT-06`, `CREDIT-07`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/payables`

**Get AP ageing and the bills to pay** (`getPayables`)

What the company owes its suppliers as at a day (FR-AP-06), from the AP sub-ledger (INV-07): each supplier's open bills by days past due (current, 1–30, 31–60, 61–90, 90+) and unapplied credits, by currency, with the totals and their carrying amounts in the base currency; the open bills overdue or due within the window after the day, by due date; and the cash they need by currency. A bill falls due on its due date as entered, else by the supplier's or the organisation's payment terms. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `as_at` (query): The day; left out, the organisation's today.
  - `within` (query): The window of the bills to pay and the cash requirement, in days after the day.
- **Responses:**
  - `200` OK [`Payables`](../../contracts/schemas/Payables.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/purchase-orders`

**List the purchase orders** (`listPurchaseOrders`)

The organisation's purchase orders, newest date first, a page at a time (FR-AP-01; PRD-001 rev 0.87): each one's states, total and what bills take of its lines. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only orders with this status.
- **Responses:**
  - `200` OK [`PurchaseOrderPage`](../../contracts/schemas/PurchaseOrderPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/purchase-orders`

**Raise a purchase order** (`createPurchaseOrder`)

Saves a draft purchase order (FR-AP-01; PRD-001 rev 0.87): the supplier, a reference, its date, when the goods are expected, the currency, and lines with a quantity, a unit price and a GST code. It may be incomplete: getPurchaseOrder says what stops it being approved. It posts nothing, ever. Side effects: the document and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`PurchaseOrderDraft`](../../contracts/schemas/PurchaseOrderDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PurchaseOrder`](../../contracts/schemas/PurchaseOrder.schema.json)
  - `201` Created [`PurchaseOrder`](../../contracts/schemas/PurchaseOrder.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `PO-09` The order's text is too long.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `PO-09`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/purchase-orders/{purchase_order_id}`

**Get a purchase order** (`getPurchaseOrder`)

Returns a purchase order with its states, what stops a draft being approved, what bills take of each line and the bills made from it, with its ETag (FR-AP-01). Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `purchase_order_id` (path, required): The order's ID.
- **Responses:**
  - `200` OK [`PurchaseOrder`](../../contracts/schemas/PurchaseOrder.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `PO-01` Not a purchase order.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `PO-01`

### `PUT /v1/purchase-orders/{purchase_order_id}`

**Change a draft purchase order** (`replacePurchaseOrder`)

Saves a draft order's new content as its next version (FR-AP-01). An approved order never changes: close it and raise another (PO-10). Side effects: the new version and its audit event. Needs If-Match with the order's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `purchase_order_id` (path, required): The order's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`PurchaseOrderDraft`](../../contracts/schemas/PurchaseOrderDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PurchaseOrder`](../../contracts/schemas/PurchaseOrder.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `PO-01` Not a purchase order.
  - `409` Conflict: `PO-10` The order is not a draft; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `PO-09` The order's text is too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `PO-01`, `PO-09`, `PO-10`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/purchase-orders/{purchase_order_id}/approval`

**Ask for approval of a purchase order** (`requestPurchaseOrderApproval`)

Asks for another person's approval of a draft order at the version read (FM-2, SOD-3): what approvePurchaseOrder asks for when the order's total is above the self-approval threshold (PO-14). The answer is 202 with an approval request, which a person who holds A on "Bills, vendor credits", did not ask and did not prepare the order approves, stepped up; on the approval the order is approved, by the approver. The request holds the order's version, status and total in the base currency, so a change to the draft voids it. Asking needs a person signed in who passed step-up, and If-Match with the order's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `purchase_order_id` (path, required): The order's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `PO-01` Not a purchase order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `PO-10` The order is not a draft; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `PO-03` The order's supplier is not an active vendor; `PO-04` The order has no date; `PO-05` The order has no lines, or no total; `PO-06` A line's account cannot take it; `PO-07` A line's quantity or price is not one an order takes; `PO-08` Not a GST code for purchases; `PO-09` The order's text is too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `PO-01`, `PO-03`, `PO-04`, `PO-05`, `PO-06`, `PO-07`, `PO-08`, `PO-09`, `PO-10`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/purchase-orders/{purchase_order_id}/approve`

**Approve a purchase order** (`approvePurchaseOrder`)

Approves a draft order as a bill is posted (FR-AP-01, FM-2; the owner's decision of 23 Oct 2026): it takes its number and is approved by the caller, and posts nothing. Every problem comes back together. An order whose total in the base currency is above the self-approval threshold is refused (PO-14): ask with requestPurchaseOrderApproval; at or below it, an approval in Founder mode is logged as self-approved for the weekly review (FM-3). Side effects: the document's change to approved and its audit events. Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key; Prefer: return=preview approves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `purchase_order_id` (path, required): The order's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PurchaseOrder`](../../contracts/schemas/PurchaseOrder.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `PO-01` Not a purchase order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `PO-10` The order is not a draft; `PO-11` The order is not approved, or is closed; `PO-14` The order needs another person's approval; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `PO-02` The saved order cannot be read; `PO-03` The order's supplier is not an active vendor; `PO-04` The order has no date; `PO-05` The order has no lines, or no total; `PO-06` A line's account cannot take it; `PO-07` A line's quantity or price is not one an order takes; `PO-08` Not a GST code for purchases; `PO-09` The order's text is too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `PO-01`, `PO-02`, `PO-03`, `PO-04`, `PO-05`, `PO-06`, `PO-07`, `PO-08`, `PO-09`, `PO-10`, `PO-11`, `PO-14`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/purchase-orders/{purchase_order_id}/bill`

**Make a bill from a purchase order** (`billPurchaseOrder`)

Makes a draft bill from an approved order (FR-AP-01; the owner's decision of 23 Oct 2026: billed by quantity): of each line, the quantity asked for, or everything no bill takes yet. Each bill line names its order line and quantity; its amount is the part by cumulative RND-LINE, so a line's parts add up to its amount, and its GST is what the supplier is expected to charge. Enter the supplier's invoice date and number, correct the amounts to the invoice, and post it as any bill: a posted bill never takes more of a line than is left (PO-13). Side effects: the draft bill and its audit event. Needs If-Match with the order's ETag. Idempotent with Idempotency-Key; Prefer: return=preview makes nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `purchase_order_id` (path, required): The order's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`OrderBillRequest`](../../contracts/schemas/OrderBillRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Bill`](../../contracts/schemas/Bill.schema.json)
  - `201` Created [`Bill`](../../contracts/schemas/Bill.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `PO-01` Not a purchase order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `PO-10` The order is not a draft; `PO-11` The order is not approved, or is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `PO-02` The saved order cannot be read; `PO-12` More than is left of the order.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `PO-01`, `PO-02`, `PO-10`, `PO-11`, `PO-12`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/purchase-orders/{purchase_order_id}/close`

**Close a purchase order** (`closePurchaseOrder`)

Closes an approved order, with a reason (FR-AP-01): no more bills are made from it, and a draft bill made from it no longer posts; the bills posted already stand. Side effects: the document's change to closed and its audit event with the reason. Needs If-Match with the order's ETag. Idempotent with Idempotency-Key; Prefer: return=preview closes nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `purchase_order_id` (path, required): The order's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`OrderCloseRequest`](../../contracts/schemas/OrderCloseRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PurchaseOrder`](../../contracts/schemas/PurchaseOrder.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `PO-01` Not a purchase order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `PO-10` The order is not a draft; `PO-11` The order is not approved, or is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `PO-02` The saved order cannot be read.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `PO-01`, `PO-02`, `PO-10`, `PO-11`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/purchases/gst-codes`

**Get the GST codes for purchases** (`getPurchaseGSTCodes`)

The GST codes a bill dated on the day given is read with (FR-AP-02, ENG-31): the organisation's active sg-gst version in force then, else the one this release carries; its standard rate on the day; and each code for purchases with whether its input tax can be claimed. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `on` (query): The bill's date; left out, the codes of the newest version.
- **Responses:**
  - `200` OK [`PurchaseGSTCodes`](../../contracts/schemas/PurchaseGSTCodes.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/recurring-bills`

**List the recurring bills** (`listRecurringBills`)

The organisation's recurring bills by name, a page at a time (FR-AP-09; PRD-001 rev 0.84): each one's schedule, next occurrence, content and statuses. Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`RecurringBillPage`](../../contracts/schemas/RecurringBillPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/recurring-bills`

**Add a recurring bill** (`createRecurringBill`)

Adds a recurring bill, active: a bill's supplier, currency, lines and notes, and a schedule (every 1, 3, 6 or 12 months on a day of the month, from a start, until an optional end; FR-AP-09, FR-AR-07; PRD-001 rev 0.84). The scheduler makes the draft bill of each occurrence, dated on it, as scheduler:bills.recurring; a person adds the supplier's number and posts it under the usual approvals: nothing recurring posts by itself, and whoever wrote the content does not approve its drafts (SOD-3). Its content may be incomplete: its statuses say what the bill rules would refuse. Side effects: the recurring bill, its first content version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`RecurringBillInput`](../../contracts/schemas/RecurringBillInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringBill`](../../contracts/schemas/RecurringBill.schema.json)
  - `201` Created [`RecurringBill`](../../contracts/schemas/RecurringBill.schema.json)
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

### `GET /v1/recurring-bills/{recurring_bill_id}`

**Get a recurring bill** (`getRecurringBill`)

Returns a recurring bill with its statuses and its ETag (FR-AP-09; PRD-001 rev 0.84). Read-only; no side effects.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `recurring_bill_id` (path, required): The recurring bill's ID.
- **Responses:**
  - `200` OK [`RecurringBill`](../../contracts/schemas/RecurringBill.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PUT /v1/recurring-bills/{recurring_bill_id}`

**Change a recurring bill** (`replaceRecurringBill`)

Replaces a recurring bill's name, schedule and content (FR-AP-09; PRD-001 rev 0.84). A changed supplier, currency, lines or notes is a new content version, written by the caller; drafts already made keep theirs. A changed schedule takes effect after the last occurrence made. Side effects: the next version and its audit event. Needs If-Match with the recurring bill's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `recurring_bill_id` (path, required): The recurring bill's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`RecurringBillInput`](../../contracts/schemas/RecurringBillInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringBill`](../../contracts/schemas/RecurringBill.schema.json)
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

### `POST /v1/recurring-bills/{recurring_bill_id}/pause`

**Pause a recurring bill** (`pauseRecurringBill`)

Stops an active recurring bill making drafts until it is resumed (FR-AP-09; PRD-001 rev 0.84). Draft bills already made stay. Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `recurring_bill_id` (path, required): The recurring bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringBill`](../../contracts/schemas/RecurringBill.schema.json)
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

### `POST /v1/recurring-bills/{recurring_bill_id}/resume`

**Resume a recurring bill** (`resumeRecurringBill`)

Starts a paused recurring bill again (FR-AP-09; PRD-001 rev 0.84): its next draft is for its first occurrence on or after today, so the occurrences it missed while paused are skipped, not made late. Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Bills, vendor credits
- **Parameters:**
  - `recurring_bill_id` (path, required): The recurring bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringBill`](../../contracts/schemas/RecurringBill.schema.json)
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

### `POST /v1/supplier-refunds`

**Record a refund from a supplier** (`recordSupplierRefund`)

Records money a supplier paid back against a credit with it (a posted vendor credit, or what a payment of bills left unapplied), by hand, and posts it at once under supplier_refund@1 (FR-AP-03): Dr the money account with what came in, Cr 2010 with the amount at the credit's rate, and the realised FX to 7030 (ENG-41). It settles at most what is left of the credit (CREDIT-05). Side effects: the refund, its number (RF-), the entry, an allocation, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SupplierRefundDraft`](../../contracts/schemas/SupplierRefundDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`SupplierRefundPosting`](../../contracts/schemas/SupplierRefundPosting.schema.json)
  - `201` Created [`SupplierRefundPosting`](../../contracts/schemas/SupplierRefundPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CREDIT-02` The saved application cannot be read; `CREDIT-03` Not a posted vendor credit or a payment with an amount unapplied; `CREDIT-05` More than the bill has open or the credit has left; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-24` A line is in another currency; `LEDGER-25` Not an active money account; `LEDGER-26` A payment's amount is not positive or not in its currency; `LEDGER-27` What a base-currency account paid is missing or not wanted; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CREDIT-02`, `CREDIT-03`, `CREDIT-05`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-24`, `LEDGER-25`, `LEDGER-26`, `LEDGER-27`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `GET /v1/supplier-refunds/{refund_id}`

**Get a refund from a supplier** (`getSupplierRefund`)

Returns a refund with its ETag: the credit it settles, the money account, the amount, the carrying amount it cleared, the realised FX (ENG-41), the lines its entry wrote, and the pending request to void it. Read-only.

- **Permission:** View on Bills, vendor credits
- **Parameters:**
  - `refund_id` (path, required): The refund's ID.
- **Responses:**
  - `200` OK [`SupplierRefund`](../../contracts/schemas/SupplierRefund.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `CREDIT-10` Not a supplier refund.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `CREDIT-02` The saved application cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `CREDIT-02`, `CREDIT-10`

### `POST /v1/supplier-refunds/{refund_id}/void`

**Void a refund from a supplier** (`voidSupplierRefund`)

Voids a refund with a reversing entry on the date given, with a reason, and a reversing allocation, which reopens the credit (FR-GL-12, FR-AR-18). The date is not before the refund's (CREDIT-07). In Founder mode voiding needs a second person (FM-1): it is refused (CREDIT-11), and requestSupplierRefundVoid asks for the approval. Needs If-Match with the refund's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `refund_id` (path, required): The refund's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`SupplierRefundPosting`](../../contracts/schemas/SupplierRefundPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `CREDIT-10` Not a supplier refund; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `CREDIT-06` The application is not posted; `CREDIT-11` Voiding needs a second person; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CREDIT-02` The saved application cannot be read; `CREDIT-07` The void is dated before the application; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CREDIT-02`, `CREDIT-06`, `CREDIT-07`, `CREDIT-10`, `CREDIT-11`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/supplier-refunds/{refund_id}/void-approval`

**Ask for approval to void a refund from a supplier** (`requestSupplierRefundVoid`)

Asks for another person's approval to void a refund on the date given, with a reason (FR-GL-12, FM-1): what voidSupplierRefund asks for in Founder mode (CREDIT-11). The answer is 202 with an approval request, which a person who holds A on "Bills, vendor credits" and did not ask approves, stepped up; on the approval the void posts and the credit reopens. Needs a person signed in who passed step-up, and If-Match with the refund's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Bills, vendor credits
- **Parameters:**
  - `refund_id` (path, required): The refund's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `CREDIT-10` Not a supplier refund.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `CREDIT-06` The application is not posted; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CREDIT-02` The saved application cannot be read; `CREDIT-07` The void is dated before the application.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `CREDIT-02`, `CREDIT-06`, `CREDIT-07`, `CREDIT-10`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

## Sales

Invoices to customers: drafted, approved, issued and voided; posted history never changes (FR-AR-03, FR-GL-12).

### `GET /v1/contacts/{contact_id}/statement`

**Work out a customer's statement** (`getCustomerStatement`)

Works out a customer's statement of account in one currency from one day to another (FR-AR-08; PRD-001 rev 0.138): the balance brought forward, each invoice, credit note, payment received, refund of a credit, write-off and void in the period with a running balance, the balance due from the AR sub-ledger, what of it is overdue, and its ageing by days past due, which adds up to it; adds_up says whether the lines come to the balance due, and currencies lists the customer's currencies in the period. Nothing is kept: createStatement makes one to send. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `contact_id` (path, required): The contact's ID.
  - `currency` (query): Its currency, ISO 4217; left out, the customer's own, else the organisation's base currency.
  - `from` (query, required): Its first day.
  - `to` (query, required): Its last day, today at the latest.
- **Responses:**
  - `200` OK [`Statement`](../../contracts/schemas/Statement.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `STATEMENT-01` The statement's period is not valid; `STATEMENT-02` Statements are of customers.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STATEMENT-01`, `STATEMENT-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/customer-credit-applications`

**Apply a customer credit to an invoice** (`applyCustomerCredit`)

Applies a credit with a customer (an issued credit note, or what a customer receipt left unapplied) to one of the customer's issued invoices in its currency (FR-AR-04, FR-AR-18): it settles both by the amount, at most what the invoice has open and what is left of the credit (APPLY-05), and posts under customer_credit_application@1 only what their stored rates make differ, the realised FX to 7030 (ENG-41); in the base currency it posts nothing. Side effects: the application, two allocations, the entry when one posts, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview applies nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CustomerCreditApplicationDraft`](../../contracts/schemas/CustomerCreditApplicationDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerCreditApplicationPosting`](../../contracts/schemas/CustomerCreditApplicationPosting.schema.json)
  - `201` Created [`CustomerCreditApplicationPosting`](../../contracts/schemas/CustomerCreditApplicationPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `APPLY-03` Not an issued credit note or a receipt with an amount unapplied; `APPLY-04` Not an issued invoice of the credit's customer in its currency; `APPLY-05` More than the invoice has open or the credit has left; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-26` A payment's amount is not positive or not in its currency; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APPLY-03`, `APPLY-04`, `APPLY-05`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-26`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `GET /v1/customer-credit-applications/{application_id}`

**Get a credit applied to an invoice** (`getCustomerCreditApplication`)

Returns an application of a customer credit to an invoice with its ETag: the credit and the invoice, the amount, the carrying amounts it cleared, the realised FX (ENG-41), the lines its entry wrote if any, and the pending request to void it. Read-only.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `application_id` (path, required): The application's ID.
- **Responses:**
  - `200` OK [`CustomerCreditApplication`](../../contracts/schemas/CustomerCreditApplication.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `APPLY-01` Not a customer credit application.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `APPLY-02` The saved application cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `APPLY-01`, `APPLY-02`

### `POST /v1/customer-credit-applications/{application_id}/void`

**Void a credit applied to an invoice** (`voidCustomerCreditApplication`)

Voids an application on the date given, with a reason (FR-GL-12): its entry, if it posted one, is reversed, and each of its allocations is reversed on that date, which reopens the invoice and the credit by the amount applied. The date is not before the application's (APPLY-07). In Founder mode voiding needs a second person (FM-1): it is refused (APPLY-09), and requestCustomerCreditApplicationVoid asks for the approval. Needs If-Match with the application's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `application_id` (path, required): The application's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerCreditApplicationPosting`](../../contracts/schemas/CustomerCreditApplicationPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `APPLY-01` Not a customer credit application; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APPLY-06` The application is not posted; `APPLY-09` Voiding needs a second person; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `APPLY-02` The saved application cannot be read; `APPLY-07` The void is dated before the application; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APPLY-01`, `APPLY-02`, `APPLY-06`, `APPLY-07`, `APPLY-09`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/customer-credit-applications/{application_id}/void-approval`

**Ask for approval to void a credit applied to an invoice** (`requestCustomerCreditApplicationVoid`)

Asks for another person's approval to void an application on the date given, with a reason (FR-GL-12, FM-1): what voidCustomerCreditApplication asks for in Founder mode (APPLY-09). The answer is 202 with an approval request, which a person who holds A on "Invoices, credit notes" and did not ask approves, stepped up; on the approval the void posts and the invoice and the credit reopen. Needs a person signed in who passed step-up, and If-Match with the application's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `application_id` (path, required): The application's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `APPLY-01` Not a customer credit application.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APPLY-06` The application is not posted; `APR-06` A change to it is waiting for approval already; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `APPLY-02` The saved application cannot be read; `APPLY-07` The void is dated before the application.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APPLY-01`, `APPLY-02`, `APPLY-06`, `APPLY-07`, `APR-06`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/customer-deposits`

**List the customer deposits** (`listCustomerDeposits`)

The deposits and retainers customers paid ahead of their invoices, newest date first, a page at a time, narrowed by status, dates and text in the number (FR-AR-09). Each has its customer, kind, money account, amount, the GST it bore, what 2410 holds of it and what is left of it. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only deposits with this status.
  - `from` (query): Only deposits dated on or after this day.
  - `to` (query): Only deposits dated on or before this day.
  - `q` (query): Only deposits whose number contains this text, ignoring case.
- **Responses:**
  - `200` OK [`CustomerDepositPage`](../../contracts/schemas/CustomerDepositPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `DEPOSIT-01` Not a customer deposit.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid; `DEPOSIT-02` The saved deposit cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `DEPOSIT-01`, `DEPOSIT-02`

### `POST /v1/customer-deposits`

**Record a customer deposit** (`recordCustomerDeposit`)

Records money a customer paid ahead of an invoice, by hand, and posts it at once under customer_deposit@1 (FR-AR-09): Dr the money account with what came in, Cr 2410 Customer deposits in the base currency at the rate it came in, and Cr 2200 with the GST a part payment bears when received once the organisation is GST-registered (ENG-45; RND-GST-INCL); a security deposit bears none until it is used. The final invoice deducts it. The money has come in already, so no approval is asked. Side effects: the deposit, its number (DP-), the entry, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CustomerDepositDraft`](../../contracts/schemas/CustomerDepositDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerDepositPosting`](../../contracts/schemas/CustomerDepositPosting.schema.json)
  - `201` Created [`CustomerDepositPosting`](../../contracts/schemas/CustomerDepositPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `DEPOSIT-03` The deposit's customer is not a customer; `DEPOSIT-04` Not a part payment or a security deposit; `DEPOSIT-05` The deposit's GST code is not one it takes; `DEPOSIT-09` A text of the deposit is too long; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-24` A line is in another currency; `LEDGER-25` Not an active money account; `LEDGER-26` A payment's amount is not positive or not in its currency; `LEDGER-27` What a base-currency account paid is missing or not wanted; `LEDGER-32` A line's GST rate is not one the rules read; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `DEPOSIT-03`, `DEPOSIT-04`, `DEPOSIT-05`, `DEPOSIT-09`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-24`, `LEDGER-25`, `LEDGER-26`, `LEDGER-27`, `LEDGER-32`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `GET /v1/customer-deposits/{deposit_id}`

**Get a customer deposit** (`getCustomerDeposit`)

Returns a deposit with its ETag: its customer, kind, money account, rate, amount, the GST it bore, what 2410 holds of it, what is used and left of it, the lines its entry wrote (ledger_impact), and the pending request to void it. Read-only.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `deposit_id` (path, required): The deposit's ID.
- **Responses:**
  - `200` OK [`CustomerDeposit`](../../contracts/schemas/CustomerDeposit.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `DEPOSIT-01` Not a customer deposit.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `DEPOSIT-02` The saved deposit cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `DEPOSIT-01`, `DEPOSIT-02`

### `POST /v1/customer-deposits/{deposit_id}/void`

**Void a customer deposit** (`voidCustomerDeposit`)

Voids a deposit with a reversing entry on the date given, with a reason (FR-GL-12). Nothing may use it: an invoice that deducts it or a refund of it is voided first (DEPOSIT-08). The date is not before the deposit's (DEPOSIT-07). In Founder mode voiding needs a second person (FM-1): it is refused (DEPOSIT-10), and requestCustomerDepositVoid asks for the approval. Needs If-Match with the deposit's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `deposit_id` (path, required): The deposit's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerDepositPosting`](../../contracts/schemas/CustomerDepositPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `DEPOSIT-01` Not a customer deposit; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `DEPOSIT-06` The deposit is not posted; `DEPOSIT-08` Invoices or refunds use the deposit; `DEPOSIT-10` Voiding needs a second person; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `DEPOSIT-02` The saved deposit cannot be read; `DEPOSIT-07` The void is dated before the deposit; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `DEPOSIT-01`, `DEPOSIT-02`, `DEPOSIT-06`, `DEPOSIT-07`, `DEPOSIT-08`, `DEPOSIT-10`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/customer-deposits/{deposit_id}/void-approval`

**Ask for approval to void a customer deposit** (`requestCustomerDepositVoid`)

Asks for another person's approval to void a deposit on the date given, with a reason (FR-GL-12, FM-1): what voidCustomerDeposit asks for in Founder mode (DEPOSIT-10). The answer is 202 with an approval request, which a person who holds A on "Invoices, credit notes" and did not ask approves, stepped up; on the approval the void posts. Needs a person signed in who passed step-up, and If-Match with the deposit's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `deposit_id` (path, required): The deposit's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `DEPOSIT-01` Not a customer deposit.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `DEPOSIT-06` The deposit is not posted; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `DEPOSIT-02` The saved deposit cannot be read; `DEPOSIT-07` The void is dated before the deposit.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `DEPOSIT-01`, `DEPOSIT-02`, `DEPOSIT-06`, `DEPOSIT-07`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/customer-receipts`

**List the customer receipts** (`listCustomerReceipts`)

The receipts from customers recorded by hand, newest date first, a page at a time, narrowed by status, dates and text in the number (FR-AR-18). Each has its customer, money account, total and what it settled of each invoice. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only receipts with this status.
  - `from` (query): Only receipts dated on or after this day.
  - `to` (query): Only receipts dated on or before this day.
  - `q` (query): Only receipts whose number contains this text, ignoring case.
- **Responses:**
  - `200` OK [`CustomerReceiptPage`](../../contracts/schemas/CustomerReceiptPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `RECEIPT-01` Not a customer receipt.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid; `RECEIPT-02` The saved receipt cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `RECEIPT-01`, `RECEIPT-02`

### `POST /v1/customer-receipts`

**Record a customer receipt** (`recordCustomerReceipt`)

Records money a customer paid, by hand, and posts it at once under the rule customer_receipt@1 (FR-AR-18; ENG-41): Cr 1100 with what it settles of each invoice at that invoice's stored rate, Dr the money account with what came in (at the receipt's rate, or what a base-currency account received for invoices in another currency), and the realised FX to 7030; the part that settles what is left of an invoice clears its carrying amount, the cent of RND-FX a part leaves on a base-only line. It settles at most what each invoice has open (RECEIPT-05); what it received beyond the invoices is kept as a credit with the customer. The money has come in already, so no approval is asked. Side effects: the receipt, its number, the entry, an allocation per invoice, a new ledger revision, balances, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CustomerReceiptDraft`](../../contracts/schemas/CustomerReceiptDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerReceiptPosting`](../../contracts/schemas/CustomerReceiptPosting.schema.json)
  - `201` Created [`CustomerReceiptPosting`](../../contracts/schemas/CustomerReceiptPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-24` A line is in another currency; `LEDGER-25` Not an active money account; `LEDGER-26` A payment's amount is not positive or not in its currency; `LEDGER-27` What a base-currency account paid is missing or not wanted; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECEIPT-03` The receipt's customer is not a customer; `RECEIPT-04` Not an issued invoice of the receipt's customer in its currency; `RECEIPT-05` More than the invoice has open; `RECEIPT-08` A text of the receipt is too long; `RECEIPT-09` The receipt settles too many invoices; `RECEIPT-11` What is left of the invoice is above the settlement tolerance; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-24`, `LEDGER-25`, `LEDGER-26`, `LEDGER-27`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECEIPT-03`, `RECEIPT-04`, `RECEIPT-05`, `RECEIPT-08`, `RECEIPT-09`, `RECEIPT-11`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `GET /v1/customer-receipts/{receipt_id}`

**Get a customer receipt** (`getCustomerReceipt`)

Returns a receipt recorded by hand with its ETag: its customer, money account, rate, what it settled of each invoice with the carrying amount cleared and the realised FX (ENG-41), the lines its entry wrote (ledger_impact), and the pending request to void it. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `receipt_id` (path, required): The receipt's ID.
- **Responses:**
  - `200` OK [`CustomerReceipt`](../../contracts/schemas/CustomerReceipt.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `RECEIPT-01` Not a customer receipt.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `RECEIPT-02` The saved receipt cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `RECEIPT-01`, `RECEIPT-02`

### `POST /v1/customer-receipts/{receipt_id}/void`

**Void a customer receipt** (`voidCustomerReceipt`)

Voids a recorded receipt with a reversing entry on the date given, with a reason, and a reversing allocation for each invoice it settled, which reopens the invoices (FR-GL-12, FR-AR-18); posted history never changes. The date is not before the receipt's (RECEIPT-07). In Founder mode voiding needs a second person (FM-1): it is refused (RECEIPT-10), and requestCustomerReceiptVoid asks for the approval. Side effects: the reversing entry and allocations, a new ledger revision, balances, audit events with the reason. Needs If-Match with the receipt's ETag. Idempotent with Idempotency-Key; Prefer: return=preview voids nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `receipt_id` (path, required): The receipt's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerReceiptPosting`](../../contracts/schemas/CustomerReceiptPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal; `RECEIPT-01` Not a customer receipt.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `RECEIPT-06` The receipt is not posted; `RECEIPT-10` Voiding needs a second person; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `RECEIPT-02` The saved receipt cannot be read; `RECEIPT-07` The void is dated before the receipt; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `RECEIPT-01`, `RECEIPT-02`, `RECEIPT-06`, `RECEIPT-07`, `RECEIPT-10`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/customer-receipts/{receipt_id}/void-approval`

**Ask for approval to void a customer receipt** (`requestCustomerReceiptVoid`)

Asks for another person's approval to void a recorded receipt with a reversing entry on the date given, with a reason (FR-GL-12, FM-1): what voidCustomerReceipt asks for in Founder mode (RECEIPT-10). The answer is 202 with an approval request, which a person who holds A on "Invoices, credit notes" and did not ask approves, stepped up; on the approval the void posts and the invoices reopen. Asking needs a person signed in who passed step-up, and If-Match with the receipt's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `receipt_id` (path, required): The receipt's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `RECEIPT-01` Not a customer receipt.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `RECEIPT-06` The receipt is not posted; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `RECEIPT-02` The saved receipt cannot be read; `RECEIPT-07` The void is dated before the receipt.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `RECEIPT-01`, `RECEIPT-02`, `RECEIPT-06`, `RECEIPT-07`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/customer-refunds`

**Record a refund to a customer** (`recordCustomerRefund`)

Records money paid back to a customer against a credit with it (an issued credit note, or what a customer receipt left unapplied), by hand, and posts it at once under customer_refund@1 (FR-AR-04, FR-AR-18): Dr 1100 with the amount at the credit's rate, Cr the money account with what left it, and the realised FX to 7030 (ENG-41). It settles at most what is left of the credit (APPLY-05). A customer deposit is refunded under deposit_refund@1 (FR-AR-09): Dr 2410 with its part of the deposit less its GST part, Dr 2200 with that GST part, Cr the money account, and the realised FX to 7030 (RND-DEPOSIT). Side effects: the refund, its number (CR-), the entry, an allocation, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CustomerRefundDraft`](../../contracts/schemas/CustomerRefundDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerRefundPosting`](../../contracts/schemas/CustomerRefundPosting.schema.json)
  - `201` Created [`CustomerRefundPosting`](../../contracts/schemas/CustomerRefundPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `APPLY-02` The saved application cannot be read; `APPLY-03` Not an issued credit note or a receipt with an amount unapplied; `APPLY-05` More than the invoice has open or the credit has left; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-24` A line is in another currency; `LEDGER-25` Not an active money account; `LEDGER-26` A payment's amount is not positive or not in its currency; `LEDGER-27` What a base-currency account paid is missing or not wanted; `LEDGER-33` A deposit the invoice deducts is not one it takes; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APPLY-02`, `APPLY-03`, `APPLY-05`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-24`, `LEDGER-25`, `LEDGER-26`, `LEDGER-27`, `LEDGER-33`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `GET /v1/customer-refunds/{refund_id}`

**Get a refund to a customer** (`getCustomerRefund`)

Returns a refund to a customer with its ETag: the credit it settles, the money account, the amount, the carrying amount it cleared, the realised FX (ENG-41), the lines its entry wrote, and the pending request to void it. Read-only.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `refund_id` (path, required): The refund's ID.
- **Responses:**
  - `200` OK [`CustomerRefund`](../../contracts/schemas/CustomerRefund.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `APPLY-10` Not a customer refund.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `APPLY-02` The saved application cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `APPLY-02`, `APPLY-10`

### `POST /v1/customer-refunds/{refund_id}/void`

**Void a refund to a customer** (`voidCustomerRefund`)

Voids a refund with a reversing entry on the date given, with a reason, and a reversing allocation, which reopens the credit (FR-GL-12, FR-AR-18). The date is not before the refund's (APPLY-07). In Founder mode voiding needs a second person (FM-1): it is refused (APPLY-11), and requestCustomerRefundVoid asks for the approval. Needs If-Match with the refund's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `refund_id` (path, required): The refund's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerRefundPosting`](../../contracts/schemas/CustomerRefundPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `APPLY-10` Not a customer refund; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APPLY-06` The application is not posted; `APPLY-11` Voiding needs a second person; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `APPLY-02` The saved application cannot be read; `APPLY-07` The void is dated before the application; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APPLY-02`, `APPLY-06`, `APPLY-07`, `APPLY-10`, `APPLY-11`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/customer-refunds/{refund_id}/void-approval`

**Ask for approval to void a refund to a customer** (`requestCustomerRefundVoid`)

Asks for another person's approval to void a refund on the date given, with a reason (FR-GL-12, FM-1): what voidCustomerRefund asks for in Founder mode (APPLY-11). The answer is 202 with an approval request, which a person who holds A on "Invoices, credit notes" and did not ask approves, stepped up; on the approval the void posts and the credit reopens. Needs a person signed in who passed step-up, and If-Match with the refund's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `refund_id` (path, required): The refund's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `APPLY-10` Not a customer refund.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APPLY-06` The application is not posted; `APR-06` A change to it is waiting for approval already; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `APPLY-02` The saved application cannot be read; `APPLY-07` The void is dated before the application.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APPLY-02`, `APPLY-06`, `APPLY-07`, `APPLY-10`, `APR-06`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/document-links/{link_id}/revoke`

**Revoke a quote's or invoice's online link** (`revokeDocumentLink`)

Stops a link working at once (FR-AR-06): whoever holds it then gets LINK-01. A link is revoked once; to share the document again, email it again, which makes a new link. Side effects: the revocation and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview revokes nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `link_id` (path, required): The link's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`DocumentLinkOut`](../../contracts/schemas/DocumentLinkOut.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LINK-01` A link changes only in its first view and its revocation, once each; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LINK-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/invoices`

**List the invoices** (`listInvoices`)

The organisation's invoices at their current version, newest date first (a draft without a date last), a page at a time, narrowed by status, dates and text in the number (FR-AR-03). Each has its customer, totals, due date and states. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `kind` (query): Invoices, the default, or credit notes.
  - `status` (query): Only invoices with this status.
  - `from` (query): Only invoices dated on or after this day.
  - `to` (query): Only invoices dated on or before this day.
  - `q` (query): Only invoices whose number contains this text, ignoring case.
- **Responses:**
  - `200` OK [`InvoicePage`](../../contracts/schemas/InvoicePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-01`, `INVOICE-04`

### `POST /v1/invoices`

**Save a draft invoice** (`createInvoice`)

Saves a customer's invoice as a draft (✎) at version 1 (FR-AR-03), without a number: it takes the next from the invoice sequence when it is issued. A draft is checked for its form only; getInvoice shows what issuing it would post, and issueInvoice checks it all. Side effects: a draft document and its audit event; nothing posts until issueInvoice. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`InvoiceDraft`](../../contracts/schemas/InvoiceDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Invoice`](../../contracts/schemas/Invoice.schema.json)
  - `201` Created [`Invoice`](../../contracts/schemas/Invoice.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/invoices/{invoice_id}`

**Get an invoice** (`getInvoice`)

Returns an invoice's current version with its ETag: its customer, lines with their nets, its GST by code and totals, its approval, ledger, delivery, settlement and due states, the monthly releases of its spread lines (FR-GL-03), what stops a draft being issued (✖), the lines issuing it writes or wrote (ledger_impact), and the pending request to issue, void or edit it. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
- **Responses:**
  - `200` OK [`Invoice`](../../contracts/schemas/Invoice.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-01`, `INVOICE-04`

### `PUT /v1/invoices/{invoice_id}`

**Change a draft invoice** (`replaceInvoice`)

Saves the draft's new content as its next version, recording who saved it; the earlier versions are kept. Only a draft changes this way; an issued invoice is edited with editInvoice. Side effects: the new version and its audit event. Needs If-Match with the draft's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`InvoiceDraft`](../../contracts/schemas/InvoiceDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Invoice`](../../contracts/schemas/Invoice.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `409` Conflict: `INVOICE-02` The invoice is not a draft; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-01`, `INVOICE-02`, `INVOICE-04`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/invoices/{invoice_id}/document`

**Render an invoice as a document** (`getInvoiceDocument`)

Renders an invoice's current version with the template that applies to its customer (FR-TPL-05) through the render service, and finds the IRAS particulars in it (FR-AR-05): once the organisation is GST-registered on its date it is a tax invoice with the supplier's name, address and GST number, the number and date, the customer's name and address, each line's description and GST rate, the total before GST, the GST and the total with GST; in another currency also its totals in SGD at its rate; with sales.simplified_tax_invoices on and a total with GST of at most S$1,000.00, a simplified tax invoice. A draft renders without a number. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
- **Responses:**
  - `200` OK [`InvoiceDocument`](../../contracts/schemas/InvoiceDocument.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `409` Conflict: `SALESDOC-01` Not laid out as an invoice.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `SALESDOC-02` The invoice's checks stop it being laid out.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-01`, `INVOICE-04`, `SALESDOC-01`, `SALESDOC-02`, `TPL-17`

### `POST /v1/invoices/{invoice_id}/edit`

**Edit an issued invoice** (`editInvoice`)

Corrects an issued invoice in an open period (FR-GL-12): saves the content sent as its next version, reverses the entry it had on that entry's own date, and posts the new version's entry, in one transaction; the invoice stays issued, keeps its number, and records who posted the edit. An invoice that receipts or credits settle is edited only once they are voided (INVOICE-13). An edit needs the approval a void needs: in Founder mode it is refused (INVOICE-14), and in any mode a new version whose total is above the self-approval threshold is refused (INVOICE-10); requestInvoiceEdit asks for the approval. Side effects: a new version, two entries, a new ledger revision, balances, audit events with the reason. Needs If-Match with the invoice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview shows the two entries and saves nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`InvoiceEdit`](../../contracts/schemas/InvoiceEdit.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`InvoiceEdited`](../../contracts/schemas/InvoiceEdited.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `INVOICE-02` The invoice is not a draft; `INVOICE-03` The invoice is not issued; `INVOICE-10` The invoice needs another person's approval; `INVOICE-11` Voiding needs a second person; `INVOICE-13` Receipts or credits settle the invoice; `INVOICE-14` Editing needs a second person; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DOC-04` An edit posts its reversal and its new version together; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `INVOICE-01`, `INVOICE-02`, `INVOICE-03`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-10`, `INVOICE-11`, `INVOICE-13`, `INVOICE-14`, `INVOICE-15`, `INVOICE-16`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `STORAGE-DOC-02`, `STORAGE-DOC-04`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/invoices/{invoice_id}/edit-approval`

**Ask for approval to edit an issued invoice** (`requestInvoiceEdit`)

Asks for another person's approval to edit an issued invoice with the content sent (FR-GL-12, FM-1, FM-2): what editInvoice asks for in Founder mode (INVOICE-14) or above the self-approval threshold (INVOICE-10). The content must pass the posting rules. The answer is 202 with an approval request whose changes list each field and line that changes and the total receivable before and after; it keeps the content, and its envelope holds the content's SHA-256 with the invoice's version, status and total, so any change to the invoice voids it. A person who holds A on "Invoices, credit notes" and did not ask approves it, stepped up; on the approval the edit posts as the approver, with the requester as the one who prepared the new version. Asking needs a person signed in who passed step-up, and If-Match with the invoice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`InvoiceEdit`](../../contracts/schemas/InvoiceEdit.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `INVOICE-02` The invoice is not a draft; `INVOICE-03` The invoice is not issued; `INVOICE-13` Receipts or credits settle the invoice; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `INVOICE-01`, `INVOICE-02`, `INVOICE-03`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-13`, `INVOICE-15`, `INVOICE-16`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/invoices/{invoice_id}/email`

**Email an invoice to the customer** (`emailInvoice`)

Emails an issued invoice (FR-AR-06) to the customer's billing email, or the address given, from the organisation's invoices sender, with the subject and body of the email template that applies to the customer and the PDF of its current version attached, rendered with the document template that applies (FR-TPL-05). A tax invoice that lacks a particular IRAS requires, such as the customer's address, is not sent (SALESDOC-05); a draft or voided one never is (SALESDOC-03). Sending again sends the same version again. Side effects: the PDF in the invoice's evidence (source generated), the email message in the delivery log, the send, and their audit events; its delivery state follows the provider's events. Needs If-Match with the invoice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`EmailDocument`](../../contracts/schemas/EmailDocument.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Invoice`](../../contracts/schemas/Invoice.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `SALESDOC-01` Not laid out as an invoice; `SALESDOC-03` Only an issued invoice or a sent quote is emailed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `SALESDOC-02` The invoice's checks stop it being laid out; `SALESDOC-04` There is no address to send it to; `SALESDOC-05` The tax invoice lacks a particular IRAS requires.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again; `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `INVOICE-01`, `INVOICE-04`, `SALESDOC-01`, `SALESDOC-02`, `SALESDOC-03`, `SALESDOC-04`, `SALESDOC-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TPL-17`

### `POST /v1/invoices/{invoice_id}/issue`

**Issue a draft invoice** (`issueInvoice`)

Issues a draft invoice: gives it the next number from the invoice sequence and posts it under the rule invoice@1 (FR-AR-03): Dr 1100 with the total, Cr each line's revenue account with its net (2400 for a spread line), Cr 2200 with the output GST once the organisation is GST-registered (RND-GST-EXCL), and for an invoice in another currency the conversion's difference to 7030, written with the invoice's change to posted and its number in one transaction (ENG-02). Every problem comes back together, and a refusal uses no number. Approval as for bills: an invoice whose total in the base currency is above the self-approval threshold is refused (INVOICE-10): ask with requestInvoiceIssue; at or below it, a Founder-mode issue is logged as self-approved for the weekly review (FM-2, FM-3). Side effects: the number, the entry, a new ledger revision, balances, audit events. Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`InvoicePosting`](../../contracts/schemas/InvoicePosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `INVOICE-02` The invoice is not a draft; `INVOICE-03` The invoice is not issued; `INVOICE-10` The invoice needs another person's approval; `INVOICE-11` Voiding needs a second person; `INVOICE-13` Receipts or credits settle the invoice; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `INVOICE-01`, `INVOICE-02`, `INVOICE-03`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-10`, `INVOICE-11`, `INVOICE-13`, `INVOICE-15`, `INVOICE-16`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/invoices/{invoice_id}/issue-approval`

**Ask for approval to issue an invoice** (`requestInvoiceIssue`)

Asks for another person's approval to issue a draft invoice at the version read (FM-2, SOD-3): what issueInvoice asks for when the invoice's total is above the self-approval threshold (INVOICE-10). The answer is 202 with an approval request, which a person who holds A on "Invoices, credit notes", did not ask and did not prepare the invoice approves, stepped up; on the approval the invoice is issued, as the approver. The request holds the invoice's version, status and total, so a change to the draft voids it. Asking needs a person signed in who passed step-up, and If-Match with the invoice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `INVOICE-02` The invoice is not a draft; `INVOICE-03` The invoice is not issued; `INVOICE-13` Receipts or credits settle the invoice; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `INVOICE-01`, `INVOICE-02`, `INVOICE-03`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-13`, `INVOICE-15`, `INVOICE-16`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/invoices/{invoice_id}/void`

**Void an issued invoice** (`voidInvoice`)

Voids an issued invoice with a reversing entry on the date given, with a reason (FR-GL-12); posted history never changes, and the invoice keeps its number. An invoice that receipts or credits settle is voided only once they are (INVOICE-13). In Founder mode voiding needs a second person (FM-1): it is refused (INVOICE-11), and requestInvoiceVoid asks for the approval. Side effects: the reversing entry, a new ledger revision, balances, audit events with the reason. Needs If-Match with the invoice's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`InvoicePosting`](../../contracts/schemas/InvoicePosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `INVOICE-02` The invoice is not a draft; `INVOICE-03` The invoice is not issued; `INVOICE-10` The invoice needs another person's approval; `INVOICE-11` Voiding needs a second person; `INVOICE-13` Receipts or credits settle the invoice; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `INVOICE-01`, `INVOICE-02`, `INVOICE-03`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-10`, `INVOICE-11`, `INVOICE-13`, `INVOICE-15`, `INVOICE-16`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`

### `POST /v1/invoices/{invoice_id}/void-approval`

**Ask for approval to void an invoice** (`requestInvoiceVoid`)

Asks for another person's approval to void an issued invoice with a reversing entry on the date given, with a reason (FR-GL-12, FM-1): what voidInvoice asks for in Founder mode (INVOICE-11). The answer is 202 with an approval request, which a person who holds A on "Invoices, credit notes" and did not ask approves, stepped up; on the approval the reversal posts. Asking needs a person signed in who passed step-up, and If-Match with the invoice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `INVOICE-02` The invoice is not a draft; `INVOICE-03` The invoice is not issued; `INVOICE-13` Receipts or credits settle the invoice; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `INVOICE-01`, `INVOICE-02`, `INVOICE-03`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-13`, `INVOICE-15`, `INVOICE-16`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/public/document`

**Open a quote or invoice through its link** (`getPublicDocument`)

Public: no session. Opens the quote or invoice a link's token (in the Fyniq-Link header, from the link's fragment) was made for (FR-AR-06, FR-AR-16): what its PDF shows, a quote's status and answer, and whether the customer can answer it here. The first opening is recorded as the document's delivery viewed (the time only, never the reader's address). A link opens its own document's version and nothing else, for 180 days at most, until revoked, voided or cancelled, or an invoice is paid in full; every link that opens nothing answers the same 404 LINK-01. At most 30 a minute per link. Never cached or indexed.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `Fyniq-Link` (header, required): The link's token, from the fragment of the link the email carried (after #/d/#). Never put it in a URL.
- **Responses:**
  - `200` OK [`PublicDocument`](../../contracts/schemas/PublicDocument.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `LINK-01` The link opens no document.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `SALESDOC-02` The invoice's checks stop it being laid out.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `LINK-01`, `SALESDOC-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/public/document/accept`

**Accept a quote through its link** (`acceptQuoteByLink`)

Public: no session. The customer accepts the sent quote or order a link's token opens (FR-AR-16), with the name of who accepts and a comment, kept as the quote's answer and its evidence (FR-DOC-05): recorded as the link, with the request's address in the audit trail. Only a sent quote, still valid, with no answer: a quote has one answer, which never changes (QUOTE-07). At most 5 answers a minute per link. Idempotent with Idempotency-Key; Prefer: return=preview checks it and records nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `Fyniq-Link` (header, required): The link's token, from the fragment of the link the email carried (after #/d/#). Never put it in a URL.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`PublicAnswerRequest`](../../contracts/schemas/PublicAnswerRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PublicDocument`](../../contracts/schemas/PublicDocument.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `LINK-01` The link opens no document.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `QUOTE-05` The quote is not sent; `QUOTE-07` The quote has its answer already; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-QUOTE-01` The quote's answer is recorded once, of a sent quote.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `QUOTE-03` The quote's validity date is missing or before its date; `QUOTE-08` The answer has no date, or a text is too long; `SALESDOC-02` The invoice's checks stop it being laid out.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `LINK-01`, `QUOTE-03`, `QUOTE-05`, `QUOTE-07`, `QUOTE-08`, `SALESDOC-02`, `STORAGE-LOCK-01`, `STORAGE-QUOTE-01`, `STORAGE-RETRY-01`

### `POST /v1/public/document/decline`

**Decline a quote through its link** (`declineQuoteByLink`)

Public: no session. The customer declines the sent quote or order a link's token opens (FR-AR-16), with a comment, kept as the quote's answer: recorded as the link, with the request's address in the audit trail. Only a sent quote, still valid, with no answer (QUOTE-07). At most 5 answers a minute per link. Idempotent with Idempotency-Key; Prefer: return=preview checks it and records nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `Fyniq-Link` (header, required): The link's token, from the fragment of the link the email carried (after #/d/#). Never put it in a URL.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`PublicAnswerRequest`](../../contracts/schemas/PublicAnswerRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`PublicDocument`](../../contracts/schemas/PublicDocument.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `LINK-01` The link opens no document.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `QUOTE-05` The quote is not sent; `QUOTE-07` The quote has its answer already; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-QUOTE-01` The quote's answer is recorded once, of a sent quote.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `QUOTE-03` The quote's validity date is missing or before its date; `QUOTE-08` The answer has no date, or a text is too long; `SALESDOC-02` The invoice's checks stop it being laid out.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `LINK-01`, `QUOTE-03`, `QUOTE-05`, `QUOTE-07`, `QUOTE-08`, `SALESDOC-02`, `STORAGE-LOCK-01`, `STORAGE-QUOTE-01`, `STORAGE-RETRY-01`

### `GET /v1/public/document/pdf`

**Get the PDF of a quote or invoice through its link** (`getPublicDocumentPDF`)

Public: no session. The PDF of the quote or invoice a link's token opens (FR-AR-06), rendered with the template that applies to its customer, as emailing it rendered it. Refused as getPublicDocument refuses. At most 30 a minute per link. Never cached or indexed.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `Fyniq-Link` (header, required): The link's token, from the fragment of the link the email carried (after #/d/#). Never put it in a URL.
- **Responses:**
  - `200` OK [`PublicPDF`](../../contracts/schemas/PublicPDF.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `LINK-01` The link opens no document.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `SALESDOC-02` The invoice's checks stop it being laid out.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again; `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `LINK-01`, `SALESDOC-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TPL-17`

### `GET /v1/quotes`

**List the quotes or sales orders** (`listQuotes`)

The organisation's quotes, or its sales orders, at their current version, newest date first (a draft without a date last), a page at a time, narrowed by status, dates and text in the number (FR-AR-01). Each has its customer, totals, validity date and status. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `kind` (query): Quotes, the default, or sales orders.
  - `status` (query): Only those with this status of their document: draft, sent (whatever the customer answered, or expired) or cancelled.
  - `from` (query): Only those dated on or after this day.
  - `to` (query): Only those dated on or before this day.
  - `q` (query): Only those whose number contains this text, ignoring case.
- **Responses:**
  - `200` OK [`QuotePage`](../../contracts/schemas/QuotePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `QUOTE-02` The saved quote cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-04`, `QUOTE-01`, `QUOTE-02`

### `POST /v1/quotes`

**Save a draft quote or sales order** (`createQuote`)

Saves a quote to a customer, or with kind sales_order an order the customer placed, as a draft (✎) at version 1 (FR-AR-01), without a number: it takes the next from its sequence when it is sent. Its lines are an invoice's; a quote names the last day it is valid. It posts nothing. Side effects: a draft document and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`QuoteDraft`](../../contracts/schemas/QuoteDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `201` Created [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `QUOTE-06` A quote credits no invoice and deducts no deposit.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `QUOTE-06`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/quotes/pipeline`

**Get the sales pipeline** (`getSalesPipeline`)

What the open quotes and sales orders have still to invoice in the base currency on a day (FR-AR-01, FR-AR-02): those accepted and not invoiced in full, and those sent and still valid on it when they name a validity date; not declined, expired or cancelled. Each counts what no issued invoice takes of its lines, at its rate. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `as_at` (query): The day to read it for; left out, the organisation's today.
- **Responses:**
  - `200` OK [`Pipeline`](../../contracts/schemas/Pipeline.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/quotes/{quote_id}`

**Get a quote or sales order** (`getQuote`)

Returns a quote or sales order with its ETag: its customer, lines, totals, what it comes to in the base currency, validity date, status, and for a draft what stops it being sent. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `quote_id` (path, required): The quote's ID.
- **Responses:**
  - `200` OK [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `QUOTE-02` The saved quote cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-04`, `QUOTE-01`, `QUOTE-02`

### `PUT /v1/quotes/{quote_id}`

**Replace a draft quote or sales order** (`replaceQuote`)

Saves a draft's new content as its next version (FR-AR-01); its kind never changes. A sent quote never changes: cancel it and send another (QUOTE-04). Needs If-Match with the draft's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `quote_id` (path, required): The quote's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`QuoteDraft`](../../contracts/schemas/QuoteDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `409` Conflict: `QUOTE-04` The quote is not a draft; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `QUOTE-06` A quote credits no invoice and deducts no deposit.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `QUOTE-01`, `QUOTE-04`, `QUOTE-06`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/quotes/{quote_id}/accept`

**Record that the customer accepted a quote** (`acceptQuote`)

Records that the customer accepted a sent quote or order (FR-AR-01), on the day given or the organisation's today, with who accepted it and a note: its status becomes accepted, and it stays in the pipeline. A quote has one answer, which never changes, and one that expired takes none (QUOTE-07). The customer's own acceptance through the online link follows (FR-AR-16). Side effects: the answer and its audit event. Needs If-Match with the quote's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `quote_id` (path, required): The quote's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`QuoteAnswer`](../../contracts/schemas/QuoteAnswer.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `QUOTE-05` The quote is not sent; `QUOTE-07` The quote has its answer already; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-QUOTE-01` The quote's answer is recorded once, of a sent quote.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `QUOTE-02` The saved quote cannot be read; `QUOTE-03` The quote's validity date is missing or before its date; `QUOTE-08` The answer has no date, or a text is too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-15`, `INVOICE-16`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `QUOTE-01`, `QUOTE-02`, `QUOTE-03`, `QUOTE-05`, `QUOTE-07`, `QUOTE-08`, `STORAGE-LOCK-01`, `STORAGE-QUOTE-01`, `STORAGE-RETRY-01`

### `POST /v1/quotes/{quote_id}/cancel`

**Cancel a quote or sales order** (`cancelQuote`)

Cancels a sent quote or order, with a reason (FR-AR-01): its status becomes cancelled and it leaves the pipeline; it posts nothing. A draft is simply not sent (QUOTE-05); one that issued invoices are made from is not cancelled until they are voided (QUOTE-12), and a draft invoice made from it can no longer be issued (QUOTE-11). Side effects: the document's change and its audit event with the reason. Needs If-Match with the quote's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `quote_id` (path, required): The quote's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`QuoteCancel`](../../contracts/schemas/QuoteCancel.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `QUOTE-05` The quote is not sent; `QUOTE-07` The quote has its answer already; `QUOTE-12` Invoices are made from the quote; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `QUOTE-02` The saved quote cannot be read; `QUOTE-03` The quote's validity date is missing or before its date.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-15`, `INVOICE-16`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `QUOTE-01`, `QUOTE-02`, `QUOTE-03`, `QUOTE-05`, `QUOTE-07`, `QUOTE-12`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/quotes/{quote_id}/decline`

**Record that the customer declined a quote** (`declineQuote`)

Records that the customer declined a sent quote or order (FR-AR-01), on the day given or the organisation's today, with a note: its status becomes declined, and it leaves the pipeline. A quote has one answer, which never changes (QUOTE-07). Side effects: the answer and its audit event. Needs If-Match with the quote's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `quote_id` (path, required): The quote's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`QuoteAnswer`](../../contracts/schemas/QuoteAnswer.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `QUOTE-05` The quote is not sent; `QUOTE-07` The quote has its answer already; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-QUOTE-01` The quote's answer is recorded once, of a sent quote.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `QUOTE-02` The saved quote cannot be read; `QUOTE-03` The quote's validity date is missing or before its date; `QUOTE-08` The answer has no date, or a text is too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-15`, `INVOICE-16`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `QUOTE-01`, `QUOTE-02`, `QUOTE-03`, `QUOTE-05`, `QUOTE-07`, `QUOTE-08`, `STORAGE-LOCK-01`, `STORAGE-QUOTE-01`, `STORAGE-RETRY-01`

### `POST /v1/quotes/{quote_id}/email`

**Email a quote or sales order to the customer** (`emailQuote`)

Emails a sent quote or order (FR-AR-06, FR-AR-16) to the customer's billing email, or the address given, from the organisation's invoices sender, with the subject and body of the email template that applies to the customer and the PDF of its current version attached, rendered with the quote template that applies (FR-TPL-05). A draft or cancelled one is never sent (SALESDOC-03); sendQuote sends a draft first. Side effects: the PDF in the quote's evidence (source generated), the email message in the delivery log, the send, and their audit events. Needs If-Match with the quote's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `quote_id` (path, required): The quote's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`EmailDocument`](../../contracts/schemas/EmailDocument.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `SALESDOC-03` Only an issued invoice or a sent quote is emailed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `QUOTE-02` The saved quote cannot be read; `SALESDOC-02` The invoice's checks stop it being laid out; `SALESDOC-04` There is no address to send it to; `SALESDOC-05` The tax invoice lacks a particular IRAS requires.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again; `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `INVOICE-04`, `QUOTE-01`, `QUOTE-02`, `SALESDOC-02`, `SALESDOC-03`, `SALESDOC-04`, `SALESDOC-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TPL-17`

### `POST /v1/quotes/{quote_id}/invoice`

**Make an invoice from a quote or sales order** (`invoiceQuote`)

Makes a draft invoice from a quote or order the customer accepted (FR-AR-02; QUOTE-09): of each line, the quantity asked for, at most what no invoice, issued or draft, takes yet (QUOTE-10), or everything that is left. Each invoice line is the quote's line at that quantity and names it (lines[].quote); the invoice is for the quote's customer, in its currency, with its reference, and takes its own date and the rate for it. Enter the invoice's date, change it to what was delivered, and issue it as any invoice; the quote shows invoiced once issued invoices take every line. Side effects: the draft invoice and its audit event. Needs If-Match with the quote's ETag. Idempotent with Idempotency-Key; Prefer: return=preview makes nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `quote_id` (path, required): The quote's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`QuoteInvoiceRequest`](../../contracts/schemas/QuoteInvoiceRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Invoice`](../../contracts/schemas/Invoice.schema.json)
  - `201` Created [`Invoice`](../../contracts/schemas/Invoice.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `QUOTE-09` The quote is not accepted; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `QUOTE-02` The saved quote cannot be read; `QUOTE-03` The quote's validity date is missing or before its date; `QUOTE-10` More than is left of the quote to invoice.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-15`, `INVOICE-16`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `QUOTE-01`, `QUOTE-02`, `QUOTE-03`, `QUOTE-09`, `QUOTE-10`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/quotes/{quote_id}/send`

**Send a quote or sales order** (`sendQuote`)

Sends a draft (FR-AR-01): it takes its number (QT- or SO-) and its status becomes sent; it posts nothing. It is checked as an invoice of the same content is, and a quote needs a validity date on or after its date (QUOTE-03); every problem comes back together, and a refused quote takes no number. Emailing it with its PDF follows (FR-AR-06). Side effects: the document's change and its audit event. Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `quote_id` (path, required): The quote's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Quote`](../../contracts/schemas/Quote.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `QUOTE-01` Not a quote or a sales order.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `QUOTE-04` The quote is not a draft; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `QUOTE-02` The saved quote cannot be read; `QUOTE-03` The quote's validity date is missing or before its date.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-15`, `INVOICE-16`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-28`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `QUOTE-01`, `QUOTE-02`, `QUOTE-03`, `QUOTE-04`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/receivables`

**Get AR ageing** (`getReceivables`)

What customers owe the company as at a day (FR-AR-10), from the AR sub-ledger (INV-07): each customer's open invoices by days past due (current, 1–30, 31–60, 61–90, 90+) and unapplied credits, by currency, with the totals and their carrying amounts in the base currency, and the unrealised FX exposure of each foreign currency at the day's rate from the cascade (ENG-40). An invoice falls due on its due date as entered, else by the customer's or the organisation's payment terms. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `as_at` (query): The day; left out, the organisation's today.
- **Responses:**
  - `200` OK [`Receivables`](../../contracts/schemas/Receivables.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/recurring-invoices`

**List the recurring invoices** (`listRecurringInvoices`)

The organisation's recurring invoices by name, a page at a time (FR-AR-07; PRD-001 rev 0.135): each one's schedule, next occurrence, content and statuses. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`RecurringInvoicePage`](../../contracts/schemas/RecurringInvoicePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/recurring-invoices`

**Add a recurring invoice** (`createRecurringInvoice`)

Adds a recurring invoice, active: an invoice's customer, reference, currency, lines and notes, and a schedule (every 1 or 2 weeks from the start's weekday, or every 1, 3, 6 or 12 months on a day of the month, from a start, until an optional end; FR-AR-07; PRD-001 rev 0.135). The scheduler makes the draft invoice of each occurrence, dated on it, as scheduler:invoices.recurring; a person issues it under the usual approvals, and whoever wrote the content does not approve its drafts (SOD-3). A line naming a quote is refused (INVOICE-09). Its content may be incomplete: its statuses say what the invoice rules would refuse. Side effects: the recurring invoice, its first content version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`RecurringInvoiceInput`](../../contracts/schemas/RecurringInvoiceInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringInvoice`](../../contracts/schemas/RecurringInvoice.schema.json)
  - `201` Created [`RecurringInvoice`](../../contracts/schemas/RecurringInvoice.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `RECURRING-02` The recurring journal is not in that state; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/recurring-invoices/{recurring_invoice_id}`

**Get a recurring invoice** (`getRecurringInvoice`)

Returns a recurring invoice with its statuses and its ETag (FR-AR-07; PRD-001 rev 0.135). Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `recurring_invoice_id` (path, required): The recurring invoice's ID.
- **Responses:**
  - `200` OK [`RecurringInvoice`](../../contracts/schemas/RecurringInvoice.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PUT /v1/recurring-invoices/{recurring_invoice_id}`

**Change a recurring invoice** (`replaceRecurringInvoice`)

Replaces a recurring invoice's name, schedule and content (FR-AR-07; PRD-001 rev 0.135). A changed customer, reference, currency, lines or notes is a new content version, written by the caller; drafts already made keep theirs. A changed schedule takes effect after the last occurrence made. Side effects: the next version and its audit event. Needs If-Match with the recurring invoice's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `recurring_invoice_id` (path, required): The recurring invoice's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`RecurringInvoiceInput`](../../contracts/schemas/RecurringInvoiceInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringInvoice`](../../contracts/schemas/RecurringInvoice.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `RECURRING-02` The recurring journal is not in that state; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/recurring-invoices/{recurring_invoice_id}/approval`

**Ask for a recurring invoice to be approved to issue and send** (`requestRecurringInvoiceApproval`)

Asks for the current content version of a recurring invoice set to issue and send to be approved (FR-AR-07; the owner's decision of 24 Oct 2026): another person who holds A on "Invoices, credit notes", never who wrote that version (SOD-3), approves it through the approvals, and the scheduler then issues and emails each occurrence's invoice, each listed for the weekly review (FM-3). A change of content withdraws the approval. Refused for one set to make drafts, or whose content is approved already (RECURRING-03). Side effects: the request and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `recurring_invoice_id` (path, required): The recurring invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `RECURRING-02` The recurring journal is not in that state; `RECURRING-03` The recurring invoice needs no approval now; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `RECURRING-03`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/recurring-invoices/{recurring_invoice_id}/pause`

**Pause a recurring invoice** (`pauseRecurringInvoice`)

Stops an active recurring invoice making drafts until it is resumed (FR-AR-07; PRD-001 rev 0.135). Draft invoices already made stay. Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `recurring_invoice_id` (path, required): The recurring invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringInvoice`](../../contracts/schemas/RecurringInvoice.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `RECURRING-02` The recurring journal is not in that state; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `POST /v1/recurring-invoices/{recurring_invoice_id}/resume`

**Resume a recurring invoice** (`resumeRecurringInvoice`)

Starts a paused recurring invoice again (FR-AR-07; PRD-001 rev 0.135): its next draft is for its first occurrence on or after today, so the occurrences it missed while paused are skipped, not made late. Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `recurring_invoice_id` (path, required): The recurring invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`RecurringInvoice`](../../contracts/schemas/RecurringInvoice.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `RECURRING-02` The recurring journal is not in that state; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid; `RECURRING-01` The schedule has no occurrence.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-08`, `INVOICE-09`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `RECURRING-01`, `RECURRING-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/sales-reminders`

**List the reminders due to customers** (`listSalesReminders`)

The reminders due as at the organisation's today, earliest first, a page at a time (FR-AR-08; PRD-001 rev 0.137): each issued invoice with something still open whose customer's schedule (its group's, else 3 days before the due date, on it, and 7 and 30 days after) has reached a step not yet sent or skipped, and each sent quote or order not answered within 3 days of its validity date. Only the latest step due is listed; deciding it settles the earlier ones. While receipts are recorded by hand, nothing is sent by itself: the Inbox shows these for a person to send or skip. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`SalesReminderPage`](../../contracts/schemas/SalesReminderPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/sales-reminders/{document_id}/send`

**Send a reminder due** (`sendSalesReminder`)

Sends the reminder due on an invoice, quote or order at the step the list gives (FR-AR-08; PRD-001 rev 0.137), to the customer's billing email or the address given, from the organisation's reminders sender: worded with the payment reminder email template once the invoice is overdue, else with the invoice's or quote's own, with the PDF already sent of its current version (rendered when none was) and an online link. A reminder not due (settled, answered, not reached, or decided) is refused (REMINDER-02). Side effects: the email message, the send, the link, the decision, and their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `document_id` (path, required): The invoice, quote or order the reminder is of.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SendSalesReminder`](../../contracts/schemas/SendSalesReminder.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`SalesReminderDecision`](../../contracts/schemas/SalesReminderDecision.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `REMINDER-02` That reminder is not due; `SALESDOC-03` Only an issued invoice or a sent quote is emailed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-REMINDER-01` A reminder is decided once, for an issued invoice or a sent quote.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `SALESDOC-02` The invoice's checks stop it being laid out; `SALESDOC-04` There is no address to send it to; `SALESDOC-05` The tax invoice lacks a particular IRAS requires.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again; `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `REMINDER-02`, `SALESDOC-02`, `SALESDOC-03`, `SALESDOC-04`, `SALESDOC-05`, `STORAGE-LOCK-01`, `STORAGE-REMINDER-01`, `STORAGE-RETRY-01`, `TPL-17`

### `POST /v1/sales-reminders/{document_id}/skip`

**Skip a reminder due** (`skipSalesReminder`)

Skips the reminder due on an invoice, quote or order at the step the list gives, with a reason if given (FR-AR-08; PRD-001 rev 0.137): nothing is sent, and the next step in the schedule is listed when it comes. A reminder not due is refused (REMINDER-02). Side effects: the decision and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `document_id` (path, required): The invoice, quote or order the reminder is of.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SkipSalesReminder`](../../contracts/schemas/SkipSalesReminder.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`SalesReminderDecision`](../../contracts/schemas/SalesReminderDecision.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `REMINDER-02` That reminder is not due; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-REMINDER-01` A reminder is decided once, for an issued invoice or a sent quote.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `REMINDER-02`, `STORAGE-LOCK-01`, `STORAGE-REMINDER-01`, `STORAGE-RETRY-01`

### `GET /v1/sales/gst-codes`

**Get the GST codes for sales** (`getSalesGSTCodes`)

The GST codes an invoice dated on the day given is read with (FR-AR-03, ENG-31): the organisation's active sg-gst version in force then, else the one this release carries; its standard rate on the day; and each code for supplies, with whether it bears GST at the standard rate. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `on` (query): The invoice's date; left out, the codes of the newest version.
- **Responses:**
  - `200` OK [`SalesGSTCodes`](../../contracts/schemas/SalesGSTCodes.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/statements`

**List the statements made** (`listStatements`)

The statements of account made, newest first, a page at a time, of one customer when customer_id is given, each as it was made, with its emails (FR-AR-08; PRD-001 rev 0.138). Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `customer_id` (query): Only this customer's statements.
- **Responses:**
  - `200` OK [`StatementPage`](../../contracts/schemas/StatementPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/statements`

**Make a statement to send** (`createStatement`)

Makes a customer's statement of account in one currency for a period ending today at the latest, as getCustomerStatement works it out, and keeps it as a statement document numbered from its sequence (ST-), saying what it said, never changed or voided (FR-AR-08; PRD-001 rev 0.138); emailStatement sends it. One that does not add up (STATEMENT-03), or of a currency in which the customer has nothing in the period (STATEMENT-04), is refused. Side effects: the statement and its audit events; it posts nothing. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`StatementRequest`](../../contracts/schemas/StatementRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Statement`](../../contracts/schemas/Statement.schema.json)
  - `201` Created [`Statement`](../../contracts/schemas/Statement.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STATEMENT-03` The statement does not add up; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `STATEMENT-01` The statement's period is not valid; `STATEMENT-02` Statements are of customers; `STATEMENT-04` The customer has nothing in that currency.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `STATEMENT-01`, `STATEMENT-02`, `STATEMENT-03`, `STATEMENT-04`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/statements/{statement_id}`

**Get a statement made** (`getStatement`)

Returns a statement of account as it was made, with its emails and its ETag (FR-AR-08; PRD-001 rev 0.138). Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `statement_id` (path, required): The statement's ID.
- **Responses:**
  - `200` OK [`Statement`](../../contracts/schemas/Statement.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `STATEMENT-05` Not a statement.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STATEMENT-05`

### `POST /v1/statements/{statement_id}/email`

**Email a statement to the customer** (`emailStatement`)

Emails a statement of account made (FR-AR-08; PRD-001 rev 0.138) to the customer's billing email, or the address given, from the organisation's invoices sender, with the subject and body of the statement email template that applies to the customer and the PDF of it attached, rendered with the statement document template that applies (FR-TPL-05) as of the day it was made. Sending again sends the same statement again. Side effects: the PDF in the statement's evidence (source generated), the email message in the delivery log, the send, and their audit events. Needs If-Match with the statement's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `statement_id` (path, required): The statement's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`EmailDocument`](../../contracts/schemas/EmailDocument.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Statement`](../../contracts/schemas/Statement.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `STATEMENT-05` Not a statement.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `SALESDOC-03` Only an issued invoice or a sent quote is emailed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `SALESDOC-04` There is no address to send it to.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again; `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `SALESDOC-03`, `SALESDOC-04`, `STATEMENT-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TPL-17`

### `GET /v1/write-offs`

**List the write-offs** (`listWriteOffs`)

The write-offs of invoices, newest date first (a draft without a date last), a page at a time, narrowed by status, dates and text in the number (FR-AR-17). Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `status` (query): Only write-offs with this status.
  - `from` (query): Only write-offs dated on or after this day.
  - `to` (query): Only write-offs dated on or before this day.
  - `q` (query): Only write-offs whose number contains this text, ignoring case.
- **Responses:**
  - `200` OK [`WriteOffPage`](../../contracts/schemas/WriteOffPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `WRITEOFF-01` Not a write-off.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid; `WRITEOFF-02` The saved write-off cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `WRITEOFF-01`, `WRITEOFF-02`

### `POST /v1/write-offs`

**Save a draft write-off** (`createWriteOff`)

Saves the write-off of what is left of an invoice as a draft, at version 1 (FR-AR-17): the invoice, the amount in its currency, the reason, the account (left out, the reason's) and why. Nothing posts: requestWriteOffPost asks another person to approve it. getWriteOff shows what posting it would do. Side effects: a draft document and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`WriteOffDraft`](../../contracts/schemas/WriteOffDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`WriteOff`](../../contracts/schemas/WriteOff.schema.json)
  - `201` Created [`WriteOff`](../../contracts/schemas/WriteOff.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `WRITEOFF-12` The explanation is missing or too long.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `WRITEOFF-12`

### `GET /v1/write-offs/{write_off_id}`

**Get a write-off** (`getWriteOff`)

Returns a write-off with its ETag: the invoice and what is open of it, the reason, the account it writes off to, why, the carrying amount it clears, its approval and ledger states, what stops a draft posting (✖), the lines posting it writes or wrote (ledger_impact), and the pending request to post or void it. Read-only; no side effects.

- **Permission:** View on Invoices, credit notes
- **Parameters:**
  - `write_off_id` (path, required): The write-off's ID.
- **Responses:**
  - `200` OK [`WriteOff`](../../contracts/schemas/WriteOff.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `WRITEOFF-01` Not a write-off.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `WRITEOFF-02` The saved write-off cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `WRITEOFF-01`, `WRITEOFF-02`

### `PUT /v1/write-offs/{write_off_id}`

**Change a draft write-off** (`replaceWriteOff`)

Saves the draft's new content as its next version, recording who saved it; a pending request to post it is voided by the change. Only a draft changes. Side effects: the new version and its audit event. Needs If-Match with the draft's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Invoices, credit notes
- **Parameters:**
  - `write_off_id` (path, required): The write-off's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`WriteOffDraft`](../../contracts/schemas/WriteOffDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`WriteOff`](../../contracts/schemas/WriteOff.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `WRITEOFF-01` Not a write-off.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress; `WRITEOFF-07` The write-off is not a draft.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `WRITEOFF-02` The saved write-off cannot be read; `WRITEOFF-12` The explanation is missing or too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `WRITEOFF-01`, `WRITEOFF-02`, `WRITEOFF-07`, `WRITEOFF-12`

### `POST /v1/write-offs/{write_off_id}/post-approval`

**Ask for approval to post a write-off** (`requestWriteOffPost`)

Asks another person to approve a draft write-off at the version read (FR-AR-17, FR-APR-01): every write-off posts only this way, in any team mode. The draft must pass the posting rules. The answer is 202 with an approval request, which a person who holds A on "Invoices, credit notes" approves, stepped up: never the one who asked, prepared the write-off, or recorded a receipt of the invoice (WRITEOFF-11, SOD-9). On the approval it posts write_off@1 as the approver: Cr 1100 with the amount at the invoice's stored rate, Dr the reason's account with the carrying amount, and an allocation that settles the invoice. The request holds the write-off's version, status and carrying amount, so a change voids it. Asking needs a person signed in who passed step-up, and If-Match with the write-off's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Invoices, credit notes. Every write-off posts only on another person's approval: never the one who prepared it or recorded a receipt of the invoice (SOD-9).
- **Parameters:**
  - `write_off_id` (path, required): The write-off's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `WRITEOFF-01` Not a write-off.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress; `WRITEOFF-07` The write-off is not a draft; `WRITEOFF-08` The write-off is not posted.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-26` A payment's amount is not positive or not in its currency; `WRITEOFF-02` The saved write-off cannot be read; `WRITEOFF-03` Not an issued invoice; `WRITEOFF-04` More than the invoice has open; `WRITEOFF-05` Not a reason for a write-off; `WRITEOFF-06` No account for the reason; `WRITEOFF-09` The void is dated before the write-off; `WRITEOFF-12` The explanation is missing or too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-23`, `LEDGER-26`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `WRITEOFF-01`, `WRITEOFF-02`, `WRITEOFF-03`, `WRITEOFF-04`, `WRITEOFF-05`, `WRITEOFF-06`, `WRITEOFF-07`, `WRITEOFF-08`, `WRITEOFF-09`, `WRITEOFF-12`

### `POST /v1/write-offs/{write_off_id}/void`

**Void a write-off** (`voidWriteOff`)

Voids a posted write-off with a reversing entry on the date given, with a reason, and a reversing allocation, which reopens the invoice (FR-GL-12, FR-AR-18); posted history never changes. In Founder mode voiding needs a second person (FM-1): it is refused (WRITEOFF-10), and requestWriteOffVoid asks for the approval. Side effects: the reversing entry and allocation, a new ledger revision, balances, audit events with the reason. Needs If-Match with the write-off's ETag. Idempotent with Idempotency-Key; Prefer: return=preview voids nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `write_off_id` (path, required): The write-off's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`Reversal`](../../contracts/schemas/Reversal.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`WriteOffPosting`](../../contracts/schemas/WriteOffPosting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required; `TEAM-04` Your roles conflict for this action.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal; `WRITEOFF-01` Not a write-off.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-ALLOCATION-01` An allocation never changes and is reversed exactly; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-PERIOD-01` No period covers the date; `WRITEOFF-08` The write-off is not posted; `WRITEOFF-10` Voiding needs a second person.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital; `WRITEOFF-02` The saved write-off cannot be read; `WRITEOFF-09` The void is dated before the write-off.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `FX-01`, `JOURNAL-01`, `JOURNAL-04`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `STORAGE-ALLOCATION-01`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `TEAM-04`, `WRITEOFF-01`, `WRITEOFF-02`, `WRITEOFF-08`, `WRITEOFF-09`, `WRITEOFF-10`

### `POST /v1/write-offs/{write_off_id}/void-approval`

**Ask for approval to void a write-off** (`requestWriteOffVoid`)

Asks for another person's approval to void a posted write-off with a reversing entry on the date given, with a reason (FR-GL-12, FM-1): what voidWriteOff asks for in Founder mode (WRITEOFF-10). The answer is 202 with an approval request, which a person who holds A on "Invoices, credit notes" and did not ask approves, stepped up; on the approval the void posts and the invoice reopens. Asking needs a person signed in who passed step-up, and If-Match with the write-off's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Post on Invoices, credit notes
- **Parameters:**
  - `write_off_id` (path, required): The write-off's ID.
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
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `WRITEOFF-01` Not a write-off.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress; `WRITEOFF-07` The write-off is not a draft; `WRITEOFF-08` The write-off is not posted.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `FX-01` No exchange rate for this currency; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-26` A payment's amount is not positive or not in its currency; `WRITEOFF-02` The saved write-off cannot be read; `WRITEOFF-03` Not an issued invoice; `WRITEOFF-04` More than the invoice has open; `WRITEOFF-05` Not a reason for a write-off; `WRITEOFF-06` No account for the reason; `WRITEOFF-09` The void is dated before the write-off; `WRITEOFF-12` The explanation is missing or too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `FX-01`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-22`, `LEDGER-23`, `LEDGER-26`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `WRITEOFF-01`, `WRITEOFF-02`, `WRITEOFF-03`, `WRITEOFF-04`, `WRITEOFF-05`, `WRITEOFF-06`, `WRITEOFF-07`, `WRITEOFF-08`, `WRITEOFF-09`, `WRITEOFF-12`

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
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CT-01` The legal name is not valid; `CT-02` Neither a customer nor a vendor; `CT-03` Not an email address; `CT-04` Not a country code; `CT-05` Not a currency; `CT-06` Payment terms out of range; `CT-07` Not a GST code; `CT-08` Not a Peppol participant ID; `CT-09` A text is too long; `CT-10` Not an account that takes postings; `CT-11` The early-payment discount terms are not a percentage and a number of days; `REMINDER-03` The customer group cannot be used that way.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `CT-01`, `CT-02`, `CT-03`, `CT-04`, `CT-05`, `CT-06`, `CT-07`, `CT-08`, `CT-09`, `CT-10`, `CT-11`, `REMINDER-03`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

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
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `CT-01` The legal name is not valid; `CT-02` Neither a customer nor a vendor; `CT-03` Not an email address; `CT-04` Not a country code; `CT-05` Not a currency; `CT-06` Payment terms out of range; `CT-07` Not a GST code; `CT-08` Not a Peppol participant ID; `CT-09` A text is too long; `CT-10` Not an account that takes postings; `CT-11` The early-payment discount terms are not a percentage and a number of days; `REMINDER-03` The customer group cannot be used that way.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CT-01`, `CT-02`, `CT-03`, `CT-04`, `CT-05`, `CT-06`, `CT-07`, `CT-08`, `CT-09`, `CT-10`, `CT-11`, `REMINDER-03`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/contacts/{contact_id}/document-templates`

**List a contact's templates** (`listContactDocumentTemplates`)

A contact's template of every kind and document type (FR-TPL-05), a page at a time: the published template chosen for it, or none, when the organisation's default applies. Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `contact_id` (path, required): The contact's ID.
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`DocumentTemplateChoicePage`](../../contracts/schemas/DocumentTemplateChoicePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/contacts/{contact_id}/document-templates/{kind}/{document_type}`

**Choose a contact's template** (`setContactDocumentTemplate`)

Chooses the template a contact's documents of a kind and document type render with, among the type's published templates, or none, so the organisation's default applies (FR-TPL-05; the owner's decision of 23 Oct 2026): set on the contact by whoever edits contacts. A customer's documents are invoices, quotes, credit notes, receipts, statements and reminders; a supplier's, remittance advice and purchase orders (TPL-20). Refused for a template that is not a published one of the kind and type (TPL-19). Side effects: the choice and its audit event on the contact; the contact's version does not change. Needs If-Match with the contact's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Contacts and items (rev 0.33)
- **Parameters:**
  - `contact_id` (path, required): The contact's ID.
  - `kind` (path, required): The kind.
  - `document_type` (path, required): The document type.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`TemplateChoiceChange`](../../contracts/schemas/TemplateChoiceChange.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`DocumentTemplateChoice`](../../contracts/schemas/DocumentTemplateChoice.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-TEMPLATE-01` The template does not change that way.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `TPL-19` Not a published template of this kind and document type; `TPL-20` The contact does not receive this document.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-TEMPLATE-01`, `TPL-19`, `TPL-20`

### `GET /v1/customer-groups`

**List the customer groups** (`listCustomerGroups`)

The customer groups by name, a page at a time, with their reminder schedules, and the standard schedule of customers in none (FR-AR-08; PRD-001 rev 0.137). Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`CustomerGroupList`](../../contracts/schemas/CustomerGroupList.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/customer-groups`

**Add a customer group** (`createCustomerGroup`)

Adds a customer group, active, with its reminder schedule: at most 6 days from -30 to 365, before (negative) or after an invoice's due date, earliest first; [] sends no reminders (FR-AR-08; PRD-001 rev 0.137). A customer joins it through its contact's customer_group_id. Side effects: the group and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Contacts and items (rev 0.33)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CustomerGroupInput`](../../contracts/schemas/CustomerGroupInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerGroup`](../../contracts/schemas/CustomerGroup.schema.json)
  - `201` Created [`CustomerGroup`](../../contracts/schemas/CustomerGroup.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `REMINDER-01` The reminder schedule is not valid; `REMINDER-03` The customer group cannot be used that way.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `REMINDER-01`, `REMINDER-03`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/customer-groups/{customer_group_id}`

**Get a customer group** (`getCustomerGroup`)

Returns a customer group with its reminder schedule and its ETag (FR-AR-08; PRD-001 rev 0.137). Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `customer_group_id` (path, required): The customer group's ID.
- **Responses:**
  - `200` OK [`CustomerGroup`](../../contracts/schemas/CustomerGroup.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PUT /v1/customer-groups/{customer_group_id}`

**Change a customer group** (`replaceCustomerGroup`)

Replaces a customer group's name, reminder schedule and whether it is active (FR-AR-08; PRD-001 rev 0.137). Its customers follow the new schedule from now on; reminders sent or skipped stay as they were. An inactive group keeps its customers and is given to no other. Side effects: the next version and its audit event. Needs If-Match with the group's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Contacts and items (rev 0.33)
- **Parameters:**
  - `customer_group_id` (path, required): The customer group's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`CustomerGroupInput`](../../contracts/schemas/CustomerGroupInput.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`CustomerGroup`](../../contracts/schemas/CustomerGroup.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `REMINDER-01` The reminder schedule is not valid; `REMINDER-03` The customer group cannot be used that way.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `REMINDER-01`, `REMINDER-03`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required; `APR-01` You asked for it, so another person approves it; `APR-05` You hold no approval right on it; `APR-11` The request's current step needs someone else; `BILL-11` You prepared it, so another person approves it; `INVOICE-12` You prepared it, so another person approves it; `JOURNAL-09` You prepared it, so another person approves it; `PO-15` You prepared it, so another person approves it; `TEAM-04` Your roles conflict for this action; `TPL-15` You saved the draft, so another person approves it; `WRITEOFF-11` Another person approves the write-off (SOD-9).
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `APPLY-01` Not a customer credit application; `APPLY-10` Not a customer refund; `BILL-01` Not a bill; `BILLPAY-01` Not a payment of bills; `CREDIT-01` Not a credit application; `CREDIT-10` Not a supplier refund; `DEPOSIT-01` Not a customer deposit; `INVOICE-01` Not an invoice; `JOURNAL-01` Not a manual journal; `PO-01` Not a purchase order; `RECEIPT-01` Not a customer receipt; `WRITEOFF-01` Not a write-off.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `API-PASSKEY-03` The challenge cannot be used; `APPLY-06` The application is not posted; `APR-02` The request was decided already; `APR-03` The request expired; `APR-04` The envelope differs: nothing was changed; `APR-06` A change to it is waiting for approval already; `APR-09` This approval policy is fixed; `APR-10` An earlier approver no longer holds the right: nothing was changed; `BILL-02` The bill is not a draft; `BILL-03` The bill is not posted; `BILL-13` Payments settle the bill; `BILLPAY-06` The payment is not posted; `CREDIT-06` The application is not posted; `DEPOSIT-06` The deposit is not posted; `DEPOSIT-08` Invoices or refunds use the deposit; `INVOICE-02` The invoice is not a draft; `INVOICE-03` The invoice is not issued; `INVOICE-13` Receipts or credits settle the invoice; `JOURNAL-02` The journal is not a draft; `JOURNAL-03` The journal is not posted; `MEM-06` The last Owner keeps the role; `MEM-07` The member holds that role already, or does not hold it; `PACK-07` This release cannot read that pack version; `PACK-09` A pinned statutory test fails; `PACK-10` Activating it needs a second approver; `PACK-13` That pack version is activated already; `PO-10` The order is not a draft; `RECEIPT-06` The receipt is not posted; `SET-04` Set by the platform; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DOC-04` An edit posts its reversal and its new version together; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-MEMBER-01` The membership or role cannot change that way; `STORAGE-PASSKEY-01` The passkey or its challenge cannot be used that way; `STORAGE-PERIOD-01` No period covers the date; `STORAGE-TEMPLATE-01` The template does not change that way; `TPL-12` That version is in force already; `WRITEOFF-07` The write-off is not a draft; `WRITEOFF-08` The write-off is not posted.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `API-PASSKEY-01` The passkey's answer was refused; `APPLY-02` The saved application cannot be read; `APPLY-07` The void is dated before the application; `APR-07` A rejection needs its reason; `APR-08` The policy's steps cannot be signed; `BILL-04` The saved bill cannot be read; `BILL-05` The bill's supplier is not an active vendor; `BILL-06` Not a GST code for purchases; `BILL-07` The due date is before the bill's date; `BILL-08` A text of the bill is too long; `BILL-14` An input-tax fact of the bill is not one Fyniq reads; `BILL-15` A line's business share is not a percentage from 0 to 100; `BILL-16` The bill does not say what the non-resident supplier's payment is for; `BILLPAY-02` The saved payment cannot be read; `BILLPAY-07` The void is dated before the payment; `CREDIT-02` The saved application cannot be read; `CREDIT-07` The void is dated before the application; `DEPOSIT-02` The saved deposit cannot be read; `DEPOSIT-07` The void is dated before the deposit; `FX-01` No exchange rate for this currency; `INVOICE-04` The saved invoice cannot be read; `INVOICE-05` The invoice's customer is not an active customer; `INVOICE-06` Not a GST code for supplies; `INVOICE-07` The due date is before the invoice's date, or on a credit note; `INVOICE-08` A text of the invoice is too long; `INVOICE-09` A line's quantity, price or discount cannot be one; `INVOICE-15` The credit note cannot credit that invoice; `INVOICE-16` The invoice cannot deduct that deposit; `JOURNAL-04` The saved journal cannot be read; `JOURNAL-11` The automatic reversal is not after the journal; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `LEDGER-17` An opening balance not in the base currency; `LEDGER-21` A bill's total is more than zero; `LEDGER-22` A system account is missing; `LEDGER-23` The line is on an account the rule posts to; `LEDGER-24` A line is in another currency; `LEDGER-26` A payment's amount is not positive or not in its currency; `LEDGER-28` A line's spread is not one the rules allow; `LEDGER-29` A share of a line's GST is not a percentage from 0 to 100; `LEDGER-30` The bill's withholding tax cannot be worked out; `LEDGER-31` An invoice's total is not more than zero; `LEDGER-32` A line's GST rate is not one the rules read; `LEDGER-33` A deposit the invoice deducts is not one it takes; `MEM-01` Not a role a member can hold; `MEM-02` The name is not valid; `MEM-03` No role, or a role named twice; `MEM-04` Not an expiry for these roles; `PO-02` The saved order cannot be read; `PO-03` The order's supplier is not an active vendor; `PO-04` The order has no date; `PO-05` The order has no lines, or no total; `PO-06` A line's account cannot take it; `PO-07` A line's quantity or price is not one an order takes; `PO-08` Not a GST code for purchases; `PO-09` The order's text is too long; `RECEIPT-02` The saved receipt cannot be read; `RECEIPT-07` The void is dated before the receipt; `SET-01` Not a value for this setting; `SET-02` A setting never changes in the past; `SET-03` The setting can only lengthen; `SET-06` Send a value or a reset; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital; `TPL-01` Not a template Fyniq serves; `TPL-02` The template's name is blank or too long; `TPL-03` Not this template format; `TPL-04` A paper size or margin outside the choices; `TPL-05` A font Fyniq does not ship, or an accent colour too pale to read; `TPL-06` A block the document does not take, or one repeated; `TPL-07` IRAS particulars cannot be removed or hidden; `TPL-08` A text the block does not take, or too long; `TPL-09` Not a field of the catalogue; `TPL-10` The email's subject is blank, on several lines or too long; `TPL-11` The email's body is blank or too long; `TPL-13` No such version of the template; `TPL-16` A test document lacks an IRAS particular; `WRITEOFF-02` The saved write-off cannot be read; `WRITEOFF-03` Not an issued invoice; `WRITEOFF-04` More than the invoice has open; `WRITEOFF-05` Not a reason for a write-off; `WRITEOFF-06` No account for the reason; `WRITEOFF-09` The void is dated before the write-off; `WRITEOFF-12` The explanation is missing or too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again; `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-PASSKEY-01`, `API-PASSKEY-03`, `API-RATE-01`, `API-UNAVAILABLE-01`, `APPLY-01`, `APPLY-02`, `APPLY-06`, `APPLY-07`, `APPLY-10`, `APR-01`, `APR-02`, `APR-03`, `APR-04`, `APR-05`, `APR-06`, `APR-07`, `APR-08`, `APR-09`, `APR-10`, `APR-11`, `BILL-01`, `BILL-02`, `BILL-03`, `BILL-04`, `BILL-05`, `BILL-06`, `BILL-07`, `BILL-08`, `BILL-11`, `BILL-13`, `BILL-14`, `BILL-15`, `BILL-16`, `BILLPAY-01`, `BILLPAY-02`, `BILLPAY-06`, `BILLPAY-07`, `CREDIT-01`, `CREDIT-02`, `CREDIT-06`, `CREDIT-07`, `CREDIT-10`, `DEPOSIT-01`, `DEPOSIT-02`, `DEPOSIT-06`, `DEPOSIT-07`, `DEPOSIT-08`, `FX-01`, `INVOICE-01`, `INVOICE-02`, `INVOICE-03`, `INVOICE-04`, `INVOICE-05`, `INVOICE-06`, `INVOICE-07`, `INVOICE-08`, `INVOICE-09`, `INVOICE-12`, `INVOICE-13`, `INVOICE-15`, `INVOICE-16`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-03`, `JOURNAL-04`, `JOURNAL-09`, `JOURNAL-11`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `LEDGER-17`, `LEDGER-21`, `LEDGER-22`, `LEDGER-23`, `LEDGER-24`, `LEDGER-26`, `LEDGER-28`, `LEDGER-29`, `LEDGER-30`, `LEDGER-31`, `LEDGER-32`, `LEDGER-33`, `MEM-01`, `MEM-02`, `MEM-03`, `MEM-04`, `MEM-06`, `MEM-07`, `PACK-07`, `PACK-09`, `PACK-10`, `PACK-13`, `PO-01`, `PO-02`, `PO-03`, `PO-04`, `PO-05`, `PO-06`, `PO-07`, `PO-08`, `PO-09`, `PO-10`, `PO-15`, `RECEIPT-01`, `RECEIPT-02`, `RECEIPT-06`, `RECEIPT-07`, `SET-01`, `SET-02`, `SET-03`, `SET-04`, `SET-06`, `STORAGE-APPROVAL-01`, `STORAGE-DOC-02`, `STORAGE-DOC-04`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-MEMBER-01`, `STORAGE-PASSKEY-01`, `STORAGE-PERIOD-01`, `STORAGE-RETRY-01`, `STORAGE-TEMPLATE-01`, `STORAGE-VERSION-01`, `TEAM-04`, `TPL-01`, `TPL-02`, `TPL-03`, `TPL-04`, `TPL-05`, `TPL-06`, `TPL-07`, `TPL-08`, `TPL-09`, `TPL-10`, `TPL-11`, `TPL-12`, `TPL-13`, `TPL-15`, `TPL-16`, `TPL-17`, `WRITEOFF-01`, `WRITEOFF-02`, `WRITEOFF-03`, `WRITEOFF-04`, `WRITEOFF-05`, `WRITEOFF-06`, `WRITEOFF-07`, `WRITEOFF-08`, `WRITEOFF-09`, `WRITEOFF-11`, `WRITEOFF-12`

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

### `GET /v1/bills/{bill_id}/evidence`

**List a bill's evidence** (`listBillEvidence`)

The files attached to a bill or a vendor credit, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.

- **Permission:** View on Evidence: add, supersede (rev 0.3). Needs the right to view bills as well.
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`BillEvidencePage`](../../contracts/schemas/BillEvidencePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`

### `POST /v1/bills/{bill_id}/evidence`

**Attach evidence to a bill** (`addBillEvidence`)

Attaches a file, such as the supplier's invoice, to a bill or a vendor credit (FR-DOC-05, FR-DOC-11; PRD-001 rev 0.86), as multipart/form-data. The file's type is decided from its content; it is scanned for malware and its image metadata, such as location, is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its audit event with the SHA-256 of the file as received and as stored, the uploader, the source and the bill (FR-DOC-07). On a draft the file can be removed; on a posted or voided bill it is committed to the retention-locked store and is append-only (FR-DOC-13). Needs If-Match with the bill's ETag, so the file goes on the bill as you read it. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and saves nothing. AI and MCP clients cannot upload: they propose doc.attach.

- **Permission:** Create on Evidence: add, supersede (rev 0.3). Needs the right to view bills as well. AI and MCP clients only propose attaching a document (doc.attach), which a person approves; the evidence rules per activity (FR-DOC-06) arrive later in P1.
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BillEvidence`](../../contracts/schemas/BillEvidence.schema.json)
  - `201` Created [`BillEvidence`](../../contracts/schemas/BillEvidence.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `DOC-01` Not a file type Fyniq accepts; `DOC-02` The file is empty or larger than 25 MB; `DOC-03` The malware scan flagged the file; `DOC-04` The evidence type is blank, or a text is too long; `DOC-05` Not a data class this record can hold; `DOC-06` Not how a file arrives here; `DOC-07` The picture cannot be read; `DOC-08` Office files with macros are refused; `DOC-09` The office file's archive is not safe to open.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `BILL-01`, `DOC-01`, `DOC-02`, `DOC-03`, `DOC-04`, `DOC-05`, `DOC-06`, `DOC-07`, `DOC-08`, `DOC-09`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/bills/{bill_id}/evidence/{evidence_id}`

**Get a bill's evidence file** (`getBillEvidence`)

Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.

- **Permission:** View on Evidence: add, supersede (rev 0.3). Needs the right to view bills as well.
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `evidence_id` (path, required): The file's ID.
- **Responses:**
  - `200` OK [`BillEvidence`](../../contracts/schemas/BillEvidence.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`

### `DELETE /v1/bills/{bill_id}/evidence/{evidence_id}`

**Remove evidence from a draft bill** (`removeBillEvidence`)

Takes a file off a draft bill or vendor credit (FR-DOC-07). Once it is posted its evidence is append-only, and the database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by the next sweep. Side effects: the removal and its audit event. Needs If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Evidence: add, supersede (rev 0.3). Needs the right to view bills as well; only while the bill is a draft (FR-DOC-07).
- **Parameters:**
  - `bill_id` (path, required): The bill's ID.
  - `evidence_id` (path, required): The file's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BillEvidence`](../../contracts/schemas/BillEvidence.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `BILL-01` Not a bill.
  - `409` Conflict: `STORAGE-EVIDENCE-01` Evidence is append-only once its record is final; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `BILL-01`, `STORAGE-EVIDENCE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/invoices/{invoice_id}/evidence`

**List an invoice's evidence** (`listInvoiceEvidence`)

The files attached to an invoice, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.

- **Permission:** View on Evidence: add, supersede (rev 0.3). Needs the right to view invoices as well.
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`InvoiceEvidencePage`](../../contracts/schemas/InvoiceEvidencePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-01`

### `POST /v1/invoices/{invoice_id}/evidence`

**Attach evidence to an invoice** (`addInvoiceEvidence`)

Attaches a file, such as the customer's purchase order, to an invoice (FR-AR-03, FR-DOC-05, FR-DOC-11; PRD-001 rev 0.117), as multipart/form-data. The file's type is decided from its content; it is scanned for malware and its image metadata, such as location, is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its audit event with the SHA-256 of the file as received and as stored, the uploader, the source and the invoice (FR-DOC-07). On a draft the file can be removed; on an issued or voided invoice it is committed to the retention-locked store and is append-only (FR-DOC-13). Needs If-Match with the invoice's ETag, so the file goes on the invoice as you read it. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and saves nothing. AI and MCP clients cannot upload: they propose doc.attach.

- **Permission:** Create on Evidence: add, supersede (rev 0.3). Needs the right to view invoices as well. AI and MCP clients only propose attaching a document (doc.attach), which a person approves; the evidence rules per activity (FR-DOC-06) arrive later in P1.
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`InvoiceEvidence`](../../contracts/schemas/InvoiceEvidence.schema.json)
  - `201` Created [`InvoiceEvidence`](../../contracts/schemas/InvoiceEvidence.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `DOC-01` Not a file type Fyniq accepts; `DOC-02` The file is empty or larger than 25 MB; `DOC-03` The malware scan flagged the file; `DOC-04` The evidence type is blank, or a text is too long; `DOC-05` Not a data class this record can hold; `DOC-06` Not how a file arrives here; `DOC-07` The picture cannot be read; `DOC-08` Office files with macros are refused; `DOC-09` The office file's archive is not safe to open.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here; `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `DOC-01`, `DOC-02`, `DOC-03`, `DOC-04`, `DOC-05`, `DOC-06`, `DOC-07`, `DOC-08`, `DOC-09`, `INVOICE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/invoices/{invoice_id}/evidence/{evidence_id}`

**Get an invoice's evidence file** (`getInvoiceEvidence`)

Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.

- **Permission:** View on Evidence: add, supersede (rev 0.3). Needs the right to view invoices as well.
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `evidence_id` (path, required): The file's ID.
- **Responses:**
  - `200` OK [`InvoiceEvidence`](../../contracts/schemas/InvoiceEvidence.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-01`

### `DELETE /v1/invoices/{invoice_id}/evidence/{evidence_id}`

**Remove evidence from a draft invoice** (`removeInvoiceEvidence`)

Takes a file off a draft invoice (FR-DOC-07). Once it is issued its evidence is append-only, and the database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by the next sweep. Side effects: the removal and its audit event. Needs If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Evidence: add, supersede (rev 0.3). Needs the right to view invoices as well; only while the invoice is a draft (FR-DOC-07).
- **Parameters:**
  - `invoice_id` (path, required): The invoice's ID.
  - `evidence_id` (path, required): The file's ID.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`InvoiceEvidence`](../../contracts/schemas/InvoiceEvidence.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `INVOICE-01` Not an invoice.
  - `409` Conflict: `STORAGE-EVIDENCE-01` Evidence is append-only once its record is final; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `INVOICE-01`, `STORAGE-EVIDENCE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

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

## Notifications

Each person's notification centre and preferences, produced from one event stream (FR-NTF-01, FR-NTF-02, FR-NTF-05).

### `GET /v1/notification-preferences`

**Get your notification preferences** (`getNotificationPreferences`)

How the caller gets each notification type by email (FR-NTF-02; the owner's decisions of 24 Oct 2026): at once for security and approvals, in the daily digest at 08:00 Singapore time for the rest, unless they chose otherwise; in the app always. Security notices are locked on. And their quiet hours. The recommended values for a caller who is not a person. Read-only; no side effects.

- **Permission:** View on Own notifications and preferences (rev 0.6)
- **Responses:**
  - `200` OK [`NotificationPreferences`](../../contracts/schemas/NotificationPreferences.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/notification-preferences/{type}`

**Choose how a notification type is emailed** (`setNotificationPreference`)

Chooses how a notification type goes to the caller by email: at once, in the daily or weekly digest, or not at all (FR-NTF-02); in the app it always shows. Refused for a security notice, which is locked on (NTF-01), and for a type that does not exist (NTF-03). Side effects: the preference and its audit event. Needs If-Match with the type's etag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `type` (path, required): The notification type.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`EmailPreferenceChange`](../../contracts/schemas/EmailPreferenceChange.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`NotificationPreferences`](../../contracts/schemas/NotificationPreferences.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `NTF-03` No such notification type.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `NTF-01` A security notice is always on; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `EML-08`, `NTF-01`, `NTF-03`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/notification-quiet-hours`

**Set your quiet hours** (`setQuietHours`)

Sets when the caller's immediate email waits until the hours end, Singapore time, or clears them when both are left out (FR-NTF-02); security notices never wait. Times are HH:MM (NTF-02); hours that end before they start run past midnight. Side effects: the preference and its audit event. Needs If-Match with quiet_hours_etag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`QuietHoursChange`](../../contracts/schemas/QuietHoursChange.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`NotificationPreferences`](../../contracts/schemas/NotificationPreferences.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own; `NTF-02` Not a time of day.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `EML-08`, `NTF-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/notifications`

**List your notifications** (`listNotifications`)

The caller's notifications, newest first, with the bell's unread count (FR-NTF-01): what happened, each with its type, severity, the record it is about and a link; an actionable one links to its Inbox item, where what waits for the caller is. Archived ones only with archived=true. Empty for a caller who is not a person. Read-only; no side effects.

- **Permission:** View on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `unread` (query): true for unread ones only.
  - `archived` (query): true for archived ones instead.
- **Responses:**
  - `200` OK [`NotificationPage`](../../contracts/schemas/NotificationPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/notifications/read-all`

**Mark all your notifications read** (`readAllNotifications`)

Marks every unread notification of the caller read (FR-NTF-01). Side effects: their notifications change for them alone. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`MarkedAll`](../../contracts/schemas/MarkedAll.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-RATE-01`, `EML-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/notifications/test-messages`

**Send yourself a test notification** (`sendTestNotification`)

Sends the caller a notification of the type notification.test through every channel they receive it on (FR-NTF-05): in the app always, and by email at once unless their preference for it says otherwise, to their verified sign-in email. It goes through the same event stream, fan-out and outbox as any notification, so its email shows in the delivery log. Side effects: the event, and the caller's notification and email once the worker fans it out (202). Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`TestNotification`](../../contracts/schemas/TestNotification.schema.json)
  - `202` Accepted [`TestNotification`](../../contracts/schemas/TestNotification.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-RATE-01`, `EML-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/notifications/{notification_id}/archive`

**Archive a notification** (`archiveNotification`)

Archives one of the caller's notifications, for good: it leaves the list and the count (FR-NTF-01). Side effects: the notification changes for the caller alone. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `notification_id` (path, required): The notification's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Notification`](../../contracts/schemas/Notification.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `EML-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/notifications/{notification_id}/read`

**Mark a notification read** (`readNotification`)

Marks one of the caller's notifications read (FR-NTF-01). Side effects: the notification changes for the caller alone. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `notification_id` (path, required): The notification's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Notification`](../../contracts/schemas/Notification.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `EML-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/notifications/{notification_id}/unread`

**Mark a notification unread** (`unreadNotification`)

Marks one of the caller's notifications unread again (FR-NTF-01). Side effects: the notification changes for the caller alone. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `notification_id` (path, required): The notification's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Notification`](../../contracts/schemas/Notification.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `EML-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

## Announcements

Banners under the header: those each person sees now, and the announcements the Owner and Administrator publish to their organisation (FR-NTF-03).

### `GET /v1/announcements`

**List the organisation's announcements** (`listAnnouncements`)

Every banner the organisation published, newest first, with its status: scheduled, live or ended (FR-NTF-03). Read-only; no side effects.

- **Permission:** View on Announcements to the organisation (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`AnnouncementList`](../../contracts/schemas/AnnouncementList.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/announcements`

**Publish a banner to the organisation** (`publishAnnouncement`)

Publishes a banner under the header for the organisation's members, or those holding the roles named, from its start (now unless later) until its end (FR-NTF-03): a message and a link (a path in Fyniq or an https address), a severity, and whether it may be dismissed (never a critical one). It never changes; withdraw it to end it early. Side effects: the announcement and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and publishes nothing.

- **Permission:** Create on Announcements to the organisation (rev 0.6)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`AnnouncementRequest`](../../contracts/schemas/AnnouncementRequest.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Announcement`](../../contracts/schemas/Announcement.schema.json)
  - `201` Created [`Announcement`](../../contracts/schemas/Announcement.schema.json)
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

### `POST /v1/announcements/{announcement_id}/withdraw`

**Withdraw an announcement** (`withdrawAnnouncement`)

Ends one of the organisation's banners now, before its end, or cancels a scheduled one (FR-NTF-03); NTF-06 when it ended or was withdrawn already. Side effects: the withdrawal and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing.

- **Permission:** Create on Announcements to the organisation (rev 0.6)
- **Parameters:**
  - `announcement_id` (path, required): The banner's or announcement's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Announcement`](../../contracts/schemas/Announcement.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `NTF-06` The announcement has ended; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `NTF-06`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/banners`

**List the banners you see now** (`listBanners`)

The banners that show under the header for the caller now (FR-NTF-03): the organisation's, from its Owner or Administrator, and the installation's, from a Super admin, while they are live and meant for everyone or for one of the caller's roles, but those the caller dismissed. Critical first. Read-only; no side effects.

- **Permission:** View on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`BannerList`](../../contracts/schemas/BannerList.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/banners/{announcement_id}/dismiss`

**Dismiss a banner** (`dismissBanner`)

Hides a banner showing to the caller, for the caller alone, on every device: the dismissal is kept on the server (FR-NTF-03). A critical banner, or one published as not dismissible, stays until it ends (NTF-04); NTF-05 for no banner showing with that ID. Side effects: the dismissal. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Own notifications and preferences (rev 0.6)
- **Parameters:**
  - `announcement_id` (path, required): The banner's or announcement's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`BannerList`](../../contracts/schemas/BannerList.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `NTF-05` No such banner showing.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `NTF-04` A banner that stays until it ends; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `EML-08` Not a person with an address of their own.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `EML-08`, `NTF-04`, `NTF-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

## Templates

Document and email templates as data: drafts, and numbered versions published under approval that never change (FR-TPL-01…05).

### `GET /v1/document-template-bundle`

**Export templates as a bundle** (`exportDocumentTemplates`)

Exports one to 50 templates as a versioned bundle (FR-TPL-07): each the version in force, or the draft of one never published, with its content's SHA-256, for importDocumentTemplates in another organisation or installation. Read-only; no side effects.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `template_id` (query): The templates, one to 50, each as its own template_id parameter.
- **Responses:**
  - `200` OK [`TemplateBundle`](../../contracts/schemas/TemplateBundle.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/document-template-bundles`

**Import templates from a bundle** (`importDocumentTemplates`)

Makes a draft of each template of a bundle from exportDocumentTemplates (FR-TPL-07), all or none. Each content must match its SHA-256 (TPL-18) and meet the template rules as they are here (TPL-01 to TPL-11); every problem comes back together. Nothing imported is in force: each draft is published under approval like any other (FR-TPL-04). Side effects: the drafts and their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`TemplateBundle`](../../contracts/schemas/TemplateBundle.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`TemplateImport`](../../contracts/schemas/TemplateImport.schema.json)
  - `201` Created [`TemplateImport`](../../contracts/schemas/TemplateImport.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `TPL-01` Not a template Fyniq serves; `TPL-02` The template's name is blank or too long; `TPL-03` Not this template format; `TPL-04` A paper size or margin outside the choices; `TPL-05` A font Fyniq does not ship, or an accent colour too pale to read; `TPL-06` A block the document does not take, or one repeated; `TPL-07` IRAS particulars cannot be removed or hidden; `TPL-08` A text the block does not take, or too long; `TPL-09` Not a field of the catalogue; `TPL-10` The email's subject is blank, on several lines or too long; `TPL-11` The email's body is blank or too long; `TPL-18` Not a bundle of templates Fyniq reads.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TPL-01`, `TPL-02`, `TPL-03`, `TPL-04`, `TPL-05`, `TPL-06`, `TPL-07`, `TPL-08`, `TPL-09`, `TPL-10`, `TPL-11`, `TPL-18`

### `GET /v1/document-template-catalogue/{document_type}`

**Read what a template is made of** (`getDocumentTemplateCatalogue`)

What templates of a document type are made of (FR-TPL-01 to FR-TPL-03): its blocks, with the texts each takes and those locked because they carry IRAS particulars; the typed field catalogue and each type's formats; the fonts, paper sizes, margins and least accent contrast; and the content a new document or email template starts from. Read-only; no side effects.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `document_type` (path, required): The document type.
- **Responses:**
  - `200` OK [`TemplateCatalogue`](../../contracts/schemas/TemplateCatalogue.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/document-template-defaults`

**List the default templates** (`listDocumentTemplateDefaults`)

The default template of every kind and document type (FR-TPL-05), a page at a time: the published template chosen, or none, when the platform's classic starter renders the type's documents as version 0. Read-only; no side effects.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`DocumentTemplateChoicePage`](../../contracts/schemas/DocumentTemplateChoicePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/document-template-defaults/{kind}/{document_type}`

**Choose a default template** (`setDocumentTemplateDefault`)

Chooses the default template of a kind and document type among its published templates, or none, so the platform's classic starter renders (FR-TPL-05; the owner's decision of 23 Oct 2026): a person who holds the approve right chooses, with no separate request, since each template was approved when it was published. A contact's override comes first. Refused for a template that is not a published one of the kind and type (TPL-19). Side effects: the choice and its audit event. Needs If-Match with the choice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Approve on Document templates: edit, publish (rev 0.7). Among templates approved when they were published, so no separate request (the owner's decision of 23 Oct 2026).
- **Parameters:**
  - `kind` (path, required): The kind.
  - `document_type` (path, required): The document type.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`TemplateChoiceChange`](../../contracts/schemas/TemplateChoiceChange.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`DocumentTemplateChoice`](../../contracts/schemas/DocumentTemplateChoice.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-TEMPLATE-01` The template does not change that way.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `TPL-19` Not a published template of this kind and document type.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-TEMPLATE-01`, `TPL-19`

### `POST /v1/document-template-previews`

**Preview template content** (`previewDocumentTemplate`)

Fills template content, saved or not, with the organisation's own details and made-up sample data of its document type (FR-TPL-01's live preview), so a detail the organisation's profile lacks shows as missing: a document as PDF/A-2a and PDF/UA-1 from the render service, with the IRAS particulars it would lack (FR-TPL-03), or an email's subject and body. Every rule the content breaks comes back together (TPL-01 to TPL-11). It saves nothing and has no side effects. TPL-17 when the render service is not configured or busy.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Request body:** [`TemplatePreviewRequest`](../../contracts/schemas/TemplatePreviewRequest.schema.json)
- **Responses:**
  - `200` OK [`TemplatePreview`](../../contracts/schemas/TemplatePreview.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `TPL-01` Not a template Fyniq serves; `TPL-02` The template's name is blank or too long; `TPL-03` Not this template format; `TPL-04` A paper size or margin outside the choices; `TPL-05` A font Fyniq does not ship, or an accent colour too pale to read; `TPL-06` A block the document does not take, or one repeated; `TPL-07` IRAS particulars cannot be removed or hidden; `TPL-08` A text the block does not take, or too long; `TPL-09` Not a field of the catalogue; `TPL-10` The email's subject is blank, on several lines or too long; `TPL-11` The email's body is blank or too long.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `TPL-01`, `TPL-02`, `TPL-03`, `TPL-04`, `TPL-05`, `TPL-06`, `TPL-07`, `TPL-08`, `TPL-09`, `TPL-10`, `TPL-11`, `TPL-17`

### `GET /v1/document-template-resolution`

**Find the template that applies** (`getAppliedDocumentTemplate`)

The template version that renders a document of a kind and document type, for a contact when named (FR-TPL-05): the contact's override, else the organisation's default, else the platform's classic starter as version 0, with the version and content hash each document records (FR-TPL-04). Read-only; no side effects.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `kind` (query, required): The kind.
  - `document_type` (query, required): The document type.
  - `contact_id` (query): The customer or supplier the document is for, whose override applies first.
- **Responses:**
  - `200` OK [`AppliedTemplate`](../../contracts/schemas/AppliedTemplate.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/document-templates`

**List document and email templates** (`listDocumentTemplates`)

The organisation's templates for invoices, quotes, credit notes, receipts, statements, reminders, remittances and purchase orders, and the emails that carry them (FR-TPL-05), a page at a time, each with its draft and the version in force (FR-TPL-04). Read-only; no side effects.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
  - `kind` (query): Only templates of this kind.
  - `document_type` (query): Only templates of this document type.
- **Responses:**
  - `200` OK [`DocumentTemplatePage`](../../contracts/schemas/DocumentTemplatePage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/document-templates`

**Make a template** (`createDocumentTemplate`)

Makes a document or email template as a draft (FR-TPL-04, FR-TPL-05), from the content sent or, left out, the catalogue's default for its kind and document type. Every rule the content breaks comes back together (TPL-01 to TPL-11): blocks the type takes, the IRAS particulars kept on invoices and credit notes (FR-TPL-03), fields from the typed catalogue (FR-TPL-02), a readable accent colour. Nothing is in force until a version is published. Side effects: the template and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`DocumentTemplateDraft`](../../contracts/schemas/DocumentTemplateDraft.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`DocumentTemplate`](../../contracts/schemas/DocumentTemplate.schema.json)
  - `201` Created [`DocumentTemplate`](../../contracts/schemas/DocumentTemplate.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `TPL-01` Not a template Fyniq serves; `TPL-02` The template's name is blank or too long; `TPL-03` Not this template format; `TPL-04` A paper size or margin outside the choices; `TPL-05` A font Fyniq does not ship, or an accent colour too pale to read; `TPL-06` A block the document does not take, or one repeated; `TPL-07` IRAS particulars cannot be removed or hidden; `TPL-08` A text the block does not take, or too long; `TPL-09` Not a field of the catalogue; `TPL-10` The email's subject is blank, on several lines or too long; `TPL-11` The email's body is blank or too long.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TPL-01`, `TPL-02`, `TPL-03`, `TPL-04`, `TPL-05`, `TPL-06`, `TPL-07`, `TPL-08`, `TPL-09`, `TPL-10`, `TPL-11`

### `GET /v1/document-templates/{template_id}`

**Read a template** (`getDocumentTemplate`)

A template with its draft, the version in force and whether the draft differs from it (FR-TPL-04). Read-only; no side effects.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `template_id` (path, required): The template's ID.
- **Responses:**
  - `200` OK [`DocumentTemplate`](../../contracts/schemas/DocumentTemplate.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `PUT /v1/document-templates/{template_id}`

**Save a template's draft** (`replaceDocumentTemplate`)

Saves a template's name and draft content, whole, as its next version (FR-TPL-04). The version in force does not change: documents keep rendering from it until a new version is published. Every rule the content breaks comes back together (TPL-01 to TPL-11). Side effects: the draft and its audit event, with the content before and after. Needs If-Match with the template's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `template_id` (path, required): The template's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`DocumentTemplateChanges`](../../contracts/schemas/DocumentTemplateChanges.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`DocumentTemplate`](../../contracts/schemas/DocumentTemplate.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `TPL-01` Not a template Fyniq serves; `TPL-02` The template's name is blank or too long; `TPL-03` Not this template format; `TPL-04` A paper size or margin outside the choices; `TPL-05` A font Fyniq does not ship, or an accent colour too pale to read; `TPL-06` A block the document does not take, or one repeated; `TPL-07` IRAS particulars cannot be removed or hidden; `TPL-08` A text the block does not take, or too long; `TPL-09` Not a field of the catalogue; `TPL-10` The email's subject is blank, on several lines or too long; `TPL-11` The email's body is blank or too long.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`, `TPL-01`, `TPL-02`, `TPL-03`, `TPL-04`, `TPL-05`, `TPL-06`, `TPL-07`, `TPL-08`, `TPL-09`, `TPL-10`, `TPL-11`

### `POST /v1/document-templates/{template_id}/approval`

**Ask for approval to publish a template** (`requestDocumentTemplatePublication`)

Asks for another person's approval to publish the template's draft or, with version, to roll back to that earlier version (FR-TPL-04, FR-APR-01). The answer is 202 with an approval request, which a person who holds A on "Document templates: edit, publish", did not ask and, for the draft, did not save it (SOD-3) approves, stepped up; on the approval the version is published, by the approver. The request holds the template's version, the SHA-256 of the content and the version in force, so a change to any voids it. Refused like publishDocumentTemplate when there is nothing to publish. Asking needs a person signed in who passed step-up, and If-Match with the template's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.

- **Permission:** Create on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `template_id` (path, required): The template's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`TemplatePublication`](../../contracts/schemas/TemplatePublication.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `202` Accepted [`Approval`](../../contracts/schemas/Approval.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `APR-06` A change to it is waiting for approval already; `STORAGE-APPROVAL-01` The approval cannot be made that way; `STORAGE-LOCK-01` Another change is in progress; `TPL-12` That version is in force already.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `TPL-01` Not a template Fyniq serves; `TPL-02` The template's name is blank or too long; `TPL-03` Not this template format; `TPL-04` A paper size or margin outside the choices; `TPL-05` A font Fyniq does not ship, or an accent colour too pale to read; `TPL-06` A block the document does not take, or one repeated; `TPL-07` IRAS particulars cannot be removed or hidden; `TPL-08` A text the block does not take, or too long; `TPL-09` Not a field of the catalogue; `TPL-10` The email's subject is blank, on several lines or too long; `TPL-11` The email's body is blank or too long; `TPL-13` No such version of the template; `TPL-16` A test document lacks an IRAS particular.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again; `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `APR-06`, `STORAGE-APPROVAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `TPL-01`, `TPL-02`, `TPL-03`, `TPL-04`, `TPL-05`, `TPL-06`, `TPL-07`, `TPL-08`, `TPL-09`, `TPL-10`, `TPL-11`, `TPL-12`, `TPL-13`, `TPL-16`, `TPL-17`

### `POST /v1/document-templates/{template_id}/publish`

**Publish a template** (`publishDocumentTemplate`)

Publishes the draft as the template's next numbered version, which never changes and which documents then render from (FR-TPL-04); with version, a rollback publishes that earlier version's content again as the next version, and leaves the draft as it is. Publishing is an approval object (FR-APR-01): in Founder mode a person who holds the approve right publishes alone and the weekly review lists it (FM-3); in Team mode it is refused (TPL-14): ask with requestDocumentTemplatePublication. Refused when the content breaks a rule now (TPL-01 to TPL-11), names no version (TPL-13) or is the version in force (TPL-12). Side effects: the version, the template's next version and their audit event. Needs If-Match with the template's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and publishes nothing.

- **Permission:** Approve on Document templates: edit, publish (rev 0.7). In Founder mode an approver publishes alone, listed for the weekly review (FM-3); in Team mode another person approves a request.
- **Parameters:**
  - `template_id` (path, required): The template's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`TemplatePublication`](../../contracts/schemas/TemplatePublication.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`DocumentTemplate`](../../contracts/schemas/DocumentTemplate.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-TEMPLATE-01` The template does not change that way; `TPL-12` That version is in force already; `TPL-14` Publishing needs another person's approval.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `TPL-01` Not a template Fyniq serves; `TPL-02` The template's name is blank or too long; `TPL-03` Not this template format; `TPL-04` A paper size or margin outside the choices; `TPL-05` A font Fyniq does not ship, or an accent colour too pale to read; `TPL-06` A block the document does not take, or one repeated; `TPL-07` IRAS particulars cannot be removed or hidden; `TPL-08` A text the block does not take, or too long; `TPL-09` Not a field of the catalogue; `TPL-10` The email's subject is blank, on several lines or too long; `TPL-11` The email's body is blank or too long; `TPL-13` No such version of the template; `TPL-16` A test document lacks an IRAS particular.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again; `TPL-17` The document could not be rendered now.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-TEMPLATE-01`, `STORAGE-VERSION-01`, `TPL-01`, `TPL-02`, `TPL-03`, `TPL-04`, `TPL-05`, `TPL-06`, `TPL-07`, `TPL-08`, `TPL-09`, `TPL-10`, `TPL-11`, `TPL-12`, `TPL-13`, `TPL-14`, `TPL-16`, `TPL-17`

### `GET /v1/document-templates/{template_id}/versions`

**List a template's versions** (`listDocumentTemplateVersions`)

A template's published versions, newest first, a page at a time, without their content (FR-TPL-04). Read-only; no side effects.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `template_id` (path, required): The template's ID.
  - `page_size` (query): How many items per page, up to 500.
  - `cursor` (query): The next_cursor of the previous page; leave it out for the first page.
- **Responses:**
  - `200` OK [`TemplateVersionPage`](../../contracts/schemas/TemplateVersionPage.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/document-templates/{template_id}/versions/{number}`

**Read a template version** (`getDocumentTemplateVersion`)

One published version of a template with its content, which never changes (FR-TPL-04). Read-only; no side effects.

- **Permission:** View on Document templates: edit, publish (rev 0.7)
- **Parameters:**
  - `template_id` (path, required): The template's ID.
  - `number` (path, required): The version's number.
- **Responses:**
  - `200` OK [`TemplateVersion`](../../contracts/schemas/TemplateVersion.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-07` A second factor is required.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-07`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`
