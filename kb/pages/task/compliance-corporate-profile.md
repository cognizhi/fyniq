---
key: compliance.corporate-profile
type: task
title: Keep the corporate profile and registers
summary: The company's details, officers, nominee registers and statements approvals; their lodgements; and the comparison with ACRA
sources:
  - docs/user-guide/corporate-profile.md sha256:f51377927a90ca5a1db38c76723442a028db538222d392bce8a7230f321214b6
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-16
aliases:
  - corporate profile
  - registered office
  - company officers
  - named company secretary
  - nominee director
  - nominee shareholder
  - lodge with ACRA
  - ACRA business profile
  - directors' approval of the financial statements
---

# Keep the corporate profile and registers

## What it is

- **Compliance → Corporate profile and registers** keeps the company as ACRA's registers hold it: name, registered office and SSIC code, the officers, the nominee registers, and the directors' approval of each year's statements.
- The UEN and the financial years stay in the organisation's settings; registrable controllers stay in the equity register ([[equity.register]]).
- The Accountant and the Company secretary record changes; the Owner and the Controller approve them; the Auditor, the Tax agent and the Board viewer see it, and the DPO sees it with identifiers hidden.

(src: docs/user-guide/corporate-profile.md#what-it-is)

## Record a change

- Each record starts as a draft: attach its evidence (the BizFile+ acknowledgement, the consent to act, the directors' resolution), then **Ask for approval**.
- Another person approves it: never the one who asked, nor the officer or nominee it is about.
- A recorded record never changes; a cessation, a lodgement day or a correction is a new version that replaces it, the old one kept in the history.
- Only the last 4 characters of an officer's identifier are kept.

(src: docs/user-guide/corporate-profile.md#record-a-change)

## The company and its officers

- Record the details from incorporation first: the checks count from that day.
- A change of the registered office, and each officer's appointment or cessation, is lodged with ACRA within 14 days.
- A director, the company secretary and the CEO are people; an auditor can be a firm, by its UEN.
- The named secretary is marked; a company needs one within six months of incorporation, and the sole director cannot also be the secretary.

(src: docs/user-guide/corporate-profile.md#the-company-and-its-officers)

## Nominee directors and shareholders

- Record each nominee, whom they act for and since when.
- A nominee tells the company within 30 days; the company enters them in its register within 7 days, then tells ACRA's central register within 2 business days after.

(src: docs/user-guide/corporate-profile.md#nominee-directors-and-shareholders)

## Checked against ACRA

- Type in ACRA's business profile, with its officers and registered office, under **Equity → Reconciliation**.
- The officers in office and the registered office are compared with it on its day; each difference is fixed in the profile or by a newer business profile.
- With no business profile, none with its officers, or one older than the last change, the comparison is **Not assessed**.

(src: docs/user-guide/corporate-profile.md#checked-against-acra)

## Lodgements and checks

- **Lodgements** lists each change ACRA must be told of, with its deadline; it is done once a version with the day lodged is recorded.
- Four checks run without the AI assistant ([[compliance.secretary]]): a secretary, the directors' approval within 30 days of the statements' deadline, the profile against ACRA, and every change lodged on time.
- A check that lacks what it needs is **Not assessed** and names it, never all clear.

(src: docs/user-guide/corporate-profile.md#lodgements-and-checks)
