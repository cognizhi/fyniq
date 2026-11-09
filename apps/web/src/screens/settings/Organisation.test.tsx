import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Organisation, valueText } from "./Organisation";
import { StatutoryPacks } from "./StatutoryPacks";

const terms = {
  key: "sales.payment_terms_days", group: "Defaults", label: "Payment terms for invoices", kind: "days", value: "30", recommended_value: "30", status: "recommended",
  reason: "An invoice to a customer with no terms of its own falls due 30 days after its date", needs_second_approver: false, set_by_platform: false, lengthen_only: false,
  min: "0", max: "365", requirement: "FR-ORG-04", guide: "settings.organisation#payment-terms", scheduled: [], version: 2,
};
const threshold = { ...terms, key: "assets.capitalisation_threshold", label: "Capitalisation threshold", kind: "amount_sgd", value: "1000.00", recommended_value: "1000.00", needs_second_approver: true, version: 0, min: undefined, max: undefined };
const region = { ...terms, key: "data.region", group: "Data region and retention", label: "Data region", kind: "text", value: "asia-southeast1", recommended_value: "asia-southeast1", set_by_platform: true };
const profile = { legal_name: "Cognizhi Pte Ltd", uen: "202612345K", fy_start_month: 4, time_zone: "Asia/Singapore", version: 5 };
const registration = { id: "r-1", registered_from: "2026-04-01", gst_number: "202612345K", return_frequency: "quarterly", quarter_start_month: 1, accounting_basis: "invoice", version: 2, recorded_at: "2026-04-02T01:00:00Z" };
const invoices = { document_type: "invoice", prefix: "INV-", next_number: 42, digits: 4, gap_free: true, example: "INV-0042", version: 0 };
const journals = { document_type: "journal", prefix: "JNL-", next_number: 7, digits: 4, gap_free: true, example: "JNL-0007", version: 3 };

function page(extra = {}) {
  return answer({
    "GET /v1/organisation/settings": { status: 200, body: { items: [terms, threshold, region] } },
    "GET /v1/organisation/profile": { status: 200, body: profile, headers: { ETag: '"5"' } },
    "GET /v1/organisation/settings/sales.payment_terms_days": { status: 200, body: { ...terms, changes: [{ version: 1, value: "14", effective_from: "2026-01-01", recorded_at: "2026-01-01T01:00:00Z" }] }, headers: { ETag: '"2"' } },
    "GET /v1/organisation/settings/assets.capitalisation_threshold": { status: 200, body: { ...threshold, changes: [] }, headers: { ETag: '"0"' } },
    "GET /v1/organisation/gst-registrations": { status: 200, body: { items: [registration] } },
    "GET /v1/organisation/gst-status": { status: 200, body: { date: "2026-11-07", registered: true, registration, return_period: { starts_on: "2026-10-01", ends_on: "2026-12-31" } } },
    "GET /v1/organisation/sequences": { status: 200, body: { items: [journals, invoices] } },
    ...extra,
  });
}

