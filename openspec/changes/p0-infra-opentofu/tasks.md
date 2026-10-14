# Tasks

## 1. Foundation (after the founder creates the organisation and billing account)

- [ ] 1.1 Create the state bucket and the `dev`, `staging` and `production` roots, each in its own project; verify `tofu plan` for each in CI (`// req: NFR-PORT-05`, `// req: CTL-CHG-05`)
- [ ] 1.2 Apply the Singapore resource-location policy; test `// req: CTL-DP-07` tries to plan a resource in another region and expects the policy to deny it
- [ ] 1.3 Set up Workload Identity Federation for this repository (`main` and tags only) and remove any key; verify `// req: SEC-04` and `// req: CTL-IAM-04` with a CI deploy and a check that no service-account keys exist

## 2. Platform resources

- [ ] 2.1 Create Artifact Registry, Secret Manager, KMS keys (CMEK, anchor signing) and Cloud SQL for PostgreSQL 18 with backups, PITR and CMEK; verify `// req: SEC-12` and `// req: CTL-DP-01` from the resource configuration
- [ ] 2.2 Create the Cloud Run services and jobs with one service account each, the Cloud Tasks queues per worker class and the Scheduler tick; verify `// req: SEC-03` by listing each account's roles
- [ ] 2.3 Create the retention-locked anchor bucket; verify the lock with an attempted delete (`// req: CTL-AUD-02`)
- [ ] 2.4 Add the Cloud Billing budgets (production US$400, development and staging US$120, alerts at 50%, 80% and 100%, amounts as variables); verify the alert channels with a test notification

## 3. Drift and access

- [ ] 3.1 Add a nightly drift check that fails on any difference; verify `// req: CTL-CHG-02` after a manual console change in `dev`
- [ ] 3.2 Grant humans no standing production write or admin role, with just-in-time elevation; verify `// req: CTL-IAM-03` from the IAM policy, then `make check`
