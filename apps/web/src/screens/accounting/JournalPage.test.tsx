import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { SignedInProvider } from "../../session";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { balances, check, firstOfNextMonth, JournalPage, type Form } from "./JournalPage";

type Account = components["schemas"]["Account"];
type Journal = components["schemas"]["Journal"];

const noEvidence = { status: 200, body: { items: [] } };
const organisation: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const acc = (id: string, code: string, name: string, extra: Record<string, unknown> = {}) =>
  ({ id, code, name, type: "expense", active: true, group: false, control: false, money_account: false, payroll_cost: false, version: 1, ...extra }) as Account;
const rent = acc("a-rent", "6110", "Rent");
const bank = acc("a-bank", "1010", "DBS current account", { type: "asset", money_account: true });
const group = acc("a-group", "6100", "Premises & office", { group: true });
const accounts = { status: 200, body: { items: [rent, bank, group] } };

const draft: Journal = {
  id: "j1",
  status: "draft",
  version: 2,
  date: "2026-10-31",
  memo: "October rent",
  lines: [
    { account_id: "a-rent", amount: { amount: "1200.00", currency: "SGD" }, description: "Rent" },
    { account_id: "a-bank", amount: { amount: "-1200.00", currency: "SGD" } },
  ],
  prepared_by: { kind: "user", id: "u1", name: "Ah Kow" },
};
const posted: Journal = { ...draft, status: "posted", number: "JNL-0001", posted_by: { kind: "user", id: "u2", name: "Wei Ling" }, posted_at: "2026-10-31T09:30:00Z" };
const entry = {
  id: "e1",
  ledger_revision: 3,
  posting_date: "2026-10-31",
  period_id: "p10",
  posting_identity: "x",
  rule_version: "journal@1",
  recorded_at: "2026-10-31T09:30:00Z",
  lines: [
    { line_no: 1, account_id: "a-rent", amount: { amount: "1200.00", currency: "SGD" }, fx_rate: "1", fx_rate_source: "base", base_amount: { amount: "1200.00", currency: "SGD" } },
    { line_no: 2, account_id: "a-bank", amount: { amount: "-1200.00", currency: "SGD" }, fx_rate: "1", fx_rate_source: "base", base_amount: { amount: "-1200.00", currency: "SGD" } },
  ],
};

function page(session: Session, org: Organisation = organisation) {
  return function Page() {
    return (
      <SignedInProvider value={{ session, organisation: org, mode: "expert" }}>
        <JournalPage />
      </SignedInProvider>
    );
  };
}

const line = (key: string, extra: Partial<Form["lines"][number]> = {}) => ({ key, account_id: "a", description: "", debit: "", credit: "", currency: "SGD", fx_rate: "", ...extra });

