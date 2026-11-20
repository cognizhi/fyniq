# Knowledge-base log

Append-only. One entry per compile, edit, lint or rollback, written with
`python3 scripts/kb.py log <op> "<text>"`. Newest last.

## [2026-10-12] compile | founders-money.md → founders.balance, founders.settle-balance, founders.pay-choice (initial compile, kb-schema@1)

## [2026-10-12] compile | founders.balance: add aliases (kb-schema@1 optional field; no vectors, AI-58 rev 0.5)

## [2026-10-13] compile | docs/user-guide/founders-money.md → founders-balance, founders-pay-choice, founders-settle-balance (product renamed Fyniq; wording only)

## [2026-10-14] compile | docs/user-guide/claims-my-claims.md, claims-approve-team.md, nemura/leave-apply.md, nemura/leave-sick.md → claims.make-a-claim, claims.approve-team-claims, leave.apply, leave.report-sick (new guide pages for the personal workspace and Nemura leave; all draft)

## [2026-10-14] compile | docs/user-guide/needs-verification.md → review.needs-verification (new: the FR-UX-41 banner, PRD-001 rev 0.22); founders.settle-balance links to it

## [2026-10-15] compile | docs/user-guide/sign-in.md, docs/user-guide/setup-first-run.md → auth.sign-in, installation.setup (new: the P0 sign-in and first-run setup screens, p0-identity-oauth-first-run slice 4c)

## [2026-10-20] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (new: organisation settings with recommended values, effective dates and the second-approver gate, p1-organisation-chart task 2.3; draft until the accountant reviews the tolerances and threshold)

## [2026-10-20] compile | docs/user-guide/members-and-roles.md → members.roles, members.invite (new: members, roles and invitations with the founder bootstrap, PRD-001 rev 0.35, p1-users-roles task 2.1)

## [2026-10-20] compile | docs/user-guide/approvals.md (new), members-and-roles.md, organisation-settings.md → approvals.decide (new), members.roles, members.invite, settings.change, settings.organisation (rev 0.36: changes under FM-1 now wait for a second approver instead of being refused)

## [2026-10-20] compile | docs/user-guide/home.md, docs/user-guide/appearance.md → home.overview, preferences.appearance (new guide pages for the app shell's first slice: Home, search and commands, appearance)

## [2026-10-20] compile | docs/user-guide/approval-policies.md, docs/user-guide/approvals.md → approvals.policies, approvals.decide (approval policies as data, steps, withdrawing a request; PRD rev 0.40)

## [2026-10-20] compile | docs/user-guide/statutory-packs.md → settings.statutory-packs (activating a pack version through the approval engine; PRD rev 0.41)

## [2026-10-20] compile | docs/user-guide/approvals.md → approvals.decide (activating a statutory pack waits for approval; links settings.statutory-packs)

## [2026-10-20] compile | docs/user-guide/team-mode.md (new), docs/user-guide/members-and-roles.md → settings.team-mode (new), members.roles, members.invite (team modes and SOD-6, PRD rev 0.42)

## [2026-10-20] compile | docs/user-guide/approvals.md → approvals.decide (a team mode switch voids waiting requests, rev 0.42)

## [2026-10-20] compile | docs/user-guide/sign-in.md → auth.sign-in (passkeys and the mandatory second factor, PRD rev 0.43)

## [2026-10-20] compile | docs/user-guide/approvals.md → approvals.decide (approvals signed with a passkey, PRD rev 0.44)

## [2026-10-20] compile | docs/user-guide/sign-in.md → auth.sign-in (your devices and sign-in emails, PRD rev 0.45)

## [2026-10-20] compile | docs/user-guide/approvals.md → approvals.decide (the review log, PRD rev 0.46)

## [2026-10-20] compile | home.md, sign-in.md, profile-and-security.md → home.overview, auth.sign-in, preferences.security (Settings and Profile and security pages in the web app)

## [2026-10-20] compile | approvals.md → approvals.decide (Inbox, the request page and its confirmation, the review log in the web app)

## [2026-10-20] compile | members-and-roles.md → members.roles, members.invite (Settings › Members and roles in the web app)

## [2026-10-20] compile | team-mode.md, approval-policies.md → settings.team-mode, approvals.policies (Settings › Team mode and Approval policies in the web app)

## [2026-10-20] compile | organisation-settings.md, statutory-packs.md → settings.organisation, settings.change, settings.statutory-packs (Settings › Organisation and Statutory parameter packs in the web app)

## [2026-10-20] compile | chart-of-accounts.md → accounting.chart (new guide for Accounting › Chart of accounts in the web app)

## [2026-10-20] compile | organisation-settings.md → settings.organisation, settings.change (related pages)

## [2026-10-20] compile | contacts-and-items.md → contacts.manage (new guide for Contacts and Items and prices in the web app)

## [2026-10-20] compile | chart-of-accounts.md → accounting.chart (related pages)

## [2026-10-20] compile | home.md → home.overview (Home's Waiting for you and Set up the company)

## [2026-10-20] compile | home.md → home.overview (the roles step on Home's checklist)

## [2026-10-21] compile | docs/user-guide/journals.md, periods-and-close.md (new) and organisation-settings.md (the years fixed once a period is open) → accounting.journals, accounting.periods (new), settings.organisation, settings.change (journal screens and periods page, p1-general-ledger-close)

## [2026-10-21] compile | docs/user-guide/opening-balances.md (new), periods-and-close.md (related link) → accounting.opening-balances (new), accounting.periods (opening balances screen, p1-general-ledger-close 3.1, 7.2)

## [2026-10-21] compile | docs/user-guide/periods-and-close.md (close a month) → accounting.periods (the close page, p1-general-ledger-close 1.2, 7.2)

## [2026-10-21] compile | docs/user-guide/periods-and-close.md (reopen a month) → accounting.periods (reopening, p1-general-ledger-close 1.3, 7.2)

## [2026-10-21] compile | docs/user-guide/general-ledger.md (new) → reports.general-ledger (the general ledger screen, p1-general-ledger-close 4.1, 7.3); journals.md (related link) → accounting.journals

## [2026-10-21] compile | docs/user-guide/organisation-settings.md (self-approval threshold) → settings.organisation, settings.change (journal approvals, p1-general-ledger-close 2.2)

## [2026-10-21] compile | docs/user-guide/journals.md (journals above the self-approval threshold, ask to reverse), approvals.md (journal approvals, self-approved log) → accounting.journals, approvals.decide (p1-general-ledger-close 2.2)

## [2026-10-21] compile | docs/user-guide/journals.md (edit a posted journal) → accounting.journals (p1-general-ledger-close 2.3, 7.1)

## [2026-10-21] compile | docs/user-guide/journals.md (journals that reverse by themselves) → accounting.journals (p1-general-ledger-close 2.4)

## [2026-10-21] compile | docs/user-guide/general-ledger.md (a new financial year) → reports.general-ledger (p1-general-ledger-close 1.4)

## [2026-10-21] compile | docs/user-guide/journals.md (a draft's statuses) → accounting.journals (p1-general-ledger-close 6.1)

## [2026-10-21] compile | docs/user-guide/journals.md, periods-and-close.md, home.md → accounting.journals, accounting.periods, home.overview (recurring journals, automatic reversals that wait, the new close checks; PRD rev 0.59-0.61)

## [2026-10-21] compile | docs/user-guide/accounting-framework.md → settings.accounting (new page: the accounting framework and policies, FR-ORG-10, rev 0.62)

## [2026-10-21] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the capitalisation threshold is confirmed by its accounting policy; link to settings.accounting)

## [2026-10-21] compile | docs/user-guide/accounting-framework.md, organisation-settings.md → settings.accounting, settings.organisation, settings.change (signing asks for a second factor while the Accounting sign-off setting is on; rev 0.63)

## [2026-10-21] compile | docs/user-guide/organisation-settings.md, docs/user-guide/periods-and-close.md → settings.organisation, settings.change, accounting.periods (flux analysis: the variance settings and the close's variances check, FR-GL-14)

## [2026-10-21] compile | docs/user-guide/periods-and-close.md, docs/user-guide/organisation-settings.md → accounting.periods, settings.organisation, settings.change (the period-close page's Variances section, FR-GL-14)

## [2026-10-22] compile | docs/user-guide/opening-balances.md → accounting.opening-balances (changing posted opening balances with another person's approval, FM-1)

