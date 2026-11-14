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

#### Scenario: A role narrowed to departments (rev 0.253)
- **GIVEN** a member holds a role narrowed to some values of the department dimension
- **WHEN** they act under a cell qualified by department (Appendix D's "dept", the Approver's)
- **THEN** it reaches only the records, lines and budgets of those values
- **AND** a role with no department scope reaches nothing under a dept-qualified cell; entity scopes come with multi-entity (P3)

#### Scenario: External user expires
- **GIVEN** an external user with an expiry date
- **WHEN** the expiry date passes
- **THEN** the user no longer has access

#### Scenario: The Owner administers alone until a second member joins
- **GIVEN** the organisation's only active member is its Owner
- **WHEN** the Owner invites someone with roles, or assigns or revokes a role, their own included
- **THEN** the change is applied and audited, and listed for the weekly review once another founder joins (rev 0.35)

#### Scenario: Role changes then wait for another Owner's approval
- **GIVEN** the organisation has two or more active members
- **WHEN** anyone invites someone with roles, or assigns or revokes a role
- **THEN** the change falls under FM-1 and waits as an approval request that another Owner approves; it is made only then (rev 0.36, superseding "refused until the approval engine")
- **AND** an invitation asked for then shows its code once to the requester, and the code works once the invitation is approved
- **AND** an invitation made during the bootstrap keeps its roles when it is accepted

#### Scenario: An invitation is a one-time code
- **WHEN** someone is invited
- **THEN** the invitation is a code shown once, valid 7 days, that works once and can be withdrawn while unused
- **AND** withdrawing grants nothing, so it stays open to the Owner and the Administrator after the bootstrap
- **AND** the organisation always keeps an Owner

#### Scenario: A member is removed from the organisation
- **WHEN** a member is removed, by the Owner alone during the bootstrap or on another Owner's approval after it (rev 0.165)
- **THEN** each of their roles ends, expired ones included, then the membership, audited, and their OAuth tokens are revoked
- **AND** their next request no longer reaches the organisation
- **AND** the last Owner is never removed, and the database refuses a role for a removed member (FY248)

#### Scenario: A removed person joins again
- **GIVEN** a person was removed from the organisation
- **WHEN** they accept a new invitation (rev 0.165)
- **THEN** they are a member again on the same membership, with the new invitation's roles

#### Scenario: An external user is given a new end date
- **GIVEN** an external user's role ended at its end date and was never ended by hand
- **WHEN** the role is given again with a new end date (rev 0.165)
- **THEN** the expired assignment ends first, and the role is given again

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

#### Scenario: TOTP with the provider, passkeys with Fyniq (rev 0.43)
- **WHEN** a person uses an authenticator app
- **THEN** the sign-in provider checks it at sign-in
- **AND** a person whose session passed a second factor in the last 5 minutes can register a passkey for the web app, with user verification required and no attestation asked for

#### Scenario: A passkey steps a session up (rev 0.43)
- **WHEN** a person answers a step-up challenge with one of their live passkeys
- **THEN** the session is stepped up for 5 minutes and records that a passkey did it
- **AND** the challenge is 32 random bytes bound to that session and its purpose, used once within 5 minutes
- **AND** an answer from another origin or site, without user verification, from another person's passkey, or with a signature counter that repeats or goes back is refused

#### Scenario: Passkeys are kept and revoked, never deleted (rev 0.43)
- **WHEN** a person registers or revokes a passkey
- **THEN** it is audited in the installation's trail
- **AND** a revoked passkey steps nobody up

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

#### Scenario: Your devices (rev 0.45)
- **WHEN** a person opens their devices
- **THEN** they see each browser they are signed in on, with its provider, browser, sign-in time and last use, the current one marked
- **AND** a device's ID never shows the session behind it

#### Scenario: Signing devices out (rev 0.45)
- **WHEN** a person signs a device out, or all but the current one
- **THEN** those sessions end at their next request, and the security log records each
- **AND** nobody signs out another person's sessions

#### Scenario: The sign-in alert by email (rev 0.45)
- **WHEN** a person signs in with an address their provider verified, after first-run setup
- **THEN** an email tells them when, with which provider and from which browser, and what to do if it was not them
- **AND** it is queued through the outbox and recorded in the installation's trail

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

#### Scenario: Allowed domains are enforced (rev 0.197)
- **WHEN** a provider accepts only some email domains
- **THEN** only an account whose verified email ends with one of them signs in, or is linked, through it
- **AND** narrowing a provider's domains so that a Super admin has no way left to sign in is refused, as turning it off is

#### Scenario: Allowed domains and joining by domain
- **WHEN** a provider is configured
- **THEN** its allowed email domains can be set
- **AND** joining by domain is off by default, so an invitation is required unless it is turned on

#### Scenario: Providers set in the deployment's configuration (rev 0.196)
- **WHEN** a provider is added or edited
- **THEN** it is set in the deployment's configuration, its client secret in Secret Manager
- **AND** Super admins enable or disable it, set its allowed domains and choose whether to end its sessions in the app
- **AND** adding or editing a provider in the app comes in P2 or P3

#### Scenario: Who opens a domain to joining (rev 0.199)
- **WHEN** a domain is opened to joining
- **THEN** an Owner claimed it for their organisation, and a domain belongs to one organisation only
- **AND** a Super admin lets the provider accept it and take requests to join; both are needed

#### Scenario: Joining by domain awaits an Owner (rev 0.196)
- **WHEN** a person whose verified email is on an allowed domain signs in through a provider with joining by domain on
- **THEN** they get a pending membership with the Employee role
- **AND** it grants nothing until an Owner approves it

#### Scenario: Linking a new provider to an account
- **WHEN** a user links a new provider to their account
- **THEN** the link is made only in a session whose second factor is less than 5 minutes old, sent from Fyniq itself, after a successful sign-in with the new provider (rev 0.196: "only on a verified email" is superseded; the email plays no part)
- **AND** a matching verified email alone never links or merges accounts
- **AND** an account at a provider that signs someone in already is not linked

#### Scenario: Removing a sign-in (rev 0.196)
- **WHEN** a user removes one of their sign-ins, in a session whose second factor is less than 5 minutes old
- **THEN** that account at its provider signs them in no more, and the sessions it signed in end
- **AND** the sign-in the session used, or the user's last, cannot be removed

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

#### Scenario: Owner succession (rev 0.181)
- **GIVEN** another Owner is permanently unavailable (the owner's decisions of 6 Nov 2026, provisional until sign-off)
- **WHEN** an Owner starts a succession with a reason and the kind of evidence, optionally naming a member to become an Owner
- **THEN** the outgoing Owner and every Owner are told at once, and for 7 days the outgoing Owner may object and its starter withdraw it, each ending it
- **AND** from the 7th day a Super admin, any one, completes it with a file attached: the named member becomes an Owner and the outgoing Owner's membership ends; anything else is refused (FY268)

#### Scenario: Last Super admin cannot sign in
- **WHEN** the last Super admin cannot sign in
- **THEN** a deployment-held recovery procedure in the runbook restores access (open question 36)
- **AND** every use is audited and notifies every Super admin and Owner
