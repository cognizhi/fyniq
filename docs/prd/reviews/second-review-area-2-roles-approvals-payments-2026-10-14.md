# Second review · Roles, approvals and payments (co-founder (second director))

The record of the second review of this area ([review plan](second-review-plan-2026-10-14.md), Area 2).
It covers PRD-001 revisions 0.16–0.22 and PRD-002 revisions 0.2–0.6. Every link opens the PRD at the
line under review, pinned to commit `7369b46`. The [verification sweep](../research/verify-sweep-2026-10-14.md)
gives the official source for each statutory or vendor fact.

**Building does not wait for this review** (PRD-001 rev 0.22). The rules below are built on the
current recommendation and the evidence on file. Until this pull request merges, the 11 pages listed
at the end show a **Needs additional verification** banner (FR-UX-41). Merging marks the area reviewed in
the [review register](review-status.json), and the next release removes the banner.

## How to review

1. For each check, read the linked lines and write **Agree**, **Change** (say what) or **Question** in
   the Verdict column of this file, in a review comment on that line or as a suggested change.
2. Answer each decision in the last column. A blank answer means the current recommendation stands.
3. Approve the pull request when you are content. If anything changes, the PRD gets a new
   `(rev 0.N)` row, and its specs are updated and stamped in this pull request before it merges.

The "Workspaces by role" canvas (https://claude.ai/artifact/DHg2xeXhQwPh1NBgTkJyhi) shows what each role sees; it is private to the founder's account until shared from its Share menu.

## Checks

| # | Check | Where to read | Verdict (Agree · Change · Question) | Notes |
|---|---|---|---|---|
| 1 | What each role sees (18 roles) against the permission matrix | [§7.1](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L487) · [Appendix D](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L3611) |  |  |
| 2 | Every workspace cut to the person's rights; the personal workspace; the line manager as a relationship, not a role | [FR-UX-40](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L575) · [FR-UX-39](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L574) · [§6.2](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L386) |  |  |
| 3 | Department payroll cost shown only as a total covering at least 3 people besides the viewer | [FR-PAY-13](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L914) |  |  |
| 4 | Approvals across Fyniq and Nemura: the signed approval, posting and corrections | [FR-PPL-09](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1328) · [FR-PPL-11](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1330) · [PRD-002 NM-PAY-09](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-002-nemura-people.md?plain=1#L162) · [PRD-002 NM-PAY-10](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-002-nemura-people.md?plain=1#L163) |  |  |
| 5 | Salary payment batches: lines from Nemura, approval and release in Fyniq, holds checked at release, bank-detail versions | [FR-PPL-10](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1329) · [FR-PPL-13](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1332) · [FR-PPL-14](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1333) · [FR-BNK-12](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L822) · [FR-PAY-11](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L908) |  |  |
| 6 | Payment release and segregation of duties | [SOD-1](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L427) · [§6.4](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L438) · [FM-5](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L454) |  |  |
| 7 | Joiner invitations as an Administrator right | [FR-ADM-01](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1136) · [PRD-002 NM-ONB-02](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-002-nemura-people.md?plain=1#L233) |  |  |
| 8 | The pages that show this area's banner (list below) and the banner's wording | [FR-UX-41](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L576) |  |  |

## Decisions

None in this area: every item is a check.

## Pages that show this area's banner

`payment-batches` (Payment batches), `payment-batch` (Payment batch record), `salary-batch` (Salary payments), `pay-run` (Pay run from Nemura), `employees` (People (from Nemura)), `employee-record` (Person (from Nemura)), `project-reports` (Project reports), `users-roles` (Users and roles), `nm-payrun-approval` (Approve a pay run), `nm-checks` (Checks), `nm-onboarding` (Joiners and leavers).

Ask for a page to be added or removed in this pull request ([review register](review-status.json)).

## Outcome

| | |
|---|---|
| Reviewer | |
| Date | |
| Result | Approved · Approved with changes · Changes requested |
| PRD revision that records the changes | |
