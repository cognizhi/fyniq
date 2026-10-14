# access/users-and-sign-in Specification

## Purpose
How people get into Fyniq and who administers it: inviting users with roles and scopes, mandatory MFA and passkeys, configurable sign-in providers including Zoho, sessions, approval-gated administration, the tenant usage view, first-run setup, the Super admin and the succession of Owners and Super admins. Derived from PRD-001 §8.21 and §8.24.

## Requirements

### Requirement: FR-ADM-01 Invite users with roles and scopes
The system SHALL let users be invited by email and assigned roles and scopes (entity, department), with expiry dates for external users.

Source: PRD-001 §8.21 · Priority M · Phase P1

#### Scenario: Invitation by email
- **WHEN** an administrator invites a person by email
- **THEN** the person receives an invitation to join the organisation

#### Scenario: Roles and scopes
- **WHEN** a user is given roles
- **THEN** each role can be scoped to an entity and a department

#### Scenario: External user expires
- **GIVEN** an external user with an expiry date
- **WHEN** the expiry date passes
- **THEN** the user no longer has access

### Requirement: FR-ADM-02 Authentication with mandatory MFA
The system SHALL make MFA (TOTP or passkey) mandatory for every user, use passkeys (WebAuthn) for step-up and approvals, allow Google sign-in with MFA, and offer SSO (SAML/OIDC) and SCIM in Enterprise mode.

Source: PRD-001 §8.21 · Priority M · Phase P1 (TOTP, passkey step-up) / P3 (SSO/SCIM)

#### Scenario: MFA for every user
- **WHEN** any user signs in
- **THEN** they must complete MFA with TOTP or a passkey

#### Scenario: Passkeys for step-up and approvals
- **WHEN** a user must pass step-up or approve an item
- **THEN** a passkey (WebAuthn) can be used

#### Scenario: Google sign-in with MFA
- **WHEN** a user signs in with Google
- **THEN** the sign-in is allowed
- **AND** MFA is still required

#### Scenario: SSO and SCIM in Enterprise mode
- **GIVEN** an organisation in Enterprise mode
- **WHEN** it sets up sign-in and provisioning
- **THEN** SSO through SAML or OIDC and provisioning through SCIM are available

#### Scenario: Configurable providers
- **WHEN** sign-in providers are set up
- **THEN** they are configurable, including Zoho (FR-ADM-08)

### Requirement: FR-ADM-03 Session management
The system SHALL manage sessions with an idle timeout, an absolute timeout, a device list, session revocation and sign-in alerts.

Source: PRD-001 §8.21 · Priority M · Phase P1

#### Scenario: Idle and absolute timeouts
- **WHEN** a session is idle for longer than the idle timeout, or lasts longer than the absolute timeout
- **THEN** the session ends

#### Scenario: Device list and revocation
- **WHEN** a user opens their sessions
- **THEN** they see the list of their devices
- **AND** they can revoke a session

#### Scenario: Sign-in alert
- **WHEN** a user signs in
- **THEN** a sign-in alert is sent to them

### Requirement: FR-ADM-04 Approval-gated administration
Every change to the team mode, the approval policies, the AI settings and task catalogue, the statutory parameters or the integrations SHALL be approval-gated and audited.

Source: PRD-001 §8.21 · Priority M · Phase P1

#### Scenario: Administrative change needs approval
- **WHEN** a user changes the team mode, an approval policy, an AI setting or the AI task catalogue, a statutory parameter or an integration
- **THEN** the change takes effect only after it is approved

#### Scenario: Administrative change is audited
- **WHEN** such a change is made
- **THEN** it is recorded in the audit trail

### Requirement: FR-ADM-05 Tenant usage and cost view
The system SHALL show a view of the tenant's usage and cost, covering AI tokens, storage and exports.

Source: PRD-001 §8.21 · Priority S · Phase P2

#### Scenario: Usage and cost view
- **WHEN** the usage and cost view is opened
- **THEN** it shows the tenant's AI tokens, storage and exports with their cost

### Requirement: FR-ADM-06 First-run setup and the Super admin
A new installation SHALL open a setup wizard that needs a one-time setup code created at deployment, and the first person to complete it SHALL become the Super admin and the Owner of the first organisation.

Source: PRD-001 §8.24 · Priority M · Phase P0

#### Scenario: First person to complete setup
- **GIVEN** a new installation
- **WHEN** the first person completes the setup wizard
- **THEN** that person becomes the Super admin and the Owner of the first organisation

#### Scenario: One-time setup code
- **WHEN** someone runs the setup wizard
- **THEN** they must enter the one-time setup code created at deployment
- **AND** the code is printed by `make setup` locally and held in Secret Manager in the cloud

#### Scenario: Email address fixed at deployment
- **GIVEN** an email address was fixed at deployment
- **WHEN** someone completes the setup wizard
- **THEN** only that email address can complete it, so nobody can claim a public installation first

#### Scenario: Wizard closes for good
- **GIVEN** setup has been completed
- **WHEN** anyone opens the setup wizard
- **THEN** it is closed

#### Scenario: Second Super admin and break-glass
- **WHEN** the setup wizard runs
- **THEN** it asks for a second Super admin (recommended)
- **AND** it explains break-glass (FM-8)

### Requirement: FR-ADM-07 Super admin scope
The Super admin SHALL manage installation-wide settings only (sign-in providers, email and messaging connections, announcements to everyone, organisations in P3, platform health) and SHALL NOT read an organisation's books without membership in it.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: Installation-wide settings
- **WHEN** the Super admin opens the installation settings
- **THEN** they manage sign-in providers, email and messaging connections, announcements to everyone, organisations (P3) and platform health

