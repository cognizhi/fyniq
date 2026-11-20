---
key: settings.sandbox
title: Test the API in a sandbox organisation
mode: expert
roles: [owner, admin]
requirements: [FR-INT-18]
status: draft            # written with the sandbox's operations; reviewed when the console screen is built
---

# Test the API in a sandbox organisation

## What it is

- A sandbox organisation is a separate organisation of this installation, named **Sandbox: …**, that holds only the **synthetic sample company**: no real books, people or bank details. An
  integrator tests the API, the webhooks and the Go and Python SDKs against it without touching real data.
- It sends **nothing outside Fyniq**. Email goes to a sink and is never delivered; no payment provider, bank, InvoiceNow access point, AI model or connector is reached; a webhook goes only to a
  host the person who runs this deployment has allowed (none unless they listed one).
- It is made new and stays a sandbox. No existing organisation becomes one, and a sandbox never turns into a real organisation.

## Find it

- Sandboxes exist only where the person who runs the deployment turned them on (`FYNIQ_SANDBOX=on`). Where they are off, asking for one answers **API-SANDBOX-01**.
- Only a **Super admin** makes one, with a passkey or authenticator code, with the API operation `createSandboxOrganisation`. With two Super admins the other one approves it (the same approval as
  for a private endpoint), and the Super admin who asked cannot.
- The Super admin who makes it is its Owner. They find it in the organisation switcher, marked as a sandbox, and make its **integration clients** and tokens there, as in any organisation. Its clients
  reach the sandbox and nothing else.

## What is different inside

- Its books are the sample company's chart, periods and journals, loaded through the same posting engine as real books. If the load failed, `loadSandboxSampleCompany` loads it again; it refuses an
  organisation that has books.
- You cannot install a connector, open a practice organisation or add a webhook to a host that is not allowed. The refusal is **API-SANDBOX-02** and says what to do.
- Background work that could leave Fyniq (a bank sync, a payment link, a provider's events, the AI, an inbound fetch) is ended without running.

## Rules to know

- Every sandbox is in the installation's audit trail with who made it, and every Super admin is told when one is made.
- Do not put real data in a sandbox: it is test data, kept and exported like any organisation's.
