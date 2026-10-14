# Tasks

## 1. Registry

- [ ] 1.1 Write `compliance/controls/` with one YAML file per Appendix F control and a JSON Schema; `make check` validates them and compares the IDs with the PRD's Appendix F rows
- [ ] 1.2 Add the runner for CI, daily and nightly checks (nightly through the Scheduler tick), storing evidence per run; verify with a dry run that lists every automated check

## 2. P0 checks

- [ ] 2.1 Wire the CI checks: `// req: CTL-CHG-03`, `// req: CTL-CHG-04`, `// req: CTL-CHG-08`, `// req: CTL-CHG-10`, `// req: CTL-CHG-11` and `// req: CTL-IAM-05`, each leaving its report as evidence
- [ ] 2.2 Add the check that release images contain no local adapter and that staging and production run with `FYNIQ_ENV` set correctly; tests `// req: CTL-CHG-09` and `// req: SEC-16`
- [ ] 2.3 Add the log-scrubbing test across services; test `// req: SEC-15`
- [ ] 2.4 Add the branch-protection check (author is not the approver, reviews required); `// req: CTL-CHG-01` reports "not enabled" until the founder switches protection on

## 3. Secure development lifecycle

- [ ] 3.1 Add the ASVS 5.0 tagging convention for security tests and a threat-model template in `docs/security/`; test `// req: SEC-20` fails on a security test without an ASVS ID, then `make check` and `make test`
