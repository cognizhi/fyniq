# quality/test-gates Specification

## Purpose
The test gates every change to Fyniq passes besides the calculation oracles: API contracts, the MCP security suite, local mode in CI, image hardening, UI components, themes, contrast, experience modes, help, tours and device classes, requirement traceability, authorisation, egress and file safety, end-to-end journeys and load. Derived from PRD-001 §16.2.

## Requirements

### Requirement: QA-API-01 Contract tests and backwards compatibility
Contract tests SHALL be run from the OpenAPI specification, and every change SHALL be checked for backwards compatibility.

Source: PRD-001 §16.2

#### Scenario: Contract tests from the specification
- **WHEN** the contract tests run
- **THEN** the API's behaviour is tested against the OpenAPI specification

#### Scenario: Backwards-compatibility check on every change
- **WHEN** a change is proposed
- **THEN** it is checked for backwards compatibility

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request that fails a contract test or the backwards-compatibility check
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-API-02 Breaking-change detection
`oasdiff` SHALL compare the OpenAPI document with the last release, and a breaking change inside `/v1` SHALL fail the build.

Source: PRD-001 §16.2

#### Scenario: Compared with the last release
- **WHEN** the build runs
- **THEN** `oasdiff` compares the OpenAPI document with the one of the last release

#### Scenario: Breaking change inside /v1
- **GIVEN** a pull request with a breaking change inside `/v1`
- **WHEN** the build runs
- **THEN** the build fails
- **AND** the merge is blocked

### Requirement: QA-API-03 No private endpoints
Every route the server registers SHALL be in the OpenAPI document, and the web app SHALL call only documented operations through the generated client.

Source: PRD-001 §16.2

#### Scenario: Undocumented route
- **GIVEN** a pull request that registers a route missing from the OpenAPI document
- **WHEN** CI runs
- **THEN** the merge is blocked

#### Scenario: Web app outside the generated client
- **GIVEN** a pull request in which the web app calls an undocumented operation or calls the API other than through the generated client
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-MCP-01 MCP security suite
An MCP security suite SHALL test the write, tool-surface, token, origin, injection, payroll-scope, rate-limit and kill-switch protections of the MCP server, and a failure SHALL block the merge.

Source: PRD-001 §16.2

#### Scenario: Agent-class tokens write only by propose
- **WHEN** the suite runs
- **THEN** an agent-class token cannot write except by `propose` (CTL-MCP-01)

#### Scenario: Tool surface and pinned definitions
- **WHEN** the suite runs
- **THEN** no tool outside the registry or on the never-list is exposed (CTL-MCP-02)
- **AND** the tool definitions match their pinned hashes

#### Scenario: Bad tokens rejected
- **WHEN** a token with the wrong audience, an expired token, a replayed token or a revoked token is presented
- **THEN** it is rejected

#### Scenario: Bad Origin rejected
- **WHEN** a request arrives with a bad `Origin`
- **THEN** it is rejected

#### Scenario: Injection strings never reach tool descriptions
- **GIVEN** injection strings placed in organisation data
- **WHEN** the tool descriptions are produced
- **THEN** the injection strings are not in them

#### Scenario: Payroll data needs the scope and the setting
- **WHEN** a client lacks the payroll scope or the payroll setting is off
- **THEN** payroll data is unreachable

#### Scenario: Rate limits and kill switches within 60 s
- **WHEN** a rate limit is reached or a kill switch is used
- **THEN** it takes effect within 60 s

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request that fails any test of the MCP security suite
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-LOCAL-01 Local mode in CI
A CI job with no cloud credentials SHALL run `make up` from the built images (the local flavour built from the same commit, ADR-T4), seed the sample company and run the smoke, golden and parity suites.

Source: PRD-001 §16.2

#### Scenario: Runs without cloud credentials
- **WHEN** the local-mode job runs
- **THEN** it has no cloud credentials
- **AND** it runs `make up` from the local flavour of the images built from the same commit

#### Scenario: Seeds and runs the suites
- **WHEN** the local stack is up
- **THEN** the job seeds the sample company
- **AND** runs the smoke, golden and parity suites

#### Scenario: What local mode proves
- **WHEN** the local-mode job passes
- **THEN** it proves domain behaviour, contracts, invariants and hardening flags
- **AND** it does not prove cloud adapters, IAM, Bucket Lock, WAF, Model Armor or live model quality, which need staging or live-eval evidence (R17)

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request for which the local-mode job fails
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-IMG-01 Image budgets and hardening
Production images SHALL be tested for their size budgets, a non-root user, no shell, pinned base digests and no local adapter symbols (§12.9, DEV-07).

Source: PRD-001 §16.2

#### Scenario: Image checks
- **WHEN** the production images are built
- **THEN** each is checked to be within its budget, to run as a non-root user, to have no shell, to use a pinned base digest and to contain no local adapter symbols

#### Scenario: A failing image blocks the merge
- **GIVEN** a pull request after which an image fails one of these checks
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-UI-01 Component tests
Component tests (Vitest and Testing Library) SHALL cover every shared component and every form's validation messages.

