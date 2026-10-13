# compliance/technology-controls Specification

## Purpose
The access and identity, change and supply-chain, data-protection and resilience controls of the control registry (`compliance/controls/`): each control, its automated check and frequency, the evidence kept, and the frameworks it maps to (ISO = ISO/IEC 27001:2022 Annex A; SOC2 = SOC 2 Trust Services Criteria; CE/CT = CSA Cyber Essentials / Cyber Trust; ASVS = OWASP ASVS 5.0). Derived from PRD-001 §F.3, §F.4, §F.5 and §F.6.

## Requirements

### Requirement: CTL-IAM-01 MFA for every user and passkey step-up for approvals
Every user SHALL be held to MFA, and approvals SHALL need a passkey step-up. The control is checked nightly, with a report kept as evidence.

Source: PRD-001 §F.3

#### Scenario: Nightly MFA check
- **WHEN** the nightly check runs
- **THEN** the number of users without MFA is 0
- **AND** the report is kept as evidence

#### Scenario: Approval needs a passkey step-up
- **WHEN** a user approves
- **THEN** a passkey step-up is required first

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.5, SOC2 CC6.1, CE and ASVS V6

### Requirement: CTL-IAM-02 Quarterly access review and expiry of external users
Access SHALL be reviewed every quarter, and external users SHALL expire. The review runs quarterly and expiry is checked nightly, with the signed review kept as evidence.

Source: PRD-001 §F.3

#### Scenario: Quarterly review task
- **WHEN** a quarter's access review falls due
- **THEN** a review task is generated
- **AND** the signed review is kept as evidence

#### Scenario: Nightly check of expired users
- **WHEN** the nightly check runs
- **THEN** the number of expired but still active users is 0

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.18, SOC2 CC6.2–6.3 and CT

### Requirement: CTL-IAM-03 No standing privileged cloud access
No person SHALL hold standing privileged access to the cloud, and just-in-time elevation SHALL be logged. The control is checked daily, with an IAM snapshot kept as evidence.

Source: PRD-001 §F.3

#### Scenario: Daily check of production project roles
- **WHEN** the daily check runs
- **THEN** no human holds Owner or Editor on a production project
- **AND** an IAM snapshot is kept as evidence

#### Scenario: Just-in-time elevation is logged
- **WHEN** a person elevates their cloud access just in time
- **THEN** the elevation is logged

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.2 and SOC2 CC6.1

### Requirement: CTL-IAM-04 No service-account keys; Workload Identity Federation for CI
No service-account key SHALL exist, and CI SHALL authenticate through Workload Identity Federation. The control is checked daily for drift, with a policy snapshot kept as evidence.

Source: PRD-001 §F.3

#### Scenario: Key creation disabled by organisation policy
- **WHEN** the daily drift check runs
- **THEN** the organisation policy `iam.disableServiceAccountKeyCreation` is confirmed enforced
- **AND** a policy snapshot is kept as evidence

#### Scenario: CI uses Workload Identity Federation
- **WHEN** CI authenticates to the cloud
- **THEN** it uses Workload Identity Federation, not a service-account key

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.2 and 8.5 and SOC2 CC6.1

### Requirement: CTL-IAM-05 Tenant isolation by row-level security
Tenants SHALL be isolated by row-level security. The control is checked on every change in CI, with the test report kept as evidence.

Source: PRD-001 §F.3

#### Scenario: Cross-tenant attack tests on every change
- **WHEN** a change runs through CI
- **THEN** the cross-tenant attack tests confirm that every attempt to reach another tenant's rows fails
- **AND** the test report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.3, SOC2 C1 and CC6.1, and ASVS V8

### Requirement: CTL-IAM-06 Authorisation matrix holds for every role and endpoint
The authorisation matrix SHALL hold for every role and every endpoint. The control is checked on every change in CI by QA-SEC-03, with the test report kept as evidence.

Source: PRD-001 §F.3

