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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-UNAVAILABLE-01`

### `POST /v1/setup`

**Complete first-run setup** (`completeSetup`)

Claims a new installation with the one-time setup code created at deployment (FR-ADM-06). You become the Super admin and the Owner of the first organisation, and receive your ten recovery codes, shown once (open question 36); the installation's own audit trail is created with it. Side effects: your user and sign-in identity, the Super admin and their recovery codes, two organisations, your membership and role, audit events. Works once: a wrong or used code changes nothing. Needs a person signed in to Fyniq; an OAuth client or the AI is refused. It takes no Idempotency-Key: the code itself makes it one-time.

- **Permission:** Create on (installation setup). Needs the one-time setup code and a first-party sign-in; an OAuth client or the AI is refused.
- **Request body:** [`SetupRequest`](../../contracts/schemas/SetupRequest.schema.json)
- **Responses:**
  - `201` Created [`SetupResult`](../../contracts/schemas/SetupResult.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in.
  - `409` Conflict: `API-SETUP-01` Setup refused.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `STORAGE-INV-05` An amount or rate is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-SETUP-01`, `API-UNAVAILABLE-01`, `STORAGE-INV-05`

### `POST /v1/super-admins`

**Become a Super admin with an invitation** (`acceptSuperAdminInvitation`)