## [2026-10-22] compile | docs/user-guide/journals.md → accounting.journals (attaching evidence to a journal, FR-DOC-05)

## [2026-10-22] compile | docs/user-guide/periods-and-close.md → accounting.periods (the close checklist's check, FR-GL-07)

## [2026-10-22] compile | docs/user-guide/periods-and-close.md → accounting.periods (working through the close checklist and changing its template, FR-GL-07)

## [2026-10-22] compile | docs/user-guide/periods-and-close.md → accounting.periods (year end on the close page, rev 0.69)

## [2026-10-22] compile | docs/user-guide/currencies.md → accounting.currencies (new: exchange rates, the cascade, adding, importing and withdrawing rates); docs/user-guide/journals.md → accounting.journals (a line's rate left blank takes the cascade's; FX-01)

## [2026-10-22] compile | docs/user-guide/currencies.md → accounting.currencies (revalue a month; FX-05 to FX-07); docs/user-guide/periods-and-close.md → accounting.periods (the revaluation close check); docs/user-guide/chart-of-accounts.md → accounting.chart (the revalued-at-month-end flag; COA-10)

## [2026-10-22] compile | docs/user-guide/currencies.md → accounting.currencies (the money accounts in other currencies, in both currencies)

## [2026-10-22] compile | docs/user-guide/currencies.md → accounting.currencies (the scheduler revalues each ended month by itself, rev 0.75)

## [2026-10-22] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (GST rounding per line or per subtotal, RND-GST-EXCL)

## [2026-10-22] compile | docs/user-guide/bills.md → purchases.bills (new: entering, posting, approving, voiding and editing bills); accounting.currencies links it

## [2026-10-23] compile | docs/user-guide/bills.md → purchases.bills (payments recorded by hand: recording, part payments, foreign bills, voids; rev 0.77)

## [2026-10-23] compile | docs/user-guide/periods-and-close.md → accounting.periods (the close's AP sub-ledger check, INV-07; rev 0.78)

## [2026-10-23] compile | docs/user-guide/bills.md → purchases.bills (what the company owes: the next 30 days' cash and the AP ageing; rev 0.79)

## [2026-10-23] compile | docs/user-guide/bills.md → purchases.bills (credit notes: entering, applying to bills, refunds; rev 0.82)

## [2026-10-23] compile | docs/user-guide/bills.md → purchases.bills (spreading a prepaid cost over months, LEDGER-28; rev 0.83)

## [2026-10-23] compile | docs/user-guide/bills.md → purchases.bills (recurring bills; rev 0.85)

## [2026-10-23] compile | docs/user-guide/bills.md → purchases.bills (attaching the supplier's invoice; rev 0.86)

## [2026-10-23] compile | docs/user-guide/purchase-orders.md → purchases.orders (new: purchase orders; rev 0.88)

## [2026-10-23] compile | docs/user-guide/bills.md → purchases.bills (links purchase orders; rev 0.88)

## [2026-10-23] compile | docs/user-guide/bills.md → purchases.bills (input tax: the facts, business share, simplified-invoice limit, agent invoices, the tax agent's decision; FR-AP-20, rev 0.89)

## [2026-10-23] compile | docs/user-guide/chart-of-accounts.md → accounting.chart (the standard chart has 87 accounts: 2330 Withholding tax payable, FR-AP-19, rev 0.90)

## [2026-10-23] compile | docs/user-guide/bills.md, contacts-and-items.md → purchases.bills, contacts.manage (non-resident suppliers, the nature of payment, the tax agent's withholding decision, a contact's tax residence; FR-AP-19, rev 0.91)

## [2026-10-23] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (Bill reminders: purchases.reminder_days; FR-AP-13, rev 0.93)

## [2026-10-23] compile | docs/user-guide/bills.md, contacts-and-items.md → purchases.bills, contacts.manage (plan and track payments: the board, the calendar, planned dates, early-payment discounts, Bills to pay; FR-AP-13, rev 0.94)

## [2026-10-23] compile | docs/user-guide/document-templates.md → settings.templates (new guide for the template screens, rev 0.100); settings.organisation links it

## [2026-10-23] compile | docs/user-guide/contacts-and-items.md, docs/user-guide/document-templates.md → contacts.manage, settings.templates (a contact's own templates, chosen with Templates in Contacts; FR-TPL-05, rev 0.101)

## [2026-10-23] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (Email-in: quarantined mail kept for 30 days, or until someone decides; FR-INT-22, rev 0.106)

## [2026-10-24] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the notification email details setting, rev 0.108)

## [2026-10-24] compile | docs/user-guide/notifications.md, announcements.md → settings.notifications, settings.announcements (the notification centre, preferences, banners and announcements screens, p1-templates-email 4.1)

## [2026-10-24] compile | docs/user-guide/periods-and-close.md → accounting.periods (the close checks accounts receivable against the open invoices)

## [2026-10-24] compile | docs/user-guide/periods-and-close.md → accounting.periods (the AR check counts the credits with customers)

## [2026-10-24] compile | docs/user-guide/organisation-settings.md, periods-and-close.md → settings.organisation, settings.change, accounting.periods (tolerances clear on a payment in full; the close's tolerance check)

## [2026-10-24] compile | docs/user-guide/periods-and-close.md → accounting.periods (the customer deposits check at close, FR-AR-09)

## [2026-10-24] compile | docs/user-guide/organisation-settings.md, docs/user-guide/document-templates.md → settings.organisation, settings.templates, settings.change (simplified tax invoices setting; what an invoice shows: tax invoice particulars, SGD amounts, the 30-day warning)

## [2026-10-24] compile | docs/user-guide/document-templates.md → settings.templates (emailing invoices and quotes, their delivery)

## [2026-10-24] compile | docs/user-guide/document-templates.md → settings.templates (the online link: viewing, answering a quote, expiry, revocation)

## [2026-10-25] compile | docs/user-guide/invoices.md → sales.invoices (new: the sales screens, p1-sales task 8.1)

## [2026-10-25] compile | docs/user-guide/quotes.md → sales.quotes (new: the quote screens, p1-sales task 8.1); docs/user-guide/invoices.md → sales.invoices (invoicing an accepted quote)

## [2026-10-25] compile | docs/user-guide/invoices.md → sales.invoices (recording a payment, p1-sales task 8.1)

## [2026-10-25] compile | docs/user-guide/invoices.md → sales.invoices (recurring invoices and issue and send, p1-sales task 8.1)

## [2026-10-25] compile | docs/user-guide/statements.md → sales.statements (new: the statements and reminders screen, p1-sales task 8.1)

## [2026-10-25] compile | docs/user-guide/invoices.md → sales.invoices (where reminders and statements are)

## [2026-10-25] compile | docs/user-guide/invoices.md → sales.invoices (what the customer sees through the link)

## [2026-10-25] compile | docs/user-guide/integrations.md → settings.integrations (new guide page for the connector screens, p1-connector-framework 9.3); settings.notifications links to it

## [2026-10-25] compile | docs/user-guide/notifications.md → settings.notifications (a connector paused is a security notice)

## [2026-10-25] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the bank charges account setting, p1-bank-import-reconcile task 4.4)

## [2026-10-25] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (auto-reconcile's limits, p1-bank-import-reconcile task 5.1)

## [2026-10-26] compile | docs/user-guide/organisation-settings.md, docs/user-guide/invoices.md → settings.organisation, settings.change, sales.invoices (incoming-funds alerts: likely paid, the expiry setting and dismissal; p1-bank-import-reconcile task 7.3, rev 0.145)

## [2026-10-26] compile | docs/user-guide/banking.md → banking.accounts, banking.add-account (new: the Banking accounts screen and the add-account wizard; p1-bank-import-reconcile task 10.1)

## [2026-10-26] compile | docs/user-guide/banking.md → banking.accounts, banking.add-account, banking.import (an account's statements and lines, and importing statements; p1-bank-import-reconcile task 10.2)

## [2026-10-26] compile | docs/user-guide/banking.md → banking.accounts, banking.add-account, banking.import, banking.categorise (the categorise queue; p1-bank-import-reconcile task 10.3)

## [2026-10-26] compile | docs/user-guide/banking.md → banking.rules, banking.auto-reconciled (new) and the banking pages rehashed (bank rules and the Auto-reconciled list; p1-bank-import-reconcile task 10.4)

