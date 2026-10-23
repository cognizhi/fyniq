# Tasks

## 1. Templates and rendering

- [x] 1.0 Redraw the template-editor canvas to the P1 half (theme, texts, optional blocks, live preview) before building it (`fyniq-openspec` Recipe C)
  (23 Oct 2026: TemplateEditor and DocumentTemplates redrawn as built through rev 0.99 and published to the settings-admin
  canvas: the four styles (a select on phones), the theme and page with the contrast check, blocks shown or hidden with locked
  IRAS blocks and texts with field chips, the field catalogue and formats, the sample preview, the publish checks, versions
  with roll back; the list's defaults, the platform starter, the four-style gallery and JSON bundles. P2 parts stay tagged.)
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
- [x] 1.3 The document types and email templates with a default per type and a per-customer override; 3–4 starter templates; export and import of a versioned bundle. Tests `// req: FR-TPL-05` and `// req: FR-TPL-07`
  (23 Oct 2026, first part, PRD-001 rev 0.98, on the owner's decisions of 23 Oct 2026: the theme's style, drawn by the
  render service (classic, band, minimal, compact), snapshot hashes per style and document type; the four starters in the
  catalogue and createDocumentTemplate's `starter`; exportDocumentTemplates and importDocumentTemplates (TPL-18). Test
  `TestTemplateStartersAndBundles`. Second part, rev 0.99: the default per type (an approver, no request), per-customer and
  per-supplier overrides on the contact (TPL-20), and the platform's classic starter as version 0; migration 000099 (FY177,
  FY178); `doctemplate.Applying` resolves contact, default, platform. Tests `TestTemplateChoicesAreAppendOnly`,
  `TestTemplateDefaultsAndOverrides` and five matrix rows.)
- [x] 1.4 The editor's P1 half as designed: theme, texts, optional blocks, live preview, Basic and Expert. Test `// req: FR-TPL-01` (P1 part)
  (23 Oct 2026, PRD-001 rev 0.100: `apps/web/src/screens/settings/TemplateEditor.tsx` at /settings/templates/$templateId:
  the style, the theme and page with the accent's contrast, blocks shown or hidden with the locked IRAS blocks, texts with a
  field picker that inserts `{{path|format}}` at the cursor, the fields, a live preview 600 ms after the last change, publish
  or ask for approval, versions with roll back. The preview now fills the organisation's own details and GST number in force
  and returns PNG pages at 96 ppi (`render.Request.Pages`; the CSP embeds no PDF). The P1 editor is the same in both modes,
  its Expert half being P2. Tests: `Templates.test.tsx` with axe, `TestPreviewPagesArePNGs`, `TestDocumentTemplatesThroughTheAPI`.)

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
  page with the connector-detail screen. Second slice, 23 Oct 2026, PRD-001 rev 0.102, on the owner's decision of that
  day (the deployment sets the providers, their keys and the sending domains; each organisation its senders per
  purpose): migration 000100 (`email_sender`, FY179; an attempt records its provider), getEmailSettings and
  setEmailSender (EML-02, EML-03), `internal/adapters/zeptomail` and `internal/adapters/resend` against fakes, the
  worker's failover after a refusal only, `wiring.ReadMail`. Tests: `TestTheFailoverSendsOnlyWhatThePrimaryRefused`,
  `TestAnEmailIsSentFromTheOrganisationsSenderOfItsPurpose`, `TestASenderChoiceIsAppendOnly`,
  `TestAnOrganisationChoosesItsSendersOnTheDeploymentsDomains`, the adapters' and two matrix rows. Third slice, rev
  0.103: `internal/mailhooks` (Resend's Svix signature, ZeptoMail's producer-signature `[verify]`), a reference per
  message, migration 000101 (`email_event` FY180, `email_suppression` FY181), the worker skipping suppressed addresses,
  listEmailSuppressions and releaseEmailSuppression (EML-04), the threat model `email-delivery.md`. Tests:
  `TestTheProvidersWebhooksRecordEventsAndSuppressHardBounces`, `TestAnEmailToASuppressedAddressIsNotSent`,
  `TestEmailEventsAndSuppressionsKeepTheirHistory`, the receiver's unit tests and two matrix rows. Fourth slice, rev
  0.104: `internal/maildns` (the records from `FYNIQ_MAIL_DNS`, the DMARC policy), getEmailSettings' `domain_checks`
  cached for an hour, the platform job `email.domains` at 04:30 recording a failed check per domain not ok. Tests:
  `TestASendingDomainIsCheckedRecordByRecord`, `TestTheCheckIsCachedForItsTTL`,
  `TestTheNightlyDomainCheckRecordsEachDomainThatIsNotOK`, `TestTheSendingDomainsRecordsAreConfiguration`,
  `TestEveryPlatformScheduleHasItsJob`. Left: the staging sandboxes (operations, at the cloud gate), and the email
  settings screen with its help, which the Super admin console and the connector-detail screen show (super-admin-console,
  connector-detail).)
- [ ] 2.2 The `InboundMail` port: messages stored with their authentication results, attachments through the evidence upload and scanner, and unknown senders quarantined. Test `// req: FR-INT-22`
  (23 Oct 2026, first slice, PRD-001 rev 0.105, on the owner's decisions of that day: readable names on the capture
  domain, members and contacts as known senders. Migration 000102: `capture_address` (FY182), the global registry
  `capture_name` read through `capture_route` (FY183), `capture_sender` (FY184); getCaptureAddresses,
  setOrganisationCaptureAddress, setMyCaptureAddress (EML-05 to EML-08); `FYNIQ_CAPTURE_DOMAIN`. Tests:
  `TestAnEmailInAddressNameBelongsToOneOrganisation`, `TestEmailInAddressesAndSenderDecisionsKeepTheirHistory`,
  `TestEmailInAddressesHaveReadableNames` and three matrix rows. Left: receiving through Resend, attachments through
  the evidence upload and scanner, the quarantine and its decisions, the screens.)
- [ ] 2.3 Interchangeable adapters and the §8.24 launch adapters and local substitutes for this change's capabilities. Tests `// req: communication/email-and-notifications Outside services behind interchangeable adapters` and `… Launch adapters and local substitutes`

## 3. Notifications

- [ ] 3.1 The notification centre and preferences (channels, digests, quiet hours; security notices always on). Tests `// req: FR-NTF-01` and `// req: FR-NTF-02`
- [ ] 3.2 Announcement banners for everyone (Super admin) and per organisation (Owner, Administrator), with critical banners non-dismissable and publishing audited. Test `// req: FR-NTF-03`
- [ ] 3.3 One event stream feeding every channel, with delivery attempts logged and retried and a test message per channel. Test `// req: FR-NTF-05`

## 4. Screens

- [ ] 4.1 Build announcements, banners, document-templates, notification-centre, notification-preferences and template-editor as designed. Add help keys and `docs/user-guide/` pages compiled into `kb/`
  (23 Oct 2026, part, rev 0.100: document-templates (`Templates.tsx` at /settings/templates: each type's document and email
  default chosen over the choice's ETag, the templates, the four-style gallery, waiting publications, the bundle in Expert
  mode) and template-editor, with the help key `settings.templates`, `docs/user-guide/document-templates.md` and
  `kb/pages/task/settings-templates.md`. Still to build: announcements, banners, notification-centre,
  notification-preferences. Rev 0.101: a contact's own templates, Templates next to a contact in Contacts, with the
  guide's "Their own templates"; the contact record's page (`p1-organisation-chart`) takes the same choices.)
- [ ] 4.2 Finish with `make check` and `make test`
