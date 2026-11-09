---
key: information-security
title: Information security policy
draft: fyniq-policy-draft@1
sources:
  - title: CSA, Cyber Essentials
    url: https://www.csa.gov.sg/our-programmes/support-for-enterprises/sg-cyber-safe-programme/cybersecurity-certification-for-organisations/cyber-essentials/
    retrieved: 2026-11-09
  - title: CSA, Summary of initiatives to help organisations enhance cybersecurity
    url: https://csa.gov.sg/News-Events/Press-Releases/2024/summary-of-initiatives-to-help-organisations-enhance-cybersecurity
    retrieved: 2026-11-09
  - title: CSA, Cyber Trust
    url: https://www.csa.gov.sg/our-programmes/support-for-enterprises/sg-cyber-safe-programme/cybersecurity-certification-for-organisations/cyber-trust/
    retrieved: 2026-11-09
controls: [CTL-CHG-03, CTL-CHG-08, CTL-IAM-05, CTL-DP-02, CTL-AUD-02]
---

# Information security policy

## Purpose

This policy says how the company protects its information, its systems and the people whose data it holds. It is
written for a company of a few people and follows the measures of CSA's Cyber Essentials mark: know your assets,
secure and protect them, keep them updated, back them up, and be ready to respond.

## Scope

Everyone who works for the company, and every device, account and service that holds or reaches company information,
including Fyniq.

## What we do

- **Assets.** We keep a list of our devices, accounts, software and cloud services, and who looks after each.
- **Secure and protect.** Every account has its own sign-in and a second factor; nobody shares a password. Devices
  lock themselves, run supported software and encrypt their disks.
- **Update.** We apply security updates promptly and remove software nobody needs.
- **Back up.** The data we cannot afford to lose is backed up, and we test that it can be restored (see the backup
  policy).
- **Respond.** We know who to call and what to do when something goes wrong (see the incident response policy).

## What Fyniq enforces

- Every change to the platform passes its security scans before it can ship (CTL-CHG-03), and runs in hardened
  images (CTL-CHG-08).
- Each company's data is kept apart from every other's by the database itself (CTL-IAM-05).
- The most sensitive fields are encrypted and masked (CTL-DP-02).
- Every saved change is in an audit trail that cannot be changed without it showing (CTL-AUD-02).

## Roles

The Owner is responsible for this policy and reviews it at least once a year, and whenever the company's systems or
risks change.