#### Scenario: Authorisation-matrix suite on every change
- **WHEN** a change runs through CI
- **THEN** the authorisation-matrix suite (QA-SEC-03) passes for every role and endpoint
- **AND** the test report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.15, SOC2 CC6.1 and ASVS V8 (L3)

### Requirement: CTL-IAM-07 First-run setup, Super admin changes and provider sign-ins
First-run setup SHALL be completed once, with the deployment's one-time setup code; Super admin changes SHALL need step-up and, with two Super admins, dual control; and sign-ins through any provider SHALL still require the platform's MFA.

Source: PRD-001 §F.3

#### Scenario: Setup-wizard tests on every change
- **WHEN** a change runs through CI
- **THEN** the setup-wizard tests confirm that setup is refused with no code, with a reused code and on a second run

#### Scenario: Provider sign-in tests with MFA
- **WHEN** a change runs through CI
- **THEN** the provider sign-in tests confirm that a sign-in through any provider still requires the platform's MFA

#### Scenario: Super admin change with two Super admins
- **GIVEN** the deployment has two Super admins
- **WHEN** a Super admin change is made
- **THEN** it needs step-up and dual control

#### Scenario: Nightly Super admin count
- **WHEN** the nightly check runs
- **THEN** the number of Super admins is at least 1

#### Scenario: Evidence kept
- **WHEN** the control is checked, on every change in CI and nightly
- **THEN** the test reports and the audit events are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.15–5.18 and 8.5 and SOC2 CC6.1–6.3

### Requirement: CTL-CHG-01 Protected branches and two-person review
Branches SHALL be protected, and every production change SHALL have a two-person review in which the author is not the approver. The control is checked daily and on every change in CI.

Source: PRD-001 §F.4

#### Scenario: Daily repository settings check
- **WHEN** the daily check runs
- **THEN** the repository settings are confirmed to protect the branches
- **AND** a settings snapshot is kept as evidence

#### Scenario: Merge audit of production changes
- **WHEN** a production change is merged
- **THEN** the merge audit confirms that its approver is not its author
- **AND** the pull request records are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.32 and 8.4 and SOC2 CC8.1

### Requirement: CTL-CHG-02 Infrastructure only through IaC with drift detected
Infrastructure SHALL change only through infrastructure as code, and drift SHALL be detected. The control is checked on every change in CI and daily, with the plan output kept as evidence.

Source: PRD-001 §F.4

#### Scenario: Daily plan expects no changes
- **WHEN** the daily plan runs
- **THEN** it is expected to show no changes, and any change it shows is drift
- **AND** the plan output is kept as evidence

#### Scenario: Policy checks on plans
- **WHEN** a change produces an infrastructure plan in CI
- **THEN** policy checks (OPA/Conftest) run on the plan

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.9 and SOC2 CC8.1

### Requirement: CTL-CHG-03 Security scans with blocking critical findings
Every change SHALL pass SAST, dependency, container and secret scans, and a critical finding SHALL block it. The control is a CI gate, with the scan reports kept as evidence.

Source: PRD-001 §F.4

#### Scenario: Scans run on every change
- **WHEN** a change runs through CI
- **THEN** the SAST, dependency, container and secret scans run
- **AND** their scan reports are kept as evidence

#### Scenario: A critical finding blocks the change
- **GIVEN** a scan reports a critical finding
- **WHEN** the CI gates are evaluated
- **THEN** the change is blocked

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.25, 8.28 and 8.8, SOC2 CC7.1 and CE

### Requirement: CTL-CHG-04 Signed images with provenance; only attested images deploy
Images SHALL be signed with provenance, and only attested images SHALL deploy. The control is checked daily, with the attestations kept as evidence.

Source: PRD-001 §F.4

#### Scenario: An unattested image does not deploy
- **GIVEN** an image without an attestation
- **WHEN** it is deployed
- **THEN** the deployment is refused

#### Scenario: Daily Binary Authorization enforcement check
- **WHEN** the daily check runs
- **THEN** it confirms that Binary Authorization is enforced
- **AND** the attestations are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.32 and SOC2 CC8.1

