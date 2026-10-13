# security/application-security Specification

## Purpose
How Fyniq is built and shipped securely: the ASVS-aligned development lifecycle, CI security gates, signed images with provenance, web and upload protections, one guarded egress client, penetration and dynamic testing, dependency hygiene, hardened images and the CI supply chain. Derived from PRD-001 §13.3.

## Requirements

### Requirement: SEC-20 Secure development lifecycle aligned to OWASP ASVS 5.0
The secure development lifecycle SHALL align to OWASP ASVS 5.0: Level 2 baseline, and Level 3 for business logic, authentication, authorisation and logging on money and approval flows.

Source: PRD-001 §13.3 · Priority M · Phase P0

#### Scenario: ASVS levels applied
- **WHEN** a feature is built
- **THEN** it meets the ASVS 5.0 Level 2 baseline
- **AND** its business logic, authentication, authorisation and logging on money and approval flows meet Level 3

#### Scenario: OWASP Top 10 review checklist
- **WHEN** a change is reviewed
- **THEN** the OWASP Top 10:2025 review checklist is applied

#### Scenario: Threat model for money movement or personal data
- **WHEN** a feature involves money movement or personal data
- **THEN** it has its own threat model

#### Scenario: Security tests carry ASVS IDs
- **WHEN** a security test is written
- **THEN** it is tagged with the ASVS IDs it verifies

### Requirement: SEC-21 CI security gates
CI SHALL run SAST, dependency and container vulnerability scanning, secret scanning, infrastructure-as-code policy checks and licence checks, and critical findings SHALL block merge.

Source: PRD-001 §13.3 · Priority M · Phase P0

#### Scenario: Gates run on every change
- **WHEN** CI runs on a change
- **THEN** SAST, dependency and container vulnerability scanning, secret scanning, infrastructure-as-code policy checks and licence checks run

#### Scenario: Critical finding blocks merge
- **WHEN** any of these gates reports a critical finding
- **THEN** the change cannot be merged

### Requirement: SEC-22 Signed images with provenance and deploy-time enforcement
Container images SHALL be signed with build provenance (Cloud Build produces SLSA level 3 provenance), and deployment SHALL be enforced with Binary Authorization (free for Cloud Run).

Source: PRD-001 §13.3 · Priority M · Phase P0 (signing) / P1 (SLSA provenance, Binary Authorization)

#### Scenario: Images are signed
- **WHEN** a container image is built
- **THEN** it is signed

#### Scenario: SLSA level 3 provenance
- **WHEN** Cloud Build builds an image
- **THEN** it produces SLSA level 3 build provenance for it

#### Scenario: Binary Authorization at deploy time
- **WHEN** an image is deployed
- **THEN** Binary Authorization enforces at deploy time that it is signed and has its build provenance
- **AND** an image that fails the check is not deployed

### Requirement: SEC-23 Web protections
The system SHALL protect its web surface with WAF and DDoS protection at the load balancer (Cloud Armor), rate limiting per user and per IP, a strict Content Security Policy, CSRF protection and output encoding.

Source: PRD-001 §13.3 · Priority M · Phase P1

#### Scenario: WAF and DDoS protection at the load balancer
- **WHEN** traffic reaches the load balancer
- **THEN** Cloud Armor applies WAF and DDoS protection

#### Scenario: Rate limiting per user and per IP
- **WHEN** one user or one IP address exceeds its rate limit
- **THEN** further requests from it are limited

#### Scenario: Strict Content Security Policy
- **WHEN** the web app is served
- **THEN** a strict Content Security Policy is sent with it

#### Scenario: CSRF protection and output encoding
- **WHEN** a state-changing request arrives or user-supplied content is rendered
- **THEN** CSRF protection applies to the request
- **AND** the content is output-encoded

### Requirement: SEC-24 File uploads
File uploads SHALL be limited by type and size, scanned for malware, stored outside the web root, and served with download headers through signed URLs.

Source: PRD-001 §13.3 · Priority M · Phase P1

#### Scenario: Type and size limits
- **WHEN** a file of a disallowed type or over the size limit is uploaded
- **THEN** it is rejected

#### Scenario: Malware scanning
- **WHEN** a file is uploaded
- **THEN** it is scanned for malware

#### Scenario: Stored outside the web root and served through signed URLs
- **WHEN** an uploaded file is stored and later downloaded
- **THEN** it is stored outside the web root
- **AND** it is served through a signed URL with download headers

