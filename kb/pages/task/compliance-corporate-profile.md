---
key: compliance.corporate-profile
type: task
title: Keep the corporate profile and registers
summary: The company's details, officers, nominee registers and statements approvals, their lodgements and ACRA check, and drafting resolutions and minutes
sources:
  - docs/user-guide/corporate-profile.md sha256:0804a5902da60da96074b952ec0f2f23b38165ac8e401bb06890126a429c69c0
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
  - directors' resolution
  - members' written resolution
  - AGM notice
  - minutes of the AGM
  - draft a resolution
  - minute book
  - signed copy
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

## Draft a resolution, notice or minutes

- Fyniq drafts the company's papers from its own templates, filled from the records on the day: directors' resolutions (approving the statements, allotting shares, appointing an officer, ending the secretary's or CEO's appointment, changing the registered office), a members' written resolution, the AGM's notice and minutes, and the notice that the statements were sent without an AGM.
- Choose the kind, fill in what it asks for, read the draft, download it as PDF and have it signed outside Fyniq.
- Attach the signed copy and choose **Record in the minute book** with the day it was signed; another person approves, and it stays in the minute book unchanged.
- Once the signed copy is attached, the text stays as signed; remove the signed copy to draft it again.
- Fyniq never signs or files a draft, and each says so at its top; the templates are Fyniq's and a lawyer has yet to confirm them.
- A directors' resolution needs the directors in office, a members' resolution the shares issued ([[equity.register]]), a notice the company secretary.
- Fyniq does not yet check a notice's period, a written resolution's majority or when minutes go in the minute book.

(src: docs/user-guide/corporate-profile.md#draft-a-resolution-notice-or-minutes)

## Lodgements and checks

- **Lodgements** lists each change ACRA must be told of, with its deadline; it is done once a version with the day lodged is recorded.
- Four checks run without the AI assistant ([[compliance.secretary]]): a secretary, the directors' approval within 30 days of the statements' deadline, the profile against ACRA, and every change lodged on time.
- A check that lacks what it needs is **Not assessed** and names it, never all clear.

(src: docs/user-guide/corporate-profile.md#lodgements-and-checks)
