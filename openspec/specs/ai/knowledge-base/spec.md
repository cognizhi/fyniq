# ai/knowledge-base Specification

## Purpose
How the AI assistant gets its context for each question and its know-how: the four context layers, the shared read-tool registry, and the compiled knowledge base (platform and organisation) with its sources, answering order, compile-index-lint cycle, grounding rules, trust and access, administration, versioning, compiler identity, search, and conversation memory. Derived from PRD-001 §9.11.

## Requirements

### Requirement: Context built fresh for every question
The assistant SHALL NOT learn the books or keep a copy of them: the orchestrator SHALL build the context fresh for every question, with figures always from tools and know-how from a compiled knowledge base.

Source: PRD-001 §9.11

#### Scenario: No copy of the books
- **WHEN** a question has been answered
- **THEN** the assistant keeps no copy of the books from it

#### Scenario: Figures always come from tools
- **WHEN** the assistant needs a figure
- **THEN** it gets the figure from a tool

#### Scenario: Know-how comes from the compiled knowledge base
- **WHEN** the assistant needs know-how
- **THEN** it gets it from the compiled knowledge base: a versioned set of Markdown pages, maintained by an LLM, compiled from trusted sources and navigated through an index

### Requirement: Context layers
The context for a question SHALL be built from four layers: fixed (cached), session, live facts and know-how, each from its own source.

Source: PRD-001 §9.11

#### Scenario: Layer 1, fixed and cached
- **WHEN** the context is built
- **THEN** layer 1 holds the rules, the never-list, the tool definitions, the glossary and the reader-filtered knowledge-base index
- **AND** it is versioned with the release and prompt-cached (AI-27)

#### Scenario: Layer 2, session
- **WHEN** the context is built
- **THEN** layer 2 holds the organisation (FY, currency, GST status, team mode, feature switches), the user's roles, Basic or Expert mode, today's date, and the current page and selected record ("Ask about this", FR-UX-17)
- **AND** it comes from the app at the moment of the question

#### Scenario: Layer 3, live facts
- **WHEN** the context is built
- **THEN** layer 3 holds figures, records, lineage, checks and search results
- **AND** they come from read tools on the semantic layer, on behalf of the user (AI-01, AI-03)

#### Scenario: Layer 4, know-how
- **WHEN** the context is built
- **THEN** layer 4 holds how features work, what statuses mean, statutory guides, and the organisation's own policies and decisions
- **AND** it comes from the compiled knowledge base, with the raw sources as the fallback

### Requirement: AI-48 Context assembly
The orchestrator SHALL assemble the context for each question in the four layers, and layer 2 SHALL be structured data, never free text from untrusted sources.

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: Assembled per question
- **WHEN** a user asks a question
- **THEN** the orchestrator builds the context for that question in the four layers

#### Scenario: Session layer is structured data
- **WHEN** layer 2 is built
- **THEN** it is structured data
- **AND** it contains no free text from untrusted sources

### Requirement: AI-49 One read-tool registry
The system SHALL keep one read-tool registry, shared with MCP (§9.7): `metrics.query`, `records.get`, `records.search`, `lineage.explain`, `checks.list`, `checks.explain`, `packs.get`, `calc`, `kb.index`, `kb.read` and `kb.search`, plus `propose` (§9.5).

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: What the tools do
- **WHEN** the registry is listed
- **THEN** `metrics.query` queries the semantic layer, `packs.get` returns the statutory values in force on a date, and `calc` is the calculator (AI-P6)

#### Scenario: Shared with MCP
- **WHEN** the in-app assistant and an MCP client list their tools
- **THEN** both come from the same registry

#### Scenario: Versioned and contract-tested
- **WHEN** a tool in the registry changes
- **THEN** it is versioned and contract-tested (AI-17)

### Requirement: AI-50 Two knowledge bases
The system SHALL keep two knowledge bases, platform and organisation, laid out by one schema file (`kb/schema.md`) that the repository skill and the runtime compiler both read.

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: Platform knowledge base sources
- **WHEN** the platform knowledge base is compiled
- **THEN** its sources are `docs/user-guide/`, the glossary, the checks catalogue and its messages, the help texts of the feature registry, statutory notes from the packs and the release notes

