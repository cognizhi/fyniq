# Tasks

## 1. Templates and rendering

- [ ] 1.0 Redraw the template-editor canvas to the P1 half (theme, texts, optional blocks, live preview) before building it (`fyniq-openspec` Recipe C)
- [x] 1.1 Template versions as JSON from the typed field catalogue, drafts, and immutable numbered versions published under approval, with rollback. Tests `// req: FR-TPL-02` (P1 part) and `// req: FR-TPL-04` (P1 part)
  (23 Oct 2026, PRD-001 rev 0.95: `internal/domain/doctemplate` holds the content (a document's page, theme and blocks, or an
  email's subject and body), the typed field catalogue per document type with its date and money formats, the locked IRAS
  blocks of invoices and credit notes from IRAS's "Invoicing customers", the accent's contrast in integer arithmetic, and the
  rules TPL-01…15. Migrations 000097 and 000098: `document_template` (the draft, saved over at the next version) and
  `document_template_version` (numbered, with the SHA-256 of the RFC 8785 JSON; never changes, FY176), and the approval
  object `document_template.publish`. Nine operations under the tag Templates; in Founder mode an approver publishes alone for
  the weekly review, otherwise another approves, never the draft's saver (SOD-3); a rollback republishes an earlier version
  and leaves the draft. Tests: `TestTemplateVersionsNeverChange`, `TestDocumentTemplatesThroughTheAPI`,
  `TestTemplatesArePublishedByApprovalInStandardMode`, the domain tests and nine matrix rows. Left for 1.2: rendering test
  documents at publication and the version and hash on each issued document.)
- [x] 1.2 The render service compiles the JSON through owned Typst sources to PDF/A-2a and PDF/UA-1, with locked IRAS blocks inserted for tax-invoice types. Tests `// req: FR-TPL-03` and byte-identical snapshots per starter template
  (23 Oct 2026, first part, PRD-001 rev 0.96, on the owner's decision of 23 Oct 2026: a private service at min
  instances 0, not a job. `internal/render` lays a version and a document's data out in Go (fields filled and formatted,
  each block's lines, rows and columns, the IRAS particular each carries; `render.Required` per document type);
  `internal/render/typst` compiles the one owned source in a sandboxed folder to PDF/A-2a and PDF/UA-1 with the vendored
  fonts, the same bytes each time (snapshot hashes per default template in `testdata/sha256.golden`), and finds the
  particulars with `typst query`; `cmd/render` serves POST /render to the render class's tokens only (local HMAC; in
  Google Cloud `gcp.CallerTokens`, the api's and the worker's accounts). The render image is the Dockerfile's `render`
  target: distroless/static (the cc base's glibc and OpenSSL failed the image scan), Typst 0.15.1 checked against its SHA-256, 30 MB. Second part, rev 0.97: previewDocumentTemplate; test documents at publication (TPL-16, TPL-17); the callers' ID tokens from the metadata server (`gcp.IdentityTokens`); OpenTofu's run.invoker for the api and the worker only, and the render entry of FYNIQ_GCP.)
- [ ] 1.3 The document types and email templates with a default per type and a per-customer override; 3–4 starter templates; export and import of a versioned bundle. Tests `// req: FR-TPL-05` and `// req: FR-TPL-07`
- [ ] 1.4 The editor's P1 half as designed: theme, texts, optional blocks, live preview, Basic and Expert. Test `// req: FR-TPL-01` (P1 part)

## 2. Email

- [ ] 2.1 The `Email` port with ZeptoMail, Resend and SMTP adapters against fakes and Mailpit: a sender per purpose, the daily domain check, webhooks, outbox sends with reconciliation before retry, failover only for unaccepted sends, and a log without bodies. Test `// req: FR-INT-21` (G4-073)
  (20 Oct 2026, first slice, PRD-001 rev 0.39: the `Email` port (`internal/ports/email.go`) and the SMTP adapter
  (`internal/adapters/smtp`: Mailpit locally, STARTTLS only elsewhere, header injection refused, a Message-ID per message;
  a 4xx or 5xx answer to the data is a refusal, a broken connection after it is unknown). Migration 000038 adds
  `email_message` (purpose, recipient, subject, provider, attempts and outcome, never the body; an outcome never changes,
  FY130; the API only queues, the worker only records attempts and outcomes). `internal/email` queues a send in the outbox
  and the worker sends it once: a refusal is retried and fails on the last attempt; an attempt that may have handed the
  message over is reconciled first, and when the provider cannot say (SMTP) the message is unknown and never resent.
  `POST /v1/organisation/email/test-messages` sends only to the organisation's own address (EML-01 without one) and
  `GET /v1/organisation/email/messages/{message_id}` shows the outcome. Left: ZeptoMail and Resend, a sender per
  purpose, the daily domain check, webhooks and suppression, failover, the staging sandboxes, and the test send's help
  page with the connector-detail screen.)
- [ ] 2.2 The `InboundMail` port: messages stored with their authentication results, attachments through the evidence upload and scanner, and unknown senders quarantined. Test `// req: FR-INT-22`
- [ ] 2.3 Interchangeable adapters and the §8.24 launch adapters and local substitutes for this change's capabilities. Tests `// req: communication/email-and-notifications Outside services behind interchangeable adapters` and `… Launch adapters and local substitutes`

## 3. Notifications

- [ ] 3.1 The notification centre and preferences (channels, digests, quiet hours; security notices always on). Tests `// req: FR-NTF-01` and `// req: FR-NTF-02`
- [ ] 3.2 Announcement banners for everyone (Super admin) and per organisation (Owner, Administrator), with critical banners non-dismissable and publishing audited. Test `// req: FR-NTF-03`
- [ ] 3.3 One event stream feeding every channel, with delivery attempts logged and retried and a test message per channel. Test `// req: FR-NTF-05`

## 4. Screens

- [ ] 4.1 Build announcements, banners, document-templates, notification-centre, notification-preferences and template-editor as designed. Add help keys and `docs/user-guide/` pages compiled into `kb/`
- [ ] 4.2 Finish with `make check` and `make test`
