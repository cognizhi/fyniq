---
key: nemura.sign-in
type: task
title: Sign in to Nemura and sign out
summary: How a person signs in to Nemura with their company's provider, what the sign-in page says when it is not ready, and how to sign out
sources:
  - docs/user-guide/nemura/sign-in.md sha256:3583866a664a23acc6610718ca6588b8f4749394c103dfec16125195ee46dea1
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - log in to Nemura
  - Nemura login
  - sign out of Nemura
  - Nemura is not set up yet
---

# Sign in to Nemura and sign out

## What it is

- Nemura has its own sessions, so people sign in even when the company does not use Fyniq.
- It never asks for an NRIC to sign in. (src: docs/user-guide/nemura/sign-in.md#what-it-is)

## Before you start

- The company sends an invitation; it is opened after signing in.
- **Nemura is not set up yet** means nobody has created the first organisation. (src: docs/user-guide/nemura/sign-in.md#before-you-start)

## Steps

1. Open Nemura's address.
2. Select **Continue with** the provider the company uses.
3. Sign in at the provider; Nemura then opens **My home**, or **People home** for the people team. See [[nemura.home]].

(src: docs/user-guide/nemura/sign-in.md#steps)

## Sign out

- **Sign out** is at the top of every page. It ends the session on this device only; other devices stay signed in. (src: docs/user-guide/nemura/sign-in.md#signing-out)

## When it fails

- **No way to sign in is set up yet**: no provider is connected; ask whoever runs Nemura.
- **We could not reach Nemura**: check the connection and select **Try again**.
- Sent back to sign-in after a while: the session ended; sign in again. (src: docs/user-guide/nemura/sign-in.md#common-problems)