## [2026-10-26] compile | docs/user-guide/banking.md → banking.reconcile (new) and the banking pages rehashed (reconciling an account for a month; p1-bank-import-reconcile task 10.5)

## [2026-10-26] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (mileage rates, FR-AP-16; p1-expense-claims task 4.1)

## [2026-10-26] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (claim deadline, FR-AP-15; p1-expense-claims task 5.1)

## [2026-10-26] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (advances account, FR-AP-17; p1-expense-claims task 6.1)

## [2026-10-26] compile | docs/user-guide/claims-my-claims.md → claims.make-a-claim (the receipt, evidence, submit, card-line and cash-advance screens built, p1-expense-claims task 8.1)

## [2026-10-26] compile | docs/user-guide/claims-my-claims.md → claims.make-a-claim (the mileage trip screen and the Mileage tab, p1-expense-claims task 8.1)

## [2026-10-26] compile | docs/user-guide/claims-my-claims.md → claims.make-a-claim (drop or paste a receipt; export the history)

## [2026-10-26] compile | docs/user-guide/claims-approve-team.md → claims.approve-team-claims (the approver's queue, returning with a comment, and the expense policy screen; p1-expense-claims task 8.2)

## [2026-10-27] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the disposal account and the disposal clearing account, p1-fixed-assets task 1.2)

## [2026-10-27] compile | docs/user-guide/fixed-assets.md → accounting.fixed-assets (new: the fixed asset register, p1-fixed-assets task 5.2)

## [2026-10-27] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (rev 0.151: the recommended mileage rates S$0.60 and S$0.25, and 1160 as the disposal clearing account)

## [2026-10-27] compile | docs/user-guide/chart-of-accounts.md, docs/user-guide/fixed-assets.md → accounting.chart, accounting.fixed-assets (rev 0.151: the default useful life on an asset account, offered by the register's form; 88 accounts with 1160)

## [2026-10-27] compile | docs/user-guide/fixed-assets.md → accounting.fixed-assets (rev 0.151: voiding an asset registered by mistake)

## [2026-10-27] compile | docs/user-guide/contacts-and-items.md → contacts.manage (vendors' bank details: the change, its approval, the freeze, showing the number; FR-CT-03, rev 0.153)

## [2026-10-28] compile | docs/user-guide/profile-and-security.md, docs/user-guide/approvals.md → preferences.security, approvals.decide (a person's own bank account for reimbursements and its approval, FR-AP-22)

## [2026-10-28] compile | docs/user-guide/payment-batches.md → purchases.payment-batches (payment batches: prepare, approve, release, pay, paid and remittance advice; FR-BNK-12)

## [2026-10-28] compile | docs/user-guide/profile-and-security.md → preferences.security (links the payment batches page)

## [2026-10-28] compile | docs/user-guide/banking.md → banking.categorise (a match to money already recorded covers a payment batch's release; hashes refreshed on the other banking pages)

## [2026-10-28] compile | docs/user-guide/payment-batches.md → purchases.payment-batches (Remittance not sent, when sending the advice gave up)

## [2026-10-28] compile | docs/user-guide/sign-in.md → auth.sign-in (joining a company with an invitation from the sign-in page)

## [2026-10-28] compile | docs/user-guide/periods-and-close.md → accounting.periods (the prepayments and deferred revenue close checks, rev 0.158)

## [2026-10-28] compile | docs/user-guide/schedules.md → accounting.schedules (new: the prepayment and deferral schedules, rev 0.158); accounting.periods links it

## [2026-10-28] compile | docs/user-guide/periods-and-close.md → accounting.periods (the close's warning for accounts missing from the balance sheet, p1-reports)

## [2026-10-28] compile | docs/user-guide/periods-and-close.md → accounting.periods (the close's cash flow and equity checks, p1-reports)

## [2026-10-28] compile | docs/user-guide/financial-statements.md → reports.statements (new: the statements' screens, p1-reports task 2.4); reports.general-ledger links it

## [2026-10-29] compile | docs/user-guide/financial-statements.md → reports.statements (export a statement, p1-reports task 3.1)

## [2026-10-29] compile | docs/user-guide/financial-statements.md → reports.statements (accountant exports in the background, p1-reports tasks 3.3, 3.4)

## [2026-10-29] compile | docs/user-guide/financial-statements.md → reports.statements (the export dialog: a statement's PDF beside Excel and CSV; Ask again for an expired export)

## [2026-10-29] compile | docs/user-guide/banking.md → banking.auto-reconciled (a batch's payment reconciles by itself when the line names the batch, rev 0.160); the other banking pages re-hashed, unchanged

## [2026-10-29] compile | docs/user-guide/payment-batches.md → purchases.payment-batches (Send again for a failed remittance advice; a batch's payment matched by itself, rev 0.160)

## [2026-10-29] compile | docs/user-guide/management-reports.md → reports.management (new: the management reports, who sees them); docs/user-guide/financial-statements.md → reports.statements (Find a report lists the management reports)

## [2026-10-29] compile | financial-statements.md → reports.statements, management-reports.md → reports.management (the P&L's monthly trend and the top expenses drawn as charts, p1-reports 5.3)

## [2026-10-29] compile | reports.kpis from docs/user-guide/kpis.md (new); home.overview recompiled for the pulse card, tiles and charts

## [2026-11-01] compile | checks.overview from docs/user-guide/checks.md (new: books health, the statuses, when the checks run, Run now, a check's owner); home.overview recompiled for books health on the pulse card

## [2026-11-01] compile | checks.overview from docs/user-guide/checks.md: marking an item reviewed (rev 0.164) and the checks of the setup, bank, sales, purchases and journals

## [2026-11-02] compile | checks.overview from docs/user-guide/checks.md: the checks of the assets, recurring entries, bank at the month end, closing and planning (p1-checks slice 3)

## [2026-11-02] compile | access-review.md, members-and-roles.md and checks.md (p1-checks slice 4, rev 0.165): new task pages settings.access-review and members.remove; checks.overview adds the Controls area and the two checks reviewed where their control lives; members pages re-hashed

## [2026-11-02] compile | integrations.md and checks.md (p1-checks slice 4b, rev 0.166): settings.integrations adds a secret's expiry date; checks.overview adds the payments' and the platform's checks

## [2026-11-03] compile | docs/user-guide/director-accounts.md (new), docs/user-guide/founders-money.md (Money I put in, steps) → founders.director-accounts (new), founders.balance; founders.pay-choice and founders.settle-balance re-hashed (p1-founders-money task 1.6)

## [2026-11-03] compile | docs/user-guide/claims-my-claims.md (Keep it as a loan to the company) → claims.make-a-claim (p1-founders-money task 2.1)

## [2026-11-03] compile | docs/user-guide/pre-incorporation-costs.md (new) → founders.pre-incorporation-costs (new) (p1-founders-money task 2.2)

## [2026-11-03] compile | docs/user-guide/director-accounts.md (Record a pay choice), founders-money.md (where the pay choice changes) → founders.pay-choice, founders.director-accounts; founders.balance and founders.settle-balance re-hashed (p1-founders-money task 3.1)

## [2026-11-03] compile | docs/user-guide/director-accounts.md (Record a director's fee) → founders.director-accounts; founders.pay-choice re-hashed (p1-founders-money task 3.2)

## [2026-11-03] compile | docs/user-guide/director-accounts.md (Record the exempt private company status) → founders.director-accounts; founders.pay-choice re-hashed (p1-founders-money task 3.3)

## [2026-11-03] compile | docs/user-guide/director-accounts.md (Year-end confirmations), founders-money.md (Confirm your balances at the year end) → founders.director-accounts, founders.balance; founders.pay-choice and founders.settle-balance re-hashed (p1-founders-money task 3.4)

## [2026-11-03] compile | docs/user-guide/organisation-settings.md (Founders' money) → settings.organisation; settings.change re-hashed (p1-founders-money task 4.1)

## [2026-11-03] compile | docs/user-guide/payment-batches.md, founders-money.md → purchases.payment-batches, founders.settle-balance, founders.balance, founders.pay-choice (directors repaid through payment batches, p1-founders-money 4.3)

## [2026-11-03] compile | docs/user-guide/director-accounts.md, founders-money.md → founders.director-accounts, founders.settle-balance, founders.balance, founders.pay-choice (the Settle a balance page, p1-founders-money 4.4)

