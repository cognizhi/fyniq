import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { SignedInProvider } from "../../session";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { JournalPage } from "./JournalPage";

type Journal = components["schemas"]["Journal"];
type Evidence = components["schemas"]["Evidence"];

const organisation: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const accounts = { status: 200, body: { items: [] } };
const draft: Journal = {
  id: "j1",
  status: "draft",
  version: 2,
  date: "2026-10-31",
  memo: "October rent accrual",
  lines: [
    { account_id: "a-rent", amount: { amount: "1200.00", currency: "SGD" } },
    { account_id: "a-accruals", amount: { amount: "-1200.00", currency: "SGD" } },
  ],
};
const file = (over: Partial<Evidence> = {}): Evidence => ({
  id: "ev1",
  journal_id: "j1",
  evidence_type: "calculation",
  file_name: "rent-accrual.xlsx",
  media_type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  size_bytes: 18432,
  sha256: "a".repeat(64),
  original_sha256: "a".repeat(64),
  source: "upload",
  data_class: "general",
  uploaded_by: "user:u1",
  uploaded_at: "2026-10-31T01:00:00Z",
  state: "attached",
  version: 1,
  ...over,
});

function page(session: Session) {
  return function Page() {
    return (
      <SignedInProvider value={{ session, organisation, mode: "expert" }}>
        <JournalPage />
      </SignedInProvider>
    );
  };
}
const at = { path: "/accounting/journals/j1", route: "/accounting/journals/$journalId" };

// req: FR-DOC-05
// req: FR-DOC-07
describe("a journal's evidence", () => {
  it("attaches a file with what it is to a draft at the version read, and takes it off again; axe finds nothing", async () => {
    let files: Evidence[] = [];
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1": { status: 200, body: draft, headers: { ETag: `"2-draft"` } },
      "GET /v1/journals/j1/evidence": () => ({ status: 200, body: { items: files } }),
      "POST /v1/journals/j1/evidence": () => {
        files = [file()];
        return { status: 201, body: file() };
      },
      "GET /v1/journals/j1/evidence/ev1": { status: 200, body: file(), headers: { ETag: `"1"` } },
      "DELETE /v1/journals/j1/evidence/ev1": () => {
        files = [file({ state: "removed", version: 2 })];
        return { status: 200, body: files[0] };
      },
    });
    renderPage(page(accountant), at);
    expect(await screen.findByText("No files are attached.")).toBeTruthy();
    // Nothing goes without the file and what it is.
    fireEvent.click(screen.getByRole("button", { name: "Attach" }));
    expect(await screen.findByText("Choose the file to attach.")).toBeTruthy();
    expect(screen.getByText("Say what the file is, such as a calculation or an invoice.")).toBeTruthy();
    expect(seen.filter((x) => x.method === "POST")).toEqual([]);
    await noViolations(document.body);

    const chosen = new File(["2026,rent,1200"], "rent-accrual.xlsx", { type: "application/octet-stream" });
    fireEvent.change(screen.getByLabelText("File"), { target: { files: [chosen] } });
    fireEvent.change(screen.getByLabelText("What it is"), { target: { value: " calculation " } });
    fireEvent.change(screen.getByLabelText("Note (optional)"), { target: { value: "Pro rata for 31 days" } });
    fireEvent.click(screen.getByRole("button", { name: "Attach" }));
    expect(await screen.findByText("Attached rent-accrual.xlsx. It can come off while the journal is a draft.")).toBeTruthy();
    const sent = seen.find((x) => x.method === "POST")!;
    expect(sent.headers.get("Content-Type")).toMatch(/^multipart\/form-data; boundary=/);
    expect(sent.headers.get("If-Match")).toBe(`"2-draft"`);
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    // The form's fields, and a part for the file: the test DOM's File reaches the runtime's request without its bytes,
    // which a browser sends, so only the part is checked here.
    expect(sent.body).toMatchObject({ evidence_type: "calculation", note: "Pro rata for 31 days", data_class: "general", source: "upload" });
    expect((sent.body as Record<string, unknown>).file).toBeDefined();
    const list = await screen.findByRole("list", { name: "Files attached" });
    expect(within(list).getByText("rent-accrual.xlsx")).toBeTruthy();
    expect(within(list).getByText(/calculation · 18 KB · added/)).toBeTruthy();

    fireEvent.click(within(list).getByRole("button", { name: "Remove rent-accrual.xlsx" }));
    expect(await screen.findByText("Removed rent-accrual.xlsx.")).toBeTruthy();
    const removed = seen.find((x) => x.method === "DELETE")!;
    expect(removed.headers.get("If-Match")).toBe(`"1"`);
    expect(await within(list).findByText("Removed")).toBeTruthy();
  });

  it("keeps a posted journal's files, and lets those who cannot attach only see them", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1": { status: 200, body: { ...draft, status: "posted", number: "JNL-0007" }, headers: { ETag: `"2-posted"` } },
      "GET /v1/journals/j1/evidence": { status: 200, body: { items: [file({ state: "final", retain_until: "2034-01-01T00:00:00+08:00" })] } },
    });
    renderPage(page(auditor), at);
    expect(await screen.findByText("Kept")).toBeTruthy();
    expect(screen.getByText(/Once it is posted they are kept/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Attach" })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Remove/ })).toBeNull();
  });

  it("shows why a file was refused", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/journals/j1": { status: 200, body: draft, headers: { ETag: `"2-draft"` } },
      "GET /v1/journals/j1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/journals/j1/evidence": problem(422, "EVIDENCE-02", "The file is not a type Fyniq keeps.", "Attach a PDF, an image or a spreadsheet."),
    });
    renderPage(page(accountant), at);
    fireEvent.change(await screen.findByLabelText("File"), { target: { files: [new File(["MZ"], "tool.exe")] } });
    fireEvent.change(screen.getByLabelText("What it is"), { target: { value: "receipt" } });
    fireEvent.click(screen.getByRole("button", { name: "Attach" }));
    expect(await screen.findByText("The file was not attached")).toBeTruthy();
  });
});
