---
key: ai-assistant
title: Data protection impact assessment of the AI assistant
draft: fyniq-dpia-draft@1
sources:
  - title: PDPC, Guide to Data Protection Impact Assessments
    url: https://www.pdpc.gov.sg/-/media/Files/PDPC/PDF-Files/Other-Guides/DPIA/Guide-to-Data-Protection-Impact-Assessments-14-Sep-2021.pdf
    retrieved: 2026-11-09
  - title: PDPC, Advisory Guidelines on Use of Personal Data in AI Recommendation and Decision Systems
    url: https://www.pdpc.gov.sg/-/media/files/pdpc/pdf-files/advisory-guidelines/advisory-guidelines-on-the-use-of-personal-data-in-ai-recommendation-and-decision-systems.pdf
    retrieved: 2026-11-09
  - title: PDPC, Advisory Guidelines on Use of Personal Data in Generative AI
    url: https://www.pdpc.gov.sg/organisations/regulations-decisions/regulatory-guidance/advisory-guidelines-on-use-of-personal-data-in-generative-ai
    retrieved: 2026-11-09
controls: [CTL-DP-05]
---

# Data protection impact assessment of the AI assistant

## The processing

The AI assistant answers members' questions about the books and drafts proposals that a person approves. It reads
figures only through read tools on the member's own behalf, and know-how from the compiled knowledge base. It never
writes to the books itself.

## Personal data and how it flows

- **To the model:** the member's question and what the read tools return for it, such as a contact's name on an
  invoice. Bank numbers, identifiers and payroll are excluded unless the company enables them.
- **Where:** the model provider the company chooses, through Fyniq's model routes. Personal data goes only to
  processing in Singapore by default (ADR-T2 Option A). [counsel to confirm] Any route outside Singapore needs its own
  transfer assessment first.
- **Kept:** conversations for the AI conversations' retention (365 days by default), then purged; proposals and their
  approvals with the records.

## Necessity and proportionality

The assistant helps people find and understand figures they may already see. It reads only what the member asking
may read, so it widens nobody's access.

## Risks to the people concerned

- A person's details sent to a model provider that keeps or trains on them.
- An answer that reveals data the member asking may not see.
- [counsel to confirm] Personal data processed outside Singapore without a transfer assessment.

## Measures

- Read tools act on the member's behalf and return only what they may see; exclusions keep bank numbers, identifiers
  and payroll out unless enabled (AI-02).
- Routes send personal data only to in-Singapore processing by default; every connection declares retention and
  training, and the Owner records a transfer assessment for any outside Singapore (AI-75, CMP-PD-06).
- The AI holds no write right; every change it proposes is approved by a person.
- Conversations are purged after their retention.

## Residual risk and decision

[counsel to confirm] With these measures, the residual risk is low. The company may enable the assistant once this
assessment is approved.

## Approval and review

The Data Protection Officer approves this assessment before the AI assistant is enabled, and reviews it when a model,
provider or route changes and at least once a year.
