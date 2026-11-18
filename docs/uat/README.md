# User acceptance tests

The scripts the founders and the accountant run before go-live (PRD-001 §17.3, A3; `p1-launch-readiness` task 7.1).
Each walks one launch journey of QA-E2E-01, or one role of §7.1's table "What each role sees" (QA-UI-08), through the
real screens. The browser journeys in `apps/web/journeys/` prove the same paths on every release; a UAT script is a
person checking that the path makes sense, that the words are right, and that the figures are the ones they expect.

- **Where:** first on the local stack (`make up`, the local sign-in page), each script in an organisation of its own
  with the sample company loaded, the synthetic company of `docs/design/screens/sample-data.json` (DEV-04, DEV-08).
  Then again in the dev cloud before `p1-go-live`, on its copy of the sample company `[live-verify]`: the sign-in there
  is the organisation's provider, and the organisations are made as `cloud-first-deployment` provides.
- **Who:** each script names its people. One founder plays them all, each in a browser window of its own, or the
  founders share them out. The accountant runs the journeys that post (record, sell, buy, pay, reconcile and close)
  and signs them off with the founders.
- **Not here:** payroll's journey and the GST F5's. Both are after go-live by the owner's decisions (rev 0.25): payroll
  with Nemura, and the F5 when the company registers for GST (`docs/prd/plans/launch-exceptions.json`). Their scripts
  come with their changes.

## The scripts

| Journey (QA-E2E-01) | Script | Who |
|---|---|---|
| Set-up | [journeys/setup.md](journeys/setup.md) | Owner, Bookkeeper |
| Record | [journeys/record.md](journeys/record.md) | Accountant, Owner |
| Sell | [journeys/sell.md](journeys/sell.md) | Bookkeeper, Accountant, Owner |
| Buy | [journeys/buy.md](journeys/buy.md) | Bookkeeper, Accountant |
| Pay | [journeys/pay.md](journeys/pay.md) | Bookkeeper, Accountant |
| Reconcile | [journeys/reconcile.md](journeys/reconcile.md) | Bookkeeper, Accountant |
| Close | [journeys/close.md](journeys/close.md) | Accountant, Owner |
| The GST monitor and calendar | [journeys/gst-monitor-calendar.md](journeys/gst-monitor-calendar.md) | Accountant |
| AI ask | [journeys/ai-ask.md](journeys/ai-ask.md) | Owner, a second founder, Accountant |
| AI propose → approve → execute | [journeys/ai-propose-approve-execute.md](journeys/ai-propose-approve-execute.md) | Owner, a second founder |

The roles of §7.1, one script each, in `roles/`: [Employee](roles/employee.md),
[Line manager](roles/line-manager.md), [Approver](roles/approver.md), [Bookkeeper](roles/bookkeeper.md),
[AR specialist](roles/ar-specialist.md), [AP specialist](roles/ap-specialist.md),
[Payment releaser](roles/payment-releaser.md), [Payroll administrator](roles/payroll-administrator.md),
[Accountant](roles/accountant.md), [Controller](roles/controller.md), [Owner](roles/owner.md),
[Administrator](roles/administrator.md), [Auditor](roles/auditor.md), [Tax agent](roles/tax-agent.md),
[Board viewer](roles/board-viewer.md), [Company secretary](roles/company-secretary.md), [DPO](roles/dpo.md) and
[HR officer](roles/hr-officer.md).

## Before a run, on the local stack

1. Start the stack: `make up`, then open `http://localhost:8080`.
2. On a new installation, set it up once (FR-ADM-06): sign in through the local sign-in page, run `make setup-code`,
   and enter the code on the setup page. The installation's own organisation is the setup's; each script makes its
   own.
3. Make the script's organisation, with the sample company loaded and an invitation for each of its people:

   ```
   make uat-organisation NAME="UAT Sell Pte Ltd" PEOPLE="Bea Bookkeeper:bookkeeper;Cal Accountant:accountant;Ann Owner:owner,controller"
   ```

   It prints the organisation's ID and one invitation code per person. Each code works once, within 24 hours.
4. For each person, open a browser window of their own (a private window, or another browser profile), go to
   `http://localhost:8080`, and sign in on the local sign-in page with a subject of their own (such as `uat-bea`) and
   their name. On "Joining by invitation", enter their code and their name, and accept the invitation.
5. The role scripts share one organisation, with a person per role:

   ```
   make uat-organisation NAME="UAT Roles Pte Ltd" PEOPLE="Ann Owner:owner;Eve Employee:employee;Liam Manager:employee;Ava Approver:approver;Bea Bookkeeper:bookkeeper;Ari Receivables:ar_specialist;Pat Payables:ap_specialist;Rae Releaser:payment_releaser;Paz Payroll:payroll_administrator;Cal Accountant:accountant;Con Controller:controller;Ada Admin:administrator;Aud Auditor:auditor;Tad Tax:tax_agent;Bo Board:board_viewer;Cos Secretary:company_secretary;Dee Protection:dpo"
   ```

   The Line manager is a relationship, not a role, and the HR officer is a role of Nemura, not of Fyniq: no invitation
   grants either (their scripts say how they are run).

The business date is the stack's today. Amounts in the scripts are the sample company's, or the ones a step enters.

## Reading a script

Each step names who does it, the screen (its ID in `docs/design/screens/inventory.json`, where its design is), what to
do, what to see and the evidence to keep. A step passes when what you see matches. When it does not, keep the
evidence, write what you saw in the sign-off's result, and raise it as a bug (`fyniq-bug-fix`): a figure that differs
is a calculation defect (QA-REL-01).

## The evidence

- A screenshot of each step's screen, named `<script>-<step>.png` (such as `sell-07.png`), and each file a step
  downloads or attaches.
- Keep a run's evidence in one folder named for the date and the place (`2026-11-20-local`), outside the repository.
  `p1-go-live` links it from the release record.
- Synthetic data only: never real company, employee or bank data, on a laptop or in the dev cloud (DEV-08).

## Sign-off

Each script ends with its sign-off: the two founders and the accountant, each with the date and the result (pass, or
fail with the issue's number). A script passes when every step passes. The launch's UAT passes when every script has
passed in the dev cloud `[live-verify]`.

`scripts/test_uat.py` (`make script-tests`, part of `make check`) checks that every launch journey of QA-E2E-01 and
every role of §7.1 has its script, that each step names a screen of the inventory, and that each script has its
evidence and its sign-off.
