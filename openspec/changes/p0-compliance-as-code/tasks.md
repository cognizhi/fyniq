# Tasks

## 1. Registry

- [x] 1.1 Write `compliance/controls/` with one YAML file per Appendix F control and a JSON Schema; `make check` validates them and compares the IDs with the PRD's Appendix F rows (`make controls` compares IDs and the PRD's text; the schema and the named targets are checked by `compliance/registry_test.go` in `make test`, because the `make check` job has no YAML library)
- [x] 1.2 Add the runner for CI, daily and nightly checks (nightly through the Scheduler tick), storing evidence per run; verify with a dry run that lists every automated check (CI checks are the CI jobs with their artefacts; daily ones the `controls` workflow; nightly ones platform jobs whose runs `compliance.evidence` records; `jobs controls` is the dry run)

## 2. P0 checks

- [ ] 2.1 [live-verify] Wire the CI checks: `// req: CTL-CHG-03`, `// req: CTL-CHG-04`, `// req: CTL-CHG-08`, `// req: CTL-CHG-10`, `// req: CTL-CHG-11` and `// req: CTL-IAM-05`, each leaving its report as evidence (15 Oct 2026: all but CTL-CHG-04 run with their artefacts; CTL-CHG-04 waits for image signing, p0-infra-opentofu) (16 Oct 2026: images are signed and verified on every pull request with `make image-sign`, so what CTL-CHG-04 still lacks is its own check, Binary Authorization's daily enforcement check, which needs the cloud; it moves to the gate before the first cloud deployment with the other staging tasks)
- [x] 2.2 Add the check that release images contain no local adapter and that staging and production run with `FYNIQ_ENV` set correctly; tests `// req: CTL-CHG-09` and `// req: SEC-16`
- [x] 2.3 Add the log-scrubbing test across services; test `// req: SEC-15` (in the QA-LOCAL-01 smoke: synthetic NRIC, FIN, bank number and tokens sent to the stack, then every service's log is searched)
- [x] 2.4 Add the branch-protection check (author is not the approver, reviews required); `// req: CTL-CHG-01` reports "not enabled" until the founder switches protection on

## 3. Secure development lifecycle

- [x] 3.1 Add the ASVS 5.0 tagging convention for security tests and a threat-model template in `docs/security/`; test `// req: SEC-20` fails on a security test without an ASVS ID, then `make check` and `make test`