### Requirement: CTL-CHG-05 Separate projects per environment and no production data in dev
Each environment SHALL have its own cloud project, and no production data SHALL be in development. The control is checked daily and weekly.

Source: PRD-001 §F.4

#### Scenario: Daily organisation structure check
- **WHEN** the daily check runs
- **THEN** it confirms that each environment is in its own project
- **AND** a snapshot is kept as evidence

#### Scenario: Weekly DLP scan of non-production
- **WHEN** the weekly scan runs
- **THEN** a DLP scan of the non-production environments looks for production data
- **AND** the scan is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.31 and SOC2 C1

### Requirement: CTL-CHG-06 SBOM per release
Every release SHALL have an SBOM. The release pipeline produces it on each release, and the SBOM is kept as evidence.

Source: PRD-001 §F.4

#### Scenario: Release pipeline produces the SBOM
- **WHEN** a release is built
- **THEN** a release pipeline step produces its SBOM
- **AND** the SBOM is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.28 and SOC2 CC7.1

### Requirement: CTL-CHG-07 Security tests mapped to ASVS and DAST on staging
Security tests SHALL be mapped to ASVS IDs, and a DAST baseline scan SHALL run on staging. The control runs on every change in CI and nightly, with the reports kept as evidence.

Source: PRD-001 §F.4

#### Scenario: Security tests carry ASVS IDs
- **WHEN** a change runs through CI
- **THEN** the security tests run, each mapped to its ASVS ID
- **AND** the reports are kept as evidence

#### Scenario: Nightly DAST baseline scan
- **WHEN** the nightly run happens
- **THEN** a DAST baseline scan runs on staging
- **AND** its report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.29 and ASVS

### Requirement: CTL-CHG-08 Images hardened and within budget
Images SHALL be hardened and within their size budget (§12.9). The control is checked on every change in CI, with the image report kept as evidence.

Source: PRD-001 §F.4

#### Scenario: Image gate on every change
- **WHEN** a change runs through CI
- **THEN** the `make images` gate and QA-IMG-01 check every image's hardening and budget
- **AND** the image report is kept as evidence

#### Scenario: An unhardened or oversized image fails the gate
- **GIVEN** an image that is not hardened or exceeds its budget
- **WHEN** the image gate runs
- **THEN** the gate fails

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.9 and 8.19, SOC2 CC7.1 and CE

### Requirement: CTL-CHG-09 No local adapters in staging or production
No local adapter SHALL run in staging or production (DEV-07). The control is checked on every change in CI and on each deploy, with the test report kept as evidence.

Source: PRD-001 §F.4

#### Scenario: Symbol check on production images
- **WHEN** a production image is checked
- **THEN** the symbol check confirms that it contains no local adapter symbols

#### Scenario: Start-up refusal
- **GIVEN** a staging or production service configured with a local adapter
- **WHEN** it starts
- **THEN** it refuses to start

#### Scenario: Checked on every change and each deploy
- **WHEN** a change runs through CI or a deploy happens
- **THEN** the symbol check and the start-up refusal test run
- **AND** the test report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.31 and SOC2 CC8.1

### Requirement: CTL-CHG-10 Versions, changelog and tags only from release-please
Versions, the changelog and tags SHALL be produced only by release-please from Conventional Commits, and the release pull request SHALL be reviewed. The control is checked on every change in CI, with the release record kept as evidence.

Source: PRD-001 §F.4

#### Scenario: Commit messages checked on pull requests
- **WHEN** a pull request runs through CI
- **THEN** its commit messages are checked against Conventional Commits

#### Scenario: Tags are protected
- **WHEN** a tag is created other than by release-please
- **THEN** tag protection prevents it

#### Scenario: Release pull request reviewed
- **WHEN** release-please opens a release pull request
- **THEN** the pull request is reviewed before it merges
- **AND** the release record is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.32 and SOC2 CC8.1

