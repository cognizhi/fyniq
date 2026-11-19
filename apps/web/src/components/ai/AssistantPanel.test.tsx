import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { routeOf } from "./assistant";
import { AssistantPanel } from "./AssistantPanel";

const revenue = { id: "F00000000000000aa", measure: "revenue", name: "Revenue", value: "48200.00", figure: "SGD 48,200.00", unit: "amount", currency: "SGD",
  from: "2026-10-01", to: "2026-10-31", ledger_revision: 1204, source: "the profit and loss's revenue", lineage: "/v1/reports/statements/pl?from=2026-10&to=2026-10&as_at_revision=1204" };
const turn = (t: Record<string, unknown>) => ({ id: "t-1", question: "What was our revenue last month?", status: "answered", answer: "", segments: [], cited: [], sources: [],
  regenerated: false, withheld: 0, asked_at: "2026-11-15T01:00:00Z", ...t });
const stream = (...events: [string, unknown][]) => ({ status: 200, events: events.map(([e, d]) => `event: ${e}\ndata: ${JSON.stringify(d)}\n\n`).join("") });

function panel(props: { enabled?: boolean; mode?: "basic" | "expert"; context?: { kind: "invoice" | "bill" | "figure"; id?: string; label: string } } = {}) {
  return () => <AssistantPanel enabled={props.enabled ?? true} mode={props.mode ?? "basic"} context={props.context} onClearContext={() => {}} onClose={() => {}} />;
}

