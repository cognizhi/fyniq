# platform/portability-and-cost Specification

## Purpose
How Fyniq stays portable across clouds and cheap to run: ports with GCP, local, AWS and Azure adapters, the two hard ties stated plainly, the storage port and unit of work, PostgreSQL compatibility, adapter conformance, infrastructure as code per cloud, idle cost, spending limits and the cost model. Derived from PRD-001 §12.12 (ADR-T8) and §15.

## Requirements

### Requirement: Ports and adapters
The domain SHALL depend only on ports (interfaces), each with a GCP adapter (built and run in production), a local adapter (ADR-T4) and named AWS and Azure equivalents, built when a deployment needs them.

Source: PRD-001 §12.12

#### Scenario: AWS and Azure adapters built on demand
- **WHEN** a deployment needs AWS or Azure
- **THEN** the named equivalent adapters for that cloud are built

#### Scenario: Outside services are connectors
- **WHEN** Fyniq integrates an outside service
- **THEN** it does so as a connector (FR-INT-25)

#### Scenario: Local adapters prove the domain is cloud-free
- **WHEN** the domain runs on the local adapters
- **THEN** it runs with no cloud dependency (DEV-07)

#### Scenario: Second cloud is a P3 rehearsal
- **WHEN** a second cloud is planned
- **THEN** it is a P3 rehearsal (NFR-PORT-06), not a P1 cost

### Requirement: Adapters per port and cloud
Each port SHALL map to the named GCP (production), AWS, Azure and local services of the port table.

Source: PRD-001 §12.12

#### Scenario: Compute
- **WHEN** services run
- **THEN** they run on Cloud Run with min instances 0 (GCP), ECS on Fargate (AWS; App Runner is closed to new customers), Container Apps with min replicas 0 (Azure), or Docker Compose (local)

#### Scenario: Long runs
- **WHEN** a long run is started
- **THEN** it runs as a Cloud Run job (GCP), ECS RunTask on Fargate (AWS), a Container Apps job (Azure), or a Compose one-off container (local)

#### Scenario: Queue and timer
- **WHEN** work is queued or timed
- **THEN** it uses Cloud Tasks plus the Cloud Scheduler tick (GCP); EventBridge Scheduler (one-time and cron) plus ECS RunTask, with SQS for short delays (AWS); Storage Queues or Service Bus scheduled messages plus Container Apps jobs (Azure); or the push dispatcher with Cloud Tasks semantics (local; rev 0.23), with River for self-hosting

#### Scenario: Database
- **WHEN** the database runs
- **THEN** it is Cloud SQL for PostgreSQL (GCP), Aurora or RDS for PostgreSQL (AWS; Aurora can pause at 0 capacity), Azure Database for PostgreSQL Flexible Server (Azure), or a PostgreSQL container (local)

#### Scenario: WORM storage
- **WHEN** records are written to WORM storage
- **THEN** it is Cloud Storage with Bucket Lock (GCP), S3 Object Lock (AWS), Blob immutable storage with a locked policy (Azure), or fake-gcs-server (local)

#### Scenario: Keys and secrets
- **WHEN** keys and secrets are used
- **THEN** they come from Cloud KMS and Secret Manager (GCP), AWS KMS and Secrets Manager (AWS), Key Vault (Azure), or a Tink keyset and `.env`, the file §12.8 names (local)

#### Scenario: Sign-in
- **WHEN** a user signs in
- **THEN** sign-in uses Identity Platform (GCP, ADR-T3), Cognito or Ory Kratos + Hydra (AWS), Entra External ID or Ory Kratos + Hydra (Azure), or the Firebase Authentication emulator (local)

#### Scenario: AI endpoint
- **WHEN** the AI calls a model
- **THEN** it calls Claude on Vertex AI (GCP, ADR-T2), Claude on Amazon Bedrock (AWS), Claude in Microsoft Foundry (Azure), or replayed responses or an OpenAI-compatible server on the machine (local, AI-79)
- **AND** on any cloud it may call any provider connection of §9.9.1 (AI-70…AI-79)

#### Scenario: Telemetry
- **WHEN** telemetry is emitted
- **THEN** OpenTelemetry sends it to Cloud Logging, Monitoring and Trace (GCP), CloudWatch (AWS), Azure Monitor (Azure), or the console (local)

### Requirement: Database extensions and Singapore regions on every cloud
The platform's database extensions (`pgcrypto`, `pgaudit`, `pg_partman`, `pg_cron`) SHALL be ones available on RDS, Aurora and Azure Flexible Server for PostgreSQL 17–18 (the versions NFR-PORT-02 and §11.3 target; 18 is available on each), and a deployment on each cloud SHALL be able to use a Singapore region.

