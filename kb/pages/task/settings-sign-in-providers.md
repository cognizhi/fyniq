---
key: settings.sign-in-providers
type: task
title: Turn sign-in providers on or off, and accept only some email domains
summary: A Super admin's page of the ways people sign in: turn each on or off, end its sessions, set allowed email domains; the last way in; two Super admins
sources:
  - docs/user-guide/sign-in-providers.md sha256:fe0540471c0b57c0eafa97877e046971bdbcc6a61ea404d5e7df977ae5814c11
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-07
aliases:
  - sign-in providers
  - turn off google sign-in
  - turn off zoho sign-in
  - allowed email domains
  - restrict sign-in to our domain
  - last way in
  - sign-in methods
---

# Turn sign-in providers on or off, and accept only some email domains

## What the page holds

- **Settings → Sign-in providers** lists the ways people sign in to every organisation on the installation, such as Google or Zoho; only a Super admin sees it.
- Each provider shows whether it is on, the email domains it accepts and its sessions in the last 30 days.
- Providers are set in the deployment's configuration; adding one from the page comes later.
- Fyniq's own second step (a passkey or an authenticator code) is asked for whichever provider signs someone in ([[auth.sign-in]]).

(src: docs/user-guide/sign-in-providers.md#what-it-is)

## Turn a provider on or off

- Select the provider's switch; turning it off can also sign out everyone signed in with it now, or let them finish.
- **Save** and confirm it's you. A provider turned off leaves the sign-in page at once; nobody signs in or links an account with it, and its settings are kept for when it is turned on again.

(src: docs/user-guide/sign-in-providers.md#turn-a-provider-on-or-off)

## Accept only some email domains

- **Domains** next to the provider: type the domains, such as cognizhi.com, separated by commas; empty accepts any domain.
- Only an account whose verified email ends with one of them can then sign in, or be linked, with that provider ([[preferences.security]]).

(src: docs/user-guide/sign-in-providers.md#accept-only-some-email-domains)

## Let people join by their domain

- In **Domains**, **Let people with these domains ask to join**: a person signing in through the provider with a verified email on one of them, without a Fyniq account, may ask to join the organisation that claimed the domain; an Owner approves each ([[members.invite]]).
- Off by default (an invitation is needed), and it needs the domains set.

(src: docs/user-guide/sign-in-providers.md#let-people-join-by-their-domain)

## The last way in, and two Super admins

- Fyniq refuses to turn off a provider, or narrow its domains, when a Super admin would be left with no way to sign in; **Last way in for a Super admin** marks it. That Super admin links another sign-in on their profile first.
- With two Super admins every change waits for the other's approval, on this page or under **Settings → Integrations** in **Installation changes waiting** ([[settings.integrations]]); nobody approves their own, an undecided change expires after a week, and every Super admin is told of each change.

(src: docs/user-guide/sign-in-providers.md#the-last-way-in-stays-on) (src: docs/user-guide/sign-in-providers.md#with-two-super-admins)

## Common problems

- "That would leave a Super admin no way to sign in.": keep the provider on, or have the Super admin link a sign-in with another provider first.
- "Is not a domain name.": give the domain as email addresses end, such as example.com, without the @.

(src: docs/user-guide/sign-in-providers.md#common-problems) (src: docs/user-guide/sign-in-providers.md#related)
