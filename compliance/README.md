# compliance

Controls as code (PRD-001 §14.1, Appendix F).

| Path | What |
|---|---|
| `controls/CTL-*.yaml` | The control registry: one file per Appendix F control. The fields from `section` to `evidence` are the PRD's row, kept equal to it by `make controls` (part of `make check`; `python3 scripts/controls.py sync` rewrites them after a PRD change, `generate` adds a new control's file). `owner` is the role that answers for it (by area: financial → Controller; identity → Administrator; data protection → DPO; the rest → Owner; founder decision, 15 Oct 2026). `checks` says, per frequency, how it is checked here (`make <target>`, `job <platform job>`, `test <Go test>` or `ci <CI job>`) and where its evidence is, or when the check arrives |
| `control.schema.json` | The schema of a control file; `registry_test.go` validates every file, checks that each control's checks cover the PRD's frequencies, and that every running check names a target, job, test or CI job that exists |
| `registry.go` | The registry as a Go package, for the runner and the Compliance Center (P1) |
| `evidence.go` | The runner's nightly part: the `compliance.evidence` platform job (04:00 Singapore time, through the Scheduler tick) writes one record per scheduled check from its job's latest run, write-once, to the evidence store (`compliance/evidence/<date>/<control>-<frequency>.json`; locally `.data/blobs/`), and fails, so its heartbeat lapses and the absence alert fires, when a check's job failed, never ran or is stale. `ledger.verify` (02:30) is CTL-FIN-01's nightly check. `go run -tags local ./cmd/jobs controls` is the dry run: every check, its owner and how it runs or when it arrives |
| `scan-waivers.toml` | The accepted findings of the security scans (`make scan`, `scripts/scan.py`; SEC-21, CTL-CHG-03): tool, rule, path, why and an expiry. Reviewed in the pull request that adds or renews a waiver; an expired waiver, or one that matches nothing any more, fails the scan |

The evidence of CI checks is the workflow artefacts (`test-report`, `security-scan-reports`, `image-reports`,
`control-ctl-chg-01`), kept 90 days, the most a public repository allows; the scheduled checks' records go
to the evidence store, which p0-infra-opentofu makes a retention-locked bucket.
