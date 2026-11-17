# UAT: AI ask

- **Journey:** AI ask (QA-E2E-01)
- **People:** Ann Owner (Owner), Ben Founder (Owner and Controller), Cal Accountant (Accountant)
- **Proves:** AI-01 (it sees only what the person can see), AI-02 (identity and bank numbers excluded), AI-03 and AI-68
  (figures from Fyniq's read tools, as facts with their source), AI-08 (every number grounded), AI-12 (the activity
  log), AI-13 and golden rule 6 (off until an Owner turns it on with a second approver; the switch), AI-69 (how answers
  render)
- **Organisation:** `make uat-organisation NAME="UAT AI Ask Pte Ltd" PEOPLE="Ann Owner:owner;Ben Founder:owner,controller;Cal Accountant:accountant"`

## Before you start

- On the local stack the model answers in replay: a question gets the answer recorded for it, and a question with no
  recording gets "The assistant could not finish this answer." Ask the questions the AI browser journey of
  `apps/web/journeys/` asks; its set-up of the AI provider connection and the assistant's model route is the one to
  copy here `[verify]` (task 5.2's AI journeys land with the chain's assistant).
- In the dev cloud the live model answers any question, on the synthetic sample company only `[live-verify]`.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Cal | `ai-assistant-panel` | Press Ctrl+J (⌘J on a Mac). | "The assistant is switched off": "The organisation's Owner switched the AI off, or has not switched it on. Nothing you ask is sent anywhere." | Screenshot of the panel |
| 2 | Ann | `ai-settings` | Open Settings › AI. Under "The AI switch", ask to turn it on. | "Nothing is sent to any AI provider. Turning it on needs a second approver." The request is sent, and the AI stays off until it is approved. | Screenshot of the request |
| 3 | Ben | `approval-confirmation` | Open the request in the Inbox, approve it and confirm. | "Approved: the change is made." Settings › AI shows the AI on. | Screenshot of the switch |
| 4 | Cal | `ai-assistant-panel` | Press Ctrl+J. | "Ask about your books in your own words. Every figure comes from Fyniq, with where it came from." Below the question: "Uses only what you can see. It never sees identity or bank account numbers, nor payroll unless an Owner allows it." Everything the AI writes is violet, with the AI label. | Screenshot of the panel |
| 5 | Cal | `ai-assistant-panel` | Ask the browser journey's question about cash (or, in the dev cloud, "How much cash do we have today?"). | "Reading the books", then an answer whose figures carry chips under "Where the figures come from". | Screenshot of the answer |
| 6 | Cal | `bank-accounts` | Open each chip, and Banking's cash position. | Each chip opens the record or report it names; the answer's figures equal the ones on those pages to the cent. | Screenshot of each page |
| 7 | Cal | `financial-statement` | Open the profit and loss for last month and use "Ask about this" on its revenue. | The panel opens with the figure as its context and explains where it comes from, citing the accounts it adds up. | Screenshot of the explanation |
| 8 | Cal | `ai-assistant-panel` | Ask it to post a journal or pay a bill. | It changes nothing: it says what it may do, and that any change is a proposal a person approves. | Screenshot of the answer |
| 9 | Ann | `ai-activity-log` | Open Settings › AI › activity. | Cal's questions, the read tools each answer used, and its model; no bank or identity number in them. | Screenshot of the log |
| 10 | Ann | `ai-settings` | Turn the AI off. | Off at once; Cal's panel says the assistant is switched off; Banking, the ledger and the checks carry on. | Screenshot of the switch |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