### Requirement: CTL-CHG-11 GitHub Actions pinned by SHA with least-privilege permissions
GitHub Actions SHALL be pinned by SHA, and workflows SHALL have least-privilege permissions (SEC-28). The control is checked on every change in CI, with the lint report kept as evidence.

Source: PRD-001 §F.4

#### Scenario: Workflow lint on every change
- **WHEN** a change runs through CI
- **THEN** a workflow lint (e.g. zizmor or actionlint) checks that every action is pinned by SHA and every workflow has least-privilege permissions
- **AND** the lint report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.30 and 8.28 and SOC2 CC8.1

### Requirement: CTL-DP-01 CMEK on database, storage and backups with key rotation
The database, storage and backups SHALL be encrypted with customer-managed keys (CMEK), and the keys SHALL rotate. The control is checked daily, with a configuration snapshot kept as evidence.

Source: PRD-001 §F.5

#### Scenario: Daily CMEK configuration check
- **WHEN** the daily configuration check runs
- **THEN** it confirms CMEK on the database, storage and backups, enforced by the organisation policy `gcp.restrictNonCmekServices`
- **AND** a configuration snapshot is kept as evidence

#### Scenario: Keys rotate
- **WHEN** the daily configuration check runs
- **THEN** it confirms that key rotation is configured

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.24 and SOC2 C1

### Requirement: CTL-DP-02 Sensitive fields envelope-encrypted and masked
Sensitive fields (NRIC, bank numbers, salaries) SHALL be envelope-encrypted and masked. The control is checked on every change in CI, with the test report kept as evidence.

Source: PRD-001 §F.5

#### Scenario: Sensitive fields stored encrypted
- **WHEN** an NRIC, a bank number or a salary is stored
- **THEN** it is envelope-encrypted

#### Scenario: API masking tests on every change
- **WHEN** a change runs through CI
- **THEN** the API masking tests confirm that NRIC, bank numbers and salaries are masked in API responses
- **AND** the test report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.11, PDPA protection, and SOC2 C1 and P

### Requirement: CTL-DP-03 No NRIC or secrets in logs, analytics or AI traces
No NRIC and no secret SHALL appear in logs, analytics or AI traces. The control is checked weekly, with the scan report kept as evidence.

Source: PRD-001 §F.5

#### Scenario: Weekly Sensitive Data Protection scan
- **WHEN** the weekly Sensitive Data Protection scans of logs, analytics and AI traces run
- **THEN** they find zero NRIC-pattern hits
- **AND** the scan report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.12 and PDPA

### Requirement: CTL-DP-04 NRIC never used for authentication or file passwords
An NRIC SHALL NOT be used for authentication or as a file password. The control is checked on every change in CI, with the test report kept as evidence.

Source: PRD-001 §F.5

#### Scenario: Lint rule on every change
- **WHEN** a change runs through CI
- **THEN** a lint rule checks that no code uses an NRIC for authentication or as a file password

#### Scenario: Payslip delivery test
- **WHEN** the payslip delivery test runs
- **THEN** it confirms that no payslip is protected with a password based on the NRIC
- **AND** the test report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to PDPA (PDPC/CSA advisory)

### Requirement: CTL-DP-05 Data inventory, retention schedule and DPIAs current
The data inventory, the retention schedule and the DPIAs SHALL be kept current. They are reviewed every quarter, with the inventory version kept as evidence.

Source: PRD-001 §F.5

#### Scenario: Review due date monitored
- **GIVEN** the quarterly review of the data inventory, retention schedule and DPIAs has a due date
- **WHEN** the due date passes without a review
- **THEN** the monitor detects the overdue review

#### Scenario: Inventory version kept
- **WHEN** the quarterly review is completed
- **THEN** the inventory version is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to PDPA accountability and ISO 5.34

### Requirement: CTL-DP-06 Breach runbook with PDPA timers and twice-yearly tabletop
A breach runbook with the PDPA timers SHALL exist, and it SHALL be exercised in a tabletop twice a year. The control acts on each breach or suspected breach and semi-annually, with the exercise report kept as evidence.

