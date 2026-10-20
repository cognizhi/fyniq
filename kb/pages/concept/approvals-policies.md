---
key: approvals.policies
type: concept
title: Approval policies: who approves each kind of change
summary: What an approval policy is, what its steps can say, how a policy is changed and approved, and what happens to waiting requests when it changes
sources:
  - docs/user-guide/approval-policies.md sha256:7de035d79ac26073652fe74d029e93b99111d59aacff936f50d945800d8a0b96
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - approval policy
  - who approves
  - approval steps
  - quorum
  - two approvals
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

## Change a policy

- **Settings → Approval policies** (Expert mode), find the kind of change and select **Change policy**; set each step's approvals and who may approve (**Add a step** adds one), then **Ask for this policy** and confirm it's you if asked.
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
- **A change to this policy is waiting already**: approve or reject that request first.

(src: docs/user-guide/approval-policies.md#common-problems)
