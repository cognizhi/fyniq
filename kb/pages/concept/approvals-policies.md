---
key: approvals.policies
type: concept
title: Approval policies: who approves each kind of change
summary: Who approves each kind of change: a policy's steps, its rules (the first matching rule decides the steps), changing a policy, and waiting requests
sources:
  - docs/user-guide/approval-policies.md sha256:b529e4d672ce43c2725036887309fded90153dc7dfbe3dbfaa36a081af9aba79
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-06
aliases:
  - approval policy
  - who approves
  - approval steps
  - quorum
  - two approvals
  - approval rule
  - approval conditions
  - approve by amount
---

# Approval policies: who approves each kind of change

## What a policy is

- An approval policy says who approves one kind of change, such as a setting or a role.
- It has one or more steps, taken in order; each step needs a number of approvals from people who may approve that kind of change, such as an Owner.
- Until a policy is changed, the built-in policy applies: one approval from anyone who may approve that kind of change, other than the person who asked ([[approvals.decide]]).

(src: docs/user-guide/approval-policies.md#what-it-is)

## What a step can say

- How many people approve it: 1 to 5; each person approves a request once, in one step.
- Who may approve it: anyone who may approve that kind of change, only people with some roles, or only some named members, never roles and members together.
- A policy only narrows who approves; it never lets someone approve a change their role does not allow ([[members.roles]]).
- A policy has 1 to 5 steps; step 2 starts only when step 1 has all its approvals.

(src: docs/user-guide/approval-policies.md#what-a-step-can-say)

## Rules: different steps for different changes

- A policy can have rules; each says which changes it covers and the steps they take. The steps set first are the **default steps**, used when no rule matches.
- A rule covers a change that meets all its conditions, and sets at least one: **Amount at or above** (in the base currency), **Departments** (a line carries one), **Accounts** (a line posts to one), **Vendors** (the bill, order or payment is for one).
- Rules are checked top to bottom; **the first rule that matches decides the steps**, so put the strictest first.
- A condition on something a change does not have never matches: a setting has no amount, so an amount rule never covers it.
- Journals, bills, invoices, purchase orders and payment batches can match rules; a policy has at most 10 rules.
- The approval page says which rule chose a request's steps; a request keeps its steps, and waiting requests are cancelled when the policy changes ([[approvals.decide]]).

(src: docs/user-guide/approval-policies.md#rules-different-steps-for-different-changes)

## Change a policy

- **Settings → Approval policies** (Expert mode), find the kind of change and select **Change policy**; set the default steps' approvals and who may approve (**Add a step** adds one).
- **Add a rule** adds a rule: set its conditions and steps; **Move rule 1 up** or **down** reorders, **Remove rule 1** removes it.
- Then **Ask for this policy** and confirm it's you if asked.
- Fyniq shows **Waiting for approval**; the policy has not changed yet.
- An Administrator asks; an Owner or a Controller who did not ask approves it, like any other change.
- Nobody can change who approves policy changes themselves, so a change can never lock the company out.

(src: docs/user-guide/approval-policies.md#change-a-policy)

## When a policy changes

- Every earlier version is kept, with who asked for it and when it was approved.
- Requests of that kind still waiting are cancelled; ask for those changes again.

(src: docs/user-guide/approval-policies.md#what-happens-when-a-policy-changes)

## Common problems

- **The steps cannot be signed**: a step names a role that never approves that kind of change, a member without such a role, or more approvals than there are people who may give them; change the step or give the right people the role first.
- **A rule has no condition**: set an amount, departments, accounts or vendors, or remove the rule.
- **A change to this policy is waiting already**: approve or reject that request first.

(src: docs/user-guide/approval-policies.md#common-problems)