// req: FR-ORG-04
// req: FR-ORG-08
// req: FR-INT-14
describe("Organisation settings", () => {
  it("writes each value by its kind", () => {
    expect(valueText("switch", "true")).toBe("On");
    expect(valueText("days", "1")).toBe("1 day");
    expect(valueText("minutes", "30")).toBe("30 minutes");
    expect(valueText("years", "7")).toBe("7 years");
    expect(valueText("months", "6")).toBe("6 months");
    expect(valueText("months", "1")).toBe("1 month");
    expect(valueText("amount_sgd", "1000.00")).toBe("S$1,000.00");
    expect(valueText("percent", "3")).toBe("3%");
  });

  it("shows the profile and each setting with its status, and axe finds nothing", async () => {
    page();
    renderPage(Organisation, { path: "/settings/organisation" });
    expect(await screen.findByText("Cognizhi Pte Ltd")).toBeTruthy();
    expect(screen.getByText("April")).toBeTruthy();
    const defaults = await screen.findByRole("list", { name: "Defaults" });
    expect(within(defaults).getByText("S$1,000.00")).toBeTruthy();
    expect(within(defaults).getByText("Needs a second approver")).toBeTruthy();
    expect(within(screen.getByRole("list", { name: "Data region and retention" })).getByText("Set by the platform")).toBeTruthy();
    await noViolations(document.body);
  });

  it("previews a change without saving, then saves it from a date over the version shown", async () => {
    const seen = page({ "PATCH /v1/organisation/settings/sales.payment_terms_days": (_b: unknown, req: Request) => ({ status: 200, body: { ...terms, value: "45", status: "changed", version: req.headers.get("Prefer") ? 2 : 3 } }) });
    renderPage(Organisation, { path: "/settings/organisation" });
    fireEvent.click(await screen.findByRole("button", { name: "Change: Payment terms for invoices" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText("14 days")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("New value"), { target: { value: "45" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Preview" }));
    expect(await within(dialog).findByText(/passes every rule. Nothing is saved yet/)).toBeTruthy();
    const preview = seen.find((s) => s.method === "PATCH");
    expect(preview?.headers.get("Prefer")).toBe("return=preview");
    expect(preview?.headers.get("If-Match")).toBe('"2"');
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/Payment terms for invoices: 45 days from/)).toBeTruthy();
    const saves = seen.filter((s) => s.method === "PATCH");
    expect(saves).toHaveLength(2);
    expect(saves[1]?.headers.get("Prefer")).toBeNull();
    expect((saves[1]?.body as { value: string }).value).toBe("45");
  });

  it("refuses a value of the wrong shape before asking", async () => {
    page();
    renderPage(Organisation, { path: "/settings/organisation" });
    fireEvent.click(await screen.findByRole("button", { name: "Change: Capitalisation threshold" }));
    const dialog = await screen.findByRole("dialog");
    const input = await within(dialog).findByLabelText("New value");
    fireEvent.change(input, { target: { value: "S$1,500" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask for approval" }));
    expect(await within(dialog).findByText(/without the S\$ or commas/)).toBeTruthy();
    expect(calls()).not.toContain("PATCH /v1/organisation/settings/assets.capitalisation_threshold");
  });

  // req: FM-1
  it("says a setting under dual control waits for approval", async () => {
    page({ "PATCH /v1/organisation/settings/assets.capitalisation_threshold": { status: 202, body: { id: "a-40", summary: "Change the capitalisation threshold to 1,500.00", status: "pending" } } });
    renderPage(Organisation, { path: "/settings/organisation" });
    fireEvent.click(await screen.findByRole("button", { name: "Change: Capitalisation threshold" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(await within(dialog).findByLabelText("New value"), { target: { value: "1500.00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText(/Change the capitalisation threshold to 1,500.00/)).toBeTruthy();
    expect(screen.getByText("Waiting for approval")).toBeTruthy();
  });

  // req: FR-ORG-01
  it("saves only the profile fields changed, over the organisation's version", async () => {
    const seen = page({ "PATCH /v1/organisation/profile": { status: 200, body: { ...profile, uen: "202699999K", version: 6 } } });
    renderPage(Organisation, { path: "/settings/organisation" });
    fireEvent.click(await screen.findByRole("button", { name: "Edit the profile" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("UEN"), { target: { value: "202699999K" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the profile" }));
    await waitFor(() => expect(calls()).toContain("PATCH /v1/organisation/profile"));
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.body).toEqual({ uen: "202699999K" });
    expect(patch?.headers.get("If-Match")).toBe('"5"');
  });
});

// req: FR-ORG-03
// req: FR-ORG-04
describe("GST registration and document numbering", () => {
  it("shows today's GST status, each registration and the next numbers, and axe finds nothing", async () => {
    page();
    renderPage(Organisation, { path: "/settings/organisation" });
    expect(await screen.findByText("Registered today")).toBeTruthy();
    expect(screen.getByText(/return period 1 Oct 2026 to 31 Dec 2026/)).toBeTruthy();
    const registrations = await screen.findByRole("list", { name: "GST registration" });
    expect(within(registrations).getByText("From 1 Apr 2026")).toBeTruthy();
    expect(within(registrations).getByText(/Quarterly returns, quarters from January, April, July and October/)).toBeTruthy();
    const numbering = await screen.findByRole("list", { name: "Document numbering" });
    expect(within(numbering).getByText("INV-0042")).toBeTruthy();
    await noViolations(document.body);
  });

  it("records a registration as IRAS's notice gives it, and ends one by changing only its end", async () => {
    const seen = page({
      "POST /v1/organisation/gst-registrations": { status: 201, body: { ...registration, id: "r-2", gst_number: "M90012345A", registered_from: "2028-01-01", version: 1 } },
      "PATCH /v1/organisation/gst-registrations/r-1": { status: 200, body: { ...registration, deregistered_from: "2027-07-01", version: 3 } },
    });
    renderPage(Organisation, { path: "/settings/organisation" });
    fireEvent.click(await screen.findByRole("button", { name: "Record a registration" }));
    let dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("GST registration number"), { target: { value: " M90012345A " } });
    fireEvent.change(within(dialog).getByLabelText("Registered from"), { target: { value: "2028-01-01" } });
    fireEvent.change(within(dialog).getByLabelText("Returns"), { target: { value: "monthly" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record the registration" }));
    expect(await screen.findByText("The GST registration M90012345A is recorded.")).toBeTruthy();
    const post = seen.find((x) => x.method === "POST");
    expect(post?.body).toEqual({ registered_from: "2028-01-01", gst_number: "M90012345A", return_frequency: "monthly", quarter_start_month: 1 });
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();

    fireEvent.click(await screen.findByRole("button", { name: "Change: 202612345K" }));
    dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Deregistered from (optional)"), { target: { value: "2027-07-01" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the change" }));
    await waitFor(() => expect(calls()).toContain("PATCH /v1/organisation/gst-registrations/r-1"));
    const patch = seen.find((x) => x.method === "PATCH");
    expect(patch?.body).toEqual({ deregistered_from: "2027-07-01" });
    expect(patch?.headers.get("If-Match")).toBe('"2"');
  });

  it("changes a document's numbering over its version, showing the next number first", async () => {
    const seen = page({ "PATCH /v1/organisation/sequences/invoice": { status: 200, body: { ...invoices, prefix: "INV/26/", next_number: 100, example: "INV/26/0100", version: 1 } } });
    renderPage(Organisation, { path: "/settings/organisation" });
    fireEvent.click(await screen.findByRole("button", { name: "Change: Invoices" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Prefix"), { target: { value: "inv/26/" } });
    fireEvent.change(within(dialog).getByLabelText("Next number"), { target: { value: "100" } });
    expect(within(dialog).getByText("INV/26/0100")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the numbering" }));
    expect(await screen.findByText("Invoices are numbered from INV/26/0100.")).toBeTruthy();
    const patch = seen.find((x) => x.method === "PATCH");
    expect(patch?.body).toEqual({ prefix: "INV/26/", next_number: 100 });
    expect(patch?.headers.get("If-Match")).toBe('"0"');
  });
});

const gst = { id: "sg-gst@2026.1", pack: "sg-gst", title: "Singapore GST", effective_from: "1994-04-01", review_date: "2027-01-01", digest: "ab".repeat(32), pinned_passed: 22, pinned_total: 22, readable: true };

// req: ENG-32
// req: FR-ORG-05
describe("Statutory parameter packs", () => {
  it("lists the versions and asks to activate one whose pinned tests pass", async () => {
    const seen = answer({
      "GET /v1/statutory-packs": { status: 200, body: { items: [gst] } },
      "GET /v1/approvals": { status: 200, body: { items: [] } },
      "GET /v1/statutory-packs/sg-gst@2026.1": {
        status: 200,
        body: { ...gst, verifier: "A founder", conditions: "", sources: [{ id: "s1", kind: "official guidance", title: "GST rates", url: "https://www.iras.gov.sg/", retrieved: "2026-10-01" }], pinned: [{ case: "standard rate", table: "rates", want: "9", got: "9", pass: true, why: "IRAS page" }] },
        headers: { ETag: '"0"' },
      },
      "POST /v1/statutory-packs/sg-gst@2026.1/activate": { status: 202, body: { id: "a-50", summary: "Activate sg-gst@2026.1", status: "pending" } },
    });
    renderPage(StatutoryPacks, { path: "/settings/statutory-packs" });
    fireEvent.click(await screen.findByRole("button", { name: /Singapore GST/ }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByRole("link", { name: /GST rates/ })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(within(dialog).getByRole("button", { name: "Activate" }));
    expect(await screen.findByText(/Activate sg-gst@2026.1/)).toBeTruthy();
    const post = seen.find((s) => s.method === "POST");
    expect(post?.headers.get("If-Match")).toBe('"0"');
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
  });
});
