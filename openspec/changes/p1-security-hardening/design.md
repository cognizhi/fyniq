# Design

## Context

The survey of 10 Nov 2026 (`8eb2102f`) found this state.

**What exists:**
- `make image-sign`: signs and attests with a throwaway key, in CI.
- `make image-scan`: syft and grype; only a Critical finding blocks.
- Images pinned by digest in the Dockerfiles and compose.
- `internal/egress`, which only connector traffic uses.
- The CSP and other headers in the web image; HSTS everywhere.
- Same-origin CSRF checks (`authn.SameOrigin`, `Sec-Fetch-Site`).
- Per-client rate limits per organisation (FR-INT-16).
- The evidence size and type checks, and a ClamAV adapter wired by `FYNIQ_CLAMD_ADDR`.
- Dependabot for Docker, compose and actions only.
- Sign-in alert emails, and the vendor-bank notice.
- `audit.verify`'s check; the breach register and its tabletop review.
- `/version`, and `platform_version` in reports.

**What is missing:**
- a release workflow (`release-please.yml:47` holds only a placeholder), SLSA provenance and Binary Authorization;
- Cloud Armor and a load balancer (`run.tf:144`: "from go-live");
- per-IP limits;
- signed download links (`GET /v1/evidence/{id}/file` answers base64 JSON);
- an egress client for the payment, email, sign-in, render and MCP adapters;
- private endpoints;
- alerting beyond notices;
- a central log store, cloud posture and DAST;
- an incident runbook;
- an About dialog;
- version and SHA on exported reports.

Five control rows of Appendix F name this change as where they arrive: CTL-CHG-04, CTL-CHG-05 (weekly), CTL-CHG-07,
CTL-DP-03 and CTL-RES-04.

## Goals / Non-Goals

**Goals**
- Every requirement of the change built, and tested offline. The cloud parts are built as OpenTofu with module tests
  on mock providers and marked `[live-verify]`.
- Each of the five control rows turns `running` or `cloud`.

**Non-Goals**
- Deploying to staging or production by digest. `cloud-first-deployment` does that, with the workflow this change
  builds.
- The AI red team's pattern hits, which `p1-ai-assistant` raises through this change's alert hook.
- Penetration testing (`p2-security-certification`).
- New screens. The incident register is a tab of an existing Compliance screen, the private endpoints are API only,
  and the About dialog is part of the shell.

## Decisions

### D1. The release workflow (SEC-22, NFR-IMG-05, CTL-CHG-04; rev 0.246)

`.github/workflows/release.yml` runs when release-please publishes a release, on its `vX.Y.Z` tag (the `release` event, as `release-evidence.yml`: the release App's token starts workflows).

1. **Build and push.** `make images FLAVOURS=release`, for both platforms, pushed to Artifact Registry by digest.
   It authenticates through Workload Identity Federation (the CI identity of `infra/gcp/modules/ci-identity`); no key.
2. **Sign and attest the SBOM.** `cosign sign` keyless with the job's OIDC identity, and `cosign attest` of the SPDX
   SBOM, both by digest.
3. **Provenance.** `slsa-framework/slsa-github-generator`'s `generator_container_slsa3.yml` attests each digest
   (SLSA Build L3: an isolated, reusable workflow signs it).
4. **Verify, then create the attestation.** `cosign verify` and `cosign verify-attestation --type slsaprovenance`
   pin the certificate identity to this workflow and repository. Only then does the job create a Binary
   Authorization attestation, with a Cloud KMS asymmetric key (`gcloud container binauthz attestations
   sign-and-create`).
5. **Publish.** The digests go to the GitHub release beside `release-evidence.yml`'s evidence.

**Binary Authorization in `infra/`.** It is a module: an attestor (`fyniq-release`) on that key, and a project policy
that requires its attestation for Cloud Run. Breakglass is off, and the policy is evaluated for every service
[live-verify]. Cloud Run's Binary Authorization uses attestors, not keyless signatures, which is why the workflow
verifies first and attests after.

**Every action is pinned by commit (SEC-28):** the workflow archtest refuses anything else, and the generator is
referenced by its release tag, as its own verifier requires. `make image-sign` keeps its throwaway-key self-test in
CI: a wrong key and an unsigned image must still fail.

**The scan gate.** `scripts/scan.py` and `scripts/images.py` block High findings as well as Critical, unless
`compliance/scan-waivers.toml` waives them with a reason and an expiry.

**CTL-CHG-04** becomes a `cloud` check: the module test, and the daily read of the policy's evaluation
[live-verify].

**Alternatives:** Cloud Build's provenance, and both (the owner's decision). Keyless signatures straight into Binary
Authorization: Cloud Run's policy cannot read them.

