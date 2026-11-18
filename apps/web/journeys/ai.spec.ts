import { api, expect, organisation, test } from "./harness";
import type { Page } from "@playwright/test";

// The AI journeys (QA-E2E-01; p1-launch-readiness design D6, the owner's decision of 17 Nov 2026): the AI on, its chat
// agent routed to local mode's scripted model, which answers these two questions as a right model would. So they run
// the real path offline on every pull request: the panel, the API, the AI service, the read tools, numeric grounding, a
// proposal, its approval in the Inbox and the executor. How well a model answers is the eval suites' (QA-AI-01…03).
// Requirements: AI-08 "Numbers come only from tools"; AI-68 "Every figure states where it comes from"; AI-P3 and AI-32
// "AI proposes; a named human approves"; AI-65's tier-1 company-secretary task type.

interface Fact {
  id: string;
  measure: string;
  value?: string;
  currency?: string;
  from?: string;
  to?: string;
}

interface Turn {
  question: string;
  status: string;
  cited: Fact[];
}

interface Proposal {
  id: string;
  task_type: string;
  status: string;
  summary: string;
  approval_id?: string;
}

// ask opens the assistant on page and asks it question, as a person does.
async function ask(page: Page, question: string) {
  await page.getByRole("button", { name: "Assistant" }).click();
  const panel = page.getByRole("complementary", { name: "Assistant" });
  await panel.getByLabel("Ask the assistant").fill(question);
  await panel.getByRole("button", { name: "Send" }).click();
  return panel;
}

// lastTurn is the person's latest conversation's last turn, as the API kept it.
async function lastTurn(page: Page): Promise<Turn> {
  const list = await api<{ items: { id: string }[] }>(page, "/v1/assistant/conversations");
  const c = await api<{ turns: Turn[] }>(page, `/v1/assistant/conversations/${list.items[0]!.id}`);
  return c.turns.at(-1)!;
}

// query is the semantic layer's own answer, asked from the page as the web app asks it (a write's same-origin rule).
async function query(page: Page, body: object): Promise<{ facts: Fact[] }> {
  return page.evaluate(async (b) => {
    const r = await fetch("/api/v1/metrics:query", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(b) });
    if (!r.ok) throw new Error(`POST /api/v1/metrics:query: ${r.status} ${await r.text()}`);
    return (await r.json()) as { facts: Fact[] };
  }, body);
}

// req: QA-E2E-01
// req: AI-08
// req: AI-68
test("AI ask: the Owner asks for January's revenue; the answer's figure is the books' and says where it comes from", async ({ browser }) => {
  const j = await organisation(browser, "Journey AI Ask Pte Ltd", [{ key: "owner", name: "Ann Owner", roles: ["owner"] }], { scriptedAI: true });
  const { page } = j.people.owner!;
  await page.goto("/");
  const panel = await ask(page, "What was revenue in January 2026?");

  const books = (await query(page, { measure: "revenue", from: "2026-01", to: "2026-01" })).facts[0]!;
  expect(books).toMatchObject({ measure: "revenue", from: "2026-01-01", to: "2026-01-31", currency: "SGD" });
  const shown = Number(books.value).toLocaleString("en-SG", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  await expect(panel.getByText(shown).first()).toBeVisible({ timeout: 30_000 });
  await expect(panel.getByRole("group", { name: "Where the figures come from" }).getByRole("link").first()).toBeVisible();

  const turn = await lastTurn(page);
  expect(turn.status).toBe("answered");
  expect(turn.cited).toHaveLength(1);
  expect(turn.cited[0]).toMatchObject({ measure: "revenue", value: books.value, currency: "SGD", from: "2026-01-01", to: "2026-01-31" });
});

// req: QA-E2E-01
// req: AI-P3
// req: AI-32
test("AI propose → approve → execute: the AI proposes a compliance task, the Company secretary approves it, and it exists", async ({ browser }) => {
  test.setTimeout(9 * 60_000); // the executor takes the approved proposal at the stack's next tick: 20 s in CI, 5 minutes by default
  const j = await organisation(
    browser,
    "Journey AI Propose Pte Ltd",
    [
      { key: "owner", name: "Ann Owner", roles: ["owner"] },
      { key: "cos", name: "Cos Secretary", roles: ["company_secretary"] },
    ],
    { scriptedAI: true },
  );
  const ann = j.people.owner!.page;
  await ann.goto("/");
  const panel = await ask(
    ann,
    'Create a compliance task titled "Book the room for the AGM" for the company secretary, due by 30 June 2027, with the finding key agent:agm-room-2027.',
  );
  await expect(panel.getByText("I proposed it. It waits for your approval in Fyniq.")).toBeVisible({ timeout: 30_000 });

  // The proposal waits, and nothing changed yet: no task exists until a person approves it.
  const listed = await api<{ items: Proposal[] }>(ann, "/v1/ai/proposals");
  const p = listed.items.find((x) => x.task_type === "sec.create_task");
  expect(p).toMatchObject({ status: "pending" });
  const before = await api<{ items: { title: string }[] }>(ann, "/v1/compliance/tasks");
  expect(before.items.map((t) => t.title)).not.toContain("Book the room for the AGM");

  // The Company secretary, who approves the task type, approves it in the Inbox.
  const cos = j.people.cos!.page;
  await cos.goto("/inbox");
  await cos.getByRole("link", { name: /Book the room for the AGM/ }).first().click();
  await expect(cos).toHaveURL(new RegExp(`/approvals/${p!.approval_id}$`));
  await expect(cos.getByText("Book the room for the AGM").first()).toBeVisible();
  await expect(cos.getByText("Approving it now would be refused")).toHaveCount(0); // approving signs; the executor makes it
  await cos.getByRole("button", { name: "Approve…" }).click();
  await cos.getByRole("dialog").getByRole("button", { name: /^Approve/ }).click();
  await expect(cos.getByText("Approved: the change is made.")).toBeVisible();

  // The executor makes it, as the approving person: the task exists, and the proposal says it was executed.
  await expect
    .poll(async () => (await api<Proposal>(ann, `/v1/ai/proposals/${p!.id}`)).status, { timeout: 6 * 60_000, intervals: [5_000] })
    .toBe("executed");
  const after = await api<{ items: { title: string; owner_role?: string; due_on?: string }[] }>(cos, "/v1/compliance/tasks");
  expect(after.items.find((t) => t.title === "Book the room for the AGM")).toMatchObject({ due_on: "2027-06-30" });
});