## [2026-11-03] compile | docs/user-guide/director-accounts.md, founders-money.md → founders.director-accounts, founders.settle-balance, founders.balance, founders.pay-choice (converting a balance to shares, p1-founders-money 5.3)

## [2026-11-03] compile | docs/user-guide/director-accounts.md, organisation-settings.md → founders.director-accounts, founders.pay-choice, settings.organisation, settings.change (the register of share issues and a conversion's ACRA steps, p1-founders-money 5.4)

## [2026-11-03] compile | docs/user-guide/director-accounts.md → founders.director-accounts, founders.pay-choice (the related-party schedule, FR-EQ-10)

## [2026-11-04] compile | docs/user-guide/invoices.md → task/sales-invoices (the credit notes of one invoice credit at most its total together, p1-sales 3.3)

## [2026-11-04] compile | docs/user-guide/fixed-assets.md → task/accounting-fixed-assets (a disposal's GST code is always a supply code, B.6 rev 0.169)

## [2026-11-04] compile | docs/user-guide/financial-statements.md, kpis.md → task/reports-statements, task/reports-kpis (a balance sheet with an unplaced account does not balance, its movements under Other accounts; subscription accounts exclude other income and headings)

## [2026-11-05] compile | docs/user-guide/currencies.md → accounting.currencies (FX-08: an averaged rate refused where it would set a GST amount, ENG-42)

## [2026-11-05] compile | docs/user-guide/chart-of-accounts.md → accounting.chart (the Dimensions section and the values of a dimension, FR-DIM-01)

## [2026-11-06] compile | docs/user-guide/periods-and-close.md → accounting.periods (the close's check of amounts without a required dimension, C.2 #60)

## [2026-11-06] compile | docs/user-guide/journals.md, docs/user-guide/chart-of-accounts.md → accounting.journals, accounting.chart (dimension values on journal lines; an account's required dimensions)

## [2026-11-06] compile | docs/user-guide/invoices.md → sales.invoices (dimension values on an invoice's lines)

## [2026-11-06] compile | docs/user-guide/bills.md → purchases.bills (dimension values on a bill's lines)

## [2026-11-06] compile | docs/user-guide/banking.md → banking.categorise, banking.rules (dimension values on a bank line and a rule), the other banking pages re-hashed unchanged

## [2026-11-06] compile | docs/user-guide/claims-my-claims.md → claims.make-a-claim (a claim line's department or project, rev 0.173)

## [2026-11-06] compile | docs/user-guide/financial-statements.md, docs/user-guide/general-ledger.md → reports.statements, reports.general-ledger (the P&L and the general ledger by dimension)

## [2026-11-06] compile | docs/user-guide/chart-of-accounts.md → accounting.chart (merging an account into another, FR-COA-04 rev 0.174)

## [2026-11-06] compile | team-mode.md, payment-batches.md, approvals.md, organisation-settings.md → settings.team-mode, purchases.payment-batches, approvals.decide, settings.organisation, settings.change (break-glass, FM-8, PRD-001 rev 0.175)

## [2026-11-06] compile | accountant-review.md (new), periods-and-close.md → accounting.reviews (new), accounting.periods (the accountant's review, FM-7, PRD-001 rev 0.176)

## [2026-11-06] compile | approvals.md → approvals.decide (what approving would post, FR-APR-02, PRD-001 rev 0.177)

## [2026-11-06] compile | docs/user-guide/approval-policies.md → approvals.policies (FR-APR-01 rules: the first matching rule decides the steps, rev 0.178)

## [2026-11-06] compile | docs/user-guide/approval-policies.md → approvals.policies (rules decide who, the self-approval threshold whether, FM-2 rev 0.178)

## [2026-11-06] compile | docs/user-guide/banking.md, team-mode.md → banking.reconcile, settings.team-mode (SOD-5's override, SOD-6 when approving, PRD-001 rev 0.179); other banking pages rehashed

## [2026-11-06] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (sign out after inactivity, SEC-05, PRD-001 rev 0.180)

## [2026-11-06] compile | docs/user-guide/members-and-roles.md → members.remove (Owner succession, FR-ADM-09, PRD-001 rev 0.181); members.roles, members.invite rehashed


## [2026-11-06] compile | organisation-settings.md, bills.md, journals.md → settings.organisation, purchases.bills, accounting.journals, settings.change (evidence rules, FR-DOC-06, PRD-001 rev 0.182)

## [2026-11-06] compile | bills.md, journals.md → purchases.bills, accounting.journals (the NRIC flag, FR-DOC-09, PRD-001 rev 0.183)

## [2026-11-06] compile | evidence-search.md (new) → compliance.evidence (new) (evidence search and missing evidence, FR-DOC-10, PRD-001 rev 0.184)

## [2026-11-06] compile | organisation-settings.md → settings.organisation, settings.change (link to Accounting › Evidence, FR-DOC-10)

## [2026-11-06] compile | docs/user-guide/contacts-and-items.md → contacts.manage (what a contact stands at: balances, the year's totals, ageing and activity; FR-CT-02)

## [2026-11-06] compile | docs/user-guide/notifications.md → settings.notifications (a notification by row follows the role; SEC-34)

## [2026-11-06] compile | docs/user-guide/contacts-and-items.md → contacts.manage (each item's sales this financial year, FR-ITM-01, rev 0.187)

## [2026-11-06] compile | docs/user-guide/evidence-search.md, journals.md → compliance.evidence, accounting.journals (open or save an evidence file, FR-DOC-01, rev 0.188)

## [2026-11-06] compile | docs/user-guide/journals.md → accounting.journals (a manual journal to a control account shows a warning, INV-07, question 55, rev 0.189)

## [2026-11-06] compile | docs/user-guide/search.md, home.md → shell.search (new), home.overview (the global search, FR-UX-02, rev 0.190)

## [2026-11-06] compile | docs/user-guide/invoices.md → sales.invoices (sent again: the copy kept as issued, FR-DOC-04, rev 0.191)

## [2026-11-06] compile | docs/user-guide/approvals.md → approvals.decide (the Inbox's keys, FR-UX-03, rev 0.192)

## [2026-11-06] compile | docs/user-guide/home.md → home.overview (copy and paste amounts, FR-UX-20, rev 0.193)

## [2026-11-06] compile | docs/user-guide/profile-and-security.md → preferences.security (link or remove a sign-in, FR-ADM-08, PRD-001 rev 0.196)

## [2026-11-07] compile | sign-in-providers.md (new), sign-in.md, profile-and-security.md → settings.sign-in-providers (new), auth.sign-in, preferences.security (provider switches and allowed domains, FR-ADM-08, PRD-001 rev 0.197)

## [2026-11-07] compile | members-and-roles.md, sign-in.md, profile-and-security.md → members.remove, auth.sign-in, preferences.security (account recovery, SEC-35, PRD-001 rev 0.198); members.invite, members.roles rehashed

## [2026-11-07] compile | sign-in.md, members-and-roles.md, sign-in-providers.md → auth.sign-in, members.invite, settings.sign-in-providers (joining by email domain, FR-ADM-08, PRD-001 rev 0.199); members.remove, members.roles rehashed

## [2026-11-07] compile | logo-and-brand.md (new) → settings.brand (new) (logo, icon, SVG cleaning and accent colour, FR-ORG-09, PRD-001 rev 0.200)

## [2026-11-07] compile | docs/user-guide/logo-and-brand.md → settings.brand (the logo drawn for documents, the brand in force when a document was issued, BRAND-04; rev 0.201)

## [2026-11-07] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (GST registration and document numbering on Settings › Organisation; FR-ORG-03, FR-ORG-04)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (new: Equity › Holders and Classes, FR-EQ-15/16); docs/user-guide/contacts-and-items.md → contacts.manage (the investor mark)

## [2026-11-07] compile | docs/user-guide/director-accounts.md, equity-register.md → founders.director-accounts, founders.pay-choice, equity.register (the register of share issues moves to the equity register)

## [2026-11-07] compile | docs/user-guide/chart-of-accounts.md → accounting.chart (the standard chart has 89 accounts: 2040 Share application monies, rev 0.205)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (equity transactions: drafting, ACRA steps, the effective date, reversals, money for shares from the bank; FR-EQ-17)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (the cap table: who owns the company at any day, compare two days, the drill-down to each movement, the export; FR-EQ-20)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (SAFE and note conversions at a priced round, slice 4b-i)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (the SAFEs and notes page, recording one, templates, the conversion preview; slice 4c)

