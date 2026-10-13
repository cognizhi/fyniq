# platform/release-and-versioning Specification

## Purpose
How Fyniq is versioned and released: Conventional Commits, release-please releases, tags and changelog, upgrade notes, the review and approval of a release, and the versioning scheme of the platform, the public API, events, database migrations, statutory packs, the AI catalogue, images, the knowledge base and the PRD. Derived from PRD-001 §12.10 (ADR-T5, part 2).

## Requirements

### Requirement: Release automation with release-please
Commit messages SHALL follow Conventional Commits 1.0, and release-please (GitHub Action, manifest mode) SHALL read them on every push to `main` and keep a release pull request open with the next version and the generated `CHANGELOG.md` entry.

Source: PRD-001 §12.10

#### Scenario: Release pull request kept open
- **WHEN** a commit is pushed to `main`
- **THEN** release-please updates the open release pull request with the next version and the generated `CHANGELOG.md` entry

#### Scenario: Merging the release pull request
- **WHEN** the release pull request is merged
- **THEN** the release is tagged `vX.Y.Z` and a GitHub release is published
- **AND** the GitHub release triggers the image builds and the staging deployment

#### Scenario: Release tooling in the repository
- **WHEN** the repository is checked
- **THEN** it holds `release-please-config.json`, `.release-please-manifest.json`, `.github/workflows/release-please.yml` and a commit-message check on pull requests

### Requirement: Platform version
The platform (all services and the web app) SHALL carry one SemVer version `MAJOR.MINOR.PATCH`, `0.x` until go-live (P1) and `1.0.0` at go-live, kept in `VERSION`, `.release-please-manifest.json` and the tag `vX.Y.Z`.

Source: PRD-001 §12.10

#### Scenario: Bump from commit types
- **WHEN** release-please computes the next version
- **THEN** `fix` bumps the patch, `feat` bumps the minor, and `!` or `BREAKING CHANGE:` bumps the major

#### Scenario: Breaking change while 0.x
- **GIVEN** the platform version is `0.x`
- **WHEN** a release contains `!` or `BREAKING CHANGE:`
- **THEN** the minor version is bumped

#### Scenario: Go-live version
- **WHEN** the platform goes live (P1)
- **THEN** its version is `1.0.0`

### Requirement: Public API version
The public API SHALL be versioned by path major (`/v1`) plus the OpenAPI `info.version`, which equals the platform version, with `contracts/openapi/` as the source of truth.

Source: PRD-001 §12.10

#### Scenario: New path major only for breaking changes
- **WHEN** the public API changes
- **THEN** a new path major is introduced only for a breaking change (FR-INT-06)

#### Scenario: oasdiff blocks accidental breaks
- **WHEN** a change to `contracts/openapi/` breaks the API without a new path major
- **THEN** `oasdiff` blocks it

#### Scenario: OpenAPI version follows the platform
- **WHEN** a platform version is released
- **THEN** the OpenAPI `info.version` equals the platform version

### Requirement: Event schema versions
Each event type SHALL carry its own version (`type: fyniq.invoice.posted.v1`), with `contracts/events/` as the source of truth.

Source: PRD-001 §12.10

#### Scenario: Breaking event change gets a new version
- **WHEN** an event schema changes in a breaking way
- **THEN** the event type gets a new version

### Requirement: Database schema migrations
The database schema SHALL change only by ordered goose migrations (`db/migrations/NNNNNN_name.sql`), expand-and-contract, never edited after merge.

Source: PRD-001 §12.10

#### Scenario: Each schema change is a migration
- **WHEN** the database schema changes
- **THEN** the change is a new ordered goose migration in `db/migrations/`, following expand-and-contract

#### Scenario: Merged migrations are not edited
- **GIVEN** a migration has been merged
- **WHEN** the schema needs a further change
- **THEN** a new migration is added and the merged one is not edited

### Requirement: Statutory parameter pack versions
Statutory parameter packs SHALL be versioned `<pack>@<year>.<n>` (e.g. `sg-cpf@2027.1`) with an effective date, as data in `packs/`, released independently (ENG-32/33).

Source: PRD-001 §12.10

#### Scenario: Pack version and effective date
- **WHEN** a new version of a statutory pack is released
- **THEN** it is named `<pack>@<year>.<n>` and carries an effective date

#### Scenario: Independent release
- **WHEN** a statutory pack changes
- **THEN** it is released independently of the platform version (ENG-32/33)

### Requirement: AI catalogue, prompt and tool versions
Each entry of the AI task catalogue, prompts and tools SHALL carry a `task_version`, `prompt_version` or `tool_version`, with `contracts/ai/` as the source of truth.

Source: PRD-001 §12.10

#### Scenario: Any change bumps the version
- **WHEN** a catalogue entry, prompt or tool changes in any way
- **THEN** its version is bumped

