# Proposal

## Why

Fyniq goes live with real books, payee bank details and people's data, and PRD-001 §13 asks for more than P0 built.
P0 signs images with a throwaway key, and no release workflow builds, signs or deploys them. Nothing proves where an
image came from, and nothing stops an unsigned one from running. Outside the app there is no WAF and no per-IP limit,
and uploads are served as JSON rather than through signed links. Only connector traffic passes the egress guard. No
alert fires on a suspicious admin sign-in, a mass export or repeated refusals. There is no incident runbook and no
cloud posture feed, and five control rows of Appendix F wait for this change:
- CTL-CHG-04 and CTL-CHG-05 (weekly);
- CTL-CHG-07;
- CTL-DP-03;
- CTL-RES-04.

The launch plan puts it in wave 7, before go-live. Its cloud parts are built as code with module tests and verified
in the dev cloud later.

## What Changes

- **The owner's decisions of 10 Nov 2026, taken interactively (PRD-001 rev 0.246):**
  - **Provenance from GitHub Actions (SEC-22, NFR-IMG-05).** A release workflow on the `vX.Y.Z` tag builds the images
    and pushes them to Artifact Registry by digest. It signs them keyless with its OIDC identity (cosign), attests their
    SBOM, and attests SLSA Build L3 provenance through the slsa-github-generator reusable workflow. Binary
    Authorization admits only images whose attestation the workflow made after it verified that signature and that
    provenance [live-verify].
    - *Not chosen:* Cloud Build's provenance, and both.
  - **Impossible travel from the load balancer (SEC-31).** The load balancer in front of Cloud Run adds the client's
    country and region as request headers (Cloud Armor's geolocation, [live-verify]); locally a stand-in header does.
    An admin signing in from another country sooner than travel allows, or from a new device, raises an alert.
    - *Not chosen:* a GeoIP database in the image, and new-device alerts alone.
  - **DAST in CI on the local stack (CTL-CHG-07).** OWASP ZAP's baseline scan runs in CI against the stack started
    from the change's images, as the local smoke test starts it, and nightly on staging once it exists [live-verify].
    - *Not chosen:* staging only.
- **Images (SEC-22, NFR-IMG-05, NFR-IMG-07, CTL-CHG-04):**
  - the release workflow above;
  - Binary Authorization's attestor and policy in `infra/`;
  - grype blocking High findings as well as Critical;
  - reproducible builds: the same commit builds the same digests, checked by building twice.
- **Outbound requests (SEC-36, SEC-37):**
  - every outbound call (the payment, email, sign-in, render and MCP delegation adapters, OIDC discovery, webhooks)
    goes through the one egress client, with an allow-list per configured host, and an architecture test refuses any
    other HTTP client;
  - a private-network destination is reachable only once the Super admin registers its address, TLS identity, service,
    organisation and bound credential.
- **The web surface (SEC-23, SEC-24):**
  - the load balancer with Cloud Armor (OWASP rules, a per-IP rate-based ban, the geolocation headers) in `infra/`;
  - per-IP limits in the app for the routes that take no session (sign-in, joining, public documents);
  - uploads downloaded through short-lived signed links with download headers, not as JSON;
  - ClamAV as a Cloud Run service [live-verify].
- **Detection and response (SEC-30, SEC-31, SEC-32, SEC-33, CTL-RES-04, CTL-DP-03, CTL-CHG-05):**
  - security alerts, as notices and as log entries Cloud Monitoring alerts on: admin sign-ins (new device, impossible
    travel), vendor bank changes, mass exports, repeated authorisation failures, audit-chain failures, and a hook for
    the AI red team's pattern hits;
  - Cloud Audit Logs and the app's security events sunk to a retention-locked bucket, with alert policies [live-verify];
  - cloud posture from Security Command Center behind a port, with a local fake, whose High and Critical findings feed
    the Compliance Center and CTL-RES-04's SLA;
  - a weekly scan of the logs for NRIC and secrets (CTL-DP-03), and of non-production for production data (CTL-CHG-05);
  - the incident response runbook with the PDPA timers, and an incident register in the Compliance Center.
- **Dependencies and releases (SEC-26, REL-04, REL-05, CTL-CHG-07):**
  - Dependabot for Go, npm and Python, and govulncheck in `make scan`;
  - the release pull request is never auto-approved, and a test keeps it so;
  - an About dialog with the version and git SHA, and both stamped in every exported report;
  - the ZAP baseline as above, with its waivers beside the scans'.

## Capabilities

### New Capabilities

None: the requirements are approved in the main specs (PRD-001 §13, Appendix F), and rev 0.246's changes are made
there directly.

### Modified Capabilities

None as deltas: `skip_specs: true`. Rev 0.246 edits SEC-22 in `security/application-security`, SEC-31 in
`security/detection-and-response` and CTL-CHG-07 in `compliance/technology-controls`, stamped against the PRD.

## Impact

- **Requirements:**
  - `security/application-security`: SEC-22, SEC-23, SEC-24, SEC-26, SEC-36, SEC-37;
  - `security/detection-and-response`: SEC-30 to SEC-33;
  - `platform/container-images`: NFR-IMG-05, NFR-IMG-07;
  - `platform/release-and-versioning`: REL-04, REL-05;
  - `compliance/technology-controls`: CTL-CHG-04, CTL-CHG-05, CTL-CHG-07, CTL-DP-03, CTL-RES-04.
- **Code:**
  - `.github/workflows/` (release, DAST, Dependabot);
  - `scripts/images.py` and `scripts/scan.py`;
  - `deploy/docker/`;
  - `internal/egress` and every adapter's HTTP client;
  - a private-endpoint registry;
  - per-IP limits in `internal/api`;
  - the evidence download link;
  - `internal/domain/security` (alerts);
  - a posture port with GCP and local adapters;
  - the incident register;
  - the web About dialog;
  - `internal/reportexport`.
- **Infrastructure:** `infra/gcp/modules/` gains the edge (load balancer, Cloud Armor), Binary Authorization, the log
  sink and alert policies, Security Command Center notifications and ClamAV, each with module tests [live-verify].
- **Database:** new migrations for private endpoints, authorisation-failure counts and the incident register.
- **API:** additive operations: the evidence download link, the private endpoints, the incidents, and the platform
  version.
- **Not here:**
  - the deployment of the images by digest to staging and production, which `cloud-first-deployment` makes;
  - the AI red team's pattern hits, which arrive with `p1-ai-assistant`;
  - penetration testing, which is `p2-security-certification`.