#### Scenario: Never from the PRD or internal documents
- **WHEN** the platform knowledge base is compiled
- **THEN** it is never compiled from the PRD or other internal documents

#### Scenario: Organisation pages rendered from settings without an LLM
- **WHEN** organisation pages are made from settings (approval policies, team mode, recommended-default decisions and their reasons)
- **THEN** they are rendered deterministically, with no LLM

#### Scenario: Organisation pages compiled from notes and policies
- **WHEN** an administrator writes notes or policies
- **THEN** the organisation knowledge base compiles pages from them with an LLM
- **AND** uploaded agreements and minutes are compiled from P2 (AI-54)

#### Scenario: Every page has front matter
- **WHEN** a page is written
- **THEN** its front matter holds its key, type, title, one-line summary, sources with SHA-256, data class, taint, compiler model and prompt version, and status

### Requirement: AI-51 Answering order
The assistant SHALL answer by reading the index, then `kb.read` for the relevant pages, falling back to `kb.search` over the raw sources when a page is stale or missing, and SHALL take figures and record states only from read tools.

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: Index first, then pages
- **WHEN** the assistant answers a know-how question
- **THEN** it reads the index, then the relevant pages with `kb.read`

#### Scenario: Raw sources when a page is stale or missing
- **WHEN** the relevant page is stale or missing
- **THEN** the assistant uses `kb.search` over the raw sources

#### Scenario: Figures and record states from read tools only
- **WHEN** an answer states a figure or a record's state
- **THEN** it comes from a read tool, not from a knowledge-base page

#### Scenario: Citations resolve to what the user can see
- **WHEN** an answer cites its sources to the user
- **THEN** each citation resolves to something the user can see (a guide page, a record, a document)
- **AND** never to a compiled page

#### Scenario: Page versions are logged
- **WHEN** an answer used knowledge-base pages
- **THEN** the AI activity log records the page versions used (AI-12)

### Requirement: AI-52 Compile, index, lint
The system SHALL compile, index and lint the knowledge base on every source change.

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: Compiling in the repository
- **WHEN** a source changes in the repository
- **THEN** an agent follows the `fyniq-knowledge-base` skill to compile it
- **AND** a person reviews the diff in the pull request

#### Scenario: Compiling at runtime
- **WHEN** a source changes at runtime
- **THEN** the runtime compiler job compiles it (AI-57)

#### Scenario: The index is generated, never hand-written
- **WHEN** pages change
- **THEN** a script regenerates the index from the pages' front matter, so it cannot drift

#### Scenario: Deterministic lint on every change
- **WHEN** the knowledge base changes
- **THEN** a deterministic lint checks front matter, source hashes, coverage of every source, links, citations and number grounding

#### Scenario: Number grounding in the deterministic lint
- **WHEN** the deterministic lint checks a number, including a number in a code span
- **THEN** the number must appear as a whole token in the section its paragraph cites
- **AND** passing proves only that the number exists in the source, not that the claim is right

#### Scenario: Semantic lint at each release and weekly
- **WHEN** a release is made, and weekly for organisation pages
- **THEN** a semantic lint looks for contradictions, superseded claims, orphan pages, missing pages and missing cross-references

#### Scenario: What happens to findings
- **WHEN** the lint finds a problem
- **THEN** it fails CI for the platform knowledge base
- **AND** it becomes an Inbox task for the organisation's knowledge base

### Requirement: AI-53 Grounding rules for pages
Knowledge-base pages SHALL contain no organisation figures, ever; every claim SHALL cite a source section, and every number in a page SHALL appear verbatim in one of its cited sources (deterministic lint).

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: No organisation figures
- **WHEN** a page is compiled or edited
- **THEN** it contains no organisation figures

#### Scenario: Statutory values with pack key and effective date
- **WHEN** a page states a statutory value
- **THEN** the value carries its pack key and effective date
- **AND** the assistant re-reads it with `packs.get` before stating it

#### Scenario: Statutory rules carry their pack version
- **WHEN** a page states a statutory rule (applicability, exceptions, effective dates)
- **THEN** the rule carries its pack key and version

