# Runbook: the annual penetration test

**When:** every 12 months (the review `security-assessments` in Compliance › Reviews and attestations is due from 30 days before,
and the control CTL-CHG-12 is overdue after); and before the first external customer is onboarded (SEC-25); and after a change
that moves the trust boundary (a new sign-in provider, a new public surface, a new MCP or API scope class).
**Requirements:** PRD-001 SEC-25, CTL-CHG-12, CTL-RES-04, SEC-33; `p2-security-certification` design D4.
**Who:** the Owner books the tester and records the result in Compliance › Controls › Security assessments (the register,
which only the Owner writes). The test itself is an outside firm's work `[live-verify]`: nothing here has been run against a real
firm or a real staging.

1. **Choose an independent tester**: not the people who build or run Fyniq. Ask for CREST-style or equivalent credentials and a
   report that maps its findings to OWASP ASVS 5.0 levels and a severity scale `[verify the firm's scale against the register's five]`.
2. **Scope it in writing**: the web app, the public API, the OAuth server and the MCP server on **staging**, never production; the
   AI service through its read tools only; no testing of the cloud provider's own services; no denial of service; business hours
   or agreed windows; one named contact on each side. Give the tester a test identity per role (`STAGING_ORIGIN`, the staging test
   identities `[live-verify]`), never a real person's account or real data (DEV-08).
3. **Freeze what they test**: note the release tag and image digests under test.
4. **During the test**, the tester reports a Critical finding at once. The platform owner opens an incident
   (docs/runbooks/incident-response.md) when a finding shows data exposed or a control bypassed.
5. **Record the assessment** in the register the day the report arrives: kind, tester, independent, scope, dates, a summary, the
   report's SHA-256 (`shasum -a 256 <report>`) and where it is kept. **Keep the report outside Fyniq**: it describes how to break the
   system. Add each finding with the tester's reference, a title and its severity; each is due by its severity from the day found: a
   Critical one in 7 days, a High one in 30, a Medium one in 90, a Low one in 180.
6. **Fix and retest.** Fix each finding on a branch with a test that fails first (the `fyniq-bug-fix` skill), release it, then
   record the step **fixed** with the day. Ask the tester to retest; record **retested** with the day. A fix not retested 30 days on
   shows overdue. A risk taken on purpose is **accepted** by an Owner with its reason before it is retested; an acceptance is
   reviewed at the next test.
7. **Close the loop**: add any finding found at the retest with **Add a finding**; tell the DPO if personal data was in scope
   (docs/runbooks/incident-response.md, the breach timers); update the threat model of any area a finding touched; write the new
   test into the suite with its `// asvs:` ID.

Continuous DAST (CTL-CHG-07) runs besides this, in CI and nightly on staging once it exists `[live-verify]`; its findings follow
`compliance/scan-waivers.toml` and CTL-RES-04's SLA, not this register.