Redeems a Super admin's invitation: you (a new user, or the one your sign-in belongs to) become a Super admin and receive your ten recovery codes, shown once (FR-ADM-06/09). Side effects: your user and sign-in identity when new, the Super admin, the recovery codes, events in the installation's trail. A wrong, used or expired invitation changes nothing. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Create on (Super admins). Needs a valid invitation and a person signed in to Fyniq; an OAuth client or the AI is refused.
- **Request body:** [`SuperAdminInvitation`](../../contracts/schemas/SuperAdminInvitation.schema.json)
- **Responses:**
  - `201` Created [`NewSuperAdmin`](../../contracts/schemas/NewSuperAdmin.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in.
  - `409` Conflict: `API-SUPERADMIN-01` Code refused; `API-SUPERADMIN-02` Super admin change refused.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-SUPERADMIN-01`, `API-SUPERADMIN-02`, `API-UNAVAILABLE-01`

### `POST /v1/super-admins/invitations`

**Invite the second Super admin** (`createSuperAdminInvitation`)

Creates the one-time invitation that makes its redeemer the installation's second Super admin (FR-ADM-06/07; one per founder, open question 36). Side effects: the invitation (it lasts 7 days and replaces an unused one) and its event in the installation's trail. Refused when the installation has two Super admins already. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Responses:**
  - `201` Created [`OneTimeCode`](../../contracts/schemas/OneTimeCode.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin.
  - `409` Conflict: `API-SUPERADMIN-02` Super admin change refused.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-SUPERADMIN-02`, `API-UNAVAILABLE-01`

### `POST /v1/super-admins/recovery`

**Recover your Super admin access** (`recoverSuperAdmin`)

Gives a Super admin who lost their sign-in their access back, with two proofs (SEC-35, FR-ADM-09): one of their printed recovery codes with the other Super admin's confirmation, or the deployment's two-person recovery (docs/runbooks/super-admin-recovery.md). The sign-in you use now replaces your others. Side effects: your sign-in identities, ten new recovery codes (shown once; the old ones stop working), the revocation of the tokens of the replaced sign-ins (AI-38) and events in the installation's trail. Wrong, used or expired codes change nothing. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Create on (Super admins). Needs two proofs (SEC-35): a recovery code with the other Super admin's confirmation, or the deployment's two-person recovery; and a person signed in to Fyniq. An OAuth client or the AI is refused.
- **Request body:** [`SuperAdminRecovery`](../../contracts/schemas/SuperAdminRecovery.schema.json)
- **Responses:**
  - `200` OK [`RecoveredSuperAdmin`](../../contracts/schemas/RecoveredSuperAdmin.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in.
  - `409` Conflict: `API-SUPERADMIN-01` Code refused.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-SUPERADMIN-01`, `API-UNAVAILABLE-01`

### `POST /v1/super-admins/recovery-codes`

**Replace your recovery codes** (`replaceSuperAdminRecoveryCodes`)

Gives you ten new recovery codes, shown once; your unused codes stop working (FR-ADM-09). Side effects: the codes and their event in the installation's trail. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Create on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1).
- **Responses:**
  - `200` OK [`RecoveryCodes`](../../contracts/schemas/RecoveryCodes.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`

### `POST /v1/super-admins/{user_id}/recovery-confirmations`

**Confirm the other Super admin's recovery** (`confirmSuperAdminRecovery`)

Creates the one-time confirmation that, with one of their printed recovery codes, gives the other Super admin their access back: the second proof a recovery needs (SEC-35, FR-ADM-09). Confirm only after you have spoken to them. Side effects: the confirmation (it lasts 24 hours and replaces an unused one) and its event in the installation's trail. Nobody confirms their own recovery. It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first.

- **Permission:** Approve on (Super admins). Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1). The other Super admin confirms; nobody confirms their own recovery.
- **Parameters:**
  - `user_id` (path, required): The Super admin whose recovery you confirm: not you.
- **Responses:**
  - `201` Created [`OneTimeCode`](../../contracts/schemas/OneTimeCode.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in; `API-AUTHZ-05` Step-up required; `API-AUTHZ-06` Not a Super admin.
  - `409` Conflict: `API-SUPERADMIN-02` Super admin change refused.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-AUTHZ-05`, `API-AUTHZ-06`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-SUPERADMIN-02`, `API-UNAVAILABLE-01`

## Session

Signing in to the web app: the providers, and your own session (SEC-05, FR-ADM-08).

### `GET /v1/session`

**Get your session** (`getSession`)

Who you are signed in as: the provider, your user, the organisation your session is in, your roles there, whether you are a Super admin, until when your step-up counts (SEC-05, FR-ADM-07), and the organisation's feature switches (FR-ORG-06). The web app reads it after signing in; a 401 means it should show the sign-in page. Needs a person signed in; an OAuth client or the AI is refused. Read-only; no side effects.

- **Permission:** View on (your session). Needs a person signed in to Fyniq; an OAuth client or the AI is refused.
- **Responses:**
  - `200` OK [`SessionInfo`](../../contracts/schemas/SessionInfo.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`, `API-UNAVAILABLE-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`

## Organisation

The organisation your session is in.

### `GET /v1/organisation`

**Get your organisation** (`getOrganisation`)

Returns the organisation your session is in: its name and base currency, which every screen shows. Read-only; no side effects. Every member of the organisation may call it.

- **Permission:** View on (membership)
- **Responses:**
  - `200` OK [`Organisation`](../../contracts/schemas/Organisation.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DATES-01` The dates overlap another version; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `GST-01` The registration's dates do not fit; `GST-02` The GST registration number is not valid; `GST-03` Not a return frequency; `GST-04` Not a quarters' start month; `GST-05` An accounting basis Fyniq does not offer; `STORAGE-ORG-01` The organisation's details are not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `GST-01`, `GST-02`, `GST-03`, `GST-04`, `GST-05`, `STORAGE-DATES-01`, `STORAGE-LOCK-01`, `STORAGE-ORG-01`, `STORAGE-RETRY-01`

### `GET /v1/organisation/gst-registrations/{registration_id}`

**Get a GST registration** (`getGSTRegistration`)

Returns one GST registration with its ETag (FR-ORG-03). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `registration_id` (path, required): The registration's ID.
- **Responses:**
  - `200` OK [`GSTRegistration`](../../contracts/schemas/GSTRegistration.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
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
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `GST-01`, `GST-02`, `GST-03`, `GST-04`, `GST-05`, `STORAGE-DATES-01`, `STORAGE-LOCK-01`, `STORAGE-ORG-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

### `GET /v1/organisation/gst-status`

**Get the GST status on a date** (`getGSTStatus`)

Whether the organisation is GST-registered on a date, under which registration, and the return period that contains the date, from the registrations' effective dates, return frequency and quarters (FR-ORG-03). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `date` (query, required): The date to ask about, YYYY-MM-DD.
- **Responses:**
  - `200` OK [`GSTStatus`](../../contracts/schemas/GSTStatus.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/organisation/profile`

**Get the organisation's profile** (`getOrganisationProfile`)

What the organisation's documents print about it (legal name, UEN, addresses, contact details, the bank and PayNow details customers pay to, the footer) and how its financial years run (the start month, the books' start, the first year's end) and its time zone (FR-ORG-01, FR-ORG-02), with the organisation's ETag. Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Responses:**
  - `200` OK [`OrganisationProfile`](../../contracts/schemas/OrganisationProfile.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `PATCH /v1/organisation/profile`

**Change the organisation's profile** (`updateOrganisationProfile`)

Changes the fields sent and keeps the rest: a field left out keeps its value, and "" clears it. Every rule that fails is reported at once (UEN, email, time zone, a first financial year that ends after the books start, on the day before a year starts). Side effects: the organisation's next version and an audit event with the changes, bank and PayNow numbers masked. Needs If-Match with the organisation's ETag. Approval policy: the Owner approves profile changes once the approval engine arrives (P1). Prefer: return=preview checks the change and saves nothing.

- **Permission:** Create on Organisation profile, financial year, defaults (rev 0.29). The Owner approves changes to the profile; the approval itself arrives with the approval engine (P1).
- **Parameters:**
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`ProfileChanges`](../../contracts/schemas/ProfileChanges.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`OrganisationProfile`](../../contracts/schemas/OrganisationProfile.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `ORG-01` A text is blank or too long; `ORG-02` Not a UEN; `ORG-03` Not an email address; `ORG-04` Not a time zone; `ORG-05` Not a month; `ORG-06` The first financial year does not fit; `ORG-07` Not a bank account number; `STORAGE-ORG-01` The organisation's details are not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `ORG-01`, `ORG-02`, `ORG-03`, `ORG-04`, `ORG-05`, `ORG-06`, `ORG-07`, `STORAGE-LOCK-01`, `STORAGE-ORG-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `GET /v1/organisation/sequences/{document_type}`

**Get a document number sequence** (`getSequence`)

A document type's number sequence with its ETag; "0" while it still has its defaults (FR-ORG-04). Read-only; no side effects.

- **Permission:** View on Organisation profile, financial year, defaults (rev 0.29)
- **Parameters:**
  - `document_type` (path, required): The document type.
- **Responses:**
  - `200` OK [`Sequence`](../../contracts/schemas/Sequence.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress; `STORAGE-SEQUENCE-01` The number sequence cannot be set that way.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `ORG-08` Not a prefix; `ORG-09` The next number does not fit; `ORG-10` Not a number of digits.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `ORG-08`, `ORG-09`, `ORG-10`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-SEQUENCE-01`, `STORAGE-VERSION-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

### `PATCH /v1/organisation/settings/{key}`

**Change an organisation setting** (`updateSetting`)

Records a new value for a setting, or with reset its recommended value, from a day: today in the organisation's time zone or later, never earlier (FR-ORG-08). Earlier values are kept with their dates. A setting set by the platform never changes, and one under dual control (FM-1: thresholds, retention, AI) is refused until the approval engine arrives: nothing is applied. Side effects: the setting's next version and its audit event, with the value before and after. Needs If-Match with its ETag ("0" before any change). Approval policy: the Owner approves once the approval engine arrives (P1). Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Organisation profile, financial year, defaults (rev 0.29). The Owner approves once the approval engine arrives (P1); a setting under dual control (FM-1) is refused until then.
- **Parameters:**
  - `key` (path, required): The setting's key.
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`SettingUpdate`](../../contracts/schemas/SettingUpdate.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Setting`](../../contracts/schemas/Setting.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `SET-04` Set by the platform; `SET-05` Needs a second approver; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `SET-01` Not a value for this setting; `SET-02` A setting never changes in the past; `SET-03` The setting can only lengthen; `SET-06` Send a value or a reset.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `SET-01`, `SET-02`, `SET-03`, `SET-04`, `SET-05`, `SET-06`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-ACCOUNT-01` The account cannot change that way; `STORAGE-ACCOUNT-02` The account cannot sit there; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `COA-01` Not an account code; `COA-02` A text is blank or too long; `COA-03` Not an account type; `COA-04` Not a function; `COA-05` Not a GST code; `COA-06` Not a currency; `COA-07` Not a system role; `COA-08` Changes only with an approval; `COA-09` A system account stays active.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `COA-01`, `COA-02`, `COA-03`, `COA-04`, `COA-05`, `COA-06`, `COA-07`, `COA-08`, `COA-09`, `STORAGE-ACCOUNT-01`, `STORAGE-ACCOUNT-02`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/accounts/{account_id}`

**Get an account** (`getAccount`)

Returns one account with its ETag. Read-only; no side effects.

- **Permission:** View on Chart of accounts, dimensions
- **Parameters:**
  - `account_id` (path, required): The account's ID.
- **Responses:**
  - `200` OK [`Account`](../../contracts/schemas/Account.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-ACCOUNT-01` The account cannot change that way; `STORAGE-ACCOUNT-02` The account cannot sit there; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `COA-01` Not an account code; `COA-02` A text is blank or too long; `COA-03` Not an account type; `COA-04` Not a function; `COA-05` Not a GST code; `COA-06` Not a currency; `COA-07` Not a system role; `COA-08` Changes only with an approval; `COA-09` A system account stays active.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `COA-01`, `COA-02`, `COA-03`, `COA-04`, `COA-05`, `COA-06`, `COA-07`, `COA-08`, `COA-09`, `STORAGE-ACCOUNT-01`, `STORAGE-ACCOUNT-02`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `API-CHART-01` The organisation has accounts already; `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CHART-01`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-02`, `API-RATE-01`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

## Journals

Manual journals: drafted, posted and reversed; posted history never changes (FR-GL-12).

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/journals/{journal_id}`

**Get a journal** (`getJournal`)

Returns a manual journal's current version and status, with its ETag. Read-only; no side effects.

- **Permission:** View on Journals
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
- **Responses:**
  - `200` OK [`Journal`](../../contracts/schemas/Journal.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `JOURNAL-04` The saved journal cannot be read.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`, `JOURNAL-04`

### `POST /v1/journals/{journal_id}/post`

**Post a draft journal** (`postJournal`)

Posts a draft: the posting engine turns its current version into a balanced entry, written with the journal's change to posted in one transaction (ENG-02). Side effects: a journal entry, a new ledger revision, balances, audit events. Needs If-Match with the draft's ETag. Approval policy: the Accountant's amount limit and SOD-3 apply from P1. Idempotent with Idempotency-Key; the same version never posts twice.

- **Permission:** Post on Journals. The Accountant's amount limit and SOD-3 arrive with the approval engine (P1).
- **Parameters:**
  - `journal_id` (path, required): The journal's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `If-Match` (header): The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`JournalPosting`](../../contracts/schemas/JournalPosting.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-02` The journal is not a draft; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `JOURNAL-04` The saved journal cannot be read; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`, `JOURNAL-02`, `JOURNAL-04`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/journals/{journal_id}/reverse`

**Reverse a posted journal** (`reverseJournal`)

Voids a posted journal with a reversing entry on the date given (FR-GL-12); posted history never changes. Side effects: the reversing entry, a new ledger revision, balances, audit events with the reason. Needs If-Match with the journal's ETag. Approval policy: in Founder mode voiding needs a second person (FM-1) from P1. Idempotent with Idempotency-Key.

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `JOURNAL-03` The journal is not posted; `STORAGE-DOC-02` The document cannot move to that status; `STORAGE-DUPLICATE-01` Already done; `STORAGE-INV-02` Posted history cannot change; `STORAGE-INV-03` The period is closed; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `JOURNAL-04` The saved journal cannot be read; `LEDGER-01` The journal has no date; `LEDGER-02` The journal has too few lines; `LEDGER-03` A line is zero; `LEDGER-04` An account does not exist; `LEDGER-05` An account is a group heading; `LEDGER-06` An account is inactive; `LEDGER-07` A line needs an exchange rate; `LEDGER-08` An exchange rate does not fit; `LEDGER-09` The journal does not balance; `LEDGER-10` A currency does not balance; `LEDGER-11` A text is too long; `LEDGER-12` The entry is not posted; `LEDGER-13` A base-currency line has a rate; `LEDGER-14` A base amount is too large; `LEDGER-15` A line posts to share capital; `LEDGER-16` The reversal is dated before its entry; `STORAGE-INV-01` The entry does not balance; `STORAGE-INV-04` The account cannot take postings; `STORAGE-INV-05` An amount or rate is not valid; `STORAGE-INV-13` A manual journal cannot post to share capital.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`, `JOURNAL-03`, `JOURNAL-04`, `LEDGER-01`, `LEDGER-02`, `LEDGER-03`, `LEDGER-04`, `LEDGER-05`, `LEDGER-06`, `LEDGER-07`, `LEDGER-08`, `LEDGER-09`, `LEDGER-10`, `LEDGER-11`, `LEDGER-12`, `LEDGER-13`, `LEDGER-14`, `LEDGER-15`, `LEDGER-16`, `STORAGE-DOC-02`, `STORAGE-DUPLICATE-01`, `STORAGE-INV-01`, `STORAGE-INV-02`, `STORAGE-INV-03`, `STORAGE-INV-04`, `STORAGE-INV-05`, `STORAGE-INV-13`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MONEY-01` An amount is not a plain decimal; `MONEY-02` An amount has too many decimals; `MONEY-03` An amount is too large; `MONEY-04` A currency is not valid; `MONEY-05` An exchange rate is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `MONEY-01`, `MONEY-02`, `MONEY-03`, `MONEY-04`, `MONEY-05`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `CT-01` The legal name is not valid; `CT-02` Neither a customer nor a vendor; `CT-03` Not an email address; `CT-04` Not a country code; `CT-05` Not a currency; `CT-06` Payment terms out of range; `CT-07` Not a GST code; `CT-08` Not a Peppol participant ID; `CT-09` A text is too long; `CT-10` Not an account that takes postings.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `CT-01`, `CT-02`, `CT-03`, `CT-04`, `CT-05`, `CT-06`, `CT-07`, `CT-08`, `CT-09`, `CT-10`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/contacts/{contact_id}`

**Get a contact** (`getContact`)

Returns a contact with its ETag and the contacts it may duplicate (FR-CT-01, FR-CT-04). Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `contact_id` (path, required): The contact's ID.
- **Responses:**
  - `200` OK [`Contact`](../../contracts/schemas/Contact.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
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
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `CT-01`, `CT-02`, `CT-03`, `CT-04`, `CT-05`, `CT-06`, `CT-07`, `CT-08`, `CT-09`, `CT-10`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/members`

**Join an organisation with an invitation** (`acceptMemberInvitation`)

Redeems an invitation: you (a new user, or the one your sign-in belongs to) become a member of the organisation that made it, with its roles; your session is in that organisation from your next request (FR-ADM-01). Needs a person signed in. Side effects: your user and sign-in identity when new, the membership, the roles and their audit events. A wrong, used, withdrawn or expired code, or an organisation you belong to already, changes nothing. It takes no Idempotency-Key and no preview: its code makes it one-time (FR-INT-08, rev 0.35).

- **Permission:** Create on (joining by invitation). The invitation's code decides; a person signed in, never the AI or an OAuth client.
- **Request body:** [`JoinRequest`](../../contracts/schemas/JoinRequest.schema.json)
- **Responses:**
  - `201` Created [`Joined`](../../contracts/schemas/Joined.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised; `API-AUTHZ-04` A person must be signed in.
  - `409` Conflict: `API-SUPERADMIN-01` Code refused.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `API-UNAVAILABLE-01` Not available here.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-AUTHZ-04`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `API-SUPERADMIN-01`, `API-UNAVAILABLE-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

### `POST /v1/members/invitations`

**Invite someone to the organisation** (`createMemberInvitation`)

Makes a one-time invitation that gives its redeemer the roles named, valid 7 days (FR-ADM-01). Give the code in person or on a call; invitation emails arrive later. A role change falls under FM-1: until the organisation has a second member its Owner acts alone (the founder bootstrap, rev 0.35), and after that it is refused until the approval engine; an invitation made before keeps its roles. Side effects: the invitation and its audit event. It takes no Idempotency-Key and no preview: its code makes it one-time (FR-INT-08, rev 0.35).

- **Permission:** Create on Users, roles, team mode. Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they are refused until the approval engine.
- **Request body:** [`NewMemberInvitation`](../../contracts/schemas/NewMemberInvitation.schema.json)
- **Responses:**
  - `201` Created [`MemberInvitationCode`](../../contracts/schemas/MemberInvitationCode.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `MEM-05` Needs a second approver; `MEM-06` The last Owner keeps the role; `MEM-07` The member holds that role already, or does not hold it; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-MEMBER-01` The membership or role cannot change that way.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `MEM-01` Not a role a member can hold; `MEM-02` The name is not valid; `MEM-03` No role, or a role named twice; `MEM-04` Not an expiry for these roles.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `MEM-01`, `MEM-02`, `MEM-03`, `MEM-04`, `MEM-05`, `MEM-06`, `MEM-07`, `STORAGE-LOCK-01`, `STORAGE-MEMBER-01`, `STORAGE-RETRY-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `STORAGE-LOCK-01` Another change is in progress.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `POST /v1/members/{member_id}/roles`

**Give a member a role** (`assignRole`)

Gives a member a role, theirs until it is revoked or, for an external user, until it expires (FR-ADM-01). A role change falls under FM-1: until the organisation has a second member its Owner acts alone, their own roles included (the founder bootstrap, rev 0.35), and after that it is refused until the approval engine. Side effects: the role and its audit event; the member's next request holds it, and their OAuth tokens are revoked (AI-38). Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Users, roles, team mode. Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they are refused until the approval engine.
- **Parameters:**
  - `member_id` (path, required): The membership's ID.
  - `Idempotency-Key` (header): A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08).
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Request body:** [`RoleGrant`](../../contracts/schemas/RoleGrant.schema.json)
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Member`](../../contracts/schemas/Member.schema.json)
  - `201` Created [`Member`](../../contracts/schemas/Member.schema.json)
  - `400` Bad Request: `API-INPUT-01` The request cannot be read.
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `MEM-05` Needs a second approver; `MEM-06` The last Owner keeps the role; `MEM-07` The member holds that role already, or does not hold it; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-MEMBER-01` The membership or role cannot change that way.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `MEM-01` Not a role a member can hold; `MEM-02` The name is not valid; `MEM-03` No role, or a role named twice; `MEM-04` Not an expiry for these roles.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `MEM-01`, `MEM-02`, `MEM-03`, `MEM-04`, `MEM-05`, `MEM-06`, `MEM-07`, `STORAGE-LOCK-01`, `STORAGE-MEMBER-01`, `STORAGE-RETRY-01`

### `DELETE /v1/members/{member_id}/roles/{role}`

**End a member's role** (`revokeRole`)

Ends a member's role; their next request no longer holds it (FR-ADM-01). The organisation always keeps an Owner. A role change falls under FM-1: until the organisation has a second member its Owner acts alone (the founder bootstrap, rev 0.35), and after that it is refused until the approval engine. Side effects: the revocation and its audit event, and the member's OAuth tokens are revoked (AI-38). Prefer: return=preview checks it and saves nothing.

- **Permission:** Create on Users, roles, team mode. Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they are refused until the approval engine.
- **Parameters:**
  - `member_id` (path, required): The membership's ID.
  - `role` (path, required): The role to end.
  - `Prefer` (header): return=preview runs every check of the command and answers with what it would return, saving nothing (FR-INT-14): no Idempotency-Key is claimed, and IDs in the answer are not kept.
- **Responses:**
  - `200` OK; or, with Prefer: return=preview, what the command would return, with nothing saved (FR-INT-14). [`Member`](../../contracts/schemas/Member.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `409` Conflict: `MEM-05` Needs a second approver; `MEM-06` The last Owner keeps the role; `MEM-07` The member holds that role already, or does not hold it; `STORAGE-LOCK-01` Another change is in progress; `STORAGE-MEMBER-01` The membership or role cannot change that way.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid; `MEM-01` Not a role a member can hold; `MEM-02` The name is not valid; `MEM-03` No role, or a role named twice; `MEM-04` Not an expiry for these roles.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `MEM-01`, `MEM-02`, `MEM-03`, `MEM-04`, `MEM-05`, `MEM-06`, `MEM-07`, `STORAGE-LOCK-01`, `STORAGE-MEMBER-01`, `STORAGE-RETRY-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `409` Conflict: `API-IDEMPOTENCY-02` The first request with this Idempotency-Key is still running; `STORAGE-DUPLICATE-01` Already done; `STORAGE-LOCK-01` Another change is in progress.
  - `413` Request Entity Too Large: `API-INPUT-03` The request is too large.
  - `415` Unsupported Media Type: `API-INPUT-04` The request is not JSON.
  - `422` Unprocessable Entity: `API-IDEMPOTENCY-01` The Idempotency-Key was used for another request; `API-IDEMPOTENCY-03` The Idempotency-Key expired; `API-INPUT-02` A field is not valid; `ITM-01` Not an item code; `ITM-02` The item's name is not valid; `ITM-03` Neither a product nor a service; `ITM-04` A text is too long; `ITM-05` Not a currency, or priced twice; `ITM-06` Not a unit price; `ITM-07` Not a GST code; `ITM-08` Not a revenue account that takes postings.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-RATE-01`, `ITM-01`, `ITM-02`, `ITM-03`, `ITM-04`, `ITM-05`, `ITM-06`, `ITM-07`, `ITM-08`, `STORAGE-DUPLICATE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

### `GET /v1/items/{item_id}`

**Get an item** (`getItem`)

Returns an item with its prices and its ETag (FR-ITM-01). Read-only; no side effects.

- **Permission:** View on Contacts and items (rev 0.33)
- **Parameters:**
  - `item_id` (path, required): The item's ID.
- **Responses:**
  - `200` OK [`Item`](../../contracts/schemas/Item.schema.json)
  - `401` Unauthorized: `API-AUTH-01` Sign-in required.
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
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
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `ITM-01`, `ITM-02`, `ITM-03`, `ITM-04`, `ITM-05`, `ITM-06`, `ITM-07`, `ITM-08`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `422` Unprocessable Entity: `API-CURSOR-01` The cursor is not valid; `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-CURSOR-01`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
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
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IDEMPOTENCY-01`, `API-IDEMPOTENCY-02`, `API-IDEMPOTENCY-03`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-01`, `API-INPUT-02`, `API-INPUT-03`, `API-INPUT-04`, `API-NOT-FOUND-01`, `API-RATE-01`, `API-UNAVAILABLE-01`, `DOC-01`, `DOC-02`, `DOC-03`, `DOC-04`, `DOC-05`, `DOC-06`, `DOC-07`, `DOC-08`, `DOC-09`, `JOURNAL-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`

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
  - `403` Forbidden: `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
- **Error codes:** `API-AUTH-01`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`

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
  - `403` Forbidden: `API-AUTH-02` Sent from another site; `API-AUTHZ-01` Not allowed for your roles; `API-AUTHZ-02` No organisation; `API-AUTHZ-03` Operation not authorised.
  - `404` Not Found: `API-NOT-FOUND-01` Not found; `JOURNAL-01` Not a manual journal.
  - `409` Conflict: `STORAGE-EVIDENCE-01` Evidence is append-only once its record is final; `STORAGE-LOCK-01` Another change is in progress.
  - `412` Precondition Failed: `API-IF-MATCH-01` The record changed; `STORAGE-VERSION-01` The record changed since you read it.
  - `422` Unprocessable Entity: `API-INPUT-02` A field is not valid.
  - `428` Precondition Required: `API-IF-MATCH-02` If-Match required.
  - `429` Too Many Requests: `API-RATE-01` Too many requests.
  - `500` Internal Server Error: `API-FAILURE-01` Something went wrong.
  - `503` Service Unavailable: `STORAGE-RETRY-01` Busy: try again.
- **Error codes:** `API-AUTH-01`, `API-AUTH-02`, `API-AUTHZ-01`, `API-AUTHZ-02`, `API-AUTHZ-03`, `API-FAILURE-01`, `API-IF-MATCH-01`, `API-IF-MATCH-02`, `API-INPUT-02`, `API-NOT-FOUND-01`, `API-RATE-01`, `JOURNAL-01`, `STORAGE-EVIDENCE-01`, `STORAGE-LOCK-01`, `STORAGE-RETRY-01`, `STORAGE-VERSION-01`
