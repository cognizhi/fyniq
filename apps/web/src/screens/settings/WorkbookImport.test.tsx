import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { WorkbookImport } from "./WorkbookImport";

const step = { id: "s-1", step: "M1", state: "valid", started_at: "2026-11-13T01:00:00Z", started_by: "user:u-1", workbook_file_id: "f-1", errors: 0, warnings: 1 };
const migration = { id: "m-1", status: "open", started_at: "2026-11-13T01:00:00Z", started_by: "user:u-1", steps: [step], files: [] };
const report = {
  step_id: "s-1",
  state: "valid",
  errors: 0,
  warnings: 1,
  company: "Cognizhi Pte Ltd",
  books_start: "2026-01-01",
  base_currency: "SGD",
  validated_at: "2026-11-13T01:02:00Z",
  sheets: [
    { sheet: "Accounts", rows: 85, errors: 0, warnings: 0 },
    { sheet: "Contacts", rows: 7, errors: 0, warnings: 1 },
  ],
  problems: [{ sheet: "Contacts", row: 9, column: "Email", severity: "warning", code: "MIGRATION-04", message: "the email is not an address", fix: "Correct the email" }],
};
const mapping = {
  step_id: "s-1",
  version: 2,
  signed: false,
  errors: 0,
  made_at: "2026-11-13T01:03:00Z",
  made_by: "user:u-1",
  accounts: [
    { workbook: "1020", name: "DBS current", fyniq: "1020", action: "existing" },
    { workbook: "6999", name: "Odd costs", fyniq: "6999", action: "create" },
  ],
  problems: [],
  gst_codes: [],
  departments: [],
  projects: [],
  budget_lines: [],
  bank_rules: [],
  people: [],
  natures: [],
};

const noCommit = { status: "none", done: 0, total: 0, kinds: [] };
const committed = { status: "committed", path: "history", done: 120, total: 120, committed_at: "2026-11-13T02:00:00Z", kinds: [{ kind: "journal", done: 120 }] };
// depreciationLine is a register line's key: an account, a month and a kind.
const depreciationLine = ["1590", "2026-06", "movement"].join(".");
const reconciliation = {
  status: "ready",
  path: "history",
  books_start: "2026-01-01",
  through: "2026-09",
  ledger_revision: 40,
  accounts: 85,
  figures: 1649,
  undecided: 1,
  checks_differ: 1,
  checks_run_at: "2026-11-13T03:00:00Z",
  not_compared: ["gst_f5"],
  differences: [
    { key: depreciationLine, account: "1590", name: "Accumulated depreciation", kind: "movement", month: "2026-06", workbook: "616.67", fyniq: "616.66", difference: "0.01", proposed_class: "a" },
  ],
  checks: [
    { key: "check.1", number: 1, row: 6, name: "Trial balance balances", workbook: "fine", fyniq: [{ key: "TB", status: "ok" }], same: true, missing: false },
    { key: "check.7", number: 7, row: 12, name: "Bank reconciled", workbook: "fine", fyniq: [{ key: "BANK", status: "warning" }], same: false, missing: false },
  ],
};

function started(extra = {}) {
  return answer({
    "GET /v1/migrations": { status: 200, body: { items: [migration] } },
    "GET /v1/migrations/m-1": { status: 200, body: migration, headers: { ETag: '"m1"' } },
    "GET /v1/migrations/m-1/report": { status: 200, body: report },
    "GET /v1/migrations/m-1/mapping": { status: 200, body: mapping, headers: { ETag: '"map2"' } },
    "GET /v1/migrations/m-1/commit": { status: 200, body: noCommit },
    ...extra,
  });
}

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;