## [2026-11-07] compile | docs/user-guide/director-accounts.md, docs/user-guide/equity-register.md → founders.director-accounts, founders.pay-choice (hash), equity.register (a founder's balance into a SAFE or note; C.2 #29; slice 4d)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (the option pool and grants: resolutions, grants, vesting, own grants; EQUITY-16, EQUITY-17)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (fully diluted counts the options and the unallocated pool; slice 5a-ii)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (a grant's forfeitures and expiries; EQUITY-18; slice 5b-i)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (an exercise of options as an issue for cash; slice 5b-ii)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (Equity › Option pool and My shares and options; slice 5c)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (the round simulator: model a round, save it, send it for approval)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (investor rights, registrable controllers and the exempt status re-test)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (reconcile the register to ACRA's business profile)

## [2026-11-07] compile | docs/user-guide/equity-register.md → equity.register (a holder's investments)

## [2026-11-07] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the GST registration monitor and its warning setting, p1-gst slice 2)

## [2026-11-07] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the statutory calendar's settings and the evidence rule for filings, p1-gst slice 3a)

## [2026-11-07] compile | docs/user-guide/statutory-calendar.md, docs/user-guide/gst-registration.md → compliance.calendar, compliance.record-filing, compliance.gst, compliance.gst-forecast (new guide pages, p1-gst slice 4); settings.organisation links them

## [2026-11-08] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the Cash buffer setting, planning.buffer_months; p1-planning slice 3)

## [2026-11-08] compile | docs/user-guide/home.md → home.overview (Home's tiles from the plans: cash runs out, revenue and costs against budget; p1-planning slice 5)

## [2026-11-08] compile | docs/user-guide/financial-statements.md → reports.statements (the P&L's budget and forecast comparatives; p1-planning slice 5)

## [2026-11-08] compile | docs/user-guide/periods-and-close.md, docs/user-guide/organisation-settings.md → accounting.periods, settings.organisation, settings.change (the close's budget variances; p1-planning slice 5)

## [2026-11-08] compile | docs/user-guide/notifications.md → settings.notifications (the statutory deadlines and the cash buffer alert among the notifications; p1-planning slice 6)

## [2026-11-08] compile | docs/user-guide/cash-flow.md, docs/user-guide/home.md → planning.cash (new), home.overview (Cash ahead and its Home tile; p1-planning slice 8)

## [2026-11-08] compile | docs/user-guide/feasibility.md → planning.feasibility (new; p1-planning slice 8)

## [2026-11-08] compile | docs/user-guide/cash-flow.md → planning.cash (links the feasibility studies)

## [2026-11-08] compile | docs/user-guide/budgets.md → planning.budgets (new; p1-planning slice 8)

## [2026-11-08] edit | reports.statements links Budget vs Actual's page (planning.budgets)

## [2026-11-08] compile | docs/user-guide/forecast.md → planning.forecast (new; p1-planning slice 8); home.overview links it

## [2026-11-09] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the purge switch and the security events' 400 days, PRD-001 rev 0.230; Records kept for corrected)

## [2026-11-09] compile | docs/user-guide/audit-trail.md → compliance.audit-trail (new: Compliance › Audit trail, p1-audit-explorer slice 11)

## [2026-11-09] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (each purge is listed on the audit trail; links compliance.audit-trail)

## [2026-11-09] compile | docs/user-guide/compliance-controls.md → compliance.controls (new: Compliance › Controls, p1-compliance-center slice 5)

## [2026-11-09] compile | docs/user-guide/checks.md → checks.overview (links the nightly verification to Compliance › Controls)

## [2026-11-09] compile | docs/user-guide/compliance-risks-policies.md → compliance.risks (new: the risk register, p1-compliance-center slice 6)

## [2026-11-09] compile | docs/user-guide/compliance-controls.md → compliance.controls (links the risk register)

## [2026-11-09] compile | docs/user-guide/compliance-policies.md (new), compliance-risks-policies.md → compliance.policies (new), compliance.risks (the policy library, CMP-07)

## [2026-11-09] compile | docs/user-guide/compliance-reviews.md (new), compliance-policies.md → compliance.reviews (new), compliance.policies (the recurring reviews, CMP-03)

## [2026-11-09] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the Data Protection Officer, CMP-PD-01)

## [2026-11-09] compile | docs/user-guide/compliance-privacy-notices.md (new), compliance-policies.md → compliance.privacy-notices (new), compliance.policies (the privacy notices and consent, CMP-PD-03)

## [2026-11-09] compile | docs/user-guide/compliance-data-protection.md (new), compliance-reviews.md → compliance.data-protection (new), compliance.reviews (the inventory and its quarterly review, CMP-PD-02, CMP-05)

## [2026-11-09] compile | docs/user-guide/compliance-data-protection.md → compliance.data-protection (impact assessments, CMP-PD-08)

## [2026-11-09] compile | docs/user-guide/compliance-data-protection.md, profile-and-security.md → compliance.data-protection, preferences.security (access and correction requests, CMP-PD-04)

## [2026-11-09] compile | docs/user-guide/compliance-reviews.md → compliance.reviews (the impact assessments' and requests' reviews)

## [2026-11-09] compile | docs/user-guide/compliance-data-protection.md, compliance-reviews.md → compliance.data-protection, compliance.reviews (breach register, PDPA timers and tabletop exercises, CMP-PD-05, CTL-DP-06)

## [2026-11-09] compile | docs/user-guide/compliance-data-protection.md, compliance-reviews.md → compliance.data-protection, compliance.reviews (overseas transfers, CMP-PD-06, CTL-DP-07)

## [2026-11-10] compile | docs/user-guide/contacts-and-items.md, audit-trail.md → contacts.manage, compliance.audit-trail (a person or a company; sealed and shredded personal values, §11.8)

## [2026-11-10] compile | docs/user-guide/compliance-data-protection.md → compliance.data-protection (shredding at the end of retention and anonymisation, §11.8, §11.9)

## [2026-11-11] compile | docs/user-guide/home.md → home.overview (REL-05: Help, About Fyniq, the version and commit in every export's footer)

## [2026-11-11] compile | docs/user-guide/compliance-controls.md → compliance.controls (SEC-32: the incident register on the Incidents tab)

## [2026-11-11] compile | docs/user-guide/compliance-controls.md → compliance.controls (the restore drill register, DR-06)

## [2026-11-11] compile | docs/user-guide/compliance-controls.md → compliance.controls (the heartbeat drill register, NFR-OBS-02)

## [2026-11-11] compile | docs/user-guide/home.md → home.overview (Follow long work: the operations tray, p1-operations-dr task 11.2)

## [2026-11-11] compile | docs/user-guide/home.md → home.overview (Follow long work: on a phone, the notification and Reports)

## [2026-11-11] compile | docs/user-guide/general-ledger.md, docs/user-guide/financial-statements.md → reports.general-ledger, reports.statements (the general ledger export has no limit of lines, NFR-PERF-05)

## [2026-11-11] compile | docs/user-guide/checks.md → checks.books-health (the checks run at most once a minute through a burst of changes)

## [2026-11-11] compile | docs/user-guide/forecast.md → planning.forecast (PRD-001 rev 0.247: the results first, a recorded forecast's plan folded)

## [2026-11-12] compile | docs/user-guide/financial-statements.md → reports.statements (the Trace panel: a figure's breakdown, ledger lines as at the statement's revision, source and history; FR-UX-04)

## [2026-11-12] compile | organisation-settings.md, members-and-roles.md, sign-in.md → settings.organisation, sign-in, members (the hold after an account recovery, 24 to 72 hours; SEC-35 rev 0.253)

## [2026-11-13] compile | docs/user-guide/chart-of-accounts.md → task/accounting-chart (the standard chart has 90 accounts: 3900 Opening balance clearing, rev 0.257)

## [2026-11-13] compile | docs/user-guide/data-export.md → settings.data (new: Settings › Data and export, the full tenant export, FR-MIG-04)

## [2026-11-13] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the Data region and retention section names Settings › Data and export)

## [2026-11-13] compile | docs/user-guide/workbook-import.md → settings.import (new: Settings › Import, M1 and M2 and the generic import); docs/user-guide/data-export.md → settings.data (links Settings › Import)

