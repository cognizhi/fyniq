---
key: settings.sandbox
type: task
title: Test the API in a sandbox organisation
summary: What a sandbox organisation is, who makes one and how, what it holds, what it never sends outside Fyniq, and what is refused inside it
sources:
  - docs/user-guide/sandbox-organisation.md sha256:ab255ce2899c8f35bb03461978c1ad75eb066a3029524e576f39c2772f94f7c7
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - sandbox
  - sandbox organisation
  - test the API
  - integration testing
  - sample company for developers
---

# Test the API in a sandbox organisation

## What it is

- A sandbox organisation is a separate organisation of the installation, named "Sandbox: …", holding only the synthetic sample company, for testing the API, webhooks and SDKs without real data.
- It sends nothing outside Fyniq: email goes to a sink, no payment provider, bank, InvoiceNow access point, AI model or connector is reached, and a webhook goes only to a host the deployment's operator allowed.
- It is made new and stays a sandbox: no existing organisation becomes one, and a sandbox never becomes a real organisation.

(src: docs/user-guide/sandbox-organisation.md#what-it-is)

## Find it

- Sandboxes exist only where the deployment's operator turned them on; otherwise the answer is API-SANDBOX-01.
- Only a Super admin makes one, with step-up; with two Super admins the other approves it and the one who asked cannot. The maker is its Owner, finds it in the organisation switcher and makes its integration clients there; they reach the sandbox only.

(src: docs/user-guide/sandbox-organisation.md#find-it)

## What is different inside

- Its books are the sample company's chart, periods and journals; `loadSandboxSampleCompany` loads them again if the first load failed, and refuses an organisation that has books.
- A connector, a practice organisation and a webhook to a host that is not allowed are refused with API-SANDBOX-02; background work that could leave Fyniq is ended without running.

(src: docs/user-guide/sandbox-organisation.md#what-is-different-inside)

## Rules to know

- Every sandbox is in the installation's audit trail and every Super admin is told when one is made; do not put real data in a sandbox.

(src: docs/user-guide/sandbox-organisation.md#rules-to-know)
