---
key: settings.access-review
title: Review who can do what each quarter
mode: expert
roles: [owner, administrator, controller, auditor, dpo]
requirements: [FR-CTL-06, SEC-06]
---

# Review who can do what each quarter

## What it is

**Settings → Access review** shows who can do what in Fyniq, as it stands today. Once each calendar
quarter ends, an Owner reviews it and attests it. It is in Expert mode, for the Owner, the
Administrator, the Controller, the Auditor and the data protection officer.

## What it shows

- **People and their access**: each member's roles, an external person's end date and any roles
  that have ended, when they last signed in, and how they passed their second factor (a passkey, or
  an authenticator app at sign-in). The **Review** column points out what to look at: a role
  conflict, roles that ended while the person is still a member, or no sign-in for 30 days.
- **Role conflicts**: a person who manages users and roles and also records or approves
  transactions. Founder mode allows it, because another founder reviews what they do alone each
  week; Standard mode blocks it unless an approved override lets them keep it.
- **Integration clients**: the connectors installed, what they may reach, and whether their last
  test passed.
- **Earlier quarters**: each attested quarter, who attested it, when, and their note.

Change roles or remove someone in **Settings → Members and roles**
([Invite people and give them roles](members-and-roles.md)).

## Attest the quarter

When a quarter has ended, the page names it at the top. Review everyone's access, make or ask for
any changes, then at the bottom write what you checked, if you like, and select **Attest**. Fyniq
asks you to confirm it's you. Only an Owner can attest.

- Each quarter is attested once. Attesting the latest quarter also answers for any quarter before
  it that was missed.
- What you attest is kept exactly as you saw it, with its fingerprint (SHA-256), in the audit
  trail. It is never changed.
- If someone's access changed while you were reading, the page asks you to look again before you
  attest. A sign-in alone does not count as a change.
- Until the quarter is attested, the check "An Owner attests the access review each quarter"
  warns, from 14 days after the quarter ends. It cannot be marked reviewed on the Checks page.

## Download it

Select **Download CSV** for a spreadsheet of the review: one row per person, invitation not yet
used, and integration. Each download is recorded in the audit trail.
