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