Source: PRD-001 §F.5

#### Scenario: The runbook runs the PDPA timers
- **WHEN** the breach runbook is started
- **THEN** its PDPA timers run

#### Scenario: Tabletop exercise twice a year
- **WHEN** a half-year's tabletop falls due
- **THEN** an exercise task is created and the exercise is recorded
- **AND** the exercise report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to PDPA breach notification, ISO 5.24–5.28 and SOC2 CC7.3–7.5

### Requirement: CTL-DP-07 Resource locations restricted to Singapore
Cloud resource locations SHALL be restricted to Singapore, except for documented transfers. The control is checked daily, with a policy snapshot kept as evidence.

Source: PRD-001 §F.5

#### Scenario: Location policy and endpoint restrictions
- **WHEN** the daily check runs
- **THEN** it confirms the organisation policy `gcp.resourceLocations` and the endpoint restrictions are enforced
- **AND** a policy snapshot is kept as evidence

#### Scenario: Only documented transfers leave Singapore
- **WHEN** a resource outside Singapore is in use
- **THEN** it is a documented transfer

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to PDPA transfer limitation and ISO 5.23

### Requirement: CTL-RES-01 HA, PITR and backups configured as specified
High availability, point-in-time recovery (PITR) and backups SHALL be configured as specified. The control is checked daily, with a snapshot kept as evidence.

Source: PRD-001 §F.6

#### Scenario: Daily configuration check
- **WHEN** the daily configuration check runs
- **THEN** it confirms that high availability, PITR and backups are configured as specified
- **AND** a snapshot is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.13 and SOC2 A1.2

### Requirement: CTL-RES-02 Restore drill with ledger and audit-chain verification
Restores SHALL be drilled, with the ledger and the audit chain verified on the restored copy. The control runs monthly and quarterly, with the drill report kept as evidence.

Source: PRD-001 §F.6

#### Scenario: Automated monthly PITR restore
- **WHEN** a month's restore falls due
- **THEN** an automated PITR restore runs with ledger and audit-chain verification
- **AND** the drill report is kept as evidence

#### Scenario: Quarterly full drill
- **WHEN** a quarter's drill falls due
- **THEN** a full restore drill runs with ledger and audit-chain verification
- **AND** the drill report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.30 and 8.13, SOC2 A1.3 and CE

### Requirement: CTL-RES-03 SLO monitoring and alerting
Service-level objectives SHALL be monitored with alerting. The control is checked daily, with the configuration and the incident log kept as evidence.

Source: PRD-001 §F.6

#### Scenario: Daily burn-rate alert check
- **WHEN** the daily check runs
- **THEN** it confirms that the burn-rate alerts are configured
- **AND** the configuration and the incident log are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.16 and SOC2 A1.1 and CC7.2

### Requirement: CTL-RES-04 Security Command Center findings fixed within SLA
High and critical Security Command Center findings SHALL be fixed within their SLA. The control is checked daily, with the findings report kept as evidence.

Source: PRD-001 §F.6

#### Scenario: Daily overdue-finding check
- **WHEN** the daily check runs
- **THEN** it detects any high or critical finding not fixed within its SLA
- **AND** the findings report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.8, SOC2 CC7.1 and CT

### Requirement: CTL-RES-05 Scheduler heartbeat alert configured and Scheduler jobs enabled
The Scheduler heartbeat alert SHALL be configured and the Scheduler jobs SHALL be enabled (NFR-OBS-02). The control is checked daily and quarterly, with a configuration snapshot and a test record kept as evidence.

Source: PRD-001 §F.6

#### Scenario: Daily configuration check
- **WHEN** the daily configuration check runs
- **THEN** it confirms the absence policy, its notification channel and that the Scheduler jobs exist and are enabled
- **AND** a configuration snapshot is kept as evidence

#### Scenario: Quarterly alert test in staging
- **WHEN** the quarterly test disables the tick in staging
- **THEN** the alert is raised
- **AND** the test record is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.16 and SOC2 A1.1 and CC7.2