Source: PRD-001 §16.2

#### Scenario: Shared components and validation messages covered
- **WHEN** the component tests run
- **THEN** every shared component and every form's validation messages are tested

#### Scenario: Missing or failing component test blocks the merge
- **GIVEN** a pull request with a shared component or a form validation message that is untested, or a failing component test
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-UI-02 Storybook stories in every theme with no accessibility violations
Every shared component SHALL have Storybook stories in the light, dark and high-contrast themes, with the axe add-on reporting 0 violations.

Source: PRD-001 §16.2

#### Scenario: Stories in three themes
- **WHEN** a shared component's stories are built
- **THEN** they show it in the light, dark and high-contrast themes
- **AND** the axe add-on reports 0 violations

#### Scenario: A missing story or a violation blocks the merge
- **GIVEN** a pull request with a shared component lacking a story in one of the themes, or with an axe violation
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-UI-03 Token contrast and data-viz palette validators
The token contrast validator (`docs/design-system/.../scripts/check_contrast.py`, later `apps/web`) and the data-viz palette validator (FR-VIZ-07) SHALL pass on every pull request.

Source: PRD-001 §16.2

#### Scenario: Validators run
- **WHEN** a pull request runs through CI
- **THEN** the token contrast validator and the data-viz palette validator (FR-VIZ-07) run

#### Scenario: A failing validator blocks the merge
- **GIVEN** a pull request for which either validator fails
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-UI-04 Experience modes
Every route, field group and action SHALL be tagged in the feature registry; the same action SHALL send identical API requests in Basic and Expert mode; and safety information (FR-UX-24) SHALL be visible in both modes.

Source: PRD-001 §16.2

#### Scenario: Everything is tagged
- **WHEN** the feature registry is checked
- **THEN** every route, field group and action is tagged

#### Scenario: Identical API requests in both modes
- **WHEN** the same action is performed in Basic mode and in Expert mode
- **THEN** it sends identical API requests

#### Scenario: Safety information in both modes
- **WHEN** a page with safety information (FR-UX-24) is shown in Basic or Expert mode
- **THEN** the safety information is visible

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request with an untagged route, field group or action, a difference in API requests between modes, or safety information hidden in a mode
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-UI-05 Help coverage
Every route, field, column, chip and tile in the feature registry SHALL have a help key with text in both modes; every guide link SHALL resolve; and every route SHALL map to a guide page.

Source: PRD-001 §16.2

#### Scenario: Help keys with text in both modes
- **WHEN** help coverage is checked
- **THEN** every route, field, column, chip and tile in the feature registry has a help key with text in Basic and Expert mode

#### Scenario: Guide links and pages
- **WHEN** help coverage is checked
- **THEN** every guide link resolves
- **AND** every route maps to a guide page

#### Scenario: A gap blocks the merge
- **GIVEN** a pull request with a missing help key or text, a broken guide link or a route without a guide page
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-UI-06 Tours
An end-to-end test per role SHALL complete the first-run tour by keyboard only, with reduced motion and with animation, with 0 axe violations, and SHALL assert that nothing was written during the tour other than the user's own tour state (`user_help_state`).

Source: PRD-001 §16.2

#### Scenario: Tour completed per role
- **WHEN** the tour test runs for each role
- **THEN** it completes the first-run tour by keyboard only, once with reduced motion and once with animation
- **AND** axe reports 0 violations

#### Scenario: No writes except the tour state
- **WHEN** the tour test finishes
- **THEN** it asserts that nothing was written during the tour other than the user's own `user_help_state`

#### Scenario: G4-104 Tour completed and resumed
- **WHEN** the first-run tour is completed and resumed
- **THEN** only `user_help_state` is written
- **AND** no business data changes

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request for which a tour test fails
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-UI-07 Device classes and motion
Visual snapshots SHALL cover every P1 route at the five reference frames of §7.10, in light and dark; an end-to-end journey SHALL run per device class; and with motion off or reduced, no animation or transition SHALL run.

Source: PRD-001 §16.2

#### Scenario: Visual snapshots at the reference frames
- **WHEN** the visual snapshot tests run
- **THEN** every P1 route is captured at the five reference frames of §7.10, in light and dark

#### Scenario: Journey per device class
- **WHEN** the end-to-end tests run
- **THEN** a journey runs for each device class (compact, medium, expanded)
- **AND** it includes a fold or unfold resize that keeps an unsaved draft

#### Scenario: No motion when motion is off or reduced
- **WHEN** organisation motion is Off, Reduce motion is set, or `prefers-reduced-motion` is set
- **THEN** no animation or transition runs, and the computed durations are 0

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request for which a snapshot, journey or motion test fails
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-TRACE-01 Requirement traceability
CI SHALL extract every `M` requirement of the current phase from the PRD tables (ID, priority, phase) and list those no test references; the list MUST be empty at the phase gate. The extractor is a P0 script, `scripts/trace_requirements.py`.

Source: PRD-001 §16.2