// req: AI-22
// req: AI-08
// req: AI-68
// req: FR-UX-17
describe("The assistant panel", () => {
  it("shows a know-how answer with a chip to the guide page it comes from, never to a compiled page (AI-51)", async () => {
    answer({
      "POST /v1/assistant/conversations": { status: 201, body: { id: "c-1", title: "How soon must a claim be submitted?", created_at: "2026-11-15T01:00:00Z" } },
      "POST /v1/assistant/conversations/c-1/turns": stream(
        ["answer", turn({
          segments: [{ text: "A claim is submitted within 30 days of the receipt date.", sources: [{ guide: "claims.approve-team", title: "Approving your team's claims", anchor: "who-approves", href: "/help/claims.approve-team#who-approves" }] }],
          sources: [{ guide: "claims.approve-team", title: "Approving your team's claims", anchor: "who-approves", href: "/help/claims.approve-team#who-approves" },
            { guide: "claims.make-a-claim", title: "Make a claim", anchor: "receipts", draft: true, href: "/help/claims.make-a-claim#receipts" }],
        })],
      ),
    });
    renderPage(panel());
    fireEvent.click(await screen.findByRole("button", { name: "What was our revenue last month?" }));
    const words = await screen.findByText(/within 30 days of the receipt date/);
    const message = words.closest(".fy-msg--ai") as HTMLElement;
    const group = within(message).getByRole("group", { name: "Where the explanation comes from" });
    const chip = within(group).getByRole("link", { name: /Approving your team's claims/ });
    expect(chip.getAttribute("href")).toBe("/help/claims.approve-team#who-approves");
    expect(within(group).getByRole("link", { name: /Make a claim \(awaiting review\)/ })).toBeTruthy();
    for (const a of within(message).getAllByRole("link")) expect(a.getAttribute("href") ?? "").not.toMatch(/knowledge|kb\//);
    await noViolations();
  });

  it("answers in violet with the AI badge, each figure from its fact with a chip to its report, streaming its status; axe finds nothing", async () => {
    const seen = answer({
      "POST /v1/assistant/conversations": { status: 201, body: { id: "c-1", title: "What was our revenue last month?", created_at: "2026-11-15T01:00:00Z" } },
      "POST /v1/assistant/conversations/c-1/turns": stream(
        ["status", { text: "Reading the books" }],
        ["status", { text: "Reading the books", tool: "metrics.query" }],
        ["answer", turn({ segments: [{ text: "Revenue in October was " }, { fact: revenue.id, figure: revenue.figure }, { text: "." }], cited: [revenue] })],
      ),
    });
    renderPage(panel());
    expect(await screen.findByRole("group", { name: "Suggested questions" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "What was our revenue last month?" }));
    const fact = await screen.findByText("SGD 48,200.00");
    expect(fact.className).toContain("fy-fact");
    const message = fact.closest(".fy-msg--ai") as HTMLElement;
    expect(within(message).getByText("AI")).toBeTruthy();
    const chip = within(message).getByRole("link", { name: /Revenue, 1 Oct 2026 to 31 Oct 2026/ });
    expect(chip.getAttribute("href")).toBe("/reports/statements/pl");
    expect(within(message).queryByText(/ledger revision 1204/)).toBeNull(); // Basic mode: no query details
    expect(seen.find((x) => x.path === "/v1/assistant/conversations")?.body).toEqual({ title: "What was our revenue last month?" });
    expect(seen.find((x) => x.path === "/v1/assistant/conversations/c-1/turns")?.body).toEqual({ question: "What was our revenue last month?" });
    expect(screen.getByRole("status").textContent).toBe("The assistant answered.");
    await noViolations();
  });

  it("shows each figure's source and query in Expert mode", async () => {
    answer({
      "POST /v1/assistant/conversations": { status: 201, body: { id: "c-1", title: "Revenue?", created_at: "2026-11-15T01:00:00Z" } },
      "POST /v1/assistant/conversations/c-1/turns": stream(["answer", turn({ segments: [{ fact: revenue.id, figure: revenue.figure }], cited: [revenue] })]),
    });
    renderPage(panel({ mode: "expert" }));
    fireEvent.change(await screen.findByLabelText("Ask the assistant"), { target: { value: "Revenue?" } });
    fireEvent.keyDown(screen.getByLabelText("Ask the assistant"), { key: "Enter" });
    expect(await screen.findByText(/revenue · 48200.00 · \/v1\/reports\/statements\/pl\?from=2026-10.* · ledger revision 1204/)).toBeTruthy();
  });

  // req: AI-69
  it("renders only Fyniq's Markdown subset: text never becomes HTML, and links go only to Fyniq's own pages", async () => {
    answer({
      "POST /v1/assistant/conversations": { status: 201, body: { id: "c-1", title: "x", created_at: "2026-11-15T01:00:00Z" } },
      "POST /v1/assistant/conversations/c-1/turns": stream(["answer", turn({ segments: [
        { text: "**Cash** is fine. <img src=x onerror=alert(1)> See [the KPIs](/reports/kpis), not [this](https://evil.example/steal) or [that](//evil.example).\n\n- one item" },
      ] })]),
    });
    const { container } = renderPage(panel());
    fireEvent.change(await screen.findByLabelText("Ask the assistant"), { target: { value: "Cash?" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    const strong = await screen.findByText("Cash");
    expect(strong.tagName).toBe("STRONG");
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText(/<img src=x onerror=alert\(1\)>/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "the KPIs" }).getAttribute("href")).toBe("/reports/kpis");
    expect(screen.queryByRole("link", { name: "this" })).toBeNull();
    expect(screen.queryByRole("link", { name: "that" })).toBeNull();
    expect(screen.getByText(/one item/)).toBeTruthy();
  });

  it("says the assistant is switched off and sends nothing", async () => {
    const seen = answer({});
    renderPage(panel({ enabled: false }));
    expect(await screen.findByText("The assistant is switched off")).toBeTruthy();
    expect((screen.getByLabelText("Ask the assistant") as HTMLTextAreaElement).disabled).toBe(true);
    expect(screen.queryByRole("group", { name: "Suggested questions" })).toBeNull();
    expect(seen).toHaveLength(0);
    await noViolations();
  });

  it("says the budget is spent, and offers a new conversation when one is full", async () => {
    answer({
      "POST /v1/assistant/conversations": { status: 201, body: { id: "c-1", title: "x", created_at: "2026-11-15T01:00:00Z" } },
      "POST /v1/assistant/conversations/c-1/turns": stream(["error", { code: "AI-BUDGET-02", detail: "The organisation's AI budget is spent this month.", fix: "An Owner can raise it." }]),
    });
    renderPage(panel());
    fireEvent.change(await screen.findByLabelText("Ask the assistant"), { target: { value: "Revenue?" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByText("The AI budget is spent for this month")).toBeTruthy();
    expect(screen.getByText("An Owner can raise it.")).toBeTruthy();

    answer({ "POST /v1/assistant/conversations/c-1/turns": problem(409, "AI-LIMIT-03", "This conversation has 40 questions, the most one may hold.", "Start a new conversation") });
    fireEvent.change(screen.getByLabelText("Ask the assistant"), { target: { value: "And costs?" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.click(await screen.findByRole("button", { name: "Start a new conversation" }));
    expect(screen.getByRole("group", { name: "Suggested questions" })).toBeTruthy();
  });

  it("lists your conversations, opens one with its turns and deletes one after asking", async () => {
    const seen = answer({
      "GET /v1/assistant/conversations": { status: 200, body: { items: [{ id: "c-9", title: "Cash runway", created_at: "2026-11-14T01:00:00Z" }] } },
      "GET /v1/assistant/conversations/c-9": { status: 200, body: { id: "c-9", title: "Cash runway", created_at: "2026-11-14T01:00:00Z",
        turns: [turn({ id: "t-9", question: "What is our runway?", segments: [{ text: "Cash lasts " }, { missing: "the burn rate." }] })] } },
      "DELETE /v1/assistant/conversations/c-9": { status: 204 },
    });
    renderPage(panel());
    fireEvent.click(await screen.findByRole("button", { name: "Your conversations" }));
    fireEvent.click(await screen.findByRole("button", { name: /^Cash runway/ }));
    expect(await screen.findByText("What is our runway?")).toBeTruthy();
    expect(screen.getByText("(I could not establish the burn rate)")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Your conversations" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete Cash runway" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete Cash runway for good" }));
    await waitFor(() => expect(seen.some((x) => x.method === "DELETE" && x.path === "/v1/assistant/conversations/c-9")).toBe(true));
  });

  it("opens the page each fact came from", () => {
    expect(routeOf("/v1/reports/statements/bs?from=2026-01&to=2026-10")).toBe("/reports/statements/bs");
    expect(routeOf("/v1/invoices/0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b")).toBe("/sales/invoices/0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b");
    expect(routeOf("/v1/reports/general-ledger?account_id=x&from=2026-10-01")).toBe("/reports/general-ledger");
    expect(routeOf("/v1/search?q=Tan")).toBe("/search?q=Tan");
    expect(routeOf("/v1/statutory-values?pack=sg-gst")).toBeUndefined();
  });
});

const bill = { kind: "bill" as const, id: "b-1", label: "Supplier Co · S-77" };
const projection = (lowest: string, on: string, below?: string) => ({ today: "2026-11-15", days: 90, currency: "SGD", opening: { amount: "50000.00", currency: "SGD" },
  lowest: { amount: lowest, currency: "SGD" }, lowest_on: on, below_buffer_on: below, buffer: {}, daily: [], weekly: [], not_included: [],
  movements: [{ key: "b-1", kind: "bill", id: "b-1", number: "S-77", expected_on: "2026-11-20", basis: "due", amount: { amount: "-8000.00", currency: "SGD" } }] });

// req: FR-UX-17
// req: QA-AI-05
describe("The assistant panel's lanes and ratings", () => {
  it("rates an answer with a reason, once, saying who reviews it", async () => {
    const seen = answer({
      "POST /v1/assistant/conversations": { status: 201, body: { id: "c-1", title: "x", created_at: "2026-11-15T01:00:00Z" } },
      "POST /v1/assistant/conversations/c-1/turns": stream(["answer", turn({ segments: [{ text: "Revenue fell." }] })]),
      "POST /v1/assistant/conversations/c-1/turns/t-1/feedback": { status: 201, body: { id: "f-1", turn_id: "t-1", conversation_id: "c-1", person: "user:u", rating: "down",
        reason: "Wrong month.", classes: [], given_at: "2026-11-15T01:01:00Z", state: "waiting", masked: false } },
    });
    renderPage(panel());
    fireEvent.change(await screen.findByLabelText("Ask the assistant"), { target: { value: "Revenue?" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.click(await screen.findByRole("button", { name: "Not helpful" }));
    fireEvent.change(screen.getByLabelText("What was wrong with it?"), { target: { value: "Wrong month." } });
    fireEvent.click(screen.getByRole("button", { name: "Send the rating" }));
    expect(await screen.findByText(/You rated it not helpful: an Owner or Administrator will review it/)).toBeTruthy();
    expect(seen.find((x) => x.path.endsWith("/feedback"))?.body).toEqual({ rating: "down", reason: "Wrong month." });
    expect(screen.queryByRole("button", { name: "Helpful" })).toBeNull();
    await noViolations();
  });

  it("explains what is asked about, sending its context with the question", async () => {
    const seen = answer({
      "POST /v1/assistant/conversations": { status: 201, body: { id: "c-1", title: "x", created_at: "2026-11-15T01:00:00Z" } },
      "POST /v1/assistant/conversations/c-1/turns": stream(["answer", turn({ segments: [{ text: "It is the bill's total." }] })]),
    });
    renderPage(panel({ context: bill }));
    expect(await screen.findByText("Supplier Co · S-77")).toBeTruthy(); // the context chip
    fireEvent.click(screen.getByRole("radio", { name: "Explain" }));
    fireEvent.click(screen.getByRole("button", { name: "Explain Supplier Co · S-77" }));
    await waitFor(() => expect(seen.find((x) => x.path === "/v1/assistant/conversations/c-1/turns")?.body).toEqual({
      question: "Explain Supplier Co · S-77: where it comes from and what it is made of.", context: bill }));
  });

  it("simulates the bill asked about through the cash what-if, saving nothing; moves between lanes with the arrow keys", async () => {
    const seen = answer({
      "GET /v1/cash-projection": { status: 200, body: projection("12000.00", "2026-11-20", "2026-11-20") },
      "POST /v1/cash-projection:what-if": { status: 200, body: projection("20000.00", "2026-12-31") },
    });
    renderPage(panel({ context: bill }));
    const ask = await screen.findByRole("radio", { name: "Ask" });
    fireEvent.keyDown(ask, { key: "ArrowLeft" });
    expect(screen.getByRole("radio", { name: "Simulate" }).getAttribute("aria-checked")).toBe("true");
    expect(await screen.findByText(/Supplier Co · S-77: -8,000.00 SGD|Supplier Co · S-77: −8,000.00 SGD/)).toBeTruthy();
    fireEvent.click(screen.getByLabelText(/it does not move in the next 90 days/));
    fireEvent.click(screen.getByRole("button", { name: "Simulate" }));
    const table = await screen.findByRole("table", { name: "The cash projection, now and in the what-if" });
    expect(within(table).getByText(/12,000.00, 20 Nov 2026/)).toBeTruthy();
    expect(within(table).getByText(/20,000.00, 31 Dec 2026/)).toBeTruthy();
    expect(within(table).getByText("None")).toBeTruthy();
    expect(seen.find((x) => x.path === "/v1/cash-projection:what-if")?.body).toEqual({ days: 90, moves: [{ key: "b-1", leave_out: true }] });
    expect(screen.getByText(/Nothing was saved or changed/)).toBeTruthy();
    expect(screen.queryByLabelText("Ask the assistant")).toBeNull();
    await noViolations();
  });

  it("asks for an invoice or a bill to simulate when there is none", async () => {
    answer({});
    renderPage(panel());
    fireEvent.click(await screen.findByRole("radio", { name: "Simulate" }));
    expect(screen.getByRole("link", { name: "Cash ahead" }).getAttribute("href")).toBe("/planning/cash");
  });
});