#### Scenario: A newer pack is in force
- **GIVEN** the pack in force on the question's date is newer than the one a page's statutory rule cites
- **WHEN** the assistant answers
- **THEN** it answers from `packs.get` and says the guide may be out of date
- **AND** it states no rule it cannot confirm from the pack (fail closed)

#### Scenario: Claims cite sources and numbers appear verbatim
- **WHEN** the deterministic lint checks a page
- **THEN** every claim cites a source section
- **AND** every number in the page appears verbatim in one of its cited sources

#### Scenario: G4-091 Guide page outdated by a newer pack
- **GIVEN** a compiled guide page states a GST or ACRA rule that a newer pack has changed
- **WHEN** the assistant is asked about it
- **THEN** the assistant answers from `packs.get`, says the guide may be out of date and states no unconfirmed rule
- **AND** the semantic lint flags the page

### Requirement: AI-54 Trust and access
A page SHALL inherit the strictest data class and the taint of its sources, and the index, the pages and the raw-source fallback of `kb.search` SHALL be filtered per reader when the question is asked (AI-01, AI-02).

Source: PRD-001 §9.11 · Priority M · Phase P1 / P2 (documents)

#### Scenario: Pages inherit data class and taint
- **WHEN** a page is compiled from several sources
- **THEN** it takes the strictest data class of its sources and their taint

#### Scenario: Filtered per reader at question time
- **WHEN** a reader asks a question
- **THEN** the index, the pages and the raw-source fallback of `kb.search` are filtered for that reader at that moment
- **AND** no summary reveals a page the reader may not open

#### Scenario: Untrusted text stays data
- **WHEN** a page's text derives from untrusted sources
- **THEN** it is passed to the model as data (AI-18)

#### Scenario: P1 compiles trusted sources only
- **GIVEN** phase P1
- **WHEN** pages are compiled
- **THEN** only trusted sources are compiled

#### Scenario: Pages from uploaded documents need approval from P2
- **GIVEN** phase P2 or later
- **WHEN** a page compiled from uploaded documents is to be published
- **THEN** it needs an Owner's or Administrator's approval first

#### Scenario: Approval publishes but does not make a page trusted
- **WHEN** a page from uploaded documents is approved
- **THEN** it is published but keeps `taint: untrusted`
- **AND** its text stays data (AI-18)

#### Scenario: G4-093 Access to a source revoked
- **GIVEN** a reader's access to a source is revoked
- **WHEN** their question would reach that source through the raw-source fallback
- **THEN** no content, summary or citation from it reaches the reader

### Requirement: AI-55 Visibility and administration
Compiled pages and the index SHALL NOT be shown in the normal interface, and Settings → AI → Knowledge base SHALL let the Owner and the Administrator administer them, with every action audited (AUD-01).

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: Users see the user guide
- **WHEN** a user uses the normal interface
- **THEN** they see the user guide, not compiled pages or the index

#### Scenario: Owner and Administrator administer the knowledge base
- **WHEN** the Owner or the Administrator opens Settings → AI → Knowledge base
- **THEN** they can browse the index and pages with their sources, diff any two versions, edit, roll back, recompile and see lint findings

#### Scenario: The Auditor can view
- **WHEN** the Auditor opens Settings → AI → Knowledge base
- **THEN** they can view it

#### Scenario: Every action is audited
- **WHEN** anyone acts in Settings → AI → Knowledge base
- **THEN** the action is audited (AUD-01)

### Requirement: AI-56 Versioning
The platform knowledge base SHALL be versioned in git, and the organisation knowledge base SHALL keep append-only page versions and numbered snapshots.

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: Platform knowledge base in git
- **WHEN** the platform knowledge base changes
- **THEN** the change is a git diff, reviewed as a pull request and stamped with the release version
- **AND** rollback is by revert

#### Scenario: Organisation rollback creates a new version
- **WHEN** an organisation page is rolled back
- **THEN** a new version restoring the old content is created

#### Scenario: Publishing an organisation change is an approval object
- **WHEN** an organisation knowledge-base change is published
- **THEN** it is an approval object (FR-APR-01)
- **AND** by default Founder mode allows self-approval with the weekly review (FM-2, FM-3) (open question 18)