## [2026-11-13] compile | docs/user-guide/workbook-import.md → settings.import (M3's commit and M4's register and sign-off)

## [2026-11-13] compile | docs/user-guide/workbook-import.md → settings.import (M5's parallel month and M6's archive and go-live)

## [2026-11-14] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the AI assistant switches off at once, AI-13)

## [2026-11-14] compile | docs/user-guide/organisation-settings.md, docs/user-guide/checks.md → settings.organisation, settings.change, checks.overview (AI routes: the fallbacks threshold and C.2 #55)

## [2026-11-14] compile | docs/user-guide/compliance-data-protection.md → compliance.data-protection (the AI inventory, CMP-AI-02)

## [2026-11-14] compile | docs/user-guide/ai-settings.md → settings.ai (new page: the AI switch, budgets and usage, AI-13, AI-77)

## [2026-11-14] compile | docs/user-guide/ai-providers.md → settings.ai-providers (new page: the AI providers, AI-71…AI-75)

## [2026-11-14] compile | docs/user-guide/ai-providers.md → settings.ai-providers (a connection's own page: turn it on or off, replace the key, its models, qualify a model, where its prompts go)

## [2026-11-14] compile | docs/user-guide/ai-routes.md → settings.ai-routes (new page: model routes and the route editor, AI-74, AI-76, AI-26)

## [2026-11-14] compile | docs/user-guide/ai-activity.md → settings.ai-activity (new page: the AI activity log and your own AI plan, AI-12, AI-73)

## [2026-11-14] compile | docs/user-guide/ai-providers.md → settings.ai-providers (a list not read whole for 14 days carries no personal data or payroll, AI-84)

## [2026-11-14] compile | docs/user-guide/ai-providers.md → settings.ai-providers (models that draw a plan's quota at another rate, AI-73)

## [2026-11-15] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (AI and personal data: the never-send list, AI payroll access and the fields the AI never reads; p1-ai-assistant task 4.1)

## [2026-11-15] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (AI limits: questions a minute and a day, reads per question, questions per conversation; p1-ai-assistant task 5.3)

## [2026-11-15] compile | docs/user-guide/ai-activity.md → settings.ai-activity (questions to the assistant in the AI activity log; p1-ai-assistant task 6.2)

## [2026-11-15] compile | docs/user-guide/ai-activity.md → settings.ai-activity (feedback on the assistant's answers and its review, QA-AI-05; p1-ai-assistant task 6.4)

## [2026-11-15] compile | docs/user-guide/ai-assistant.md → ai.assistant (the assistant panel; p1-ai-assistant task 7.1), linked from settings.ai-activity

## [2026-11-15] compile | docs/user-guide/appearance.md, docs/user-guide/home.md → preferences.appearance, home.find-your-way (on a phone, Appearance, Help and Sign out are under More)

## [2026-11-15] compile | docs/user-guide/ai-assistant.md → ai.assistant (Ask about this, Explain, Simulate, rating an answer; p1-ai-assistant task 7.2)

## [2026-11-15] compile | docs/user-guide/ai-settings.md, docs/user-guide/ai-activity.md → settings.ai, settings.ai-activity (the assistant's monitoring, QA-AI-04; p1-ai-assistant task 8.4)

## [2026-11-15] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the AI task types setting, ai.task_types: off at once when narrowed)

## [2026-11-15] compile | docs/user-guide/ai-proposals.md → ai.proposals (reviewing and deciding an AI proposal: the summary apart from the AI's reasons, items, rejection reasons, expiry)

## [2026-11-15] compile | docs/user-guide/ai-proposals.md → ai.proposals (the AI tasks page: its proposal history, catalogue and never-list; turning a task type on or off)

## [2026-11-15] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the company secretary assistant's settings)

## [2026-11-15] compile | docs/user-guide/ai-proposals.md → ai.proposals (the compliance task the AI may propose, sec.create_task)

## [2026-11-15] compile | docs/user-guide/ai-proposals.md → ai.proposals (the reminder the AI may draft, sec.draft_reminder)

## [2026-11-15] compile | docs/user-guide/company-secretary.md → compliance.secretary (the company secretary assistant's workspace)

## [2026-11-15] compile | docs/user-guide/company-secretary.md → compliance.secretary (the tasks in the Inbox)

## [2026-11-16] compile | docs/user-guide/corporate-profile.md → compliance.corporate-profile (p1-company-secretary task 6.1: the corporate profile and registers screen)

## [2026-11-16] compile | docs/user-guide/equity-register.md → equity.register (the extract's officers and registered office, p1-company-secretary tasks 2.5 and 6.1)

## [2026-11-16] compile | docs/user-guide/company-secretary.md → compliance.secretary (a run without its AI step, rev 0.283)

## [2026-11-16] compile | docs/user-guide/corporate-profile.md → compliance.corporate-profile (drafting resolutions, notices and minutes from Fyniq's templates, FR-CS-05)

## [2026-11-16] compile | docs/user-guide/corporate-profile.md → compliance.corporate-profile (the signed copy and the minute book, FR-CS-05)

## [2026-11-16] compile | docs/user-guide/ai-proposals.md → ai.proposals-approve (sec.draft_document, FR-CS-05)

## [2026-11-17] compile | docs/user-guide/evidence-search.md → compliance.evidence (files open from the search, only my files, show more, filings and GST forecasts searched, a filing missing its acknowledgement)

## [2026-11-17] compile | docs/user-guide/journals.md, docs/user-guide/bills.md → accounting.journals, purchases.bills (a HEIC photo is not accepted yet: export it as JPEG)

## [2026-11-17] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the organisation's motion switch, FR-UX-38)

## [2026-11-17] compile | docs/user-guide/statutory-calendar.md → compliance.record-filing, compliance.calendar (the Company secretary records the corporate items, PRD-001 rev 0.285)

## [2026-11-17] compile | docs/user-guide/home.md → home.overview (Your work by role and Upcoming deadlines on Home, §7.1)

## [2026-11-17] compile | docs/user-guide/keyboard-shortcuts.md → shell.shortcuts (new page: keyboard-first entry, FR-UX-06)

## [2026-11-17] compile | docs/user-guide/profile-and-security.md → preferences.security (its Keyboard section, linking shell.shortcuts)

## [2026-11-17] compile | docs/user-guide/checks.md → checks.overview (run a stopped background task again, C.2 #12, G4-073)

## [2026-11-17] compile | docs/user-guide/help.md → help.guide (the user guide in the app, FR-UX-32)

## [2026-11-17] compile | docs/user-guide/home.md → home.overview (Help opens the user guide)

## [2026-11-17] compile | docs/user-guide/invoices.md, bills.md, claims-my-claims.md → sales.invoices, purchases.bills, claims.my-claims (Has advanced details in Basic mode, FR-UX-25)

## [2026-11-17] compile | docs/user-guide/banking.md → banking.categorise (Plain and Accountant views on the grid, FR-UX-08); the other pages citing it re-hashed, unchanged

## [2026-11-18] compile | docs/user-guide/glossary.md → reference.glossary (new page: the glossary the terms on pages read, FR-UX-11, FR-UX-30)

## [2026-11-18] compile | docs/user-guide/home.md → home.overview (What happened? Where to record it, and empty lists' first actions, FR-UX-11)

## [2026-11-18] compile | docs/user-guide/evidence-search.md → compliance.evidence (how long files are kept, FR-DOC-02, G4-068)

## [2026-11-18] compile | docs/user-guide/statutory-packs.md → settings.statutory-packs (an update waiting to be activated, ENG-33, G4-103)

## [2026-11-18] compile | docs/user-guide/help.md → help.guide (Take the tour, FR-UX-34)

## [2026-11-18] compile | docs/user-guide/home.md → home.overview (the Administrator's, Company secretary's and DPO's work on their landing pages, the Auditor's engagement; QA-UI-08)

## [2026-11-18] compile | docs/user-guide/approvals.md → approvals.decide (FM-6: unusual items in the weekly review, rev 0.287)

## [2026-11-18] compile | settings.organisation: the Personal access tokens setting (security.personal_access_tokens, off until an Owner turns it on with a second approver; FR-INT-02)

## [2026-11-18] compile | settings.change: personal access tokens join the settings that need a second approver

## [2026-11-18] compile | docs/user-guide/bills.md, journals.md → purchases.bills, accounting.journals (a HEIC photo is converted where the installation has a converter)

