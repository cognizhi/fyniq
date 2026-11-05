import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, calls, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ChartOfAccounts } from "./ChartOfAccounts";
import { kindsLeft, requiredOn } from "./Dimensions";

type Dimension = components["schemas"]["Dimension"];
type DimensionValue = components["schemas"]["DimensionValue"];
type Account = components["schemas"]["Account"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const rent = { id: "a1", code: "6110", name: "Rent", type: "expense", active: true, group: false, control: false, money_account: false, payroll_cost: false, parent_id: null, version: 1 };
const dim = (id: string, kind: Dimension["kind"], name: string, extra: Partial<Dimension> = {}) => ({ id, kind, name, active: true, version: 1, ...extra }) as Dimension;
const value = (id: string, dimension_id: string, code: string, name: string, extra: Partial<DimensionValue> = {}) =>
  ({ id, dimension_id, code, name, active: true, version: 1, ...extra }) as DimensionValue;
const dept = dim("d1", "department", "Department");
const grant = dim("d2", "custom", "Grant", { place: 1, active: false });
const eng = value("v1", "d1", "ENG", "Engineering");
const ops = value("v2", "d1", "OPS", "Operations", { active: false });

const page = (dims: Dimension[], values: DimensionValue[], more: Record<string, Parameters<typeof answer>[0][string]> = {}) =>
  answer({
    "GET /v1/accounts": { status: 200, body: { items: [rent] } },
    "GET /v1/dimensions": { status: 200, body: { items: dims } },
    "GET /v1/dimension-values": { status: 200, body: { items: values } },
    ...more,
  });

// req: FR-DIM-01
describe("the chart's dimensions", () => {
  it("offers Department and Project once each and up to 8 custom dimensions", () => {
    expect(kindsLeft([])).toEqual(["department", "project", "custom"]);
    expect(kindsLeft([dept, grant])).toEqual(["project", "custom"]);
    const eight = Array.from({ length: 8 }, (_, i) => dim(`c${i}`, "custom", `C${i}`, { place: i + 1 }));
    expect(kindsLeft([dept, dim("p", "project", "Project"), ...eight])).toEqual([]);
  });

  it("says which accounts require each dimension, by code", async () => {
    const travel = { ...rent, id: "a2", code: "6200", name: "Travel", required_dimension_ids: ["d1"] } as Account;
    const office = { ...rent, id: "a3", code: "6110", name: "Rent", required_dimension_ids: ["d1", "d2"] } as Account;
    const project = dim("d3", "project", "Project");
    expect(requiredOn(dept, [travel, office])).toBe("6110 Rent, 6200 Travel");
    expect(requiredOn(project, [travel, office])).toBe("Optional everywhere");
    answer({
      "GET /v1/accounts": { status: 200, body: { items: [office, travel] } },
      "GET /v1/dimensions": { status: 200, body: { items: [dept, project, grant] } },
      "GET /v1/dimension-values": { status: 200, body: { items: [eng] } },
    });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    const table = await screen.findByRole("table", { name: "Dimensions" });
    expect(within(table).getByRole("columnheader", { name: "Required on" })).toBeTruthy();
    const [, department, proj, custom] = within(table).getAllByRole("row") as [HTMLElement, HTMLElement, HTMLElement, HTMLElement];
    expect(within(department).getByText("6110 Rent, 6200 Travel")).toBeTruthy();
    expect(within(proj).getByText("Optional everywhere")).toBeTruthy();
    expect(within(custom).getByText("Nowhere while out of use")).toBeTruthy(); // Grant requires nothing while out of use
  });

  it("lists each dimension with its values in use, marks one out of use, and axe finds nothing", async () => {
    page([dept, grant], [eng, ops]);
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    const table = await screen.findByRole("table", { name: "Dimensions" });
    const [, department, custom] = within(table).getAllByRole("row") as [HTMLElement, HTMLElement, HTMLElement];
    expect(within(department).getByText("Engineering")).toBeTruthy();
    expect(within(department).queryByText(/Operations/)).toBeNull();
    expect(within(custom).getByText("Custom")).toBeTruthy();
    expect(within(custom).getByText("Out of use")).toBeTruthy();
    expect(screen.getByText("2 of 10 used · Department and Project are built in")).toBeTruthy();
    await noViolations(document.body);
  });

  it("adds a dimension of a kind still free, checking its name first", async () => {
    const seen = page([dept], [], { "POST /v1/dimensions": { status: 201, body: dim("d3", "custom", "Region", { place: 1 }) } });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Add a dimension" }));
    const dialog = await screen.findByRole("dialog");
    const kind = within(dialog).getByLabelText("Kind") as HTMLSelectElement;
    expect(Array.from(kind.options).map((o) => o.value)).toEqual(["project", "custom"]);
    expect((within(dialog).getByLabelText("Name") as HTMLInputElement).value).toBe("Project");
    fireEvent.change(kind, { target: { value: "custom" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the dimension" }));
    expect(await within(dialog).findByText(/Give the dimension a name/)).toBeTruthy();
    expect(calls()).not.toContain("POST /v1/dimensions");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: " Region " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the dimension" }));
    expect(await screen.findByText("Region added.")).toBeTruthy();
    const post = seen.find((s) => s.method === "POST");
    expect(post?.body).toEqual({ kind: "custom", name: "Region" });
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("renames a dimension and puts it out of use, over its version", async () => {
    const seen = page([dept], [eng], { "PUT /v1/dimensions/d1": { status: 200, body: { ...dept, name: "Teams", active: false, version: 2 } } });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Change Department" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByLabelText("Kind")).toBeNull();
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Teams" } });
    fireEvent.click(within(dialog).getByLabelText("In use: takes new values"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the dimension" }));
    expect(await screen.findByText("Teams saved.")).toBeTruthy();
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.body).toEqual({ name: "Teams", active: false });
    expect(put?.headers.get("If-Match")).toBe('"1"');
  });

  it("adds a value, checking its code first, and changes another over its version", async () => {
    const seen = page([dept], [eng, ops], {
      "POST /v1/dimension-values": { status: 201, body: value("v3", "d1", "SALES", "Sales") },
      "PUT /v1/dimension-values/v2": { status: 200, body: { ...ops, name: "Operations", active: true, version: 2 } },
    });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Values of Department" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Out of use")).toBeTruthy();
    const form = within(dialog).getByRole("form", { name: "Add a value" });
    fireEvent.change(within(form).getByLabelText("Code"), { target: { value: "R&D" } });
    fireEvent.change(within(form).getByLabelText("Name"), { target: { value: "Sales" } });
    fireEvent.click(within(form).getByRole("button", { name: "Add the value" }));
    expect(await within(form).findByText(/Give a code of a letter or digit/)).toBeTruthy();
    fireEvent.change(within(form).getByLabelText("Code"), { target: { value: "SALES" } });
    fireEvent.click(within(form).getByRole("button", { name: "Add the value" }));
    expect(await screen.findByText("SALES Sales added.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ dimension_id: "d1", code: "SALES", name: "Sales" });

    fireEvent.click(within(dialog).getByRole("button", { name: "Change OPS Operations" }));
    const change = within(dialog).getByRole("form", { name: "Change OPS" });
    expect(within(change).queryByLabelText("Code")).toBeNull();
    fireEvent.click(within(change).getByLabelText("In use: lines can take it"));
    fireEvent.click(within(change).getByRole("button", { name: "Save the value" }));
    expect(await screen.findByText("OPS Operations saved.")).toBeTruthy();
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.body).toEqual({ name: "Operations", active: true });
    expect(put?.headers.get("If-Match")).toBe('"1"');
  });

  it("shows the API's refusal in plain words", async () => {
    page([dept], [], { "POST /v1/dimension-values": problem(422, "DIM-03", "Department has the value ENG already, named Engineering.") });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Values of Department" }));
    const form = within(await screen.findByRole("dialog")).getByRole("form", { name: "Add a value" });
    fireEvent.change(within(form).getByLabelText("Code"), { target: { value: "eng" } });
    fireEvent.change(within(form).getByLabelText("Name"), { target: { value: "Engineering" } });
    fireEvent.click(within(form).getByRole("button", { name: "Add the value" }));
    expect(await screen.findByText("The value was not added")).toBeTruthy();
    expect(screen.getByText(/has the value ENG already/)).toBeTruthy();
  });

  it("shows a Bookkeeper the dimensions and their values without the buttons that change them", async () => {
    page([dept, grant], [eng]);
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: bookkeeper });
    await screen.findByRole("table", { name: "Dimensions" });
    expect(screen.queryByRole("button", { name: "Add a dimension" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Change Department" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Values of Department" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Engineering")).toBeTruthy();
    expect(within(dialog).queryByRole("form")).toBeNull();
    await waitFor(() => expect(within(dialog).queryByRole("button", { name: /Change ENG/ })).toBeNull());
  });

  it("offers no new values for a dimension out of use", async () => {
    page([grant], []);
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Values of Grant" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/Grant is out of use: put it back in use to add values/)).toBeTruthy();
    expect(within(dialog).queryByRole("form")).toBeNull();
  });
});