// req: FR-GL-01
// req: FR-UX-20
describe("what a journal's form sends", () => {
  it("turns debits and credits into signed amounts, leaving out blank lines", () => {
    const out = check({ date: "2026-10-31", memo: " Rent ", reverse_on: "", lines: [line("1", { debit: "1,200.00" }), line("2", { credit: "S$1,200.00" }), line("3", { account_id: "" })] }, "SGD");
    expect(out.errors).toEqual({});
    expect(out.draft).toEqual({
      date: "2026-10-31",
      memo: "Rent",
      lines: [
        { account_id: "a", amount: { amount: "1200.00", currency: "SGD" }, description: undefined, fx_rate: undefined },
        { account_id: "a", amount: { amount: "-1200.00", currency: "SGD" }, description: undefined, fx_rate: undefined },
      ],
    });
  });

  it("says what to fix under each field", () => {
    const out = check({ date: "", memo: "", reverse_on: "", lines: [line("1", { debit: "5", credit: "5" }), line("2", { account_id: "", debit: "abc" }), line("3", { debit: "-5" }), line("4", { debit: "10", currency: "USD" })] }, "SGD");
    expect(out.draft).toBeUndefined();
    expect(out.errors).toEqual({
      date: "Enter the date the journal belongs to.",
      "1.debit": "Enter a debit or a credit, not both.",
      "2.account": "Choose the account.",
      "2.debit": "Enter an amount, such as 1,200.00.",
      "3.debit": "Enter the amount without a minus sign: a credit goes under Credit.",
      "4.fx_rate": "Enter the rate: SGD for 1 USD, such as 1.3472.",
    });
    expect(check({ date: "2026-10-31", memo: "", reverse_on: "", lines: [] }, "SGD").errors.lines).toBe("Add a line.");
  });

  // req: FR-GL-02
  it("sends a reversal date after the journal's, suggesting the first of the next month", () => {
    expect(firstOfNextMonth("2026-10-31")).toBe("2026-11-01");
    expect(firstOfNextMonth("2026-12-15")).toBe("2027-01-01");
    const lines = [line("1", { debit: "100" }), line("2", { credit: "100" })];
    expect(check({ date: "2026-10-31", memo: "", reverse_on: "2026-10-31", lines }, "SGD").errors.reverse_on).toMatch(/after the journal's date/);
    expect(check({ date: "2026-10-31", memo: "", reverse_on: "2026-11-01", lines }, "SGD").draft?.reverse_on).toBe("2026-11-01");
  });

  it("adds up each currency exactly", () => {
    const f: Form = { date: "", memo: "", reverse_on: "", lines: [line("1", { debit: "0.10" }), line("2", { debit: "0.20" }), line("3", { credit: "0.30" }), line("4", { debit: "100", currency: "USD", fx_rate: "1.35" })] };
    expect(balances(f, "SGD")).toEqual([
      { currency: "SGD", debits: "0.30", credits: "0.30", difference: "0.00" },
      { currency: "USD", debits: "100", credits: "0", difference: "100" },
    ]);
  });
});

// req: FR-GL-01
// req: FR-GL-12
// req: FR-INT-14
describe("the journal page", () => {
  it("prepares a new journal, shows whether it balances and saves it as a draft; axe finds nothing", async () => {
    answer({ "GET /v1/accounts": accounts, "POST /v1/journals": { status: 201, body: { ...draft, id: "j-new", version: 1 } } });
    renderPage(page(accountant), { path: "/accounting/journals/new" });
    const account1 = await screen.findByLabelText("Line 1: Account");
    expect(within(account1).queryByText("6100 Premises & office")).toBeNull(); // a group takes no posting
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "October rent" } });
    fireEvent.change(screen.getByLabelText("Date"), { target: { value: "2026-10-31" } });
    fireEvent.change(account1, { target: { value: "a-rent" } });
    fireEvent.change(screen.getByLabelText("Line 1: Debit"), { target: { value: "1,200.00" } });
    fireEvent.change(screen.getByLabelText("Line 2: Account"), { target: { value: "a-bank" } });
    fireEvent.change(screen.getByLabelText("Line 2: Credit"), { target: { value: "1,000" } });
    expect(screen.getByText("Debits exceed credits by 200.00 SGD")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Line 2: Credit"), { target: { value: "1,200" } });
    expect(screen.getByText("Balanced in SGD: 1,200.00 each side")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getAllByRole("button", { name: "Save as a draft" })[0]!);
    await waitFor(() => expect(seen.find((s) => s.method === "POST")).toBeTruthy());
    const post = seen.find((s) => s.method === "POST")!;
    expect(post.headers.get("Idempotency-Key")).toBeTruthy();
    expect(post.body).toEqual({
      date: "2026-10-31",
      memo: "October rent",
      lines: [
        { account_id: "a-rent", amount: { amount: "1200.00", currency: "SGD" } },
        { account_id: "a-bank", amount: { amount: "-1200", currency: "SGD" } },
      ],
    });
  });

  // req: RND-DISPLAY
  it("leaves a journal in several currencies to the server's check, never rounding on the page", async () => {
    answer({ "GET /v1/accounts": accounts });
    renderPage(page(accountant), { path: "/accounting/journals/new" });
    fireEvent.change(await screen.findByLabelText("Line 1: Debit"), { target: { value: "1,000.00" } });
    fireEvent.change(screen.getByLabelText("Line 2: Credit"), { target: { value: "740" } });
    fireEvent.change(screen.getByLabelText("Line 2: Currency"), { target: { value: "usd" } });
    expect(screen.getByText("In SGD and USD: select Check to see whether it balances in SGD.")).toBeTruthy();
    expect(screen.getByLabelText("Line 2: Rate (SGD for 1 USD)")).toBeTruthy();
  });

  it("refuses to save what does not read as a journal, and sends nothing", async () => {
    answer({ "GET /v1/accounts": accounts });
    renderPage(page(accountant), { path: "/accounting/journals/new" });
    fireEvent.change(await screen.findByLabelText("Line 1: Debit"), { target: { value: "50" } });
    fireEvent.change(screen.getByLabelText("Line 1: Credit"), { target: { value: "50" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Save as a draft" })[0]!);
    expect(await screen.findByText("Enter a debit or a credit, not both.")).toBeTruthy();
    expect(screen.getByText("Choose the account.")).toBeTruthy();
    expect(screen.getByLabelText("Line 1: Account").getAttribute("aria-invalid")).toBe("true");
    expect(seen.filter((s) => s.method !== "GET")).toEqual([]);
  });

  it("checks a draft without saving anything, then posts it at the version read", async () => {
    let posts = 0;
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1/evidence": noEvidence,
      "GET /v1/journals/j1": () => ({ status: 200, body: posts ? posted : draft, headers: { ETag: posts ? `"2-posted"` : `"2-draft"` } }),
      "POST /v1/journals/j1/post": (_b, req) => {
        if (req.headers.get("Prefer") === "return=preview") return { status: 200, body: { journal: posted, entry }, headers: { "Preference-Applied": "return=preview" } };
        posts++;
        return { status: 200, body: { journal: posted, entry } };
      },
    });
    renderPage(page(accountant), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    expect(await screen.findByText("Prepared by Ah Kow")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "Check" })[0]!);
    expect(await screen.findByRole("table", { name: "What posting writes" })).toBeTruthy();
    expect(screen.getByText("It balances and can post. Nothing was saved.")).toBeTruthy();
    const check = seen.find((s) => s.method === "POST")!;
    expect(check.headers.get("If-Match")).toBe(`"2-draft"`);
    expect(check.headers.get("Idempotency-Key")).toBeNull();
    fireEvent.click(screen.getAllByRole("button", { name: "Post" })[0]!);
    expect(await screen.findByText("Posted as JNL-0001.")).toBeTruthy();
    const sent = seen.filter((s) => s.method === "POST")[1]!;
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    expect(sent.headers.get("Prefer")).toBeNull();
    expect(await screen.findByText(/posted by Wei Ling/)).toBeTruthy();
  });

  // req: FR-ORG-02
  it("points to the periods when no period covers the date", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1/evidence": noEvidence,
      "GET /v1/journals/j1": { status: 200, body: draft, headers: { ETag: `"2-draft"` } },
      "POST /v1/journals/j1/post": problem(409, "STORAGE-PERIOD-01", "No period covers the posting date 2026-10-31.", "Create the period first."),
    });
    renderPage(page(accountant), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    await screen.findByText("Prepared by Ah Kow");
    fireEvent.click(screen.getAllByRole("button", { name: "Post" })[0]!);
    expect(await screen.findByText("The journal was not posted")).toBeTruthy();
    expect(screen.getByRole("link", { name: /Open the financial year/ }).getAttribute("href")).toBe("/accounting/close");
  });

  // req: FR-GL-01
  // req: FM-2
  it("asks for approval to post a journal above the self-approval threshold, then shows the request waiting", async () => {
    let asked = false;
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1/evidence": noEvidence,
      "GET /v1/journals/j1": () => ({ status: 200, body: asked ? { ...draft, approval_asked: "a-1" } : draft, headers: { ETag: `"2-draft"` } }),
      "POST /v1/journals/j1/post": problem(409, "JOURNAL-07", "The journal's debits are 2500.00 SGD, above the self-approval threshold of 2000.00.", "Ask for another person's approval to post it"),
      "POST /v1/journals/j1/post-approval": () => {
        asked = true;
        return { status: 202, body: { id: "a-1", object_type: "journal", operation: "journal.post", status: "pending" } };
      },
    });
    renderPage(page(accountant), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    await screen.findByText("Prepared by Ah Kow");
    fireEvent.click(screen.getAllByRole("button", { name: "Post" })[0]!);
    expect(await screen.findByText("Another person approves this journal")).toBeTruthy();
    expect(screen.getByText(/above the self-approval threshold of 2000.00/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText("Asked for approval: the journal posts when another person approves it.")).toBeTruthy();
    const sent = seen.find((s) => s.path === "/v1/journals/j1/post-approval")!;
    expect(sent.headers.get("If-Match")).toBe(`"2-draft"`);
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    expect((await screen.findByRole("link", { name: "See the request" })).getAttribute("href")).toBe("/approvals/a-1");
    expect(screen.getByText("A request to post this journal waits for approval")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Post" })).toBeNull();
  });

  // req: FM-1
  it("asks to reverse a posted journal in Founder mode, with the date and the reason", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1/evidence": noEvidence,
      "GET /v1/journals/j1": { status: 200, body: posted, headers: { ETag: `"2-posted"` } },
      "POST /v1/journals/j1/reverse-approval": { status: 202, body: { id: "a-2", object_type: "journal", operation: "journal.reverse", status: "pending" } },
    });
    renderPage(page(accountant), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    await screen.findByRole("table", { name: "Lines as entered" });
    fireEvent.click(screen.getAllByRole("button", { name: "Ask to reverse…" })[0]!);
    const dialog = await screen.findByRole("dialog", { name: "Ask to reverse this journal?" });
    fireEvent.change(within(dialog).getByLabelText("Date of the reversing entry"), { target: { value: "2026-11-01" } });
    fireEvent.change(within(dialog).getByLabelText("Why it is reversed"), { target: { value: "Posted to the wrong account" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to reverse" }));
    expect(await screen.findByText("Asked for approval: the reversal posts when another founder approves it.")).toBeTruthy();
    const sent = seen.find((s) => s.method === "POST")!;
    expect(sent.path).toBe("/v1/journals/j1/reverse-approval");
    expect(sent.body).toEqual({ date: "2026-11-01", reason: "Posted to the wrong account" });
    expect(sent.headers.get("If-Match")).toBe(`"2-posted"`);
  });

  // req: FR-GL-12
  // req: FR-INT-14
  it("edits a posted journal in Standard mode: checks both entries, then posts the edit with a reason", async () => {
    let edits = 0;
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1/evidence": noEvidence,
      "GET /v1/journals/j1": () => ({ status: 200, body: edits ? { ...posted, version: 3, date: "2026-11-02" } : posted, headers: { ETag: edits ? `"3-posted"` : `"2-posted"` } }),
      "POST /v1/journals/j1/edit": (_b, req) => {
        const reversal = { ...entry, id: "e2", posting_date: "2026-10-31", reverses_entry_id: "e1" };
        const next = { ...entry, id: "e3", posting_date: "2026-11-02" };
        if (req.headers.get("Prefer") === "return=preview") return { status: 200, body: { journal: posted, reversal, entry: next }, headers: { "Preference-Applied": "return=preview" } };
        edits++;
        return { status: 200, body: { journal: { ...posted, version: 3 }, reversal, entry: next } };
      },
    });
    renderPage(page(accountant, { ...organisation, team_mode: "standard" }), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    await screen.findByRole("table", { name: "Lines as entered" });
    fireEvent.click(screen.getAllByRole("button", { name: "Edit…" })[0]!);
    expect(await screen.findByRole("heading", { name: "Edit the posted journal" })).toBeTruthy();
    expect((screen.getByLabelText("Line 1: Debit") as HTMLInputElement).value).toBe("1200.00");
    fireEvent.change(screen.getByLabelText("Date"), { target: { value: "2026-11-02" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Post the edit" })[0]!);
    expect(await screen.findByText(/Say why/)).toBeTruthy();
    expect(seen.filter((s) => s.method === "POST")).toEqual([]);
    fireEvent.change(screen.getByLabelText("Why it is edited"), { target: { value: "Rent is due in November" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Check the edit" })[0]!);
    expect(await screen.findByRole("table", { name: "What the edit reverses" })).toBeTruthy();
    expect(screen.getByRole("table", { name: "What the new version posts" })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getAllByRole("button", { name: "Post the edit" })[0]!);
    expect(await screen.findByText("Edited. The entry it had is reversed on 31 Oct 2026, and the new version posts on 2 Nov 2026.")).toBeTruthy();
    const sent = seen.filter((s) => s.path === "/v1/journals/j1/edit")[1]!;
    expect(sent.body).toEqual({
      date: "2026-11-02",
      memo: "October rent",
      lines: [
        { account_id: "a-rent", amount: { amount: "1200.00", currency: "SGD" }, description: "Rent" },
        { account_id: "a-bank", amount: { amount: "-1200.00", currency: "SGD" } },
      ],
      reason: "Rent is due in November",
    });
    expect(sent.headers.get("If-Match")).toBe(`"2-posted"`);
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
  });

  // req: FM-1
  it("asks to post an edit in Founder mode", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1/evidence": noEvidence,
      "GET /v1/journals/j1": { status: 200, body: posted, headers: { ETag: `"2-posted"` } },
      "POST /v1/journals/j1/edit-approval": { status: 202, body: { id: "a-3", object_type: "journal", operation: "journal.edit", status: "pending" } },
    });
    renderPage(page(accountant), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    await screen.findByRole("table", { name: "Lines as entered" });
    fireEvent.click(screen.getAllByRole("button", { name: "Edit…" })[0]!);
    await screen.findByRole("heading", { name: "Edit the posted journal" });
    expect(screen.queryByRole("button", { name: "Check the edit" })).toBeNull();
    fireEvent.change(screen.getByLabelText("Line 1: Debit"), { target: { value: "1,250.00" } });
    fireEvent.change(screen.getByLabelText("Line 2: Credit"), { target: { value: "1,250.00" } });
    fireEvent.change(screen.getByLabelText("Why it is edited"), { target: { value: "Rent rose" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Ask to post the edit" })[0]!);
    expect(await screen.findByText("Asked for approval: the edit posts when another person approves it.")).toBeTruthy();
    const sent = seen.find((s) => s.method === "POST")!;
    expect(sent.path).toBe("/v1/journals/j1/edit-approval");
    expect((sent.body as { lines: { amount: { amount: string } }[] }).lines[0]!.amount.amount).toBe("1250.00");
  });

  it("reverses a posted journal on a date, with a reason, in Standard mode", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1/evidence": noEvidence,
      "GET /v1/journals/j1": { status: 200, body: posted, headers: { ETag: `"2-posted"` } },
      "POST /v1/journals/j1/reverse": { status: 200, body: { journal: { ...posted, status: "reversed" }, entry: { ...entry, posting_date: "2026-11-01" } } },
    });
    renderPage(page(accountant, { ...organisation, team_mode: "standard" }), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    const lines = await screen.findByRole("table", { name: "Lines as entered" });
    expect(within(lines).getByText("6110 Rent")).toBeTruthy();
    expect(screen.queryByLabelText("Line 1: Account")).toBeNull(); // posted: never edited
    fireEvent.click(screen.getAllByRole("button", { name: "Reverse…" })[0]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Date of the reversing entry"), { target: { value: "2026-11-01" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Reverse" }));
    expect(await within(dialog).findByText(/Say why/)).toBeTruthy();
    expect(seen.filter((s) => s.method === "POST")).toEqual([]);
    fireEvent.change(within(dialog).getByLabelText("Why it is reversed"), { target: { value: "Posted to the wrong account" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Reverse" }));
    expect(await screen.findByText("Reversed. The reversing entry is dated 1 Nov 2026.")).toBeTruthy();
    const sent = seen.find((s) => s.method === "POST")!;
    expect(sent.body).toEqual({ date: "2026-11-01", reason: "Posted to the wrong account" });
    expect(sent.headers.get("If-Match")).toBe(`"2-posted"`);
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
  });

  // req: FR-GL-02
  it("saves a journal that reverses by itself on the first of the next month, and says so once posted", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "POST /v1/journals": { status: 201, body: { ...draft, id: "j9", reverse_on: "2026-11-01" }, headers: { ETag: `"1-draft"` } },
      "GET /v1/journals/j9/evidence": noEvidence,
      "GET /v1/journals/j9": { status: 200, body: { ...posted, id: "j9", reverse_on: "2026-11-01" }, headers: { ETag: `"1-posted"` } },
    });
    renderPage(page(accountant), { path: "/accounting/journals/new", route: "/accounting/journals/new" });
    fireEvent.change(await screen.findByLabelText("Date"), { target: { value: "2026-10-31" } });
    fireEvent.click(screen.getByLabelText("Reverses by itself"));
    expect((screen.getByLabelText("Reversal date") as HTMLInputElement).value).toBe("2026-11-01");
    fireEvent.change(screen.getByLabelText("Line 1: Account"), { target: { value: "a-rent" } });
    fireEvent.change(screen.getByLabelText("Line 1: Debit"), { target: { value: "500" } });
    fireEvent.change(screen.getByLabelText("Line 2: Account"), { target: { value: "a-bank" } });
    fireEvent.change(screen.getByLabelText("Line 2: Credit"), { target: { value: "500" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Save as a draft" })[0]!);
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    expect((seen.find((s) => s.method === "POST")!.body as { reverse_on?: string }).reverse_on).toBe("2026-11-01");
  });

  it("shows when a posted journal reverses by itself", async () => {
    answer({ "GET /v1/accounts": accounts, "GET /v1/journals/j1/evidence": noEvidence,
 "GET /v1/journals/j1": { status: 200, body: { ...posted, reverse_on: "2026-11-01" }, headers: { ETag: `"2-posted"` } } });
    renderPage(page(accountant), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    expect(await screen.findByText("Reverses by itself on 1 Nov 2026")).toBeTruthy();
  });

  // req: FR-GL-02
  it("says why a posted journal's automatic reversal waits, and which recurring journal made it", async () => {
    const waiting = { severity: "warning" as const, code: "JOURNAL-WAITING", field: "reverse_on", title: "Its automatic reversal is waiting", detail: "Its automatic reversal is dated 2027-01-01, and no period covers that date yet.", fix: "Open the financial year of 2027-01-01 in Accounting › Periods and close; the reversal posts within the hour" };
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1/evidence": noEvidence,
      "GET /v1/journals/j1": {
        status: 200,
        body: { ...posted, reverse_on: "2027-01-01", prepared_by: { kind: "service", id: "scheduler:journals.recurring", name: "Scheduler" }, recurring_journal: { id: "r1", version: 2 }, checks: [waiting] },
        headers: { ETag: `"2-posted"` },
      },
    });
    renderPage(page(accountant), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    expect(await screen.findByText(/no period covers that date yet\. Open the financial year of 2027-01-01/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "a recurring journal" }).getAttribute("href")).toBe("/accounting/journals/recurring/r1");
    expect(screen.getByText(/Prepared by Scheduler/)).toBeTruthy();
  });

  // req: ENG-20
  // req: ENG-21
  it("lists what stops a draft posting, in the workbook's words and order", async () => {
    const checks = [
      { severity: "error" as const, code: "LEDGER-01", field: "date", title: "Enter the date", detail: "the journal has no date", fix: "Enter the date the journal belongs to" },
      { severity: "error" as const, code: "LEDGER-09", field: "lines", title: "Entry does not balance", detail: "the debits and the credits differ by 100.00 SGD", fix: "Make the debits equal the credits" },
    ];
    answer({ "GET /v1/accounts": accounts, "GET /v1/journals/j1/evidence": noEvidence,
 "GET /v1/journals/j1": { status: 200, body: { ...draft, checks }, headers: { ETag: `"2-draft"` } } });
    renderPage(page(accountant), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    const list = await screen.findByRole("heading", { name: "2 things to fix before it can post" });
    expect(list).toBeTruthy();
    const items = screen.getAllByText(/Enter the date|Entry does not balance/);
    expect(items[0]!.textContent).toContain("Enter the date");
    expect(screen.getByText(/differ by 100.00 SGD. Make the debits equal the credits./)).toBeTruthy();
    await noViolations(document.body);
  });

  it("shows an Owner a draft read-only, with nothing to post", async () => {
    answer({ "GET /v1/accounts": accounts, "GET /v1/journals/j1/evidence": noEvidence,
 "GET /v1/journals/j1": { status: 200, body: draft, headers: { ETag: `"2-draft"` } } });
    renderPage(page(ownerSession as Session), { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" });
    expect(await screen.findByRole("table", { name: "Lines as entered" })).toBeTruthy();
    expect(screen.getByText("An Accountant or a Tax agent changes drafts.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Post" })).toBeNull();
  });
});
