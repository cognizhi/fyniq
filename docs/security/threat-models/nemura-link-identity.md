# Threat model: the link's identity answers, the sign-out door and the event log by identity

| | |
|---|---|
| Feature and requirement IDs | `identity.access.changed` both ways, `identity.sessions.write` (`receivePeopleSessionEvent`), `link.sync.read` (`getLinkSync`, `getPeopleLinkHealth`, `repairPeopleLink`), the session's recorded way in: FR-PPL-02, FR-PPL-03, FR-PPL-08, FR-PPL-15, FR-PPL-18; PRD-002 NM-INT-02, NM-INT-04, NM-INT-10, NM-INT-12 (PRD-001 revisions 0.512, 0.514, 0.542) |
| Author, reviewer, date | Claude Code for the founders, 21 Nov 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | A sign-in identity (issuer and subject; personal data, never an email), two yes or no answers, a session's way in (provider, issuer, subject), event sequence numbers, types and states; no pay, bank or NRIC data |
| Who acts | The link's client (events and the sign-out door), the link's read-only client (the log), the API role (the answers, the replay record), the worker (sends); a person (the switch, the health page) |

## 1. What we are building

Each app tells the other, signed, what an identity may enter (a role in the finance workspace, a passkey, an active member, an approver of pay runs). Nemura tells Fyniq that a person was signed out of or removed
from Nemura and Fyniq ends the sessions of the user behind the identity when that user is mapped to a person of the link. Fyniq reads Nemura's event log by identity to find a gap and asks Nemura to send it again.
Trust boundaries: **Nemura ↔ link** (token, scope, link key signature, epoch, window); **person ↔ API** (People and HR's right); **API ↔ database** (RLS, version guards).

## 2. What can go wrong (STRIDE)

| # | Boundary or step | Threat | Impact | Likelihood |
|---|---|---|---|---|
| T1 | link ↔ Fyniq | Elevation: a forged or replayed access answer grants a role or a switch | Authority widened by another system's word | Medium |
| T2 | link ↔ Fyniq | Denial: a stolen events token signs everyone out | Sessions of people the organisation maps end | Medium |
| T3 | link ↔ Fyniq | Information disclosure: the sign-out door tells Nemura who has a Fyniq account | An account directory for another system | Medium |
| T4 | link ↔ Nemura | Information disclosure: the log or the access answers carry a payload, a name, an email or a role name | Personal or pay data leaves through a read | Medium |
| T5 | link | Tampering: the events' client reads the log or the reading client sends events; an ended link's answers decide the new link's | Custody of the two keys broken; stale authority | Medium |
| T6 | Fyniq ↔ database | Tampering: a late older answer replaces a newer; a sequence restarting with a new link is read as older | A switch or a reminder that is wrong | Low |
| T7 | person ↔ API | Elevation: a role without People and HR's right reads the health or repairs; a repair floods Nemura | Link internals exposed; a replay storm | Low |
| T8 | Fyniq ↔ Nemura | Information disclosure: every member's issuer and subject cross, including people Nemura does not know | A directory of Fyniq members at Nemura | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | The envelope is checked against the link as Fyniq or Nemura holds it (organisations, epoch, window, signature, digest); the payload is read strictly (issuer, subject, two answers); an answer is a door and a hint and grants no role | `TestNemurasWordOnAnIdentityIsAHintByVersionAndDecidesTheSwitchAndThePasskeyReminder`, `TestAnIdentitysAccessIsToldToFyniqAndFyniqsAnswerDecidesTheSwitchAndTheWarning` |
| T2 | `identity.sessions.write` is held by the link's client alone (the database check refuses it to every other kind); the door ends sessions only of a user mapped to a person of the link and touches no membership, role or passkey | `TestNemuraEndsTheSessionsOfAMappedUserAndNoOneElse`, `TestAttackOnlyTheLinkHoldsTheLinkScopes` |
| T3 | The door answers `applied` for every valid event, mapped or not; the audit event holds the reason and a count, never the identity | `TestNemuraEndsTheSessionsOfAMappedUserAndNoOneElse` |
| T4 | The log's answer has no field that could hold a payload (pinned in a test); the answers carry two booleans; the audit holds the answers and never whose | `TestFyniqsReadOnlyClientReadsTheLinksEventLogByIdentityAndNothingMore` |
| T5 | The reading scope is on the read-only client only, the events' scope on the other: the token's check and the service's refuse the wrong one; the answer is kept with its link's epoch and only the live epoch is read | `TestFyniqsReadOnlyClientReadsTheLinksEventLogByIdentityAndNothingMore`, `TestAnAnswerOfAnEndedLinkDecidesNothingForTheLinkThatFollows`, `TestAnAnswerOfAnEndedLinkDecidesNothingForTheLinkThatFollowsInFyniq` |
| T6 | The database guard refuses a version that is not newer within an epoch, or an older epoch; the sequence is the version | `TestAnAnswerOfAnEndedLinkDecidesNothingForTheLinkThatFollowsInFyniq` |
| T7 | People and HR's right alone (matrix rows); a replay asks only for runs not asked for in the last day, no run wider than 10,000; the AI and MCP surfaces reach neither | `TestTheLinksHealthFindsAGapAndRepairingAsksNemuraForEachRunOnce` |
| T8 | `CrossOnlyMappedMembers` (one constant): only a member mapped to a person of the link has their identities told to Nemura; mapping queues it | `TestAnIdentitysAccessIsToldToNemuraWhenARoleOrAPasskeyOrMembershipChanges` |

## 4. What is left

A live link made before the two scopes existed must be approved again to hold them; a mapped member of two Fyniq organisations signs in again in both when Nemura ends their sessions (sessions are the user's own);
Nemura's side sends every member's identities to Fyniq (it does not know Fyniq's mapping): owner to confirm; whether a real provider re-uses its session without a prompt is `[live-verify]`.
