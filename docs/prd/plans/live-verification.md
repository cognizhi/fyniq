# Live verification in the dev cloud: the register

Everything here is built and tested offline, against local substitutes and fakes (ADR-T4, golden rule 9). What is
left is only what Google Cloud or a live provider can prove. By the owner's decision of 26 Oct 2026, such work is
completed as development first and tagged `[live-verify]` in its change's `tasks.md`; it is verified in the dev cloud
at a later stage, by `cloud-first-deployment`, the cloud gate of the P1–P3 plan (PRD-001 §17.5). Each row stays with
its change: a change whose only open tasks are tagged `[live-verify]` is complete in local mode.

When a row is verified, record the evidence (the run, the date, who) in the release record and mark the row done here
and in its change.

## P0

| Change and task | Built and tested offline | To verify in the dev cloud |
|---|---|---|
| `p0-infra-opentofu` 1.1 | The state bucket and the `dev`, `staging` and `production` roots; `make infra-check`; the plan workflow | `tofu plan` for each root in CI against the founder's organisation and projects |
| `p0-infra-opentofu` 1.2 | The Singapore resource-location policy module and its test | The organisation policy denies a resource planned outside `asia-southeast1` |
| `p0-infra-opentofu` 1.3 | Workload Identity Federation for `main` and tags; no key resource anywhere | A CI deploy through the federation, and no service-account key in the projects |
| `p0-infra-opentofu` 2.1 | Artifact Registry, Secret Manager, KMS (CMEK, anchor signing), Cloud SQL for PostgreSQL 18 with backups, PITR and CMEK | The applied configuration, read back |
| `p0-infra-opentofu` 2.2 | Cloud Run services and jobs, one account each; Cloud Tasks queues per class; the Scheduler tick | Each account's roles, listed |
| `p0-infra-opentofu` 2.3 | The retention-locked anchor bucket (locked in production only) | An attempted delete is refused; the retention period `[verify]` with the accountant |
| `p0-infra-opentofu` 2.4 | The billing budgets and their thresholds | A test notification reaches each alert channel |
| `p0-infra-opentofu` 3.1 | The nightly drift check | It fails after a manual console change in `dev` |
| `p0-infra-opentofu` 3.2 | No standing human grant; break-glass through Privileged Access Manager | The production IAM policy, read back |
| `p0-audit-chain` 2.1 | The `Signer` port (Cloud KMS and local Ed25519), the anchor job, write-once Cloud Storage, all against fakes | Anchors signed by KMS land in the locked bucket |
| `p0-audit-chain` 2.2 | `audit.VerifyAll` nightly, failing and alerting on tampering (the ✖ check arrives with `p1-checks`) | The nightly job and its alert in the cloud |
| `p0-audit-chain` 2.3 | — | §17.3 exit item 3 in staging, with its evidence in the release record |
| `p0-ci-images-release` 2.3 | SBOM, scan and cosign signature per image on every pull request, against a throwaway registry | Keyless signing with GitHub OIDC and push by digest to Artifact Registry |
| `p0-ci-images-release` 3.2 | — | Each release deployed to staging by digest, recorded (§17.3 items 4 and 5) |
| `p0-compliance-as-code` 2.1 | Every CI check but CTL-CHG-04's | Binary Authorization's daily enforcement check (CTL-CHG-04) |
| `p0-identity-oauth-first-run` 3.2 | The Zoho adapter against a fake | A live Zoho sign-in through the US data centre (§17.3 exit item 7) |
| `p0-outbox-background-work` 1.2 | `gcp.Tasks`, `gcp.TaskTokens`, `gcp.RunJobs` and the release kit, against fakes | The outbox conformance suite against Cloud Tasks |
| `p0-outbox-background-work` 3.2 | Each class's queue, invoker and audience; another class's token refused | Only the `api` account may create executor tasks (IAM) |
| `p0-outbox-background-work` 3.3 | — | Every service idles to zero in staging; only the database stays on |

## P1

| Change | Built and tested offline | To verify in the dev cloud |
|---|---|---|
| `p1-documents-evidence` | Retention and legal hold on Cloud Storage against fake-gcs-server; the ClamAV scanner in the local stack | Extending a locked retention (which the emulator refuses); clamd beside the API, as a Cloud Run sidecar `[verify]`, with its signature updates |
| `p1-templates-email` | ZeptoMail and Resend adapters against fakes; Mailpit locally; the domain checks | A live send through each provider, the failover, inbound mail through Resend, and the sending domain's records |
| `p1-connector-framework` | The connector runtime, the egress guard, secrets by reference, against fakes | A live send through each production provider after the bootstrap; the egress guard in Cloud Run |
| `p1-general-ledger-close` | The scheduler's postings under the worker's role | The scheduler's own database role (§17.5 item 5) |
| `p1-multi-currency-fx` 7.2 | The rate feed port and the file feed; the MAS adapter is not built until MAS confirms redistribution (FR-GL-05 `[verify]`) | The MAS feed, once built |
| `p1-bank-import-reconcile` | The secrets project (OpenTofu, `tofu test`); the Wise and Airwallex adapters against fakes and their published examples; DBS reports and alerts through Mailpit | Apply the secrets project and set `FYNIQ_ORG_SECRETS_PROJECT` and `FYNIQ_ORG_SECRETS_KEY`; a live sync and a verified webhook from the Wise and Airwallex sandboxes; a DBS IDEAL report and alert through the live inbound provider (their formats `[verify]` with a redacted sample); the providers' processing regions `[verify]`; whether Wise offers a read-only token `[verify]` |
| `p1-users-roles`, `p1-mcp-first-party` | Sign-in through the local page and Identity Platform's fake; the delegation key locally | Identity Platform live; the MCP delegation key in Cloud KMS |
| `p1-operations-dr` | (not built yet) | Observability, backups, restore drills, cloud posture |
| `p1-ai-providers` | (not built yet) | Prompt caching on for every AI route whose model supports it (AI-27) |