#### Scenario: Not an organisation role
- **GIVEN** a Super admin who is not a member of an organisation
- **WHEN** they try to read that organisation's books
- **THEN** access is refused

#### Scenario: Last Super admin cannot be removed
- **GIVEN** only one Super admin exists
- **WHEN** someone tries to remove them
- **THEN** the removal is refused
- **AND** they can only be replaced, by adding a new Super admin first (succession and recovery in FR-ADM-09)

#### Scenario: Super admin changes
- **WHEN** a Super admin change is made
- **THEN** it needs step-up authentication
- **AND** it is audited
- **AND** it is dual-controlled once a second Super admin exists

### Requirement: FR-ADM-08 Configurable sign-in providers
The system SHALL offer Google and Zoho sign-in out of the box and any OpenID Connect provider by configuration, optional email and password, and SAML in Enterprise mode; each sign-in method SHALL be enabled or disabled on its own, and the platform's own MFA SHALL stay mandatory whatever the provider.

Source: PRD-001 §8.24 · Priority M · Phase P0 (Zoho issuer check, §17.3) / P1

#### Scenario: OpenID Connect provider by configuration
- **WHEN** an OpenID Connect provider is added
- **THEN** it is configured by its issuer or discovery URL, client ID, a secret held in Secret Manager, scopes and claim mapping

#### Scenario: Zoho sign-in flow
- **WHEN** a user signs in with Zoho
- **THEN** the authorisation-code flow with a client secret is used, because Zoho's discovery document offers no `id_token` response type for the implicit flow
- **AND** the scopes are `openid email profile`

#### Scenario: One provider per Zoho data centre
- **WHEN** Zoho sign-in is set up
- **THEN** there is one provider per Zoho data centre, because each has its own issuer (`https://accounts.zoho.com`, `.eu`, `.in`, `.com.au`, `.jp`, `.sg` and others)

#### Scenario: Founders' Zoho data centre
- **WHEN** the founders' installation is configured
- **THEN** one Zoho provider is configured, for the US data centre, with issuer `https://accounts.zoho.com`

#### Scenario: Live Zoho sign-in is a P0 exit test
- **WHEN** P0 exits
- **THEN** a live sign-in through each configured Zoho data centre has passed (§17.3 item 7), since only the discovery issuers (`https://accounts.zoho.<tld>` per data centre) are confirmed, not the `iss` in a real token

#### Scenario: Identities keyed by issuer and subject
- **WHEN** a user signs in through any provider
- **THEN** the user is identified by the (issuer, `sub`) pair, never by email (OpenID Connect Core §5.7)
- **AND** the sign-in records the provider used

#### Scenario: Enabling and disabling a sign-in method
- **WHEN** the Super admin disables a sign-in method (Google, Zoho, an OIDC or SAML provider, or email and password)
- **THEN** new sign-ins through it are blocked
- **AND** its sessions end if the Super admin chose that
- **AND** the change is audited and dual-controlled

#### Scenario: Last Super admin method cannot be disabled
- **WHEN** the Super admin tries to disable the last sign-in method a Super admin can sign in with
- **THEN** the platform refuses

#### Scenario: Allowed domains and joining by domain
- **WHEN** a provider is configured
- **THEN** its allowed email domains can be set
- **AND** joining by domain is off by default, so an invitation is required unless it is turned on

#### Scenario: Linking a new provider to an account
- **WHEN** a user links a new provider to their account
- **THEN** the link is made only on a verified email, in a session that has just passed step-up with the platform's MFA, after a successful sign-in with the new provider
- **AND** a matching verified email alone never links or merges accounts

#### Scenario: Linking and unlinking are notified and audited
- **WHEN** a provider is linked to or unlinked from an account
- **THEN** the user is notified on every channel
- **AND** the change is audited

#### Scenario: Platform MFA stays mandatory
- **WHEN** a user signs in through any provider
- **THEN** the platform's own MFA (TOTP or passkey) is still required, as "trust the provider's MFA" is off by default
- **AND** passkeys remain the step-up for approvals (FR-ADM-02)

#### Scenario: G4-061 New provider with a matching verified email
- **WHEN** someone signs in with a new provider whose verified email matches an existing finance user
- **THEN** there is no link and no access to the existing account without step-up with that account's MFA
- **AND** the attempt is audited

### Requirement: FR-ADM-09 Owner and Super admin succession
An organisation SHALL always have at least one Owner, Owner changes SHALL be dual-controlled (FM-1) and notify every Owner, and succession and Super admin recovery SHALL follow evidenced, audited procedures.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: At least one Owner
- **WHEN** a change would leave an organisation without an Owner
- **THEN** the change is refused

#### Scenario: Adding, removing or transferring an Owner
- **WHEN** an Owner is added, removed or transferred
- **THEN** the change is dual-controlled (FM-1)
- **AND** every Owner is notified

#### Scenario: Succession when the other Owner is permanently unavailable
- **GIVEN** the other Owner is permanently unavailable (departure, incapacity, death)
- **WHEN** succession is requested
- **THEN** it needs documentary evidence (a board resolution, or the external accountant's recorded confirmation; FR-DOC-05), a waiting period with notice to the outgoing Owner's channels, and an audited Super admin action

#### Scenario: Last Super admin cannot sign in
- **WHEN** the last Super admin cannot sign in
- **THEN** a deployment-held recovery procedure in the runbook restores access (open question 36)
- **AND** every use is audited and notifies every Super admin and Owner