## [2026-11-18] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the largest evidence file setting)

## [2026-11-18] compile | docs/user-guide/journals.md → accounting.journals (the Evidence panel's date it relates to, paste, drop and details)

## [2026-11-18] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (the Evidence panel's Rules for this activity)

## [2026-11-18] compile | docs/user-guide/home.md → home.overview (the Approver's requests waiting on Home; QA-UI-08)

## [2026-11-18] compile | docs/user-guide/banking.md and payment-batches.md → banking.accounts, banking.add-account, purchases.payment-batches (the money account's Payment file and the download label naming it, FR-BNK-12; the other banking pages re-hashed, unchanged)

## [2026-11-18] compile | docs/user-guide/ai-activity.md -> settings.ai-activity (an answer's know-how page versions in the activity log, AI-12, AI-51)

## [2026-11-18] compile | docs/user-guide/ai-assistant.md -> ai.assistant (ask how something works: know-how answers with chips to guide pages, AI-51, AI-53)

## [2026-11-18] edit | added aliases for users' own wording to 17 pages after the QA-AI-06 retrieval baseline (a retrieval miss is fixed in the compile step, AI-58)

## [2026-11-19] compile | organisation-settings: AI apps through MCP and AI app limits (p1-mcp-first-party 2.1)

## [2026-11-19] compile | organisation-settings: AI apps may register themselves (p1-mcp-first-party 11.1)

## [2026-11-19] compile | connected-apps: the Connected apps guide page (p1-mcp-first-party 9.4)

## [2026-11-19] compile | connected-apps: when an app asks to connect (p1-mcp-first-party 9.3)

## [2026-11-19] compile | connected-apps: changes an app proposes (p1-mcp-first-party 7.x)

## [2026-11-19] compile | docs/user-guide/knowledge-base.md, docs/user-guide/ai-settings.md → settings.ai-knowledge-base, settings.ai (the knowledge-base administration page and its link)

## [2026-11-19] compile | docs/user-guide/projects-projects.md → projects.projects (the projects screens, p1-projects slice 1)

## [2026-11-19] compile | docs/user-guide/knowledge-base.md → settings.ai-knowledge-base (an overlay is flagged when Fyniq changes the page it replaces)

## [2026-11-19] compile | docs/user-guide/projects-time.md → projects.time (time entry, timer and approval, p1-projects slice 2)

## [2026-11-19] compile | docs/user-guide/projects-rates.md → projects.rates (rates and time at cost, p1-projects slice 3)

## [2026-11-19] compile | every page → kb-schema@2 (the optional packs field; content unchanged); compliance.gst declares sg-gst@2026.3 and compliance.calendar sg-deadlines@2026.1 (AI-53)

## [2026-11-19] compile | docs/user-guide/integrations.md → settings.integrations (Stripe and HitPay as read-only payment providers: set up, what arrives, what posts on Reconcile now, a lost dispute, the checks; p1-payment-providers, PRD rev 0.304)

## [2026-11-19] compile | docs/user-guide/organisation-settings.md, integrations.md → settings.organisation (lost disputes: the expense limit setting), settings.integrations (a lost dispute's draft journal, resolving a set-aside event; p1-payment-providers, PRD rev 0.305)

## [2026-11-19] compile | kb/pages/task/settings-change.md re-hashed to the guide's new section (lost disputes); the page's content is unchanged

## [2026-11-19] compile | docs/user-guide/banking.md → banking.payouts (the Payouts screen and the credit-note route for a lost dispute; p1-payment-providers task 4.5, 4.6), the other banking pages re-hashed; docs/user-guide/integrations.md → settings.integrations (the credit-note route)

## [2026-11-19] compile | docs/user-guide/nemura/sign-in.md, home.md → nemura.sign-in, nemura.home (the sign-in page and the landing, p1-nemura-platform slice 7a)

## [2026-11-19] compile | docs/user-guide/nemura/sign-in.md → nemura.sign-in (joining with an invitation or the setup code)

## [2026-11-19] compile | docs/user-guide/nemura/sign-in.md → nemura.sign-in (the Owner recovery code)

## [2026-11-19] compile | docs/user-guide/nemura/people.md, person.md → nemura.people, nemura.person (the directory and a person's record, p1-nemura-records-packs slice 1)

## [2026-11-19] compile | docs/user-guide/nemura/features.md → nemura.features (the feature switches, p1-nemura-records-packs slice 3)

## [2026-11-19] compile | docs/user-guide/nemura/people.md → nemura.people (Add a person)

## [2026-11-19] compile | docs/user-guide/nemura/packs.md → nemura.packs (the country pack and leave policy, p1-nemura-records-packs slice 4)

## [2026-11-20] compile | organisation-settings.md, profile-and-security.md, keyboard-shortcuts.md, appearance.md -> settings.organisation, settings.change, preferences.security, shell.shortcuts, preferences.appearance (preferences kept on the server, the starting mode per role, number and date formats, Alt+Shift+M)

## [2026-11-20] compile | accounting.chart: the Balance column from the trial balance (p1-organisation-chart 5.2)

## [2026-11-20] compile | settings.brand: the logo on PDF reports (p1-organisation-chart 2.4)

## [2026-11-20] compile | home.md -> home.overview (the sidebar's counts, announced in words through one polite status region)

## [2026-11-20] compile | claims.my-claims: the receipt forwarded to the claimant's own address (p1-expense-claims 8.1)

## [2026-11-20] compile | nemura.my-leave from docs/user-guide/nemura/my-leave.md; nemura.home relinked (p1-nemura-leave-time slice 1)

## [2026-11-20] compile | compliance.evidence: the PDF preview of an evidence file (p1-documents-evidence 1.3, 4.2)

## [2026-11-20] compile | leave.apply recompiled, leave.approve new (p1-nemura-leave-time slice 2.1, 2.2)

## [2026-11-20] compile | settings.ai-knowledge-base recompiled: Recompile and the weekly check (p1-knowledge-base 6.3, 8.1, 8.2)

## [2026-11-20] compile | leave.report-sick recompiled (p1-nemura-leave-time 2.4)

## [2026-11-20] compile | leave.approve and nemura.my-leave: the month at a glance, certificates to verify (p1-nemura-leave-time 2.3, 2.4)

## [2026-11-20] compile | nemura.leave.government-paid (p1-nemura-leave-time 3.1, 3.2)

## [2026-11-20] compile | nemura.my-leave links nemura.leave.government-paid

## [2026-11-20] compile | home.md -> home.overview (the Auditor's engagement opens the trial balance, general ledger and access list)

## [2026-11-20] compile | projects.reports recompiled: a project's profitability card, the CSV and Excel export, project metrics (p1-projects 4.2, 4.4)

## [2026-11-20] compile | nemura.requests new; leave.approve: flexible work replies (p1-nemura-leave-time 3.3)

## [2026-11-20] compile | appearance.md -> preferences.appearance (Expert starts Compact)

## [2026-11-20] compile | ai.proposals.approve and projects.billing recompiled: the AI's draft of a project invoice, ar.draft_invoice (p1-projects)

## [2026-11-20] compile | sign-in.md -> auth.sign-in (set up how you confirm it's you: the enrolment page)

## [2026-11-20] compile | home.md -> home.overview (favourites and saved views)

## [2026-11-20] compile | nemura requests, leave.approve, nemura.my-leave, nemura.home: hours worked, pay inputs, accrual, encashment, people tiles

## [2026-11-20] compile | members-and-roles.md -> settings.members (an Approver's department scope)

## [2026-11-20] compile | installation-console.md -> settings.console (new page: platform health and the Super admins)

## [2026-11-20] compile | sign-in-providers.md -> settings.sign-in-providers (links the installation console)

## [2026-11-20] compile | members-and-roles.md -> settings.members (last sign-in and passkeys)

## [2026-11-20] compile | nemura.letters.request, nemura.letters.issue, nemura.letters.templates new (p1-nemura-self-service-letters 1.6)

## [2026-11-20] compile | docs/user-guide/nemura/letters-kets.md, person.md -> nemura.letters.kets, nemura.person (key employment terms; p1-nemura-self-service-letters slice 2)

## [2026-11-20] compile | settings.connected-apps recompiled: the Owner's view, last use and API clients (p1-mcp-first-party 3.3, 9.1, 9.2)

