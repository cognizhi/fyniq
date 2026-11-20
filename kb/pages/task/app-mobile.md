---
key: app.mobile
type: task
title: Use Fyniq on your phone: install it, approve, capture receipts, work offline
summary: Installing Fyniq as an app, approving on the way, photographing a receipt, and what waits on the phone when there is no signal
sources:
  - docs/user-guide/mobile-app.md sha256:11a9a7b382f022d60a7bc21046da8edd4952c042a26260928e1191a5db4c9f00
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - install fyniq
  - add to home screen
  - mobile app
  - phone
  - offline receipts
  - take a photo of a receipt
  - camera
  - pwa
---

# Use Fyniq on your phone: install it, approve, capture receipts, work offline

## Install it

- Open Fyniq in the phone's browser and choose **Add to Home Screen** (Safari) or **Install app** (Chrome); it then opens in its own window and keeps its own files on the phone so it starts with no signal.
- It never keeps figures, approvals or personal data on the phone: those always come from Fyniq, signed in.

(src: docs/user-guide/mobile-app.md#install-it)

## Approve on the way

- **Inbox** → choose the request → check what it changes → approve or reject with a passkey or by confirming it's you, as on a computer ([[approvals.decide]]).
- An approval email links straight to the request; opening a link approves nothing.
- An approval needs a signal: it is never queued on the phone to be sent later.

(src: docs/user-guide/mobile-app.md#approve-on-the-way)

## Capture a receipt with the camera

- **Purchases → Documents** → **Take a photo** to photograph a receipt, or **Upload a document** to choose a file; it lands in the inbox for a person to read and check ([[documents.inbox]]).

(src: docs/user-guide/mobile-app.md#capture-a-receipt-with-the-camera)

## When there is no signal

- A receipt captured with no signal is kept on the phone, sealed, and uploads by itself when you are back online; **Waiting on this device** lists what waits, with **Upload now** and **Remove**.
- It waits at most 72 hours (the organisation can change it, [[settings.organisation]]), is removed at sign-out, and is never kept on a device marked **This is a shared device**.
- When it uploads, Fyniq checks your rights as if you had just captured it: a receipt you may no longer capture is refused with the reason, and one already in the inbox is not added twice.

(src: docs/user-guide/mobile-app.md#when-there-is-no-signal)
