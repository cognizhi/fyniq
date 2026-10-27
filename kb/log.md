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
