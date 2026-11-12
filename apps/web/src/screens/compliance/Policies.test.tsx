import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PoliciesToAcknowledge, PolicyLibrary } from "./Policies";

const dpo = { ...ownerSession, roles: ["dpo"] } as Session;
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const sha = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08";
const version = (n: number, title: string, acknowledged = 1) => ({
  version: n, title, sha256: sha, from: n === 1 ? "fyniq-policy-draft@1" : `revision ${n - 1}`, published_by: "user:u-1", published_at: "2026-11-02T02:00:00Z", acknowledged,
});
const policy = (key: string, title: string, over: object = {}) => ({
  key, title, members: 4, draft: { source: "fyniq", revision: 0, sha256: sha, unpublished: true }, ...over,
});
const library = [
  policy("information-security", "Information security policy", {
    current: version(1, "Information security policy", 3), draft: { source: "fyniq", revision: 0, sha256: sha, unpublished: false },
    mine: { version: 1, due_on: "2026-12-02", acknowledged_at: "2026-11-03T01:00:00Z", overdue: false },
  }),
  policy("acceptable-use", "Acceptable use policy", {
    current: version(2, "Acceptable use policy", 1), draft: { source: "organisation", revision: 1, sha256: sha, unpublished: false },
    mine: { version: 2, due_on: "2026-11-01", overdue: true },
  }),
  policy("data-protection", "Data protection policy", {
    current: version(1, "Data protection policy", 2), draft: { source: "organisation", revision: 2, sha256: sha, unpublished: true },
    mine: { version: 1, due_on: "2026-12-02", overdue: false },
  }),
  policy("backup", "Backup policy"),
];
const one = (key: string, over: object = {}) => ({ ...library.find((p) => p.key === key)!, body: "# Policy\n\nThe text in force.\n", ...over });

// req: CMP-07
describe("The policy library", () => {
  it("asks a member to acknowledge the versions waiting for them, overdue ones called out, and records it; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/compliance/policies": { status: 200, body: { items: library } },
      "GET /v1/compliance/policies/acceptable-use": { status: 200, body: one("acceptable-use") },
      "POST /v1/compliance/policies/acceptable-use/versions/2/acknowledgements": { status: 201, body: one("acceptable-use") },
    });
    renderPage(PoliciesToAcknowledge, { path: "/inbox", session: bookkeeper });
    const list = await screen.findByRole("list", { name: "Acknowledge policy" });
    const items = within(list).getAllByRole("listitem");
    expect(items.map((li) => li.querySelector(".fy-list__title")?.textContent)).toEqual(["Acceptable use policy", "Data protection policy"]);
    const [overdue, due] = [items[0]!, items[1]!];
    expect(within(overdue).getByText(/Overdue/)).toBeTruthy();
    expect(within(due).getByText(/Due/)).toBeTruthy();
    await noViolations();

    fireEvent.click(within(overdue).getByRole("button", { name: "Read and acknowledge Acceptable use policy" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText(/The text in force/)).toBeTruthy();
    await noViolations();
    fireEvent.click(within(dialog).getByRole("button", { name: /I have read and accept it/ }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    const ack = seen.find((s) => s.method === "POST")!;
    expect(ack.path).toBe("/v1/compliance/policies/acceptable-use/versions/2/acknowledgements");
    expect(ack.headers.get("If-Match")).toBe('"v2"');
    expect(ack.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("shows nothing when no version waits", async () => {
    answer({ "GET /v1/compliance/policies": { status: 200, body: { items: [library[0]!, library[3]!] } } });
    const { container } = renderPage(PoliciesToAcknowledge, { path: "/inbox", session: bookkeeper });
    await waitFor(() => expect(container.querySelector(".fy-card")).toBeNull());
  });

  it("lists the library with each version's acknowledgements; a member neither drafts nor publishes", async () => {
    answer({ "GET /v1/compliance/policies": { status: 200, body: { items: library } } });
    renderPage(PolicyLibrary, { path: "/compliance/risks", session: bookkeeper });
    const list = await screen.findByRole("list", { name: "Policy library" });
    expect(within(list).getByText(/Version 1, published .* · 3 of 4 acknowledged/)).toBeTruthy();
    expect(within(list).getByText("Not published yet")).toBeTruthy();
    expect(within(list).queryByText(/draft/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /^Draft / })).toBeNull();
    await noViolations();
  });

  it("lets the Compliance officer adopt Fyniq's draft, and save an edit before publishing it; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/compliance/policies": { status: 200, body: { items: library } },
      "GET /v1/compliance/policies/backup": { status: 200, body: one("backup", { body: undefined, draft: { source: "fyniq", revision: 0, sha256: sha, unpublished: true, body: "# Backup policy\n" } }) },
      "POST /v1/compliance/policies/backup/versions": { status: 201, body: one("backup") },
      "GET /v1/compliance/policies/data-protection": {
        status: 200, body: one("data-protection", { draft: { source: "organisation", revision: 2, sha256: sha, unpublished: true, body: "# Data protection policy\n" } }),
      },
      "PUT /v1/compliance/policies/data-protection/draft": { status: 200, body: one("data-protection") },
    });
    renderPage(PolicyLibrary, { path: "/compliance/risks", session: dpo });
    const list = await screen.findByRole("list", { name: "Policy library" });
    expect(within(list).getByText(/Fyniq's draft ready to adopt/)).toBeTruthy();
    expect(within(list).getByText(/draft revision 2 not published/)).toBeTruthy();

    fireEvent.click(within(list).getByRole("button", { name: "Draft Backup policy" }));
    let dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText(/Fyniq's starting draft/)).toBeTruthy();
    await noViolations();
    fireEvent.click(within(dialog).getByRole("button", { name: "Publish version 1" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    const published = seen.find((s) => s.method === "POST")!;
    expect(published.path).toBe("/v1/compliance/policies/backup/versions");
    expect(published.headers.get("If-Match")).toBe('"r0"');
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(within(list).getByRole("button", { name: "Draft Data protection policy" }));
    dialog = await screen.findByRole("dialog");
    const text = await within(dialog).findByLabelText("Text (Markdown)");
    fireEvent.change(text, { target: { value: "# Data protection policy\n\nOur own words.\n" } });
    const publish = within(dialog).getByRole("button", { name: "Publish version 2" }) as HTMLButtonElement;
    expect(publish.disabled).toBe(true);
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the draft" }));
    await waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true));
    const saved = seen.find((s) => s.method === "PUT")!;
    expect(saved.headers.get("If-Match")).toBe('"r2"');
    expect(saved.body).toEqual({ body: "# Data protection policy\n\nOur own words.\n" });
  });
});