Source: PRD-001 §12.12

#### Scenario: Extensions on Azure
- **WHEN** the database runs on Azure Flexible Server
- **THEN** the four extensions are listed in `shared_preload_libraries`

#### Scenario: Singapore regions
- **WHEN** a region is chosen on GCP, AWS or Azure
- **THEN** a Singapore region is available: `asia-southeast1`, `ap-southeast-1` or Southeast Asia

### Requirement: Identity tie to Google Cloud
The platform SHALL keep a portable sign-in path open, because Identity Platform is GCP-only; ADR-T3, accepted, keeps that path available.

Source: PRD-001 §12.12

#### Scenario: Portable sign-in path
- **WHEN** sign-in must run outside Google Cloud
- **THEN** the portable path is Ory Kratos + Hydra or Keycloak (Apache-2.0)
- **AND** Zitadel is now AGPL-3.0

### Requirement: Claude in Singapore tie
The AI endpoint SHALL be chosen against the Singapore availability of Claude models on each cloud, because data residency on another cloud would change ADR-T2's options.

Source: PRD-001 §12.12

#### Scenario: Vertex AI in Singapore
- **WHEN** a Claude model on Vertex AI is needed in `asia-southeast1`
- **THEN** Opus 4.6, Sonnet 4.6, Opus 4.5 and Sonnet 4.5 have an endpoint there
- **AND** the 4.7+ and 5.x models do not (§12.5); the rev 0.7 statement that only Sonnet 4.6 had one is superseded

#### Scenario: Bedrock in Singapore
- **WHEN** Claude on Bedrock is needed in Singapore
- **THEN** only Claude Sonnet 5 runs in-region, and newer models go through the global profile

#### Scenario: Microsoft Foundry
- **WHEN** Claude in Microsoft Foundry is considered
- **THEN** it is offered only as Global or US Data Zone

### Requirement: NFR-PORT-01 Storage port
The domain SHALL depend on repository interfaces, and SQL SHALL live only in the PostgreSQL adapter.

Source: PRD-001 §15 · Priority M · Phase P0

#### Scenario: SQL outside the adapter fails the build
- **WHEN** SQL or a `pgx` import appears outside the PostgreSQL adapter
- **THEN** an architecture test fails the build

### Requirement: NFR-PORT-07 Unit of work
Repository interfaces SHALL run inside one unit of work per domain command, in which the document, ledger lines, audit event and outbox row commit or roll back together.

Source: PRD-001 §15 · Priority M · Phase P0

#### Scenario: Repository cannot manage its own transaction
- **WHEN** a repository method tries to open or commit its own transaction
- **THEN** it cannot
- **AND** an architecture test checks this

#### Scenario: Failed write leaves no partial rows
- **WHEN** an attack test fails one write in the middle of a domain command
- **THEN** no partial rows from that command remain

#### Scenario: G4-084 Command fails before the outbox row
- **WHEN** a domain command fails after writing ledger lines but before the outbox row
- **THEN** no document, ledger, audit or outbox rows from that command remain

### Requirement: NFR-PORT-02 PostgreSQL compatibility
The platform SHALL run on Cloud SQL, AlloyDB, Aurora or RDS for PostgreSQL, Azure Flexible Server and self-hosted PostgreSQL 17+.

Source: PRD-001 §15 · Priority M · Phase P0

#### Scenario: Only common extensions
- **WHEN** a database extension is used
- **THEN** it is available on all of these targets

#### Scenario: Suites pass before a target is supported
- **WHEN** a PostgreSQL target is to be supported
- **THEN** the invariant and attack suites pass on it first

### Requirement: NFR-PORT-03 No cloud SDK in the domain
Cloud SDKs SHALL be imported only in `internal/adapters/<provider>/`.

Source: PRD-001 §15 · Priority M · Phase P0

#### Scenario: Cloud SDK import outside the adapters
- **WHEN** a cloud SDK is imported outside `internal/adapters/<provider>/`
- **THEN** a dependency lint (depguard-style) fails the build

### Requirement: NFR-PORT-04 Adapter conformance
Every port SHALL have a conformance suite, and a cloud SHALL be called supported only when its adapters pass it.

Source: PRD-001 §15 · Priority M · Phase P0

#### Scenario: Suites run in CI
- **WHEN** CI runs
- **THEN** each port's conformance suite runs against the local and GCP adapters
- **AND** against AWS or Azure adapters when they exist

