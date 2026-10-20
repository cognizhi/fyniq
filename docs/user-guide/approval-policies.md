---
key: approvals.policies
title: Choose who approves a change
mode: expert
roles: [owner, admin, controller, accountant, auditor, dpo]
requirements: [FR-APR-01, FR-APR-09, FR-APR-10, SOD-8]
---

# Choose who approves a change

## What it is

An approval policy says who approves one kind of change, such as a setting or a role. Each policy
has one or more steps, taken in order. Each step needs a number of approvals from people who may
approve that kind of change, such as an Owner.

Until you change a policy, Fyniq uses its built-in policy: one approval from anyone who may
approve that kind of change, other than the person who asked.

## What a step can say

- **How many people approve it**: 1 to 5. Each person approves a request once, in one step.
- **Who may approve it**: anyone who may approve that kind of change, or only people with some
  roles, or only some named members. A step names roles or members, not both.
- A policy can only narrow who approves. It never lets someone approve a change their role does
  not allow.
- A policy has 1 to 5 steps. Step 2 starts only when step 1 has all its approvals.

## Change a policy

1. Open **Settings → Approval policies** (Expert mode) and find the kind of change.
2. Select **Change policy** and set the steps: how many approvals each needs, and who may approve.
   **Add a step** adds the next one.
3. Select **Ask for this policy**, and confirm it's you with your passkey or authenticator app when
   Fyniq asks.
4. Fyniq shows **Waiting for approval**. The policy has not changed yet.

An Administrator asks for the change. An Owner or a Controller who did not ask for it approves
it, as with any other change ([Approve a change](approvals.md)). Nobody can change who approves
policy changes themselves, so a change can never lock the company out.

## What happens when a policy changes

- Every earlier version is kept, with who asked for it and when it was approved.
- Requests of that kind that were still waiting are cancelled. Ask for those changes again.

## Common problems

- **The steps cannot be signed**: a step names a role that never approves that kind of change, a
  member without such a role, or more approvals than there are people who may give them. Change
  the step, or give the right people the role first.
- **A change to this policy is waiting already**: approve or reject that request first.
