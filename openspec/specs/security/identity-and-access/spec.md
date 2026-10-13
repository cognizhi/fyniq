# security/identity-and-access Specification

## Purpose
How people and services prove who they are and what they may reach in Fyniq: MFA and step-up, authorisation in the API and the database and on every derived output, least privilege for services, administrative cloud access, sessions, external users, OAuth clients and account recovery. Derived from PRD-001 §13.1.

## Requirements

### Requirement: SEC-01 Multi-factor sign-in and step-up
The system SHALL require every user to sign in with MFA (TOTP via Identity Platform, or a passkey), and SHALL require step-up authentication for high-risk actions and approvals (FR-APR-03/04).

Source: PRD-001 §13.1 · Priority M · Phase P1

#### Scenario: Sign-in requires a second factor
- **WHEN** a user signs in
- **THEN** MFA is required, by TOTP via Identity Platform or by a passkey

#### Scenario: SMS is not offered
- **WHEN** a user chooses or enrols a sign-in factor
- **THEN** SMS is not offered

#### Scenario: Step-up for high-risk actions and approvals
- **WHEN** a user performs a high-risk action or an approval (FR-APR-03/04)
- **THEN** step-up authentication with a passkey or TOTP is required
- **AND** the passkey is offered as the preferred, phishing-resistant method

### Requirement: SEC-02 Authorisation in the API and the database
The system SHALL authorise every request in the API layer (role and attribute checks) and in the database (row-level security keyed on organisation and, for sensitive tables, on scope).

Source: PRD-001 §13.1 · Priority M · Phase P0

#### Scenario: Role and attribute checks in the API layer
- **WHEN** a request reaches an API endpoint
- **THEN** role and attribute checks decide whether it is allowed

#### Scenario: Row-level security in the database
- **GIVEN** a request has passed the API checks
- **WHEN** it reads or writes rows
- **THEN** row-level security keyed on organisation limits it to its organisation's rows
- **AND** on sensitive tables row-level security is also keyed on scope

#### Scenario: Every role against every endpoint
- **WHEN** the authorisation tests run
- **THEN** a test matrix exercises every role against every endpoint (QA-SEC-03)

### Requirement: SEC-34 Authorisation on every derived output and channel
Every output derived from a record and every channel that carries it SHALL apply the same row scope, masking and AI exclusions as the record itself.

Source: PRD-001 §13.1 · Priority M · Phase P1

#### Scenario: Derived outputs and channels covered
- **WHEN** a derived output is produced or a channel carries record data
- **THEN** search results and snippets, counts and badges, server-sent events, document previews and thumbnails, exports, scheduled deliveries, cached report results, the reader-filtered knowledge-base index and AI traces apply the record's row scope, masking and AI exclusions

#### Scenario: No event about a record the user cannot see
- **GIVEN** a user cannot see a record
- **WHEN** an event about that record occurs
- **THEN** the event is never sent to that user

#### Scenario: Role change takes effect within 60 seconds
- **WHEN** a user's role changes
- **THEN** the change takes effect on all derived outputs and channels within 60 seconds (AI-38)

#### Scenario: G4-064 Cached report results do not cross scopes
- **GIVEN** a department approver has run a report
- **WHEN** an Accountant then runs the same report with the same parameters
- **THEN** each receives their own scope's result
- **AND** no cached result crosses scopes

### Requirement: SEC-03 Least privilege for services
The system SHALL run with least privilege for services, one service identity per service, and no human SHALL have standing production database write access.

Source: PRD-001 §13.1 · Priority M · Phase P0

#### Scenario: One identity per service
- **WHEN** the services run
- **THEN** each service runs under its own service identity

#### Scenario: AI orchestrator has no write privileges
- **WHEN** the privileges of the AI orchestrator are checked
- **THEN** it has no write privileges

#### Scenario: Proposal executor calls only domain commands
- **WHEN** the proposal executor acts
- **THEN** it can only call domain commands

#### Scenario: Migrations under a separate identity
- **WHEN** database migrations run
- **THEN** they run under a separate identity

#### Scenario: Production break-glass
- **WHEN** a human needs write access to the production database
- **THEN** it is granted only through break-glass with approval and session recording
- **AND** this production break-glass is not the founder break-glass of FM-8

### Requirement: SEC-04 Administrative access to cloud resources
Administrative access to cloud resources SHALL go through Google Cloud IAM with MFA, short-lived credentials and just-in-time elevation, and no service-account keys SHALL be used.

Source: PRD-001 §13.1 · Priority M · Phase P0

#### Scenario: Administrator access
- **WHEN** an administrator accesses cloud resources
- **THEN** access goes through Google Cloud IAM with MFA
- **AND** the credentials are short-lived
- **AND** elevated privileges are granted just in time

#### Scenario: CI/CD without service-account keys
- **WHEN** CI/CD authenticates to Google Cloud
- **THEN** it uses Workload Identity Federation
- **AND** no service-account key exists for it

