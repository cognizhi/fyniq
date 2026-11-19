# Threat model: Nemura's platform

| | |
|---|---|
| Feature and requirement IDs | `p1-nemura-platform`: PRD-002 NM-ARC-01…06, NM-NFR-01…05, NM-SEC-01…05, NM-SEC-07, NM-ROLE-01…05 and the permission matrix; PRD-001 SOD-6, AUD-01, AUD-08, SEC-03, SEC-15 |
| Author, reviewer, date | Claude Code for the owner, 19 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Nemura's own database: members, roles and their history, approvals, access requests, retention reports and proof, the audit trail. Pay, NRIC or FIN, bank and medical data arrive with the records and payroll changes and are the reason this model is written first |
| Who acts | People signing in to Nemura (employees, line managers, HR, the Payroll administrator, the Owner); Nemura's services (api, worker; executor and assistant later), each as its own service account and database role; the Cloud Tasks queue; the migrator |

## 1. What we are building

Nemura is one image that runs as several services, the role chosen at start (`NEMURA_ROLE`). Each service connects to Nemura's own
PostgreSQL database (a second database on Fyniq's instance until a first paid person's pay data, the owner's decision of 19 Nov
2026) as a role of its own. Every row carries its organisation and is held by forced row-level security; every saved change writes a
hash-chained audit event in the same transaction; background work goes through an outbox and a queue. Roles come from PRD-002
Appendix B as data. A privileged role (pay or signing rights) needs an Owner's approval; a restricted field is masked unless the
role holds the right, and every read is audited. Privacy and records are an engine: a registry of data categories, access
requests with a due date, retention with a reviewed deletion and proof, and an assessment before data leaves Singapore.

Trust boundaries: browser ↔ the api (the API's contract and its authorisation matrix); the api ↔ the database (one role per
service, row-level security); the queue ↔ the worker (a class credential); Nemura ↔ Fyniq (none today: no code or table is shared,
and the link, its keys and its events are `p1-nemura-fyniq-link`); the cloud code ↔ Fyniq's project (a shared instance and a shared
project, the owner's decisions O1 and O2).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | api → database | Information disclosure: one organisation reads another's rows (a missed filter, a pooled connection that kept the last organisation) | Another company's people data | Medium |
| T2 | Services → database | Elevation: a service holds another's grants, or the migrator's, because it was started with another's environment | A service reaches what it should not (pay, approvals) | Medium |
| T3 | Roles | Elevation: an HR administrator gives themselves, or a friend, pay or signing rights, or approves their own request | Pay fraud | Medium |
| T4 | Roles | Elevation: a second "first Owner" made through the setup path | Takeover of an organisation | Low |
| T5 | api | Information disclosure: a restricted field (NRIC or FIN, bank, medical) shown to a role that may see only its shape, or read with no trace | A person's data exposed, unseen | Medium |
| T6 | api | Information disclosure: a line manager reads someone who is not their report, or after the line ended | Pay or leave of the wrong person | Medium |
| T7 | Audit trail | Tampering / repudiation: an event edited, removed or not written, or a change made with no event | A change nobody can prove | Medium |
| T8 | Audit trail | Information disclosure: a secret, token, NRIC or FIN written into an event | Credentials or identity numbers in a long-kept store | Medium |
| T9 | Queue → worker | Spoofing: a task delivered to the worker with no credential, or the executor's; a task run twice | Work done that nobody asked for, or twice | Medium |
| T10 | Retention | Tampering: records deleted before their statutory minimum, or silently, or a hold ignored; a report left for ever | Records the law requires lost; or data kept past its purpose | Medium |
| T11 | Access requests | Repudiation / denial: a request not answered within 30 days unnoticed; an answered request edited; HR handling their own request | A PDPA breach; a false record of how it was met | Medium |
| T12 | Data leaving Singapore | Information disclosure: personal data sent to a provider outside Singapore with no assessment | A transfer the PDPA does not allow | Low |
| T13 | Image and local stack | Elevation: a local adapter or the local password in a release image; Nemura's local stack reaching Fyniq's | Local shortcuts in production | Low |
| T14 | Code | Tampering: Nemura's code reaching Fyniq's ledger, or Fyniq's reaching Nemura's, so one app writes the other's facts | A broken ownership boundary (ADR-N4) | Low |
| T15 | Cloud code | Elevation: a Nemura identity holding a role on a Fyniq resource, or a person granted directly; a region outside Singapore | A cross-app reach; data out of region | Low |