## [2026-11-20] compile | docs/user-guide/nemura/my-details.md, ask-hr.md, requests-queue.md, requests.md -> nemura.my-details, nemura.ask-hr, nemura.requests-queue, nemura.requests (p1-nemura-self-service-letters slice 3a)

## [2026-11-20] compile | docs/user-guide/nemura/leaver-account.md, home.md -> nemura.leaver-account, nemura.home (p1-nemura-self-service-letters 3.1, 3.4)

## [2026-11-20] compile | docs/user-guide/nemura/letters-templates.md -> nemura.letters.templates (automatic issue; p1-nemura-self-service-letters 4.1)

## [2026-11-20] compile | docs/user-guide/nemura/onboarding.md, leaver-account.md -> nemura.onboarding, nemura.leaver-account (p1-nemura-self-service-letters 4.2)

## [2026-11-20] compile | ai.proposals: the guide's section on ap.draft_bill and exp.draft_claim

## [2026-11-20] compile | docs/user-guide/nemura/payroll-records.md -> nemura.payroll-records (p1-nemura-payroll 2.1, 2.3)

## [2026-11-20] compile | documents.inbox: the document inbox guide

## [2026-11-20] compile | docs/user-guide/nemura/payroll-records.md -> nemura.payroll-records (pay runs; p1-nemura-payroll 3.1, 3.2)

## [2026-11-20] compile | docs/user-guide/nemura/payroll-approval.md, payroll-records.md -> nemura.payroll-approval, nemura.payroll-records (p1-nemura-payroll 3.3, 3.4)

## [2026-11-20] compile | docs/user-guide/nemura/my-pay.md, payroll-approval.md -> nemura.my-pay, nemura.payroll-approval (p1-nemura-payroll 4.1, 4.2)

## [2026-11-20] compile | docs/user-guide/nemura/payroll-approval.md -> nemura.payroll-approval (payment states, p1-nemura-payroll 5.2)

## [2026-11-20] compile | members-and-roles.md -> settings.members (the department-scoped P&L)

## [2026-11-20] compile | documents.inbox: files showing an NRIC in full are not read

## [2026-11-20] compile | docs/user-guide/nemura/payroll-final-pay.md -> nemura.payroll-final-pay (p1-nemura-payroll 5.1)

## [2026-11-20] compile | payroll-approval.md -> nemura.payroll-approval link to final pay

## [2026-11-20] compile | payroll-approval.md -> nemura.payroll-approval (hand-off files, p1-nemura-payroll 6.1)

## [2026-11-20] compile | docs/user-guide/nemura/assistant.md -> nemura.assistant; letters-issue link (p1-nemura-assistant-external 1.2)

## [2026-11-20] compile | documents.inbox: masked text, photos not read

## [2026-11-20] compile | ai.proposals: approval quality

## [2026-11-20] compile | members.roles, members.invite, members.remove: an Approver narrowed to departments, budgets, records and invitations

## [2026-11-20] compile | records.comments: comments and mentions on a record

## [2026-11-20] compile | docs/user-guide/nemura/ask-assistant.md -> nemura.ask-assistant (p1-nemura-assistant-external 2.1, 2.2)

## [2026-11-20] compile | my-pay.md link to the assistant

## [2026-11-20] compile | assistant.md -> nemura.assistant (drafts, 3.1)

## [2026-11-20] compile | my-pay.md -> nemura.my-pay (next pay day)

## [2026-11-20] compile | payroll-records.md -> nemura.payroll-records (election kind)

## [2026-11-20] compile | shell.shortcuts: working in a grid

## [2026-11-20] compile | members.invite, records.comments, accounting.journals: invitation email, grid, record tabs

## [2026-11-20] compile | payroll-approval.md -> nemura.payroll-approval (approval with proof when linked, p1-nemura-fyniq-link 3.1)

## [2026-11-20] compile | planning.budgets: entering lines in a grid

## [2026-11-20] compile | docs/user-guide/nemura/fyniq-link.md -> nemura.fyniq-link (link page, health, repair and restore; p1-nemura-fyniq-link 5.1-5.3)

## [2026-11-20] compile | payroll-approval.md -> nemura.payroll-approval (link to the Fyniq link page)

## [2026-11-20] compile | docs/user-guide/nemura/fyniq-link.md -> nemura.fyniq-link (the Finance | People switch, p1-nemura-fyniq-link 4.3)

## [2026-11-20] compile | payroll.salary-payments: the salary batch (p1-payroll-link task 8.1)

## [2026-11-20] compile | settings.people: People and HR, the go-live gate and a provider's journal (p1-payroll-link task 8.1)

## [2026-11-20] compile | settings.practice: practice organisation

## [2026-11-20] compile | payroll-records.md, my-pay.md -> kb (part month in working days)

## [2026-11-20] compile | settings.organisation: InvoiceNow settings (p2-invoicenow)

## [2026-11-20] compile | settings-people.md, nemura/sign-in.md, nemura/payroll-approval.md -> kb (one sign-in: the switch rule, the provider, the passkey reminder and warning)

## [2026-11-20] compile | sales.invoices: a payment link on an invoice (FR-AR-13, FR-PSP-02)

## [2026-11-20] compile | ai.proposals: contact change (ct.update_contact) and undo sections

## [2026-11-20] compile | ai.proposals: recurring journal and budget draft sections

## [2026-11-20] compile | nemura.payroll-approval: the CPF EZPay file (p2-payroll-outputs)

## [2026-11-20] compile | contacts.manage and sales.invoices: the customer portal (FR-AR-14)

## [2026-11-20] compile | ai.proposals: bank rule section

## [2026-11-20] compile | reports.builder: the report builder's guide compiled into its task page

## [2026-11-20] compile | reports.board-pack, equity.updates, equity.board-portal: the board pack, investor update and portal guides compiled

## [2026-11-20] compile | reports.dashboards: the configurable dashboard and spend alerts guide compiled

## [2026-11-20] compile | ai-proposals: approval quality now says edited and undone approvals, per task type and approver (AI-33)

## [2026-11-20] compile | nemura/sign-in.md -> kb (a Nemura sign-out or removal ends the mapped person's Fyniq sessions)

## [2026-11-20] compile | settings-people.md -> kb (link health and its repair)

## [2026-11-20] compile | settings-people.md -> kb (a repair asked for, not missing; the oldest part of a long log)

## [2026-11-20] compile | sales.invoices: a disputed invoice or a promise to pay (FR-AR-08)

## [2026-11-20] compile | sales.invoices: a disputed invoice or a promise to pay (FR-AR-08)

## [2026-11-20] compile | banking pages: the MT942 intraday report makes signals only (FR-BNK-17)

## [2026-11-20] compile | sales.invoices: the payment link's statuses and the per-line limit (FR-AR-13)

## [2026-11-20] compile | compliance.invoicenow: InvoiceNow page (p2-invoicenow)

## [2026-11-20] compile | compliance.filing-packs, sales.bad-debts: the tax working paper and the bad debt relief queue (p2-tax-filings)

## [2026-11-20] compile | sales.invoices: subscriptions a provider bills (FR-PSP-03, FR-PSP-07)

## [2026-11-20] compile | nemura/fyniq-link.md -> kb (alerts for events stuck for an hour near pay day and for 24 hours)

## [2026-11-20] compile | settings-people.md -> kb (link and unlink forms, unlinking settles work in flight)

## [2026-11-20] compile | settings.organisation: card receipt reminders (FR-AP-08)

## [2026-11-20] compile | claims.my-claims: receipt suggestions and the receipt reminder (FR-AP-08)

## [2026-11-20] compile | equity.board-portal: the Owner's grant of the full cap table

## [2026-11-20] compile | payroll-salary-payments.md -> kb (match a bank debit)

## [2026-11-20] compile | sales.invoices: subscription contracts (FR-AR-15)

## [2026-11-20] compile | nemura.payroll-approval: the IR8A file and the CPF and IR8A buttons (p2-payroll-outputs)

## [2026-11-20] compile | claims.my-claims: receipt suggestions come from the holder's own capture address (FR-AP-08)

## [2026-11-20] compile | compliance.invoicenow: the exclusion form and the contingency approval button

## [2026-11-20] compile | projects.billing, settings.organisation: revenue over time (FR-PRJ-11)

## [2026-11-20] compile | ai-proposals: the assistant proposes a know-how page, never a figure (AI-59)
