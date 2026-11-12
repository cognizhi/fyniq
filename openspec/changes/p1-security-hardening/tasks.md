# Tasks

Each slice is its own pull request, stacked on the one before, with its gates (`make check`, `make test`'s suites
for what it touches, `make api-check`, archtest; `make infra-check` for `infra/`). Cloud parts are built as code with
module tests on mock providers and tagged `[live-verify]`.

## 1. The plan (slice 1)

- [x] 1.1 PRD-001 rev 0.246 records the owner's decisions of 10 Nov 2026 (SEC-22, SEC-31, CTL-CHG-07). The specs are
  updated and stamped, and CTL-CHG-07's file is synced.
- [x] 1.2 This change: the proposal, the design (D1–D11) and these tasks. The build plan's paragraph says it is
  proposed.

## 2. The release workflow and Binary Authorization (slice 2)

- [ ] 2.1 `.github/workflows/release.yml` (D1), on a `v*` tag:
  - build both platforms;
  - push by digest through Workload Identity Federation;
  - cosign keyless signature and SBOM attestation;
  - slsa-github-generator provenance;
  - verify the signature and provenance against this workflow's identity, then create the Binary Authorization
    attestation;
  - publish the digests to the release.

  The workflow archtest covers it. `// req: SEC-22`, `// req: NFR-IMG-05`
- [ ] 2.2 `infra/gcp/modules/binauthz`: the KMS key, the attestor and the policy requiring it for Cloud Run, with a
  module test `[live-verify]`. CTL-CHG-04's check becomes `cloud`. `// req: CTL-CHG-04`
- [ ] 2.3 The scans block High as well as Critical, unless waived (`scripts/scan.py`, `scripts/images.py`), with a
  planted High finding refused. `// req: NFR-IMG-05`

## 3. Reproducible images (slice 3)

- [ ] 3.1 `SOURCE_DATE_EPOCH` and normalised layers in both Dockerfiles. `scripts/images.py repro` and
  `make images-repro` build twice and compare digests, and a daily CI job runs them, as do changes to the images'
  inputs. `// req: NFR-IMG-07`

## 4. One egress client (slice 4)

- [ ] 4.1 `egress.Client` with a host allow-list per connection (D3). Every adapter, OIDC discovery and token calls,
  webhooks and test-connection actions move to it. An archtest refuses any other HTTP client outside
  `internal/egress` and tests. `// req: SEC-36`, `// req: QA-SEC-04`

## 5. Private endpoints (slice 5)

- [ ] 5.1 `private_endpoint` (D3) with attack tests. `listPrivateEndpoints` and `registerPrivateEndpoint` are the
  Super admin's and audited. The guard admits a registered private address only for its organisation and credential,
  and checks the TLS key. A typed private address is refused. `// req: SEC-37`

## 6. The edge and per-IP limits (slice 6)

- [ ] 6.1 `infra/gcp/modules/edge` (D4): the load balancer, serverless NEGs, the certificate, the Cloud Armor policy
  (OWASP CRS, the rate-based ban, adaptive protection) and the geolocation and client-IP headers. Ingress is
  "internal and load balancer". It has a module test `[live-verify]`. `// req: SEC-23`
- [ ] 6.2 Per-IP limits for requests with no session (sign-in, joining, public documents, the setup code), from the
  client IP header trusted only from the load balancer or Caddy, answered `429` with `Retry-After`. Settings for the
  limits. Tests also pin the JSON `nosniff` answers and the cross-site write refusals. `// req: SEC-23`

## 7. Signed downloads and ClamAV (slice 7)

- [ ] 7.1 `createEvidenceDownload` (D5): a 5-minute link, a V4 signed URL in the cloud and an HMAC path locally,
  served as an attachment with its content type. Audited where the file is sensitive. `getEvidenceFile` is deprecated
  with its Sunset. The web app moves to the link. `// req: SEC-24`
- [ ] 7.2 ClamAV as an internal Cloud Run service in `infra/`, with a module test `[live-verify]`. `// req: SEC-24`

## 8. Security alerts (slice 8)

- [ ] 8.1 `internal/domain/security` (D7):
  - admin sign-ins: a new device, and impossible travel from the geolocation headers kept beside the security event;
  - vendor bank changes;
  - mass exports;
  - repeated authorisation failures, counted in `authz_failure`;
  - audit-chain failures;
  - the AI hook.

  Each alert is a locked critical notice `security.alert`, a `fyniq.security_alert` log entry and an audit event.
  Thresholds are settings. `// req: SEC-31`
- [ ] 8.2 The log-based metrics and alert policies in `infra/` `[live-verify]`. `// req: SEC-30`, `// req: SEC-31`

## 9. Logs, posture and the weekly scans (slice 9)

- [ ] 9.1 `infra/`: data-access audit logs for the sensitive services, and the sink to a retention-locked bucket in
  the audit project, kept 400 days `[live-verify]`. `// req: SEC-30`
- [ ] 9.2 `ports.Posture`, with a GCP adapter (SCC) and a local fake. The `posture.findings` verification lists the
  findings under CTL-RES-04 and fails it past the SLA. `// req: SEC-33`, `// req: CTL-RES-04`
- [ ] 9.3 `logs.scan`, weekly, over `ports.LogReader` with SEC-15's patterns. `nonprod.scan`, weekly in staging, is a
  Sensitive Data Protection job as code `[live-verify]`. `// req: CTL-DP-03`, `// req: CTL-CHG-05`

## 10. DAST (slice 10)

- [ ] 10.1 The ZAP baseline in CI against the local stack, signed in, with `compliance/zap-rules.tsv` and waivers.
  Its report is evidence. A nightly staging workflow `[live-verify]`. CTL-CHG-07's checks become `running` and
  `cloud`. `// req: CTL-CHG-07`

## 11. Dependencies, releases and versions (slice 11)

- [ ] 11.1 Dependabot for gomod, npm and pip, grouped, with a cooldown. `govulncheck` and `pnpm audit --prod` in
  `make scan`. `// req: SEC-26`
- [ ] 11.2 The archtest that the auto-approval never approves a pull request not opened by the owner, `CODEOWNERS`,
  and branch protection `[live-verify]`. `// req: REL-04`
- [ ] 11.3 `getPlatformVersion` and the About dialog. The version and git SHA in every exported report. Component
  test with axe. `// req: REL-05`

## 12. Incident response (slice 12)

- [ ] 12.1 `docs/runbooks/incident-response.md`, with the PDPA breach procedure and its timers. `// req: SEC-32`
- [ ] 12.2 The incident register (D10): the table with attack tests, the API with matrix rows, and the Incidents tab
  with its help keys, guide section and component test with axe. `// req: SEC-32`

## 13. Threat model and as built (slice 13)

- [ ] 13.1 `docs/security/threat-models/security-hardening.md`.
- [ ] 13.2 `make test` and `make up` pass on the last slice. The build plan's paragraph says what was built.

## Workflow follow-up

- `/opsx:verify p1-security-hardening`, then `/opsx:archive p1-security-hardening --skip-specs` once every task is
  ticked.
- `p1-launch-readiness` lists every `[live-verify]` of this change for the dev cloud.