#### Scenario: Human edits survive recompiles
- **WHEN** an admin edits a page
- **THEN** a human-edited version is created that a recompile never overwrites
- **AND** lint flags a conflict between it and a recompile for a person to resolve

#### Scenario: Platform page edits are organisation overlays
- **WHEN** an organisation edits a platform page
- **THEN** the edit creates an overlay for that organisation only

#### Scenario: Contradictions are flagged before publishing
- **WHEN** an overlay or organisation page contradicts a current setting (FR-ORG-08), the pack in force or a newer platform page
- **THEN** it is flagged for the Owner before publishing
- **AND** until a person resolves it, the assistant follows the setting or the pack and says so

#### Scenario: G4-092 Page or overlay contradicts an approved setting
- **GIVEN** an organisation policy page or overlay contradicts an approved setting
- **WHEN** it is about to be published
- **THEN** it is flagged before publishing
- **AND** until a person resolves it, the assistant follows the setting and says so

### Requirement: AI-57 Compiler identity
The runtime compiler SHALL be a Cloud Run job with its own service identity that reads sources and writes knowledge-base draft versions only.

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: The compiler writes drafts only
- **WHEN** the runtime compiler runs
- **THEN** it reads sources and writes draft versions of knowledge-base pages
- **AND** it cannot publish, read the ledger or call domain commands

#### Scenario: The orchestrator stays without write grants
- **WHEN** the compiler is deployed
- **THEN** the orchestrator still holds no write grant (CMP-AI-01)

### Requirement: AI-58 Search and scale
The assistant SHALL navigate the knowledge base index-first, and `kb.search` SHALL use PostgreSQL full-text search, with no embeddings or vector search in any phase.

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: Index-first while the index fits in 8,000 tokens
- **WHEN** the reader's index fits in 8,000 tokens
- **THEN** the assistant works index-first from the whole index

#### Scenario: Sectioned index above 8,000 tokens
- **WHEN** the reader's index exceeds 8,000 tokens
- **THEN** the assistant gets a top-level index of sections plus `kb.index(section)`

#### Scenario: Full-text search over pages, aliases and raw sources
- **WHEN** `kb.search` runs
- **THEN** it uses PostgreSQL full-text search (`tsvector`, `pg_trgm`) over the compiled pages, their `aliases` and the raw sources

#### Scenario: No embeddings or vector search
- **WHEN** knowledge-base retrieval is built, in any phase
- **THEN** it uses no embeddings or vector search

#### Scenario: Retrieval misses are fixed in the compile step
- **WHEN** a retrieval miss is found
- **THEN** it is fixed in the compile step: a better summary, an alias for the user's wording, a cross-link, a split index or pages per language

#### Scenario: Embeddings elsewhere need their own ADR
- **WHEN** embeddings are proposed for any other use in the platform
- **THEN** that use needs its own ADR

### Requirement: AI-59 No automatic filing of answers
Answers SHALL never become knowledge-base pages by themselves, because they carry one user's context and the organisation's figures.

Source: PRD-001 §9.11 · Priority C · Phase P2

#### Scenario: Answers are not filed automatically
- **WHEN** the assistant gives an answer
- **THEN** the answer does not become a knowledge-base page by itself

#### Scenario: Proposing a know-how page from P2
- **GIVEN** phase P2
- **WHEN** a conversation produces useful know-how
- **THEN** the AI may propose a know-how page from it (`kb.propose_page`, tier 1, without figures)
- **AND** an Owner or Administrator approves it

### Requirement: AI-60 Conversation memory
Conversations SHALL be kept in the AI activity log for their user (AI-12), and there SHALL be no hidden long-term memory.

Source: PRD-001 §9.11 · Priority M · Phase P1

#### Scenario: Conversations kept for their user
- **WHEN** a conversation ends
- **THEN** it is kept in the AI activity log for its user (AI-12)

#### Scenario: Remembering across conversations needs a person's approval
- **WHEN** the assistant should remember something across conversations
- **THEN** it becomes a setting or a knowledge-base page that a person approved
- **AND** it is not kept in any hidden memory
