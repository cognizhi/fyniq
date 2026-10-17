# Tasks

## 1. Templates and rendering

- [ ] 1.0 Redraw the template-editor canvas to the P1 half (theme, texts, optional blocks, live preview) before building it (`fyniq-openspec` Recipe C)
- [ ] 1.1 Template versions as JSON from the typed field catalogue, drafts, and immutable numbered versions published under approval, with rollback. Tests `// req: FR-TPL-02` (P1 part) and `// req: FR-TPL-04` (P1 part)
- [ ] 1.2 The render service compiles the JSON through owned Typst sources to PDF/A-2a and PDF/UA-1, with locked IRAS blocks inserted for tax-invoice types. Tests `// req: FR-TPL-03` and byte-identical snapshots per starter template
- [ ] 1.3 The document types and email templates with a default per type and a per-customer override; 3–4 starter templates; export and import of a versioned bundle. Tests `// req: FR-TPL-05` and `// req: FR-TPL-07`
- [ ] 1.4 The editor's P1 half as designed: theme, texts, optional blocks, live preview, Basic and Expert. Test `// req: FR-TPL-01` (P1 part)

## 2. Email

- [ ] 2.1 The `Email` port with ZeptoMail, Resend and SMTP adapters against fakes and Mailpit: a sender per purpose, the daily domain check, webhooks, outbox sends with reconciliation before retry, failover only for unaccepted sends, and a log without bodies. Test `// req: FR-INT-21` (G4-073)
- [ ] 2.2 The `InboundMail` port: messages stored with their authentication results, attachments through the evidence upload and scanner, and unknown senders quarantined. Test `// req: FR-INT-22`
- [ ] 2.3 Interchangeable adapters and the §8.24 launch adapters and local substitutes for this change's capabilities. Tests `// req: communication/email-and-notifications Outside services behind interchangeable adapters` and `… Launch adapters and local substitutes`

## 3. Notifications

- [ ] 3.1 The notification centre and preferences (channels, digests, quiet hours; security notices always on). Tests `// req: FR-NTF-01` and `// req: FR-NTF-02`
- [ ] 3.2 Announcement banners for everyone (Super admin) and per organisation (Owner, Administrator), with critical banners non-dismissable and publishing audited. Test `// req: FR-NTF-03`
- [ ] 3.3 One event stream feeding every channel, with delivery attempts logged and retried and a test message per channel. Test `// req: FR-NTF-05`

## 4. Screens

- [ ] 4.1 Build announcements, banners, document-templates, notification-centre, notification-preferences and template-editor as designed. Add help keys and `docs/user-guide/` pages compiled into `kb/`
- [ ] 4.2 Finish with `make check` and `make test`