#### Scenario: Differences and failures recorded
- **WHEN** an adapter is run against its conformance suite
- **THEN** the adapter records its capability differences and any failing conformance cases

#### Scenario: Supported only when passing
- **GIVEN** a cloud's adapters fail any conformance case
- **WHEN** the cloud's support status is stated
- **THEN** it is not called supported

### Requirement: NFR-PORT-05 Infrastructure as code per cloud
Each cloud SHALL have OpenTofu modules behind the same variables.

Source: PRD-001 §15 · Priority M · Phase P0

#### Scenario: Modules validated and policy-checked
- **WHEN** an OpenTofu module changes
- **THEN** `tofu validate` and the policy checks run on that module

#### Scenario: Same variables on every cloud
- **WHEN** the modules for two clouds are compared
- **THEN** they take the same variables

### Requirement: NFR-PORT-06 Portability rehearsal
A staging deployment on a second cloud SHALL pass the suites, at P3, when a customer or the founders need it.

Source: PRD-001 §15 · Priority M · Phase P3

#### Scenario: Second-cloud staging passes the suites
- **GIVEN** a customer or the founders need a second cloud (P3)
- **WHEN** a staging deployment runs on that cloud
- **THEN** it passes the suites

### Requirement: NFR-COST-02 Idle cost
With nobody using it, the platform SHALL bill no service compute; only the fixed-fee items listed for idle SHALL be billed.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Meaning of nobody using it
- **WHEN** idle is assessed
- **THEN** "nobody using it" means no signed-in session with a visible tab, no running scheduled job and no inbound integration traffic

#### Scenario: Fixed-fee items billed while idle
- **WHEN** the platform is idle
- **THEN** the items billed are the database, storage, the load balancer and Cloud Armor, keys and secrets, registry storage, the Scheduler tick and the independent monitor (NFR-OBS-02)

#### Scenario: Monthly bill review and cost alert
- **WHEN** service compute is billed while idle as defined
- **THEN** a cost alert is raised
- **AND** the bill is reviewed monthly

#### Scenario: G4-078 One hidden tab left open overnight
- **GIVEN** staging is left with one tab open and hidden overnight
- **WHEN** the hidden-tab limit is reached
- **THEN** the stream closes
- **AND** no service compute is billed after that, and the NFR-COST-02 alert stays quiet

### Requirement: NFR-COST-01 Infrastructure cost
Infrastructure cost at Cognizhi's own scale SHALL stay within the budget in §12.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Cost within budget
- **WHEN** the infrastructure cost at Cognizhi's own scale is reviewed
- **THEN** it is within the budget in §12

### Requirement: NFR-COST-03 Spending limits
Cloud Billing budgets SHALL alert at 50%, 80% and 100%, and AI budgets SHALL degrade AI features only, never statutory deadline checks or authorised accounting work.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Budget alerts
- **WHEN** spending reaches 50%, 80% or 100% of a Cloud Billing budget
- **THEN** an alert is raised

#### Scenario: Spend cap only on Agent Platform
- **WHEN** a Google spend cap (which pauses the service) is set
- **THEN** it is set only on Agent Platform, never on Cloud Run
- **AND** a policy check on the billing configuration verifies this

#### Scenario: AI budget degrades AI features only
- **WHEN** an in-app AI token budget (AI-13, AI-43) is exhausted
- **THEN** only AI features are degraded
- **AND** statutory deadline checks (AI-67) and authorised accounting work continue, as a test that exhausts the AI budget confirms for deadlines and postings

#### Scenario: G4-085 AI budget reached before a GST deadline
- **GIVEN** a GST F5 deadline is 3 days away
- **WHEN** the AI token budget or the Agent Platform spend cap is reached mid-month
- **THEN** AI features say they are paused
- **AND** the deadline alert, postings and approvals continue

### Requirement: NFR-COST-04 Cost model
A workload-based cost estimate SHALL be reconciled with one staging month before P1 go-live, and §12.6 SHALL be updated from it.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Estimate inputs
- **WHEN** the cost estimate is made
- **THEN** it is based on requests, stream minutes, scans, storage growth, log volume, model input, output and cache tokens, and partner quotes

#### Scenario: Reconciliation report
- **WHEN** the estimate is reconciled with a staging month before P1 go-live
- **THEN** the reconciliation report is kept in the release record
- **AND** §12.6 is updated from it

#### Scenario: G4-086 End of the staging month
- **WHEN** the staging month ends
- **THEN** predicted and billed costs are reconciled per §12.6 line
- **AND** differences over 20% are explained [proposed threshold]
