import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { coverage, type Files, messages } from "../test/forms";

const root = resolve(process.cwd(), "src"); // Vitest runs in apps/web

function read(d: string, out: Files = {}): Files {
  for (const name of readdirSync(d)) {
    const p = join(d, name);
    if (statSync(p).isDirectory()) read(p, out);
    else if (/\.tsx?$/.test(name)) out[relative(root, p).split("\\").join("/")] = readFileSync(p, "utf8");
  }
  return out;
}

// The forms whose validation messages tests do not trigger yet (task 4.2). It only shrinks: a form covered since must
// leave it, and a form not on it must be covered.
const pending = new Set<string>([
  "screens/claims/CardLines.tsx",
  "screens/claims/ClaimEditor.tsx",
  "screens/claims/ExpensePolicy.tsx",
  "screens/claims/MileageClaim.tsx",
  "screens/contacts/Contacts.tsx",
  "screens/contacts/Items.tsx",
  "screens/founders/DirectorAccounts.tsx",
  "screens/founders/DirectorSettlement.tsx",
  "screens/founders/PreIncorporationCosts.tsx",
  "screens/planning/Forecast.tsx",
  "screens/public/DocumentView.tsx",
  "screens/purchases/BillLines.tsx",
  "screens/purchases/BillPage.tsx",
  "screens/purchases/PaymentBatchPage.tsx",
  "screens/purchases/Payments.tsx",
  "screens/purchases/PurchaseOrderPage.tsx",
  "screens/purchases/RecurringBillPage.tsx",
  "screens/purchases/Withholding.tsx",
  "screens/sales/InvoiceLines.tsx",
  "screens/sales/InvoicePage.tsx",
  "screens/sales/QuotePage.tsx",
  "screens/sales/Receipts.tsx",
  "screens/sales/RecurringInvoicePage.tsx",
  "screens/sales/Statements.tsx",
  "screens/settings/Accounting.tsx",
  "screens/settings/IntegrationDetail.tsx",
  "screens/settings/Members.tsx",
  "screens/settings/Organisation.tsx",
  "screens/settings/TeamMode.tsx",
  "screens/settings/TemplateEditor.tsx",
]);

describe("the forms", () => {
  const all = coverage(read(root));
  const ok = (c: (typeof all)[number]) => c.untriggered.length === 0 && c.invalidChecked;

  it("each have tests that trigger every validation message they write and check an invalid field", () => {
    expect(all.filter((c) => !ok(c) && !pending.has(c.form)).map((c) => `${c.form}: ${c.invalidChecked ? "" : "no aria-invalid checked; "}${c.untriggered.join(" | ")}`)).toEqual([]);
    expect(all.filter((c) => ok(c) && pending.has(c.form)).map((c) => c.form)).toEqual([]);
  });

  it("are caught without: a planted form whose message no test triggers, and one whose test checks no invalid field", () => {
    const planted: Files = {
      "screens/a/New.tsx": 'import { lines } from "./lines";\nerrs.name = "Give the account a name.";\nerrs.code = `The code ${c} is taken.`;\naria-invalid',
      "screens/a/lines.tsx": 'next[k] = "Enter a debit or a credit.";\naria-invalid',
      "screens/a/New.test.tsx": 'import { New } from "./New";\nexpect("Give the account a name.");\nexpect("The code ");\naria-invalid',
      "screens/b/Other.tsx": 'errors.when = "Give the date.";\naria-invalid',
      "screens/b/Other.test.tsx": 'import { Other } from "./Other";\nexpect("Give the date.")',
    };
    expect(messages(planted["screens/a/New.tsx"]!)).toEqual(["Give the account a name.", "The code"]);
    expect(coverage(planted)).toEqual([
      { form: "screens/a/New.tsx", untriggered: [], invalidChecked: true },
      { form: "screens/a/lines.tsx", untriggered: ["Enter a debit or a credit."], invalidChecked: true },
      { form: "screens/b/Other.tsx", untriggered: [], invalidChecked: false },
    ]);
  });
});