// req: FR-MIG-01
// req: FR-MIG-03
describe("Import from the workbook", () => {
  it("starts the import once, with an idempotency key, and axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/migrations": { status: 200, body: { items: [] } },
      "POST /v1/migrations": { status: 201, body: migration },
    });
    renderPage(WorkbookImport, { path: "/settings/import" });
    await noViolations(document.body);
    fireEvent.click(await screen.findByRole("button", { name: "Start the import" }));
    await screen.findByRole("button", { name: "Start the import" });
    const post = seen.find((s) => s.method === "POST");
    expect(post?.path).toBe("/v1/migrations");
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("shows the steps, the workbook's report by sheet and each problem where it is", async () => {
    started();
    renderPage(WorkbookImport, { path: "/settings/import" });
    const sheets = await screen.findByRole("table", { name: "The workbook's sheets" });
    expect(within(sheets).getByText("Contacts")).toBeTruthy();
    expect(within(sheets).getByText("1 warning")).toBeTruthy();
    const problems = screen.getByRole("list", { name: "The workbook's problems" });
    expect(within(problems).getByText("Contacts, row 9, column Email: the email is not an address")).toBeTruthy();
    const steps = screen.getByRole("list", { name: "Import steps" });
    expect(within(steps).getByText("Map and sign").closest("li")?.getAttribute("aria-current")).toBe("step");
    await noViolations(document.body);
  });

  it("saves a changed account as the mapping's next version, then the Accountant signs it", async () => {
    const seen = started({
      "PATCH /v1/migrations/m-1/mapping": { status: 200, body: { ...mapping, version: 3 }, headers: { ETag: '"map3"' } },
      "POST /v1/migrations/m-1/mapping/sign-off": { status: 200, body: { ...mapping, signed: true, signed_at: "2026-11-13T01:10:00Z" } },
    });
    renderPage(WorkbookImport, { path: "/settings/import", session: accountant });
    fireEvent.change(await screen.findByLabelText("Fyniq account for 6999"), { target: { value: "6990" } });
    expect((screen.getByRole("button", { name: "Sign the mapping" }) as HTMLButtonElement).disabled).toBe(true); // a change to save first
    fireEvent.click(screen.getByRole("button", { name: "Save 1 change" }));
    await screen.findByRole("button", { name: "Save changes" });
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.headers.get("If-Match")).toBe('"map2"');
    expect(patch?.body).toEqual({ accounts: [{ workbook: "6999", name: "Odd costs", fyniq: "6990", action: "create" }] });
    fireEvent.click(screen.getByRole("button", { name: "Sign the mapping" }));
    await screen.findByRole("button", { name: "Sign the mapping" });
    const sign = seen.find((s) => s.method === "POST" && s.path.endsWith("/sign-off"));
    expect(sign?.headers.get("If-Match")).toBe('"map2"');
  });

  it("lets only the Accountant sign", async () => {
    started();
    renderPage(WorkbookImport, { path: "/settings/import" });
    expect(((await screen.findByRole("button", { name: "Sign the mapping" })) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Only the Accountant signs the mapping")).toBeTruthy();
  });

  it("checks a file of records first, saving nothing, then imports it", async () => {
    const result = { entity: "items", file_name: "items.csv", rows: 2, made: 2, errors: 0, problems: [] };
    const seen = started({ "POST /v1/imports/items": { status: 200, body: result } });
    renderPage(WorkbookImport, { path: "/settings/import" });
    fireEvent.change(await screen.findByLabelText("Records"), { target: { value: "items" } });
    fireEvent.change(screen.getByLabelText("File to import"), { target: { files: [new File(["Item code\r\n"], "items.csv", { type: "text/csv" })] } });
    expect((screen.getByRole("button", { name: "Import it" }) as HTMLButtonElement).disabled).toBe(true); // not checked yet
    fireEvent.click(screen.getByRole("button", { name: "Check the file" }));
    expect(await screen.findByText("Checked: 2 records would be made from 2 rows")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.headers.get("Prefer")).toBe("return=preview");
    fireEvent.click(screen.getByRole("button", { name: "Import it" }));
    expect(await screen.findByText("Imported: 2 records from items.csv")).toBeTruthy();
    const imported = seen.filter((s) => s.method === "POST" && s.path === "/v1/imports/items")[1];
    expect(imported?.headers.get("Prefer")).toBeNull();
    expect(imported?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("asks to commit by the chosen path over the signed mapping, then runs the import batch by batch", async () => {
    const signed = { ...mapping, signed: true, signed_at: "2026-11-13T01:10:00Z" };
    let batches = 0;
    const seen = started({
      "GET /v1/migrations/m-1/mapping": { status: 200, body: signed, headers: { ETag: '"map2"' } },
      "POST /v1/migrations/m-1/commit": { status: 202, body: { id: "a-1", status: "pending", object_type: "migration_commit" } },
    });
    renderPage(WorkbookImport, { path: "/settings/import" });
    fireEvent.change(await screen.findByLabelText("What to import"), { target: { value: "clean_start" } });
    fireEvent.click(screen.getByRole("button", { name: "Ask to commit" }));
    await vi.waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/migrations/m-1/commit")).toBe(true));
    const ask = seen.find((s) => s.method === "POST" && s.path === "/v1/migrations/m-1/commit");
    expect(ask?.headers.get("If-Match")).toBe('"map2"');
    expect(ask?.body).toEqual({ path: "clean_start" });

    // Approved: the batches run until the last is committed.
    cleanup();
    started({
      "GET /v1/migrations/m-1/mapping": { status: 200, body: signed, headers: { ETag: '"map2"' } },
      "GET /v1/migrations/m-1/commit": { status: 200, body: { ...committed, status: "running", done: 0, kinds: [] } },
      "POST /v1/migrations/m-1/commit:run": () => {
        batches++;
        return { status: 200, body: { ...committed, status: batches < 2 ? "running" : "committed", done: batches < 2 ? 50 : 120, imported: 50 } };
      },
    });
    renderPage(WorkbookImport, { path: "/settings/import" });
    fireEvent.click(await screen.findByRole("button", { name: "Run the import" }));
    expect(await screen.findByText("120 of 120 records imported")).toBeTruthy();
    expect(batches).toBe(2);
  });

  it("decides a line of the register on the report as read, and lets the Accountant sign once all are decided", async () => {
    const signed = { ...mapping, signed: true };
    const seen = started({
      "GET /v1/migrations/m-1/mapping": { status: 200, body: signed, headers: { ETag: '"map2"' } },
      "GET /v1/migrations/m-1/commit": { status: 200, body: committed },
      "GET /v1/migrations/m-1/reconciliation": { status: 200, body: reconciliation, headers: { ETag: '"rec1"' } },
      "PATCH /v1/migrations/m-1/reconciliation/register/check.7": { status: 200, body: reconciliation, headers: { ETag: '"rec2"' } },
    });
    renderPage(WorkbookImport, { path: "/settings/import", session: accountant });
    const figures = await screen.findByRole("table", { name: "Figures that differ" });
    expect(within(figures).getByText("Accumulated depreciation")).toBeTruthy();
    expect((within(figures).getByLabelText("Class of 1590.2026-06.movement") as HTMLSelectElement).value).toBe("a"); // as proposed
    const checks = screen.getByRole("list", { name: "Checks that differ" });
    expect(within(checks).getByText("Check 7: Bank reconciled")).toBeTruthy();
    fireEvent.change(within(checks).getByLabelText("Class of check.7"), { target: { value: "c" } });
    const decide = within(checks).getByRole("button", { name: "Decide" }) as HTMLButtonElement;
    expect(decide.disabled).toBe(true); // a defect needs its note
    fireEvent.change(within(checks).getByLabelText("Note on check.7"), { target: { value: "The workbook did not flag an unreconciled month" } });
    fireEvent.click(decide);
    await vi.waitFor(() => expect(seen.some((s) => s.method === "PATCH")).toBe(true));
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.headers.get("If-Match")).toBe('"rec1"');
    expect(patch?.body).toEqual({ class: "c", note: "The workbook did not flag an unreconciled month" });
    expect((screen.getByRole("button", { name: "Sign the register" }) as HTMLButtonElement).disabled).toBe(true); // one still to decide
    expect(screen.getByText("The GST F5 is compared once Fyniq makes the F5 return.")).toBeTruthy();
    await noViolations(document.body);
  });

  it("runs the parallel month on its own register, signs it, then archives and closes the import", async () => {
    const signedRec = { ...reconciliation, status: "signed", undecided: 0, sign_off: { current: true, signed_at: "2026-11-13T04:00:00Z", signed_by: "user:u-1" } };
    const parallel = { ...reconciliation, through: "2026-10", undecided: 0, differences: [], checks: [], checks_differ: 0 };
    const seen = started({
      "GET /v1/migrations/m-1/mapping": { status: 200, body: { ...mapping, signed: true }, headers: { ETag: '"map2"' } },
      "GET /v1/migrations/m-1/commit": { status: 200, body: committed },
      "GET /v1/migrations/m-1/reconciliation": { status: 200, body: signedRec, headers: { ETag: '"rec9"' } },
      "GET /v1/migrations/m-1/parallel/report": { status: 200, body: { ...report, as_of: "2026-10-31" } },
      "GET /v1/migrations/m-1/parallel": { status: 200, body: parallel, headers: { ETag: '"par1"' } },
      "POST /v1/migrations/m-1/parallel/sign-off": { status: 200, body: { ...parallel, status: "signed" }, headers: { ETag: '"par2"' } },
    });
    renderPage(WorkbookImport, { path: "/settings/import", session: accountant });
    expect(await screen.findByText("5 · The parallel month, through 2026-10")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Sign the parallel month" }));
    await vi.waitFor(() => expect(seen.some((s) => s.path === "/v1/migrations/m-1/parallel/sign-off")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/migrations/m-1/parallel/sign-off")?.headers.get("If-Match")).toBe('"par1"');

    // M6: the import's files, a PDF added, then archived and closed.
    cleanup();
    const signedPar = { ...parallel, status: "signed", sign_off: { current: true, signed_at: "2026-11-13T05:00:00Z", signed_by: "user:u-1" } };
    const files = [{ id: "f-1", kind: "workbook", file_name: "Hub.xlsx", media_type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", size_bytes: 1, added_at: "2026-11-13T01:00:00Z", added_by: "user:u-1", data_class: "general" }];
    const archived = started({
      "GET /v1/migrations/m-1": { status: 200, body: { ...migration, files }, headers: { ETag: '"m1"' } },
      "GET /v1/migrations/m-1/mapping": { status: 200, body: { ...mapping, signed: true }, headers: { ETag: '"map2"' } },
      "GET /v1/migrations/m-1/commit": { status: 200, body: committed },
      "GET /v1/migrations/m-1/reconciliation": { status: 200, body: signedRec, headers: { ETag: '"rec9"' } },
      "GET /v1/migrations/m-1/parallel/report": { status: 200, body: { ...report, as_of: "2026-10-31" } },
      "GET /v1/migrations/m-1/parallel": { status: 200, body: signedPar, headers: { ETag: '"par2"' } },
      "GET /v1/migrations/m-1/archive": { status: 200, body: { status: "open", copies: [] } },
      "POST /v1/migrations/m-1/archive/pdfs": { status: 201, body: files[0] },
      "POST /v1/migrations/m-1/archive": { status: 200, body: { status: "closed", copies: [] } },
    });
    renderPage(WorkbookImport, { path: "/settings/import", session: { ...ownerSession, roles: ["controller"] } as Session });
    const list = await screen.findByRole("list", { name: "The import's files" });
    expect(within(list).getByText("Hub.xlsx")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("The workbook's reports (PDF)"), { target: { files: [new File(["%PDF-1.7"], "Statements.pdf", { type: "application/pdf" })] } });
    fireEvent.click(screen.getByRole("button", { name: "Add the PDF" }));
    await vi.waitFor(() => expect(archived.some((s) => s.path === "/v1/migrations/m-1/archive/pdfs")).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Archive and go live" }));
    await vi.waitFor(() => expect(archived.some((s) => s.method === "POST" && s.path === "/v1/migrations/m-1/archive")).toBe(true));
    expect(archived.find((s) => s.method === "POST" && s.path === "/v1/migrations/m-1/archive")?.headers.get("If-Match")).toBe('"m1"');
    await noViolations(document.body);
  });
});
