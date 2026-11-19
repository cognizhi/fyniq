// AI-55 is only part proved here: the recompile button waits for the job's trigger (p1-knowledge-base task 8.2), so no `req:` tag.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { diffLines, KnowledgeBase } from "./KnowledgeBase";

const entry = (key: string, extra = {}) => ({ key, source: "organisation", type: "policy", title: "Expense policy", summary: "How claims are made.", data_class: "general", state: "published", published_version: 1, newest_version: 1,
  origin: "human_edited", open_findings: 0, ...extra });
const platform = { key: "founders.balance", source: "platform", type: "concept", title: "What the company owes a founder", summary: "s", data_class: "general", state: "platform", published_version: 0, newest_version: 0, origin: "", open_findings: 0 };
const version = (number: number, state: string, body: string, extra = {}) => ({ key: "org.expense-policy", number, state, origin: "human_edited", type: "policy", title: "Expense policy", summary: "How claims are made.", aliases: [], body,
  sources: [{ kind: "note", ref: "expense-policy", title: "Expense policy" }], data_class: "general", author: "user:u-1", created_at: "2026-11-18T08:00:00Z", human_edited: true, ...extra });
const finding = { id: "f-1", kind: "human_edit_conflict", page: "org.expense-policy", version: 2, detail: "The note changed after a person edited the page.", status: "open", created_at: "2026-11-18T09:00:00Z" };

function base(extra = {}) {
  return answer({
    "GET /v1/knowledge-base/pages": { status: 200, body: { items: [entry("org.expense-policy", { state: "draft", newest_version: 2, open_findings: 1 }), entry("org.pay", { state: "unpublished", published_version: 0, origin: "compiled", title: "Pay" }), platform] } },
    "GET /v1/knowledge-base/findings": { status: 200, body: { items: [finding] } },
    "GET /v1/knowledge-base/snapshots": { status: 200, body: { items: [{ number: 3, reason: "First.", created_at: "2026-11-18T08:00:00Z" }] } },
    "GET /v1/knowledge-base/notes": { status: 200, body: { items: [{ key: "expense-policy", kind: "policy", title: "Expense policy", body: "Claims are made within 14 days.", data_class: "general", version: 2, saved_by: "user:u-1", saved_at: "2026-11-18T08:00:00Z" }] } },
    "GET /v1/members": { status: 200, body: { items: [{ user_id: "u-1", display_name: "Ah Kow" }] } },
    ...extra,
  });
}

