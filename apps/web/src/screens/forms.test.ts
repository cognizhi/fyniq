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

// QA-UI-01 (p1-launch-readiness task 4.2): every form's validation messages are triggered by a test, and a test checks
// that the field refused is marked invalid, described by its message.
// req: QA-UI-01
describe("the forms", () => {
  it("each have tests that trigger every validation message they write and check an invalid field", () => {
    const all = coverage(read(root));
    expect(all.length).toBeGreaterThan(50); // the check reads the app (56 forms on 16 Nov 2026), not an empty folder
    expect(all.filter((c) => c.untriggered.length > 0 || !c.invalidChecked).map((c) => `${c.form}: ${c.invalidChecked ? "" : "no aria-invalid checked; "}${c.untriggered.join(" | ")}`)).toEqual([]);
  });

  it("are caught without: a planted form whose message no test triggers, and one whose test checks no invalid field", () => {
    const planted: Files = {
      "screens/a/New.tsx": 'import { lines } from "./lines";\nerrs.name = "Give the account a name.";\nerrs.code = `The code ${c} is taken.`;\naria-invalid',
      "screens/a/lines.tsx": 'next[k] = "Enter a debit or a credit.";\naria-invalid',
      "screens/a/New.test.tsx": 'import { New } from "./New";\nexpect("Give the account a name.");\nexpect("The code ");\naria-invalid',
      "screens/b/Other.tsx": 'errors.when = "Give the date.";\naria-invalid',
      "screens/b/Other.test.tsx": 'import { Other } from "./Other";\nexpect("Give the date.")',
      "screens/c/Third.tsx": 'out.push("description matches /x/");\nproblems.push("Enter the rate first.");\naria-invalid',
      "screens/c/Third.test.tsx": 'import { invalid } from "../../test/invalid";\nimport { Third } from "./Third";\nexpect("Enter the rate first.")',
    };
    expect(messages(planted["screens/a/New.tsx"]!)).toEqual(["Give the account a name.", "The code"]);
    expect(messages(planted["screens/c/Third.tsx"]!)).toEqual(["Enter the rate first."]); // a summary pushed to a list is no message
    expect(coverage(planted)).toEqual([
      { form: "screens/a/New.tsx", untriggered: [], invalidChecked: true },
      { form: "screens/a/lines.tsx", untriggered: ["Enter a debit or a credit."], invalidChecked: true },
      { form: "screens/b/Other.tsx", untriggered: [], invalidChecked: false },
      { form: "screens/c/Third.tsx", untriggered: [], invalidChecked: true }, // invalid() checks aria-invalid
    ]);
  });
});
