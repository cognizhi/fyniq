# Proposal

## Why

P0 signs people in, makes the first person the Super admin and Owner, and checks every endpoint
against one policy table. Nobody can yet:
- invite a colleague;
- hold a finance role;
- enrol a second factor;
- recover an account the way §11 and SEC-35 require.

Every P1 module's permissions are rows of Appendix D's matrix, so the role model and the matrix come
first, and each module closes its rows as it lands.

## What Changes

- **Invitations (FR-ADM-01):** invite users by email with roles and scopes (entity, department), and
  expiry dates for external users.
- **MFA and passkeys (FR-ADM-02, SEC-01):** MFA is mandatory for everyone, by TOTP through Identity
  Platform or by passkey. WebAuthn passkeys, implemented in the API, serve step-up and approvals. SMS
  is not offered.
- **Sessions (FR-ADM-03, SEC-05):** idle and absolute timeouts, a device list, revocation and sign-in
  alerts, on the P0 session cookie (SEC-08).
- **Approval-gated administration (FR-ADM-04):** changes to the team mode, approval policies, AI
  settings, statutory parameters and integrations wait for `p1-approval-engine`'s policy.
- **Super admin, sign-in and succession, completed from P0's parts (FR-ADM-07…09):**
  - the Super admin's scope;
  - sign-in providers by configuration, allowed domains and identity linking;
  - Owner succession with dual control.
- **External users and OAuth clients (SEC-06, SEC-07):** external users expire and are reviewed each
  quarter. Every OAuth client has a class, an owner, an expiry and scopes.
- **Authorisation on every output (SEC-34):** every derived output and channel applies the record's
  row scope, masking and AI exclusions. A role change takes effect within 60 seconds.
- **Account recovery (SEC-35):** two independent proofs, a cooling-off period and notices. Email or
  support alone never recovers an account (G4-060).
- **The roles of §6 and their combinations, and SOD-1…10:**
  - the permission model, with role-based rights, attribute conditions and field masking, checked in
    the API and again by row-level security;
  - the AI and integration principals.
- **Appendix D's permission matrix, all 37 areas:**
  - generated into the policy table;
  - the authorisation-matrix test (QA-SEC-03) covers every role × every operation;
  - each area's endpoints arrive with their module, and its matrix rows are asserted then.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `access/users-and-sign-in`: FR-ADM-01…04 and 07…09;
- `security/identity-and-access`: SEC-01, 05…08, 34 and 35;
- `access/roles-and-segregation`: SOD-1…10, the roles, the permission model and the 37 matrix areas.

## Impact

- **Code:** `internal/authn`, `internal/authz` (the policy table generated from Appendix D),
  `internal/signin`, `internal/members`, migrations for invitations, roles, scopes, passkeys, recovery
  and devices, the API and the screens.
- **Screens:** account-recovery, auditor-workspace, mfa-enrolment, super-admin-console and
  users-roles.
- **Security boundary.**
  - Reviewers: security (#4) and the co-founder (#6).
  - Waived before production, so the pull requests merge after automatic approval. Label one
    `needs-human-review` to hold it.
- **Cloud:** Identity Platform's TOTP is tested against its fake. The live check belongs to the cloud
  gate (§17.5 item 3).