describe("Settings › AI › Knowledge base", () => {
  it("lists Fyniq's pages and the organisation's with where each stands, filters and searches them; axe finds nothing", async () => {
    base();
    renderPage(KnowledgeBase, { path: "/settings/ai/knowledge-base" });
    expect((await screen.findAllByRole("button", { name: "Open org.expense-policy" })).length).toBe(2);
    expect(screen.getByText("founders.balance")).toBeTruthy();
    expect(screen.getByText("Newer draft")).toBeTruthy();
    expect(screen.getByText("1 open")).toBeTruthy();
    expect(screen.getByText("Snapshot 3 · 2 waiting")).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: "Platform 1" }));
    expect(within(screen.getByRole("table")).queryByRole("button", { name: "Open org.expense-policy" })).toBeNull();
    fireEvent.click(screen.getByRole("radio", { name: /All/ }));
    fireEvent.change(screen.getByLabelText("Search pages"), { target: { value: "pay" } });
    expect(screen.queryByText("founders.balance")).toBeNull();
    expect(within(screen.getByRole("table")).getByRole("button", { name: "Open org.pay" })).toBeTruthy();
    await noViolations();
  });

  it("shows a page's versions and what changed, saves an edit as the next version with the page's ETag, and rolls back", async () => {
    const seen = base({
      "GET /v1/knowledge-base/pages/org.expense-policy/versions": { status: 200, body: { key: "org.expense-policy", items: [version(2, "draft", "# E\n\nClaims in 30 days.\n"), version(1, "published", "# E\n\nClaims in 14 days.\n")] } },
      "PUT /v1/knowledge-base/pages/org.expense-policy": { status: 201, body: version(3, "draft", "x") },
      "POST /v1/knowledge-base/pages/org.expense-policy/rollback": { status: 201, body: version(3, "draft", "x") },
    });
    renderPage(KnowledgeBase, { path: "/settings/ai/knowledge-base" });
    fireEvent.click((await screen.findAllByRole("button", { name: "Open org.expense-policy" }))[0]!);
    expect(await screen.findByRole("heading", { name: "Expense policy", level: 2 })).toBeTruthy();
    const changes = within(await screen.findByRole("group", { name: "What changed" }));
    expect(changes.getByText("Claims in 14 days.").closest("div")?.textContent).toContain("Removed:");
    expect(changes.getByText("Claims in 30 days.").closest("div")?.textContent).toContain("Added:");
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Page text"), { target: { value: "# E\n\nClaims in 14 days. (src: note:expense-policy#a)\n" } });
    fireEvent.click(screen.getByRole("button", { name: "Save as a draft" }));
    await waitFor(() => expect(seen.some((x) => x.method === "PUT")).toBe(true));
    const put = seen.find((x) => x.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"v2"');
    expect(put?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(put?.body).toMatchObject({ type: "policy", title: "Expense policy", sources: [{ kind: "note", ref: "expense-policy" }] });
    fireEvent.click(await screen.findByRole("button", { name: "Roll back to v1" }));
    await waitFor(() => expect(seen.some((x) => x.path.endsWith("/rollback"))).toBe(true));
    expect(seen.find((x) => x.path.endsWith("/rollback"))?.body).toEqual({ version: 1 });
    await noViolations();
  });

  it("publishes chosen drafts, and in Team mode asks another Owner instead", async () => {
    const seen = base({
      "POST /v1/knowledge-base/publications": (body: unknown) => ((body as { pages: unknown[] }).pages.length === 1 ? problem(409, "KB-TEAM-01", "In Team mode another Owner signs a publication.") : { status: 201, body: { number: 4, reason: "", created_at: "2026-11-19T08:00:00Z" } }),
      "POST /v1/knowledge-base/publications/approval": { status: 202, body: { id: "a-1", object_type: "kb_publication", status: "pending" } },
    });
    renderPage(KnowledgeBase, { path: "/settings/ai/knowledge-base" });
    const publish = await screen.findByRole("button", { name: /^Publish\s*pages?$/ });
    expect((publish as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: /org.expense-policy/ }));
    fireEvent.change(screen.getByLabelText("Why (optional)"), { target: { value: "The policy changed." } });
    fireEvent.click(screen.getByRole("button", { name: /^Publish 1 page$/ }));
    expect(await screen.findByText("The publication waits for another Owner to sign it. It is in the approvals.")).toBeTruthy();
    expect(seen.filter((x) => x.method === "POST").map((x) => x.path)).toEqual(["/v1/knowledge-base/publications", "/v1/knowledge-base/publications/approval"]);
    expect(seen.find((x) => x.path.endsWith("/approval"))?.body).toEqual({ pages: [{ key: "org.expense-policy", version: 2 }], reason: "The policy changed." });
    fireEvent.click(screen.getByRole("checkbox", { name: /org.expense-policy/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /org.pay/ }));
    fireEvent.click(screen.getByRole("button", { name: /^Publish 2 pages$/ }));
    expect(await screen.findByText("Published as snapshot 4. The assistant reads these pages from now on.")).toBeTruthy();
    await noViolations();
  });

  it("resolves a finding with what was done, and writes and changes a note; a refusal is shown in words", async () => {
    const seen = base({
      "POST /v1/knowledge-base/findings/f-1/resolve": { status: 200, body: { ...finding, status: "resolved", resolution: "Kept the person's version." } },
      "POST /v1/knowledge-base/notes": problem(422, "API-INPUT-02", "The note holds what looks like an identity number."),
      "PUT /v1/knowledge-base/notes/expense-policy": { status: 200, body: {} },
    });
    renderPage(KnowledgeBase, { path: "/settings/ai/knowledge-base", session: ownerSession });
    fireEvent.click(await screen.findByRole("button", { name: /^Resolve:/ }));
    const resolve = screen.getAllByRole("button", { name: "Resolve" }).find((b) => !(b as HTMLButtonElement).disabled === false) as HTMLButtonElement;
    expect(resolve.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("What was done about it"), { target: { value: "Kept the person's version." } });
    fireEvent.click(screen.getAllByRole("button", { name: "Resolve" }).find((b) => !(b as HTMLButtonElement).disabled) as HTMLButtonElement);
    await waitFor(() => expect(seen.some((x) => x.path.endsWith("/resolve"))).toBe(true));
    expect(seen.find((x) => x.path.endsWith("/resolve"))?.body).toEqual({ resolution: "Kept the person's version." });
    fireEvent.click(screen.getByRole("button", { name: "Write a note" }));
    fireEvent.change(screen.getByLabelText("Key"), { target: { value: "travel" } });
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Travel" } });
    fireEvent.change(screen.getByLabelText("What it says"), { target: { value: "Book early." } });
    fireEvent.click(screen.getByRole("button", { name: "Save the note" }));
    expect(await screen.findByText("That was not done")).toBeTruthy();
    expect(screen.getByText(/identity number/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Edit Expense policy" }));
    expect(screen.queryByLabelText("Key")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save the note" }));
    await waitFor(() => expect(seen.some((x) => x.method === "PUT")).toBe(true));
    expect(seen.find((x) => x.method === "PUT")?.headers.get("If-Match")).toBe('"v2"');
  });
});

describe("diffLines", () => {
  it("marks what was removed and added and leaves the rest", () => {
    expect(diffLines("a\nb\nc", "a\nx\nc")).toEqual([
      { kind: "same", text: "a" },
      { kind: "add", text: "x" },
      { kind: "del", text: "b" },
      { kind: "same", text: "c" },
    ]);
  });
});
