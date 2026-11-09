---
key: change-management
title: Change management policy
draft: fyniq-policy-draft@1
sources:
  - title: CSA, Cyber Trust
    url: https://www.csa.gov.sg/our-programmes/support-for-enterprises/sg-cyber-safe-programme/cybersecurity-certification-for-organisations/cyber-trust/
    retrieved: 2026-11-09
  - title: CSA, Cyber Essentials
    url: https://www.csa.gov.sg/our-programmes/support-for-enterprises/sg-cyber-safe-programme/cybersecurity-certification-for-organisations/cyber-essentials/
    retrieved: 2026-11-09
controls: [CTL-CHG-01, CTL-CHG-02, CTL-CHG-03, CTL-CHG-10, CTL-CHG-11]
---

# Change management policy

## Purpose

This policy says how changes to our systems and settings are made, so that a change is deliberate, reviewed and can be
traced.

## Scope

Changes to Fyniq's code and infrastructure, and to the company's important settings: approval rules, roles, bank
details and integrations.

## What we do

- Changes are made through the agreed route, never by hand on a live system.
- Each change is reviewed by someone other than its author before it goes live.
- Each change is recorded: what changed, who made it, who approved it, and why.
- A change that goes wrong can be rolled back.

## What Fyniq enforces

- Protected branches and a second person's review for each change to the platform (CTL-CHG-01).
- Infrastructure only through code, with drift found (CTL-CHG-02).
- Security scans that block a change with a critical finding (CTL-CHG-03).
- Versions and release notes produced only from the recorded changes (CTL-CHG-10), and pinned, least-privilege
  workflows (CTL-CHG-11).
- Important settings change only with the approval their policy requires, and every change is in the audit trail.

## Roles

The Owner approves this policy and reviews it at least once a year.
