# UAT: the GST monitor and calendar

- **Journey:** GST, its launch part: the registration monitor and the statutory calendar (QA-E2E-01). The F5 return
  comes at registration, by the owner's decision (rev 0.25).
- **People:** Cal Accountant (Accountant)
- **Proves:** FR-TAX-05 (the registration monitor, retrospective and prospective, with the forecasts it rests on),
  FR-TAX-07 (the statutory calendar: deadlines, records as filed or not required, and their withdrawal)
- **Organisation:** `make uat-organisation NAME="UAT GST Pte Ltd" PEOPLE="Cal Accountant:accountant"`

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Cal | `gst-returns` | Open Compliance › GST. | "Registration monitor": "Last calendar year" and "This year so far, against the threshold" of S$1,000,000, each with its status (Below the threshold, Near the threshold, Must register, or Not assessed). "While the company is not registered, GST that overseas suppliers charge is part of the cost." | Screenshot of the monitor |
| 2 | Cal | `gst-returns` | Compare "This year so far" with the sample company's standard-rated and zero-rated sales this year (Reports › sales by customer). | The same figure: exempt and out-of-scope sales and sales of the company's own assets are left out. | Screenshot of both |
| 3 | Cal | `gst-returns` | Select "Record a forecast". Day of the forecast: today. Taxable supplies expected: 1,200,000.00. "Save the forecast". | "The forecast is drafted. Attach what it rests on, then record it." It is not in force yet. | Screenshot of the draft forecast |
| 4 | Cal | `evidence-panel` | Attach what it rests on: a synthetic signed contract (PDF). Then "Record". | The forecast is "In force". The prospective test now says "Must register", and "Apply to register for GST with IRAS" shows with its deadline. | Screenshot of the monitor |
| 5 | Cal | `statutory-calendar` | Open Compliance › Statutory calendar. | "Every filing and payment deadline, worked out from your financial years, GST periods and settings." Tiles: "Due in the next 30 days", "Overdue", "Recorded". The GST registration's deadline from step 4 is listed. | Screenshot of the calendar |
| 6 | Cal | `statutory-calendar` | Switch between Month and List, and filter by area. | The same items in each view; the filter keeps only that area's. | Screenshot of the list |
| 7 | Cal | `statutory-calendar` | Open an item due soon and "Record as filed": filed on today, acknowledgement number "UAT-ACK-0001". | The item is Recorded, with its acknowledgement. | Screenshot of the record |
| 8 | Cal | `statutory-calendar` | Open another item and record it "Not required", with the reason "UAT: waived". Then "Withdraw the record" with the reason "UAT: recorded by mistake". | The reason is kept with each; after the withdrawal the item is due again. | Screenshot of the item |
| 9 | Cal | `checks` | Open Checks. | Overdue items of the calendar also show on Checks. | Screenshot of Checks |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