#### Scenario: Gated by the evals
- **WHEN** a catalogue, prompt or tool version changes
- **THEN** the change is gated by the evals (CMP-AI-03)

### Requirement: Image tags and deployment by digest
Each image SHALL be tagged `vX.Y.Z` and `sha-<git sha>` in Artifact Registry by the release workflow, and SHALL be deployed by digest.

Source: PRD-001 §12.10

#### Scenario: Release workflow tags images
- **WHEN** the release workflow builds an image
- **THEN** it pushes the image to Artifact Registry tagged both `vX.Y.Z` and `sha-<git sha>`

#### Scenario: Deployed by digest
- **WHEN** an image is deployed
- **THEN** it is referenced by digest, not by tag

### Requirement: Knowledge base versions
Platform knowledge-base pages SHALL be kept in git (`kb/`) stamped with the platform version in page format `kb-schema@N`, and organisation pages SHALL be kept in the database as append-only versions and numbered snapshots (AI-56).

Source: PRD-001 §12.10

#### Scenario: Every source change is compiled, indexed and linted
- **WHEN** a knowledge source changes
- **THEN** the affected pages are compiled, indexed and linted

#### Scenario: Schema or compiler model change
- **WHEN** the page schema or the compiler model changes
- **THEN** the knowledge base is fully recompiled
- **AND** QA-AI-06 runs

### Requirement: PRD revisions
The PRD SHALL be revised as `0.N` before go-live and `1.N` after, with each reviewed change recorded in the revision history at its end.

Source: PRD-001 §12.10

#### Scenario: Revision numbering
- **WHEN** a reviewed change to the PRD is made
- **THEN** its revision number is `0.N` before go-live, or `1.N` after
- **AND** the change is recorded in the revision history at the end of the PRD

### Requirement: REL-01 Conventional Commits on main
Every commit on `main` SHALL follow Conventional Commits, and pull request titles SHALL be checked in CI because squash merges use them.

Source: PRD-001 §12.10 · Priority M · Phase P0

#### Scenario: Types and scopes
- **WHEN** a commit message is checked
- **THEN** its type is one of `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `revert` or `deps`
- **AND** its scopes are those in `AGENTS.md`

#### Scenario: Pull request title checked
- **WHEN** a pull request is opened or its title is edited
- **THEN** CI checks that the title is a Conventional Commit

### Requirement: REL-02 release-please owns versions and the changelog
release-please SHALL manage versions, tags, GitHub releases and `CHANGELOG.md`, and nobody SHALL edit versions or the changelog by hand.

Source: PRD-001 §12.10 · Priority M · Phase P0

#### Scenario: Versions and changelog come from release-please
- **WHEN** a version, a tag, a GitHub release or `CHANGELOG.md` changes
- **THEN** release-please made the change
- **AND** no one edited the version or the changelog by hand

### Requirement: REL-03 Upgrade notes
Upgrade notes for anything an operator, integrator or user must do SHALL go in the commit body.

Source: PRD-001 §12.10 · Priority M · Phase P0

#### Scenario: Notes in the commit body
- **WHEN** a commit requires an operator, integrator or user to do something
- **THEN** the commit body says what, in a `BREAKING CHANGE:` footer or an "Upgrade notes:" paragraph

#### Scenario: Upgrade notes paragraph copied to the changelog
- **WHEN** a commit body has an "Upgrade notes:" paragraph
- **THEN** the release pull request copies it into the changelog

### Requirement: REL-04 Release review and production approval
The release pull request SHALL be reviewed like any other change (CTL-CHG-01), and merging it SHALL be the release decision.

Source: PRD-001 §12.10 · Priority M · Phase P1

#### Scenario: Release pull request reviewed
- **WHEN** the release pull request is ready
- **THEN** it is reviewed like any other change (CTL-CHG-01)
- **AND** merging it is the release decision

#### Scenario: Manual approval for production
- **WHEN** a release is deployed to production
- **THEN** the deployment needs the manual approval of §12.4

### Requirement: REL-05 Version and git SHA recorded
`/version` (NFR-IMG-06), the web app's About dialog and every generated report (FR-RPT-08) SHALL record the platform version and git SHA.

Source: PRD-001 §12.10 · Priority M · Phase P1

#### Scenario: Version shown everywhere
- **WHEN** `/version` is called, the About dialog is opened or a report is generated
- **THEN** it shows the platform version and the git SHA

### Requirement: REL-06 release-please with a GitHub App token
release-please SHALL run with a GitHub App token, not the default `GITHUB_TOKEN`, once CI exists, so CI runs on release pull requests.

Source: PRD-001 §12.10 · Priority S · Phase P0

#### Scenario: CI runs on release pull requests
- **GIVEN** CI exists
- **WHEN** release-please opens or updates a release pull request
- **THEN** it does so with a GitHub App token
- **AND** CI runs on the release pull request