### D2. Reproducible images (NFR-IMG-07)

- **Go services.** `Dockerfile.go-service` already builds with `-trimpath -buildvcs=false -buildid=`. Both images gain
  `SOURCE_DATE_EPOCH` from the commit's time, and layers built with fixed timestamps and ownership.
- **The web image.** `Dockerfile.web` pins the Vite build's inputs and normalises file times before its copy.
- **The check.** `scripts/images.py repro` builds each image twice, from clean caches, and compares the digests;
  `make images-repro` runs it. CI runs it once a day, and on changes to `deploy/docker/` and the Dockerfiles' inputs,
  not on every pull request: two builds are slow.

### D3. One egress client (SEC-36) and private endpoints (SEC-37)

**One client.** `internal/egress` gains `egress.Client(allow …Host)`: the guard (no private, metadata or internal
address unless registered; it dials the address it checked; no redirect; a size cap) and an allow-list of host
patterns per configured connection.
- **Who moves to it:** the Airwallex, Wise, Resend, ZeptoMail, Hydra, GCP REST and render clients; MCP delegation;
  OIDC discovery and token calls; generic webhooks; and every test-connection action.
- **The test.** An architecture test refuses `http.Client{`, `http.DefaultClient`, `http.Get` and `http.Post` outside
  `internal/egress` and tests. The local flavour's stand-ins (Mailpit, fake GCS, Hydra) stay reachable through the
  local flavour's allow-list.

**Private endpoints.** They are installation-wide, so the table `private_endpoint` has no organisation in its key.
- **The registration:** the Super admin registers the address (host and port), the TLS identity (the certificate's
  SPKI SHA-256), the service, the organisation it serves, and the credential it is bound to. The API is
  `listPrivateEndpoints` and `registerPrivateEndpoint`; retiring one is a version, and each change is audited.
- **What the guard checks:** a private address only when it is registered, for that organisation and credential.
  The TLS connection must then present that key.
- **What an organisation user types never reaches a private network.** The guard refuses before it dials.

### D4. The web surface (SEC-23)

**The edge (`infra/gcp/modules/edge`).** Built as code, with module tests [live-verify]:
- a global external Application Load Balancer with a serverless NEG per public Cloud Run service;
- a Google-managed certificate;
- a Cloud Armor policy:
  - the preconfigured OWASP CRS rules at sensitivity 1, in preview first;
  - a per-IP rate-based ban on the sign-in and public routes;
  - adaptive protection;
- the custom request headers that carry the client's country and region (D7) and its IP to the services.

Cloud Run's ingress moves to "internal and load balancer" for the public services.

**Per-IP limits in the app.** A request that carries no session or token is counted per client IP, in the existing
rate-limit store with its own window: sign-in, joining, public documents and the setup code.
- **Which IP.** The client IP is the load balancer's header, trusted only from the load balancer's ranges (locally
  from Caddy).
