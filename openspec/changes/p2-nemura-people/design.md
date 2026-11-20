# Design

Decisions of 21 Nov 2026 taken while the owner was away, each with the option not taken, tagged **owner to confirm**.

## D1 Order of the slices
Options: (a) by the plan's list; (b) smallest and most self-contained first. **Chosen (b):** letter verification, grievances, onboarding plans, hiring
(requisition to hire), goals and KPIs, review cycles, pay outcomes, mixed use and the assistant's three drafts last, because each needs the one before.

## D2 Candidates are not members
A candidate has no sign-in. **Chosen:** a `candidate` record in its own table with its own consent, retention date and data-inventory class; the
person record is made only at hire (NM-REC-06). The alternative (a candidate as a person with a "candidate" status) was refused: it would put
people who never joined inside the directory, the link to Fyniq (`status: candidate` exists in the projection for pre-start hires only) and the
retention that keeps employees' records.

## D3 Letter verification shows validity, not facts
**Chosen:** the reference plus a short code printed on the letter, a rate-limited public operation, one answer for "not found" and "wrong code", and the
page shows the letter's type, date of issue, company name and whether it is revoked; never the person or any fact. The alternative (a signed URL with
the facts) was refused by NM-LTR-05's own text.

## D4 Grievances are a separate, restricted store
**Chosen:** an inquiry has its own rights row (Appendix B), is invisible to the person complained about, and is routed to another authorised person when
the default handler is the subject; its data-inventory class and retention are its own. It is never in the assistant's reads.

## D5 Fair wording is a check, not a filter
**Chosen:** a list of protected-characteristic phrases from the Tripartite Guidelines flags a draft (NM-REC-03) and the author decides; the assistant
never ranks or screens (NM-AI-05) and has no candidate-reading tool.

## D6 Pay outcomes reuse the pay path
**Chosen:** a review's recommendation creates a draft pay change or additional-wage input only through the existing preparer and approver rights
(NM-ROLE-04); the review module holds no pay write.

## D7 Nothing here needs the cloud
Outside needs: none. Job boards (MyCareersFuture) are posted to by a person (NM-REC-02); a careers page is served by Nemura itself.

## D8 Who asks for a requisition, and who reads goals (owner decisions, 21 Nov 2026)
**Chosen (the owner's):** the Owner and the HR administrator may also create a requisition (Appendix B's row Hiring is now C for the Recruiter and the HR administrator,
C/A for the Owner); approval stays with the Owner, and in founder mode the same Owner may ask and approve (the one exception to NM-ROLE-05, for requisitions only;
the audit trail names the same person twice). Every member reads company and team goals; a personal goal stays private to the person, their line manager and the people team
(Appendix B's row Performance gives the Employee "O; V (company and team goals)"). PRD-002 revision 0.69.

## D9 The offer letter, the Recruiter's list and the late plan (rev 0.77, owner-away decisions, owner to confirm)
- **Offer letter.** Options: (a) a candidate-keyed letter flow before there is a person; (b) the letter made for the person at the hire. **Chosen (b)**, the owner's: one letter flow, no second identity of a letter. (a) can follow.
- **Recruiter.** Options: (a) a new Recruiter right over all proposals (wider); (b) a list filtered to the two hiring task types with the catalogue's approver roles deciding. **Chosen (b)**: no wider than Hiring: create.
- **Title that reads as an instruction.** Options: (a) a longer deny-list only; (b) a word cap (six) plus a short list of command words. **Chosen (b)**; common words such as Assistant and System stay allowed.
- **Consent on a hired candidate.** Refused (the record is final since migration 000072); not a database change.
- **Late plan.** A flow that happened before the plan started closes its task at once by the same flow check.
