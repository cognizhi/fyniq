# The browser journeys (QA-E2E-01)

Playwright against the stack `make up` runs, one organisation per journey with the sample company, each person signed
in through the local sign-in page and joined by invitation (`p1-launch-readiness` design D6). Each step is checked on
the screen and in what the API then returns. Run them with `make test-journeys`; CI runs them in the images job.

| Journey | File | People | What it proves |
|---|---|---|---|
| Set-up | `setup.spec.ts` | Owner, Bookkeeper | Home's set-up checklist: the Owner takes the Controller's role, records the profile and invites a Bookkeeper, who joins |
| Record | `record.spec.ts` | Accountant | A manual journal, with its evidence, posted as the next number; the general ledger shows it |
| Sell | `sell.spec.ts` | Bookkeeper, Accountant, Owner and Controller | An invoice prepared, asked to issue above the self-approval threshold, approved in the Inbox; the customer owes it |
| Buy | `buy.spec.ts` | Bookkeeper, Accountant | A bill with its supplier's invoice attached, posted; the company owes the supplier |
| Pay | `pay.spec.ts` | Bookkeeper, Accountant | A posted bill's payment recorded; the bill is paid in full |
| Reconcile | `reconcile.spec.ts` | Bookkeeper, Accountant | A money account added, the bank's OFX statement imported, its line matched to the payment |
| Close | `close.spec.ts` | Accountant, Owner and Controller | January's checklist and checks, attested and closed by another person; a January posting refused |
| GST | `gst.spec.ts` | Owner and Controller, Accountant | The registration monitor and the statutory calendar: a forecast with its contract makes the company liable, and the calendar says by when |
| Smoke | `smoke.spec.ts` | Owner | The harness itself: the Owner lands in the organisation, and the screen and the API agree |
| AI ask | `ai.spec.ts` | Owner | The assistant answers January's revenue with the books' own figure, which says where it comes from; the API kept the turn with the fact it cites |
| AI propose → approve → execute | `ai.spec.ts` | Owner, Company secretary | The assistant proposes a compliance task and nothing changes; the Company secretary approves it in the Inbox; the executor makes it |
| Roles | `walkthroughs.spec.ts` | Each role of §7.1, alone | QA-UI-08: where the role lands, the areas its sidebar shows, whether Home shows the company pulse, the Inbox, and a page outside its access, which says so while the API refuses the read behind it |

Not here yet:

- **Payroll** and the **GST F5 return** are `after-go-live`, by the owner's decisions: pay runs come with the payroll
  link, and the F5 with the company's GST registration.

The AI journeys run on local mode's **scripted model** (the owner's decision of 17 Nov 2026, design D6): `cmd/seed
--scripted-ai` turns the AI on in the journey's organisation, routes its chat agent to a connection to the scripted
model, which only the local build has (`internal/adapters/local/scripted.go`), and turns the task type
`sec.create_task` on. The model answers the journeys' two questions (`scripted.json`) as a right model would, so the
journeys prove the path, offline; how well a real model answers is the eval suites' (QA-AI-01…03).

`harness.ts` makes the organisations and signs people in; `books.ts` holds the steps several journeys share (a bill
entered and posted, a payment recorded, a document attached). The API counts what a person does before they belong to
an organisation per client address (SEC-23), so the harness paces sign-ins and sends a refused request again after its
`Retry-After`.
