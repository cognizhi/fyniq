---
key: settings.statutory-packs
title: Activate a statutory pack
mode: expert
roles: [owner, controller, accountant, tax_agent, payroll, auditor]
requirements: [ENG-30, ENG-32, SOD-10, FR-ORG-05]
---

# Activate a statutory pack

## What it is

Fyniq keeps statutory values, such as GST rates and tax codes, in packs. Each pack version takes its
values from official pages, names its sources and who verified it, and comes with pinned tests worked
by hand from those pages. A new version is used in your books only after your company activates it.

## See the packs

1. Open **Settings → Statutory parameter packs**.
2. Choose a version to see when it is in force, its sources, and the result of each pinned test.

A version whose pinned tests do not all pass cannot be activated.

## Activate a version

1. Open the version and select **Activate**.
2. Confirm it's you with your passkey or authenticator app.
3. Fyniq shows **Waiting for approval**. The version is not active yet.

A Controller, an Accountant or a Tax agent asks. An Owner or a Controller who did not ask approves it,
as with any other change ([Approve a change](approvals.md)). Fyniq runs the pinned tests again before
it records the activation, with who asked, who approved and the request.

## Common problems

- **A pinned statutory test fails**: the version cannot be activated. The platform team publishes a
  corrected version.
- **That pack version is activated already**: nothing to do.
- **This release cannot read that pack version**: activate it after updating Fyniq to a release that
  carries it.
