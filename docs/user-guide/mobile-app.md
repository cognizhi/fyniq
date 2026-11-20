---
key: app.mobile
title: Use Fyniq on your phone
mode: basic
roles: [owner, admin, controller, accountant, ap_clerk, approver, employee]
requirements: [FR-UX-16, FR-APR-07, FR-DOC-12]
status: draft            # written with the installable app (P2); the camera and the offline queue are tried on real phones before it is final
---

# Use Fyniq on your phone

## Install it

Fyniq is a web app that installs like an app. Open it in your phone's browser and choose **Add to Home
Screen** (Safari) or **Install app** (Chrome). It then opens in its own window, with no browser bars,
and keeps its own files on the phone so it starts with no signal. It never keeps your figures,
approvals or personal data on the phone: those always come from Fyniq, signed in.

## Approve on the way

Open **Inbox**, choose the request, check what it changes, and approve or reject it, with your passkey
or by confirming it's you, exactly as on a computer ([Approve a change](approvals.md)). An approval
email links straight to the request. Nothing is approved by opening a link, and an approval needs a
signal: it is never queued on the phone to be sent later.

## Capture a receipt with the camera

In **Purchases → Documents**, select **Take a photo** and photograph the receipt, or **Upload a
document** to choose a file. The receipt lands in the inbox for a person to read and check.

## When there is no signal

A receipt you capture with no signal is **kept on the phone**, sealed so nothing readable is stored,
and uploads by itself when you are back online. Under **Waiting on this device** you can see what waits,
**Upload now**, or **Remove** a receipt.

- A receipt waits at most 72 hours (the organisation can change it; [Offline receipts](organisation-settings.md#offline-receipts)), then is removed.
- Signing out removes everything waiting on the phone.
- On a shared device, tick **This is a shared device**: nothing is then kept on it, and you capture again when you are back online.
- When it uploads, Fyniq checks your rights as if you had just captured it. A receipt you may no longer
  capture is refused with the reason, and one already in the inbox is not added twice.
