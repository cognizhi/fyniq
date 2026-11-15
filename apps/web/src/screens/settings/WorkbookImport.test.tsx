import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
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

function started(extra = {}) {
  return answer({
    "GET /v1/migrations": { status: 200, body: { items: [migration] } },
    "GET /v1/migrations/m-1": { status: 200, body: migration, headers: { ETag: '"m1"' } },
    "GET /v1/migrations/m-1/report": { status: 200, body: report },
    "GET /v1/migrations/m-1/mapping": { status: 200, body: mapping, headers: { ETag: '"map2"' } },
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
});