### Requirement: SEC-36 Outbound requests to configured addresses
Outbound requests to configured addresses (OIDC discovery and token URLs, SMTP and IMAP hosts, webhook and generic-webhook endpoints, connector base URLs, test-connection actions) SHALL go through one egress client (OWASP SSRF prevention).

Source: PRD-001 §13.3 · Priority M · Phase P1

#### Scenario: Refused destinations
- **WHEN** the egress client resolves a configured name
- **THEN** it refuses loopback, private, link-local and unique-local addresses, the cloud metadata service and the platform's own internal services

#### Scenario: No DNS rebinding
- **WHEN** the egress client connects
- **THEN** it connects to the address it checked

#### Scenario: Redirects
- **WHEN** a response redirects
- **THEN** the egress client re-checks the redirect target, or follows no redirect

#### Scenario: Time and size limits
- **WHEN** the egress client sends a request
- **THEN** the time and the response size are limited

#### Scenario: Test action returns pass or fail only
- **WHEN** a test action runs
- **THEN** it returns pass or fail with a reason
- **AND** never the response body

#### Scenario: Controlled egress path in production
- **WHEN** a production service sends outbound traffic
- **THEN** it goes through a controlled egress path

#### Scenario: Connector manifests declare destinations
- **WHEN** a connector is installed
- **THEN** its manifest (FR-INT-25) declares its destinations

#### Scenario: G4-097 Test connection to an internal or rebinding address
- **WHEN** "Test connection", an OIDC discovery URL or a webhook points at the metadata address, loopback, a private range, an internal service name, a redirect to one of these, or a rebinding DNS name
- **THEN** the request is refused
- **AND** the test returns pass or fail only, never a response body

### Requirement: SEC-25 Penetration testing and DAST
The system SHALL have an annual independent penetration test and continuous DAST on staging.

Source: PRD-001 §13.3 · Priority S · Phase P2

#### Scenario: Annual independent penetration test
- **WHEN** a year passes since the last independent penetration test
- **THEN** a new independent penetration test is done

#### Scenario: Penetration test before SaaS launch
- **WHEN** the first external customer is to be onboarded
- **THEN** an independent penetration test has been done before it

#### Scenario: Continuous DAST on staging
- **WHEN** staging is running
- **THEN** DAST runs against it continuously

### Requirement: SEC-26 Dependency hygiene
Dependencies SHALL be kept current by automated update pull requests and pinned versions, with an SBOM generated per release.

Source: PRD-001 §13.3 · Priority M · Phase P1

#### Scenario: Automated update pull requests
- **WHEN** a newer version of a dependency is published
- **THEN** an automated pull request proposes the update

#### Scenario: Pinned versions
- **WHEN** a dependency is declared
- **THEN** its version is pinned

#### Scenario: SBOM per release
- **WHEN** a release is built
- **THEN** an SBOM is generated for it

### Requirement: SEC-27 Hardened images
Images SHALL be hardened per §12.9 (NFR-IMG-01…07): distroless, non-root, read-only root filesystem where the platform supports it (with NFR-IMG-02's equivalent controls on Cloud Run), no shell, digests pinned, signed.

Source: PRD-001 §13.3 · Priority M · Phase P0

#### Scenario: Hardened image
- **WHEN** a service image is built
- **THEN** it is distroless, runs as non-root and has no shell
- **AND** its root filesystem is read-only where the platform supports it, and on Cloud Run NFR-IMG-02's equivalent controls apply
- **AND** its base image is pinned by digest
- **AND** it is signed

### Requirement: SEC-28 CI supply chain
CI SHALL pin third-party GitHub Actions by commit SHA, give workflow tokens least privilege, expose no secrets to pull requests from forks, and run release and deploy workflows only on `main` and tags.

Source: PRD-001 §13.3 · Priority M · Phase P0

#### Scenario: Actions pinned by commit SHA
- **WHEN** a workflow uses a third-party GitHub Action
- **THEN** the action is pinned by commit SHA

#### Scenario: Least-privilege workflow tokens
- **WHEN** a workflow job runs
- **THEN** its token has only the permissions its `permissions:` block grants for that job

#### Scenario: No secrets for pull requests from forks
- **WHEN** CI runs on a pull request from a fork
- **THEN** no secrets are available to it

#### Scenario: Release and deploy only from main and tags
- **WHEN** a release or deploy workflow is triggered
- **THEN** it runs only on `main` and tags
