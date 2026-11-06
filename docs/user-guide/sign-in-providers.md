---
key: settings.sign-in-providers
title: Sign-in providers
mode: expert
roles: [super_admin]
requirements: [FR-ADM-08, FR-ADM-07]
status: draft
---

# Sign-in providers

## What it is

**Settings → Sign-in providers** lists the ways people sign in to every organisation on this installation, such
as Google or Zoho. Only a Super admin sees it. For each provider it shows whether it is on, the email domains it
accepts, and how many sessions it started in the last 30 days.

- The providers themselves are set in the deployment's configuration. Adding a provider from this page comes later.
- Whichever provider someone uses, Fyniq still asks for its own second step: a passkey or an authenticator code.

## Turn a provider on or off

1. Select the provider's switch.
2. To turn it off, choose whether to also sign out everyone signed in with it now. Leave it unticked to let them
   finish what they are doing.
3. Select **Save** and confirm it's you.

A provider turned off disappears from the sign-in page at once, and nobody can sign in or link an account with it.
Its settings are kept, so turning it on again restores it.

## Accept only some email domains

1. Select **Domains** next to the provider.
2. Type the domains, such as cognizhi.com, separated by commas. Leave it empty to accept any domain.
3. Select **Save** and confirm it's you.

Only an account whose verified email ends with one of the domains can then sign in, or be linked, with that
provider. Others see that the provider accepts only some email domains.

## Let people join by their domain

In **Domains**, tick **Let people with these domains ask to join**. A person who signs in through
the provider with a verified email on one of the domains, and has no Fyniq account, may then ask to
join the organisation that claimed that domain; an Owner of that organisation approves each
request ([Invite people and give them roles](members-and-roles.md)). It is off by default: people
need an invitation. It needs the domains set.

## The last way in stays on

Fyniq refuses to turn off a provider, or to narrow its domains, when a Super admin would be left with no way to
sign in. **Last way in for a Super admin** marks such a provider. Ask that Super admin to link another sign-in on
their profile first.

## With two Super admins

Once there are two Super admins, every change on this page waits for the other one's approval. The provider shows
**Waits for the other Super admin**. The other Super admin approves it, or rejects it with a reason, on this page or
under **Settings → Integrations** in **Installation changes waiting**. Nobody approves their own change, and a change
nobody decides expires after a week. Every Super admin is told of each change.

## Common problems

- **"That would leave a Super admin no way to sign in."** Keep the provider on, or have the Super admin link a
  sign-in with another provider on their profile first.
- **"Is not a domain name."** Give the domain as email addresses end, such as example.com, without the @.

## Related

- [Your profile and security](profile-and-security.md)
- [Sign in to Fyniq](sign-in.md)
- [Integrations and connectors](integrations.md)
- [Invite people and give them roles](members-and-roles.md)