### Requirement: SEC-05 Sessions
The system SHALL keep sessions in secure, HTTP-only, SameSite cookies, with an idle timeout of 30 minutes (configurable) and an absolute timeout of 12 hours.

Source: PRD-001 §13.1 · Priority M · Phase P1

#### Scenario: Session cookie attributes
- **WHEN** a session is created
- **THEN** its cookie is secure, HTTP-only and SameSite

#### Scenario: Idle timeout
- **GIVEN** the idle timeout is at its default
- **WHEN** a session is idle for 30 minutes
- **THEN** the session ends
- **AND** the idle timeout can be configured

#### Scenario: Absolute timeout
- **WHEN** a session reaches 12 hours from its start
- **THEN** the session ends, whatever its activity

#### Scenario: Re-authentication for sensitive settings
- **WHEN** a user changes a sensitive setting
- **THEN** re-authentication is required

### Requirement: SEC-06 External users expire and are reviewed
External users (auditor, accountant) SHALL have expiry dates and SHALL be reviewed quarterly.

Source: PRD-001 §13.1 · Priority M · Phase P1

#### Scenario: External user has an expiry date
- **WHEN** an auditor or accountant is given access as an external user
- **THEN** the access has an expiry date
- **AND** the access ends when the expiry date passes

#### Scenario: Quarterly review of external users
- **WHEN** the quarterly access review runs
- **THEN** every external user is reviewed

### Requirement: SEC-07 OAuth clients
Every OAuth client (ADR-T3) SHALL have a class (AI-34), an owner, an expiry and scopes.

Source: PRD-001 §13.1 · Priority M · Phase P1

#### Scenario: Client registration
- **WHEN** an OAuth client is registered
- **THEN** it has a class (AI-34), an owner, an expiry and scopes

#### Scenario: Confidential clients use private_key_jwt
- **WHEN** a confidential client authenticates
- **THEN** it uses `private_key_jwt`
- **AND** never a shared secret in code

#### Scenario: Public clients use PKCE
- **WHEN** a public client obtains a token
- **THEN** it uses PKCE

#### Scenario: Redirect URIs match exactly
- **WHEN** an authorisation request names a redirect URI
- **THEN** it is accepted only if it exactly matches a redirect URI registered for the client

#### Scenario: Client lists in the quarterly access review
- **WHEN** the quarterly access review runs
- **THEN** the OAuth client lists are part of it

### Requirement: SEC-08 No bearer tokens in the browser
The web app SHALL never store bearer tokens in browser storage and SHALL use the first-party session cookie (SEC-05).

Source: PRD-001 §13.1 · Priority M · Phase P1

#### Scenario: Web app uses the session cookie
- **WHEN** the web app calls the API
- **THEN** it authenticates with the first-party session cookie
- **AND** no bearer token is stored in browser storage

#### Scenario: Bearer tokens are for API, integration and MCP clients
- **WHEN** a bearer token is issued
- **THEN** it is issued to an API, integration or MCP client only

### Requirement: SEC-35 Account recovery
The system SHALL recover an account after a lost authenticator or a failed sign-in provider only with two independent proofs, with NIST SP 800-63B-4 (account recovery) as the design reference.

Source: PRD-001 §13.1 · Priority M · Phase P1

#### Scenario: Recovery set up at enrolment
- **WHEN** a user enrols
- **THEN** the user saves recovery codes
- **AND** is asked to register a second authenticator (two passkeys, or a passkey and TOTP)

#### Scenario: Two independent proofs
- **WHEN** a user recovers an account
- **THEN** recovery needs a saved recovery code plus a still-bound authenticator, or a recovery code plus confirmation by a named recovery contact (another Owner or Administrator, with step-up)

#### Scenario: Email or support alone never recovers
- **WHEN** recovery is attempted with email alone or through a support request
- **THEN** the account is not recovered

#### Scenario: Recovery notifies and revokes
- **WHEN** an account is recovered
- **THEN** the user is notified on every channel and every Owner is notified
- **AND** all sessions and tokens of the account are revoked (AI-38)

#### Scenario: Cooling-off period
- **WHEN** an account is recovered
- **THEN** a cooling-off period starts (default 24 hours; a setting)
- **AND** during it the account cannot approve, release payments, change payee bank details, roles or sign-in methods, or use break-glass (FM-8)

#### Scenario: Recovery is audited and reviewed
- **WHEN** an account is recovered
- **THEN** the recovery is audited
- **AND** it enters the weekly review (FM-3)

#### Scenario: G4-060 Lost passkey while Zoho sign-in is down
- **GIVEN** a founder has lost their passkey and Zoho sign-in is down
- **WHEN** the founder recovers the account
- **THEN** recovery needs two independent proofs
- **AND** every channel and every Owner is notified
- **AND** sessions and tokens are revoked
- **AND** no approval, release or bank-detail change is possible during cooling-off
