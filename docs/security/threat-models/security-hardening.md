# Threat model: security hardening

| | |
|---|---|
| Feature and requirement IDs | The platform's hardening before launch: signed and attested releases, reproducible images, one egress client and registered private endpoints, the edge with Cloud Armor and per-IP limits, signed evidence downloads, security alerts, the central log store, cloud posture and the log scan, DAST, dependency hygiene, the auto-approval rule, the recorded version, the incident runbook and register: SEC-22, SEC-23, SEC-24, SEC-26, SEC-30, SEC-31, SEC-32, SEC-33, SEC-36, SEC-37, REL-04, REL-05, NFR-IMG-05, FR-DOC-08, NFR-IMG-07, CTL-CHG-04, CTL-CHG-05, CTL-CHG-07, CTL-DP-03, CTL-RES-04 (PRD-001 rev 0.246; `p1-security-hardening`) |
| Author, reviewer, date | Claude Code for the owner, 11 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Every class the platform holds, indirectly: what the controls here protect is the release, the network path, the files and the evidence of what happened. Direct: client IP addresses, countries and devices of sign-ins (personal, kept 400 days beside the chain, AUD-06); incident and alert texts (may name people); the platform's logs (must hold no secret, token, NRIC or bank number, SEC-15) |
| Who acts | The release workflow (GitHub Actions on a tag) and its attester; the jobs' account (posture and log reads); the api (signs download links as itself); the worker (sign-in checks, alerts); the edge (Cloud Armor, the load balancer); the DPO (the incident register); a Super admin (private endpoints, with dual control); attackers on the internet, in a dependency, in a connector's upstream, or holding a member's session |

## 1. What we are building

The release workflow builds the images once, scans them, signs them keyless and attests their SBOM and SLSA
provenance; a separate job verifies all of it and only then creates the Binary Authorization attestation, as an
attester reachable only from `release.yml` on a version tag. Cloud Run admits only attested images. The same commit
builds the same digests, checked daily. Every outbound request goes through one egress guard: the platform's own
Google APIs through a client that reaches only them, connectors through allow-lists per manifest, and private
endpoints only when a Super admin registered them, with dual control, and their key is pinned. The edge (an external
load balancer with Cloud Armor) fronts the public services; it names the client's address, country and region in
headers the services trust only from it, which per-IP limits and the impossible-travel rule read. Evidence files are
saved through 5-minute signed links. The platform raises SEC-31's alerts as audit events, locked critical notices and
log entries Cloud Monitoring alerts on. Cloud Audit Logs and the alerts go to a 400-day locked bucket; Security Command
Center's findings and a sample of the logs are read daily into the Compliance Center; staging is scanned weekly for
production data. ZAP scans the signed-in local stack on every pull request. Dependabot, govulncheck and pnpm audit
keep the dependencies current. Each export names the version and commit that produced it. Incidents are recorded
step by step in an insert-only register, under a runbook.

Trust boundaries: GitHub Actions ↔ the artefact registry and Binary Authorization (WIF, OIDC); internet ↔ edge ↔
services (headers set by the load balancer); services ↔ outside hosts (egress); services ↔ Google APIs (the
platform client); browser ↔ Cloud Storage (signed URLs); the platform ↔ its own logs.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | CI → registry → Cloud Run | Tampering: an image built elsewhere, or changed after its scan, runs in production | Arbitrary code with the services' identities | Medium |
| T2 | CI | Elevation: a pull request's workflow, a fork or a branch reaches the attester or a write token | Attest a malicious image; push to main | Medium |
| T3 | Build | Tampering: a build that cannot be reproduced hides a poisoned dependency or toolchain | Undetectable backdoor | Low |
| T4 | Services → internet | Elevation (SSRF): a connector, a webhook or a template makes the platform call the metadata server, an internal service or another host | Credentials or internal data | Medium |
| T5 | Services → private endpoint | Spoofing: a registered private address answered by another host, or an endpoint registered by one person to exfiltrate | Data to an attacker's host | Low |
| T6 | Internet → edge → services | Spoofing: a client sets the client-IP, country or region headers itself | Per-IP limits and impossible travel defeated | Medium |
| T7 | Internet → edge | Denial of service: floods of sign-in, setup, join or public-document requests | Lockouts, cost | High |
| T8 | Browser → storage | Information disclosure: an evidence link reused, shared or long-lived | A sensitive file read by others | Medium |
| T19 | Upload → scanner | Elevation / tampering: a crafted file exploits clamd's parsers (libxml2 and others with no patch yet), or a file passes because the scanner could not decide | Code running in the scanner; malware kept as evidence | Medium |
| T9 | Platform → people | Repudiation / denial: an attacker's sign-in, export or bank change goes unseen; alerts drown in noise | Late detection | Medium |
| T10 | Platform → logs | Information disclosure: a secret, a token, an NRIC or a bank number reaches the logs | Secrets and personal data exposed to log readers | Medium |
| T11 | Cloud configuration | Elevation: a public bucket, an open firewall or a broad grant stays open | Data exposure | Medium |
| T12 | Non-production | Information disclosure: production data copied to staging | Personal data outside production's controls (DEV-08) | Low |
| T13 | Web app | Tampering: XSS, clickjacking, a missing header on a page DAST would see | Session theft | Medium |
| T14 | Dependencies | Elevation: a known vulnerability reachable in the code, or a malicious new release | Remote code execution | Medium |
| T15 | Pull requests | Elevation: an automated approval of a pull request not the owner's (release-please, Dependabot, a fork) | Unreviewed code merged | Medium |
| T16 | Platform version | Information disclosure: the public version endpoint helps target known vulnerabilities | Easier exploitation | Low |
| T17 | Incident register | Repudiation / tampering: an incident's record changed after the fact, or kept by someone who may not | False evidence | Low |
| T18 | Scanners | Repudiation: a scanner that cannot read the code passes in silence | False assurance | Medium (it happened: gosec on Go 1.27.3) |