#### Scenario: Requirement tags in tests
- **WHEN** a test references a requirement
- **THEN** it uses `// req: FR-GL-08` in Go and TypeScript, `# req:` in Python and `-- req:` in SQL

#### Scenario: Untested M requirements fail the phase gate
- **GIVEN** an `M` requirement of the current phase that no test references
- **WHEN** the phase gate is evaluated
- **THEN** the requirement is listed
- **AND** the phase gate fails

#### Scenario: S and C requirements reported
- **GIVEN** an `S` or `C` requirement that no test references
- **WHEN** the extractor runs
- **THEN** it is reported but does not block

#### Scenario: Unparseable row fails the extractor
- **WHEN** the extractor meets a requirement row it cannot parse
- **THEN** it fails

#### Scenario: Several phases and conditional priorities
- **WHEN** a Phase cell names several phases (`P1 (files) / P2 (APIs)`)
- **THEN** the row is listed at each phase it names
- **AND** a conditional priority (`M before GST registration; else S`) counts as M

#### Scenario: Recorded phase moves must match
- **WHEN** a row's text records a phase move (`rev 0.N: P1`)
- **THEN** it must match the row's Phase cell

#### Scenario: Invariants and controls covered elsewhere
- **WHEN** the extractor runs
- **THEN** INV-* rows are left to QA-ENG-05 and CTL-* rows to the control registry

#### Scenario: A tag counts only on a test of the acceptance criteria
- **WHEN** a `req:` tag is counted
- **THEN** it counts only on a test that asserts the requirement's acceptance criteria
- **AND** the phase-gate review samples tagged tests to check this

#### Scenario: G4-001 Phase move or unparseable Phase cell
- **WHEN** a requirement row says "(rev 0.N: P1" in its text while its Phase cell says P2, or has a Phase cell the extractor cannot parse
- **THEN** `scripts/trace_requirements.py` fails and names the row

### Requirement: QA-SEC-03 Authorisation matrix
An authorisation-matrix test SHALL cover every role × every endpoint × allowed/denied, including field masking and the segregation-of-duties rules per team mode.

Source: PRD-001 §16.2

#### Scenario: Every role and endpoint
- **WHEN** the authorisation-matrix test runs
- **THEN** each role is tested against each endpoint as allowed or denied
- **AND** field masking and the segregation-of-duties rules of each team mode are tested

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request for which an authorisation-matrix case fails
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-SEC-04 Egress tests
Egress tests SHALL check that outbound requests aimed at internal or changing targets are refused (SEC-36), and that a disabled connector commits nothing new (FR-INT-20).

Source: PRD-001 §16.2

#### Scenario: Requests to internal targets refused
- **WHEN** a test-connection, webhook or OIDC discovery request is aimed at the metadata address, loopback, a private range, an internal service name, a redirect to one of them, or a DNS name that changes between check and connect
- **THEN** the request is refused

#### Scenario: Disabled connector commits nothing new
- **GIVEN** a connector with queued and in-flight work
- **WHEN** the connector is disabled
- **THEN** nothing new is committed from that work

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request for which an egress test fails
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-SEC-05 File and export safety
File and export safety fixtures SHALL show that each hostile input is neutralised, rejected or bounded as FR-DOC-08, FR-ORG-09 and FR-RPT-05 require.

Source: PRD-001 §16.2

#### Scenario: Hostile inputs
- **WHEN** the fixtures for a formula-like contact name and note, a negative amount, an SVG with a script and an external reference, a ZIP bomb disguised as XLSX, a DOCX with an external entity, a renamed executable and a hostile file name are processed
- **THEN** each is neutralised, rejected or bounded as FR-DOC-08, FR-ORG-09 and FR-RPT-05 require

#### Scenario: A failure blocks the merge
- **GIVEN** a pull request for which a file or export safety fixture fails
- **WHEN** CI runs
- **THEN** the merge is blocked

### Requirement: QA-E2E-01 End-to-end journeys
Browser end-to-end tests SHALL cover the key journeys: set-up, record, sell, buy, pay, payroll, reconcile, close, GST, AI ask and AI propose → approve → execute.

Source: PRD-001 §16.2

#### Scenario: Key journeys in the browser
- **WHEN** the end-to-end tests run
- **THEN** they drive set-up, record, sell, buy, pay, payroll, reconcile, close, GST, AI ask and AI propose → approve → execute in a browser

#### Scenario: Gate at every release
- **WHEN** a release is prepared
- **THEN** the end-to-end journeys must pass

### Requirement: QA-PERF-01 Load tests
Load tests SHALL run at the NFR-PERF targets with a 10 M-line synthetic tenant.

Source: PRD-001 §16.2

#### Scenario: Load test with a large tenant
- **WHEN** the load tests run
- **THEN** they use a 10 M-line synthetic tenant
- **AND** they measure against the NFR-PERF targets

#### Scenario: Gate at every minor release
- **WHEN** a minor release is prepared
- **THEN** the load tests must meet the NFR-PERF targets
