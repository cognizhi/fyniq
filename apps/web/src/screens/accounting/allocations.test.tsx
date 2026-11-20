import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Allocations } from "./Allocations";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const rule = { id: "33333333-3333-4333-8333-333333333333", version: 1, name: "Rent by department", accounts: ["a-rent"], key: "fixed", targets: [{ label: "Engineering", values: ["v-eng"], percent: "40" }, { label: "Sales", values: ["v-sales"], percent: "35" }] };
const accounts = { items: [{ id: "a-rent", code: "6200", name: "Office rent", type: "expense", active: true, group: false, version: 1 }, { id: "a-bank", code: "1010", name: "Bank", type: "asset", active: true, group: false, version: 1 }] };
const dimensions = { items: [{ id: "d-dept", kind: "department", name: "Department", active: true, place: 0, version: 1 }] };
const values = { items: [{ id: "v-eng", dimension_id: "d-dept", code: "ENG", name: "Engineering", active: true, version: 1 }, { id: "v-sales", dimension_id: "d-dept", code: "SALES", name: "Sales", active: true, version: 1 }] };
const periods = { items: [{ id: "p-oct", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open", financial_year: 1, version: 1 }] };
const report = {
  rule_id: rule.id, key: "fixed", from: "2026-10-01", to: "2026-10-31", weights: [{ target: "Engineering", weight: 4000 }, { target: "Sales", weight: 3500 }], unplaced_weight: 2500,
  splits: [{ account_id: "a-rent", account_code: "6200", account_name: "Office rent", source: "1000.00", parts: [{ target: "Engineering", amount: "400.00" }, { target: "Sales", amount: "350.00" }], unallocated: "250.00" }],
  lines: [{ account_id: "a-rent", amount: "400.00", description: "Engineering", values: ["v-eng"] }, { account_id: "a-rent", amount: "350.00", description: "Sales", values: ["v-sales"] }, { account_id: "a-rent", amount: "-750.00", description: "Allocated", values: [] }],
  note: "Nothing is posted: the journal is a draft that an approver posts like any other (FR-DIM-03).", proposed: false,
};

// req: FR-DIM-03
// req: QA-UI-05
describe("Allocations", () => {
  it("opens a rule, previews a month with the part left unallocated, and proposes the draft journal; axe finds nothing", async () => {
    answer({
      "GET /v1/allocation-rules": { status: 200, body: { items: [rule] } },
      ["GET /v1/allocation-rules/" + rule.id]: { status: 200, body: rule, headers: { ETag: '"1-draft"' } },
      "GET /v1/accounts": { status: 200, body: accounts },
      "GET /v1/dimensions": { status: 200, body: dimensions },
      "GET /v1/dimension-values": { status: 200, body: values },
      "GET /v1/periods": { status: 200, body: periods },
      ["POST /v1/allocation-rules/" + rule.id + "/preview"]: { status: 200, body: report },
      ["POST /v1/allocation-rules/" + rule.id + "/runs"]: { status: 201, body: { ...report, proposed: true, journal_id: "44444444-4444-4444-8444-444444444444" } },
    });
    renderPage(Allocations, { path: "/accounting/allocations", session: accountant, mode: "expert" });
    await screen.findByRole("option", { name: "Rent by department" });
    fireEvent.change(screen.getByLabelText("Open a rule"), { target: { value: rule.id } });
    await screen.findByDisplayValue("Rent by department");
    fireEvent.click(await screen.findByRole("button", { name: /Preview/ }));
    expect(await screen.findByText("Unallocated (stays on the account)")).toBeTruthy();
    expect(screen.getByText("250.00")).toBeTruthy();
    expect(seen.find((s) => s.path.endsWith("/preview"))!.body).toMatchObject({ from: "2026-10-01", to: "2026-10-31" });
    fireEvent.click(screen.getByRole("button", { name: /Propose the journal/ }));
    await waitFor(() => expect(seen.some((s) => s.path.endsWith("/runs"))).toBe(true));
    expect(await screen.findByText(/The draft journal is made/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("saves a new rule with its percentages, and shows a refusal in plain words", async () => {
    answer({
      "GET /v1/allocation-rules": { status: 200, body: { items: [] } },
      "GET /v1/accounts": { status: 200, body: accounts },
      "GET /v1/dimensions": { status: 200, body: dimensions },
      "GET /v1/dimension-values": { status: 200, body: values },
      "GET /v1/periods": { status: 200, body: periods },
      "POST /v1/allocation-rules": { status: 422, body: { type: "urn:fyniq:problem:API-INVALID-01", title: "A field is not valid", status: 422, code: "API-INVALID-01", detail: "The rule has problems.", fix: "Fix them", errors: [{ code: "ALLOC-01", field: "body.targets", message: "the percentages add up to more than 100", fix: "Lower a percentage" }] } },
    });
    renderPage(Allocations, { path: "/accounting/allocations", session: accountant, mode: "expert" });
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "Rent by department" } });
    fireEvent.click(await screen.findByLabelText(/6200 Office rent/));
    fireEvent.click(screen.getByRole("button", { name: /Save the rule/ }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/allocation-rules")).toBe(true));
    expect(seen.find((s) => s.method === "POST")!.body).toMatchObject({ name: "Rent by department", key: "fixed", accounts: ["a-rent"] });
    expect(await screen.findByText(/more than 100/)).toBeTruthy();
  });
});