- **The limit.** The answer is `429` with `Retry-After` (FR-INT-16's codes), and the limits are settings of the
  installation.

**Output encoding and CSRF.**
- **Tests that pin what exists:** every API answer is JSON with `X-Content-Type-Options: nosniff`, and no HTML is
  rendered from user input on the server (Typst documents escape their fields).
- **The CSRF matrix:** a cookie session from another site is refused on every write.

### D5. Downloads through signed links (SEC-24)

**The link.** `createEvidenceDownload` (POST, for whoever may read the file) answers a short-lived link (5 minutes):
- in the cloud, a V4 signed URL of the object, which GCS serves with `Content-Disposition: attachment` and the stored
  content type;
- locally, an HMAC-signed path the API serves with the same headers.

Asking for it is audited when the file is sensitive (AUD-05's audited reads).

**The old route.** `getEvidenceFile` (base64 JSON) stays, as the change is additive, and is deprecated with a Sunset
header 6 months on (FR-INT-06). The web app moves to the link.

**ClamAV** runs as an internal Cloud Run service in `infra/` [live-verify]. The release wiring requires
`FYNIQ_CLAMD_ADDR` there, as it does today. *Superseded (11 Nov 2026, rev 0.247):* clamd speaks raw TCP, which a
Cloud Run service does not accept. By the owner's decision it is a private **scanner service**: `cmd/scanner`, an
HTTP front, with clamd as a sidecar in the same Cloud Run service; the API and the worker call it with an ID token, as
they call the render service (`FYNIQ_SCANNER_URL`); one instance stays warm in production; locally the stack's clamd
is reached directly, as today.

### D6. Dependencies (SEC-26)

- **Dependabot** gains gomod, npm (`apps/web`) and pip (`oracle/`), grouped per ecosystem, with the 14-day cooldown
  the Docker entries use.
- **`make scan`** gains `govulncheck` (reachable vulnerabilities fail unless waived) and `pnpm audit --prod` at High.
- **The SBOM** per release exists (`release-evidence.yml`); D1 attests it to each image.

### D7. Security alerts (SEC-31; rev 0.246)

**The rules.** `internal/domain/security` holds the rules, each over facts the store already keeps:
- **An admin's sign-in.** An admin is an Owner, an Administrator or a Super admin.
  - A new device is a session from a browser, a passkey or a device key not seen for them in 90 days.
  - Impossible travel is a sign-in from another country than their last, sooner than travel allows: under 2 hours
    within one region and under 6 hours across regions. These are coarse on purpose; the thresholds are settings.
  - The country and region come from the load balancer's headers, a stand-in header locally, and are kept beside the
    security event as its IP is (AUD-06).
- **A vendor's bank details changed.** The notice exists; this adds the alert.
- **A mass export.** More than a threshold of exports or full-number reveals (AUD-05) by one person in an hour.
- **Repeated authorisation failures.** Refusals by the policy layer (API-AUTHZ-01) are counted per person per window
  in a new table (`authz_failure`, counts only, no request bodies), and an alert fires past a threshold.
- **An audit-chain verification failure.** It already fails its check; this adds the alert.
- **The AI red team's pattern hits.** An `AlertAI` hook, raised by `p1-ai-assistant`.

**What an alert is.**
- A locked security notice, `security.alert`, critical, emailed at once, to the Owners and the DPO, or to the Super
  admins for installation alerts.
- A structured log entry (`fyniq.security_alert`) that a log-based metric and an alert policy watch in Cloud
  Monitoring [live-verify].
- An audit event.

**Alternatives:** a GeoIP database, and new-device alerts alone (the owner's decision).

### D8. Logs, posture and the weekly scans (SEC-30, SEC-33, CTL-RES-04, CTL-DP-03, CTL-CHG-05)

**The log store (`infra/`) [live-verify]:**
- **Audit logs.** Data-access audit logs are enabled for the sensitive services: Cloud SQL, Secret Manager, KMS,
  Storage and IAM.
- **The sink.** A log sink sends Cloud Audit Logs and the app's security events to a retention-locked bucket in the
  audit project (§12.4), for 400 days, as the security events' retention.
- **Alerts.** Log-based metrics and alert policies watch the security alerts, IAM policy changes, Secret Manager
  access outside the services, and a failed `audit.verify`.

**Cloud posture.**
- **The port.** `ports.Posture` lists the findings: severity, category, resource, first seen, state. The GCP adapter
  reads Security Command Center (Standard tier) [live-verify]; the local adapter is a fake for tests.
- **The verification.** `posture.findings` feeds the Compliance Center: CTL-RES-04 fails when a High finding is open
  past 30 days, or a Critical one past 7 (the owner's decision of 11 Nov 2026, rev 0.247). The findings show under the control.

**The weekly scans.**
- **`logs.scan` (CTL-DP-03).** It reads a sample of the week's application logs through a `ports.LogReader` (Cloud
  Logging in the cloud [live-verify]; the local stack's container logs) and runs SEC-15's patterns: NRIC, tokens and
  bank numbers. A hit fails the control and raises an alert.
- **`nonprod.scan` (CTL-CHG-05 W).** It runs in staging only: a Sensitive Data Protection job over staging's buckets
  and database export, looking for NRIC, bank and card numbers, as code and module test [live-verify]. In production
  it reports "not applicable".

### D9. DAST (CTL-CHG-07; rev 0.246)

- **In CI.** A job starts the stack as `scripts/local-smoke.sh` does, then runs `zaproxy/zap-stable`'s baseline scan
  (pinned by digest) against Caddy's origin, signed in through the local sign-in page so the app's pages are reached.
  The rules file `compliance/zap-rules.tsv` says what fails: a High alert, unless `compliance/scan-waivers.toml`
  waives it with a reason and an expiry. The report is uploaded as evidence.
- **Nightly on staging.** The same scan, in a workflow that waits for staging's URL [live-verify].
- **Status.** CTL-CHG-07's CI check turns `running`, and its nightly one `cloud`. The ASVS mapping exists (SEC-20's
  tags); its coverage report joins the evidence.

### D10. Incident response (SEC-32)

- **The runbook.** `docs/runbooks/incident-response.md`:
  - detect, triage, contain, preserve, assess, notify, recover, learn;
  - each step's owner and tools: kill switches, token revocation, secret rotation, Cloud Armor blocks;
  - the PDPA breach procedure with its 30-day and 3-day timers, linking to the breach register;
  - who to call.
- **The incident register.** An Incidents tab of Compliance › Controls (no new screen):
  - an incident is opened with its severity and summary, and steps are added (contained, assessed, notified,
    recovered, reviewed);
  - it is insert-only and audited;
  - it can link to a breach when personal data was involved;
  - the API is `listIncidents`, `openIncident` and `stepIncident`;
  - Appendix D: "Compliance Center" C/A for the DPO, V for its readers.
- **The tabletop review** (`breach-tabletop`) exists; the runbook names it.

### D11. Releases and versions (REL-04, REL-05)

**REL-04.**
- An archtest keeps `auto-approve.yml` from approving a pull request whose author is not the owner, which
  release-please's is not. It also asserts the workflow's documented conditions stay in it.
- `CODEOWNERS` names the owner for `.github/`, `infra/` and `db/migrations/`.
- Branch protection [live-verify].

**REL-05.**
- **About dialog.** `getPlatformVersion` (`GET /v1/platform/version`, no session needed, the same body as `/version`)
  feeds an About dialog in the shell's Help menu, so the web app keeps to the generated client (QA-API-03).
- **Exports.** `internal/reportexport` stamps the version and the git SHA in every exported report's footer (XLSX,
  CSV and PDF).

## Risks / Trade-offs

- **[Most of it runs only in the cloud]**
  - Binary Authorization, Cloud Armor, the log sink, SCC, Sensitive Data Protection and ClamAV are verified only in
    the dev cloud.
  - Mitigation: module tests on mock providers, and `[live-verify]` tags that `p1-launch-readiness` lists.
- **[Impossible travel is coarse]** Country and region only (the owner's decision). Mitigation: new-device alerts
  catch the rest, and the thresholds are settings.
- **[ZAP on the local stack is not staging]** Caddy is not the load balancer. Mitigation: the nightly staging scan
  [live-verify].
- **[Two builds per reproducibility check]** Slow. Mitigation: daily, and on changes to the images' inputs only.
- **[Alert fatigue]** Too many alerts get ignored. Mitigation: thresholds as settings, one alert per window per rule
  and person.
- **[Rate limits behind a proxy]** Trusting a forwarded IP from anyone would let a caller choose their bucket.
  Mitigation: the header is trusted only from the load balancer's or Caddy's addresses.

## Migration Plan

Each slice ships as its own pull request, stacked, with its gates. Three of them need a migration, all additive:
- `private_endpoint`;
- `authz_failure`;
- the incident register.

The release workflow first runs on the next release tag; until Binary Authorization is enforced in the cloud, the
workflow's attestations are only recorded.

## Open Questions

- ~~**The SLA for SCC findings**~~ (CTL-RES-04): decided on 11 Nov 2026, 7 days for Critical and 30 for High (rev 0.247).
- **Whether the Super admin, or the operator outside the app, registers private endpoints.** The PRD says "the
  platform operator". The Super admin is the operator in the app, so they register them here.