SOD: nobody approves their own request, change or role; the approver of a privileged grant is an Owner who is neither the one who
asked, the one who grants nor the one who receives it (NM-ROLE-05, SOD-6). A team of one is handled with the founders' team modes
(PRD-001 §6.4) in `p1-nemura-fyniq-link`. RLS: every tenant table forces row-level security on `nemura.current_org()`; the scope
functions add the acting person and the day.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | `organisation_id` first in every key, composite foreign keys, forced row-level security, and the unit of work that sets `app.org_id` first in every transaction and refuses none | `TestRowLevelSecurityKeepsOrganisationsApart`, `TestTheUnitOfWorkNeedsAnOrganisationAndLeavesNoneBehind`, `TestTheDatabaseKnowsExactlyNemurasRoles` |
| T2 | One database role per service with grants listed in a golden file; a service refuses another role's database URL; only the migrator owns objects; `ALL` revoked from `PUBLIC` and `CONNECT` only for Nemura's roles | `TestEachRoleHoldsExactlyItsGrants`, `TestOnlyNemurasRolesConnectToNemurasDatabase`, `TestAServiceRefusesAnotherRolesDatabaseURL` |
| T3 | The database refuses a role for oneself and a privileged grant without an approved request of that very grant from an Owner who is not the asker, granter or recipient; an approval cannot be reused for another member or edited | `TestNobodyEscalatesTheirOwnRights`, `TestNobodyApprovesTheirOwnRequestAndSetupRunsOnce` |
| T4 | Only first-run setup grants with no granter, and only while the organisation has no Owner (NM080) | `TestNobodyApprovesTheirOwnRequestAndSetupRunsOnce` |
| T5 | The matrix is one table, compared with the PRD; a restricted field is masked or hidden by role and released in full only with the right or to its own person; every read, and every refusal, is audited with its level and never its value | `TestTheMatrixIsAppendixB`, `TestARestrictedFieldIsReleasedByNeedToKnowAndEveryReadIsAudited` |
| T6 | The reporting line is effective-dated history; a policy on `is_manager_of` matches only a manager's current reports, and nobody when no person is set | `TestTheLineManagerSeesOnlyTheirReportsThroughRowLevelSecurity` |
| T7 | The chain head is locked per append; events are append-only and untruncatable; an audited change with no event in its transaction is refused at commit; verification recomputes the chain and names a break even when the triggers are switched off | `TestAChangeAndItsAuditEventCommitTogether`, `TestTheChainDetectsTampering` |
| T8 | The shared masking removes values under a secret's name, masks NRIC or FIN and bank numbers, and finds tokens by shape in free text | `TestNoSecretReachesTheAuditTrail` |
| T9 | The worker accepts only its class's credential; a lease and a state machine in the database make a delivery run once; a dead row is audited | `TestATaskNeedsItsClassesCredential`, `TestAQueuedTaskSurvivesAStoppedConsumerAndRunsOnce`, `TestTheOutboxKeepsItsContract` |
| T10 | A deletion is a report for review (30 days, then overdue); a hold keeps what it names; a company period below the statutory minimum is refused; each deletion leaves a proof with a fingerprint and no content; the engine deletes only through a record type's own source | `TestADueDeletionIsReportedReviewedHeldAndProved`, `TestARetentionPeriodNeverGoesBelowTheMinimum`, `TestALegalHoldKeepsWhatItNames` |
| T11 | A due date 30 days out and a written-notice date, an overdue list for Checks, no handling of one's own request, a withheld category needs a recorded exception, an answered request and its decisions cannot change | `TestAnAccessRequestIsTrackedDecidedAnsweredAndKept`, `TestTheAnswerIsDueIn30DaysOrAWrittenNoticeIs`, `TestWithholdingNeedsARecordedException` |
| T12 | A transfer needs a current assessment for its flow and country; assessments are append-only | `TestPersonalDataLeavesSingaporeOnlyAfterAnAssessment` |
| T13 | The release flavour refuses outside staging and production and with a local adapter linked; the local password is compiled only into the local build; Nemura's compose services are in their own profile and share no volume | `TestBuildFlavoursRefuseTheWrongEnvironment` (the image check in `make images`), `TestNemuraServicesAreInThePeopleProfile` |
| T14 | An import rule: Nemura's packages use only their own code and a short shared list, and nothing outside imports them | `TestNemuraStaysInItsLane`, `TestNemuraRulesCatchPlantedViolations` |
| T15 | The module has no person-level grant and no key; its only project-level rights are the Cloud SQL client rights, each narrowed to the shared instance; the queue and the worker's invoker are Nemura's own; a region outside Singapore is refused | `make infra-check`: the module's `people.tftest.hcl` and the repository's rules |

## 4. What remains

- The sign-in, sessions, passkeys and token issuer (`p1-nemura-platform` slice 5), the web app and its pages, and the API operations
  for access requests and roles are not built yet: until they are, no person can reach any of this over the network, and this model
  is reviewed again with each.
- The cloud code is tested offline only: the database on the shared instance, the IAM database users and the group-role
  membership the migrations grant, the queue and the services `[live-verify]`. The reverse of T2 (a Nemura role opening Fyniq's
  database) is Fyniq's database setting and per-database IAM `[live-verify]`.
- The Singapore retention minimums are pack values (`p1-nemura-records-packs`); the PDPC exception for opinions kept solely for an
  evaluative purpose is `[verify the current edition with the DPO]`.
- Restoring Nemura alone from a shared instance (a copy, then one database) is a runbook and a drill at the cloud gate
  `[live-verify]` (PRD-001 DR-06).
- Fyniq's `NRIC`-naming rule (CMP-PD-07) scopes Fyniq's schema, API and events; Nemura's own rule (NM-PPL-02: encrypted, masked,
  audited) applies when its first NRIC or FIN field lands, in `p1-nemura-records-packs`.
