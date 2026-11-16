import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { components } from "../../api/schema";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { noAuthenticator } from "../../test/webauthn";
import { ApprovalPage } from "./ApprovalPage";
import { ProposalPage } from "./ProposalReview";
import type { Approval } from "./words";

type Proposal = components["schemas"]["AIProposalDetail"];

const hash = "cd".repeat(32);
const request: Approval = {
  id: "a-9",
  object_type: "ai_proposal",
  object_key: "p-1",
  operation: "ai_proposal.execute",
  summary: "Creates the customers of each note",
  policy: "founder/ai_proposal@2026-11-15.1",
  version: 1,
  status: "pending",
  requested_at: "2026-11-15T01:00:00Z",
  requested_by: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d99",
  requested_by_name: "Ben Ong",
  expires_at: "2026-11-18T01:00:00Z",
  current_step: 1,
  steps: [{ quorum: 1, roles: ["owner"], users: [], approvals: 0 }],
  signatures: [],
  diff: [{ field: "proposal", before: "", after: "Creates the customers of each note" }],
  envelope: '{"object":"x"}',
  envelope_hash: hash,
  you_may_approve: true,
};
const proposal: Proposal = {
  id: "p-1",
  task_type: "test.annotate_notes",
  task_name: "Annotate notes",
  task_version: 1,
  tier: 1,
  status: "pending",
  summary: "Creates the customers of each note",
  requested_by: request.requested_by,
  items: 3,
  approval_id: "a-9",
  proposed_at: "2026-11-15T01:00:00Z",
  expires_at: "2026-11-18T01:00:00Z",
  description: "Adds notes to records.",
  payload: { items: [{ note: "Room" }, { note: "Notice" }, { note: "Minutes" }] },
  payload_sha256: "ef".repeat(32),
  item_list: [
    { n: 1, item: { note: "Room" } },
    { n: 2, item: { note: "Notice" } },
    { n: 3, item: { note: "Minutes" } },
  ],
  impact: { contacts: [{ legal_name: "Room", change: "create" }] },
  evidence: [{ kind: "task", id: "agm-2027", title: "Hold the AGM" }],
  rationale: { text: "This only adds a note, nothing else.", origin: "ai" },
  agent: "chat",
  model: "claude-opus-5-5",
  prompt: "chat@3",
  you_may_approve: true,
};

function serve(p: Proposal = proposal, a: Approval = request, extra = {}) {
  return answer({
    "GET /v1/approvals/a-9": { status: 200, body: a, headers: { ETag: `"${a.version}"` } },
    "GET /v1/ai/proposals/p-1": { status: 200, body: p },
    ...extra,
  });
}

// req: AI-07
// req: AI-32
// req: AI-22
describe("an AI proposal's review", () => {
  it("shows Fyniq's summary apart from the AI's reasons, labelled as AI, and axe finds nothing", async () => {
    serve();
    const { container } = renderPage(ApprovalPage, { path: "/approvals/a-9", route: "/approvals/$approvalId" });
    expect(await screen.findByText("Written by Fyniq from the proposal and its dry run, not by the AI")).toBeTruthy();
    const reasons = screen.getByRole("heading", { name: "The AI's reasons" }).closest("section");
    expect(reasons?.className).toContain("fy-ai-card");
    expect(within(reasons as HTMLElement).getByText("This only adds a note, nothing else.")).toBeTruthy();
    const summary = screen.getByRole("heading", { name: "Summary" }).closest("section") as HTMLElement;
    expect(within(summary).queryByText(/only adds a note/)).toBeNull();
    expect(screen.getByText("Hold the AGM")).toBeTruthy();
    expect(screen.getByText("3 of 3 kept")).toBeTruthy();
    await noViolations(container);
  });

  // req: ai/task-catalogue sec.draft_document Draft only
  it("shows a drafted corporate document as its paper reads, and axe finds nothing", async () => {
    const paper = {
      title: "Directors' resolution in writing",
      author: "Cognizhi Pte. Ltd.",
      lines: ["UEN 202612345K"],
      banner: "Draft by Fyniq from the company's records on 5 January 2027. Fyniq does not sign or file it.",
      sections: [{ heading: "Resolved", items: ["That the financial statements be approved."] }],
      signatures: [{ name: "Tan Mei Lin", role: "Director" }],
      footer: "directors.approve_statements@1",
    };
    serve({ ...proposal, task_type: "sec.draft_document", task_name: "Draft a corporate document", items: 1, item_list: [],
      impact: { corporate_documents: [{ change: "draft", kind: "directors.approve_statements", title: paper.title, paper }] } });
    const { container } = renderPage(ApprovalPage, { path: "/approvals/a-9", route: "/approvals/$approvalId" });
    const changes = (await screen.findByRole("heading", { name: "What changes" })).closest("section") as HTMLElement;
    const doc = await within(changes).findByRole("article", { name: "Directors' resolution in writing" });
    expect(within(doc).getByText("That the financial statements be approved.")).toBeTruthy();
    expect(within(doc).getByText("Tan Mei Lin, Director")).toBeTruthy();
    expect(within(doc).getByText(/Fyniq does not sign or file it/)).toBeTruthy();
    expect(within(changes).queryByText("paper")).toBeNull();
    await noViolations(container);
  });

  it("approves only the items kept, named in the approval", async () => {
    noAuthenticator();
    const seen = serve(proposal, request, { "POST /v1/approvals/a-9/approve": { status: 200, body: { ...request, status: "approved" } } });
    renderPage(ApprovalPage, { path: "/approvals/a-9", route: "/approvals/$approvalId" });
    fireEvent.click(await screen.findByRole("checkbox", { name: "Keep item 2" }));
    expect(await screen.findByText("2 of 3 kept")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Approve…" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/items 1, 3 of the AI's proposal/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: /^Approve/ }));
    await waitFor(() => expect(seen.some((s) => s.path.endsWith("/approve"))).toBe(true));
    expect(seen.find((s) => s.path.endsWith("/approve"))?.body).toEqual({ envelope_hash: hash, items: [1, 3] });
  });

  it("approves a tier-1 proposal whole without naming its items, and never with none kept", async () => {
    noAuthenticator();
    const seen = serve(proposal, request, { "POST /v1/approvals/a-9/approve": { status: 200, body: { ...request, status: "approved" } } });
    renderPage(ApprovalPage, { path: "/approvals/a-9", route: "/approvals/$approvalId" });
    for (const n of [1, 2, 3]) fireEvent.click(await screen.findByRole("checkbox", { name: `Keep item ${n}` }));
    expect((screen.getByRole("button", { name: "Approve…" }) as HTMLButtonElement).disabled).toBe(true);
    for (const n of [1, 2, 3]) fireEvent.click(screen.getByRole("checkbox", { name: `Keep item ${n}` }));
    fireEvent.click(screen.getByRole("button", { name: "Approve…" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: /^Approve/ }));
    await waitFor(() => expect(seen.some((s) => s.path.endsWith("/approve"))).toBe(true));
    expect(seen.find((s) => s.path.endsWith("/approve"))?.body).toEqual({ envelope_hash: hash });
  });

  it("shows a decided proposal as it is, with nothing to choose", async () => {
    serve({ ...proposal, status: "rejected", reason: "Not this year", you_may_approve: false });
    renderPage(ProposalPage, { path: "/inbox/proposals/p-1", route: "/inbox/proposals/$proposalId" });
    expect(await screen.findByText("Not this year")).toBeTruthy();
    expect((screen.getByRole("checkbox", { name: "Keep item 1" }) as HTMLInputElement).disabled).toBe(true);
  });
});