SOD: attestation is the release workflow's alone, after verification; a private endpoint is registered by one Super
admin and approved by another; the incident register is kept by the DPO. Founder mode changes none of it. RLS: the
new tenant tables (`security_incident`, `security_incident_step`, `ip_rate_window` holds no organisation and is
installation-wide by design) force row-level security; `private_endpoint` is installation-wide, written only through
security-definer functions.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | Keyless cosign signatures, the SBOM attested to each image, SLSA provenance from the generator; the attest job verifies signature, SBOM and provenance for the tag before it attests; Binary Authorization requires the attestation for every service and job, with no allow-list | `TestTheReleaseWorkflowVerifiesBeforeItAttests`; `scripts/images.py self-test` (a signature counts only for its digest; attested only after the signature and provenance); `tofu binauthz/only_attested_images_are_admitted` `[live-verify]` |
| T2 | The attester is reachable only through `attribute.release` (release.yml on a tag) in the WIF condition; workflows hold `permissions: {}` and each job only what `archtest.WriteJobs` allows; actions pinned by SHA (the SLSA generator by tag, waived) | `TestWorkflowsPinActionsAndLimitTokens`, `TestTheReleaseWorkflowVerifiesBeforeItAttests`; the ci-identity module test |
| T3 | BuildKit `rewrite-timestamp` with the commit's time, fixed owners and modes, a pinned BuildKit, no cache; built twice, digests compared daily | `make images-repro` (NFR-IMG-07); `scripts/images.py self-test` |
| T4 | One egress guard: no private, loopback or metadata address unless named; the address checked is the address dialled; no redirects; bounded answers; the platform's Google client reaches only `*.googleapis.com`; an archtest refuses any other client | `TestEgressRefusals`, `TestEgressDialsTheAddressItChecked`, `TestEgressFollowsNoRedirectAndBoundsTheAnswer`, `TestAConnectionReachesOnlyItsOwnHosts`, `TestTheNarrowClientsReachOneDestination`, `TestEveryOutboundRequestGoesThroughEgress` |
| T5 | A private endpoint is a registered private range, never the platform's own ranges, reached only with its pinned SPKI key, registered by one Super admin and approved by another, audited | `TestARegisteredPrivateEndpointIsReachedOnlyWithItsPinnedKey`, `TestPrivateEndpointsAreRegisteredOnlyAsTheDatabaseAllows`, `TestAConnectorReachesOnlyTheEndpointsRegisteredForIt`, `TestPrivateEndpoints` |
| T6 | The headers are trusted only from the load balancer's and Caddy's addresses (`FYNIQ_TRUSTED_PROXIES`), and the services take the last value; ingress is the load balancer and internal traffic only | `TestWhereARequestCameFrom`; the edge module test (the headers set by the backend services) `[live-verify]` |
| T7 | Cloud Armor: the OWASP rule sets, Log4Shell denied, a rate-based ban per address on sign-in and the routes without a session, adaptive protection; per-IP windows in the database (sign-in 30, join 10, public 120, setup 10 a minute) | `TestPerIPLimits`, `TestPerIPWindowsCountAndPurge`, `TestReadIPLimits`; the edge module test |
| T8 | A 5-minute link per request, a V4 signed URL as the api's own account (signBlob), served as an attachment; asking for one of a sensitive file is an audited read | `TestV4SigningMatchesGooglesVectors`, `TestADownloadLinkIsSignedAndSavesTheFile`, `TestAnEvidenceFileIsSavedThroughAShortLivedLink` |
| T9 | SEC-31's rules: new device, impossible travel, a vendor's bank change, mass export, repeated refusals, the audit chain, the log scan; each an audit event, a locked critical notice and a log entry Cloud Monitoring alerts on; one per rule, person and window; thresholds are settings | `TestTheSignInRules`, `TestAnAdminsSignInIsChecked`, `TestRefusalsAndMassExportsRaiseAlerts`, `TestReadThresholds`; `security_monitoring_alerts_the_founders` (module test) |
| T10 | Redaction at the source (SEC-15) and the daily log scan with the same patterns; a finding counts kinds and never keeps the entry, fails CTL-DP-03 and alerts the Super admins; the local smoke test reads every service's log for the same | `TestLeaksNameWhatRedactionWouldRemove`, `TestTheLogScanCountsAndKeepsNothing`, `TestPlatformVerificationsRunInTheInstallationOnly`; `scripts/local-smoke.sh` |
| T11 | Security Command Center's High and Critical findings read daily; CTL-RES-04 fails past 7 days (Critical) or 30 (High), the owner's decision (rev 0.247); data-access logs on the sensitive services and a 400-day locked sink | `TestPostureFindingsPastTheirSLA`, `TestSecurityCenterListsTheActiveFindings`; `production_keeps_its_security_logs_in_the_audit_project` (module test) `[live-verify]` |
| T12 | Staging's database exported weekly and inspected by Sensitive Data Protection for NRIC, passports, card and bank numbers; the module refuses production | `staging_is_scanned_each_week_for_production_data`, `production_is_refused` (module tests) `[live-verify]` |
| T13 | ZAP's baseline with its AJAX spider against the signed-in local stack on every pull request; High alerts and FAIL rules block unless waived; strict CSP, nosniff, frame-ancestors none, CORP same-origin | `make dast` (CI), `scripts/dast.py self-test`; `TestEveryAnswerIsNosniffAndUseRunsFirst` |
| T14 | govulncheck (a vulnerability the code calls blocks) in both flavours, pnpm audit on the bundle, grype on the repository and the images; Dependabot with a 14-day cooldown, security updates at once | `scripts/scan.py --self-test`; `make scan` |
| T15 | The auto-approval runs from main's copy (pull_request_target), approves only the owner's own pull requests from this repository, runs no action, and leaves the review register and needs-human-review for a person; CODEOWNERS | `TestAutoApprovalApprovesOnlyTheOwnersPullRequests` |
| T16 | The endpoint names only what every export names and the release notes publish; patching is fast because the version is known (Dependabot, govulncheck); no build paths or dependency lists | `TestThePlatformVersionNeedsNoCredential` |
| T17 | Insert-only tables, every change refused (FY322), steps in the runbook's order, each audited; kept by the DPO only (Appendix D, Compliance Center C/A); the breach link only to the organisation's own breach | `TestAttackTheIncidentRegisterKeepsItsRules`, `TestTheIncidentRegister` |
| T18 | A package gosec cannot type-check fails the scan; a waiver that matches nothing fails it too | `scripts/scan.py --self-test` |
| T19 | clamd runs as nonroot in its own sidecar with no shell, no database role, no secret and no credential; its front accepts only the api's and the worker's tokens and keeps nothing; a file clamd cannot decide is refused, never passed; the image scan shows every library (each Debian package's record), blocks what a patch exists for and reports the rest | `TestTheScannerServiceScansForItsCallers`, `TestTheSidecarUpdatesThenRunsClamd`, `TestTheSidecarFailsWithoutSignaturesOrClamd`; `only_the_api_and_the_worker_scan` (module test); `scripts/images.py self-test` |

## 4. What remains

- Everything in the cloud is code with module tests until the dev cloud: Binary Authorization's enforcement, Cloud
  Armor's rules in preview, the load balancer's headers, the sink and its lock, SCC, Sensitive Data Protection, the
  signed URLs' signer `[live-verify]` (`infra/README.md`).
- ClamAV in the cloud is a private scanner service (the owner's decision, rev 0.247): an HTTP front with clamd as a
  sidecar; its deployment is `[live-verify]`. The clamd image's libraries carry findings no patch exists for yet
  (libxml2, glibc and others), reported and not blocking (rev 0.248); move DEBIAN_SNAPSHOT forward as fixes land.
- The nightly DAST of staging is not signed in until a staging test identity exists.
- Impossible travel reads country and region only (the owner's decision): a VPN defeats it, and new-device alerts
  catch the rest.
- The AI's and MCP clients' kill switches arrive with `p1-ai-assistant`; the runbook says so.
