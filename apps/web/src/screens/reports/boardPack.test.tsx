import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BoardPortal } from "../board/BoardPortal";
import { InvestorUpdates } from "../equity/InvestorUpdates";
import { BoardPack } from "./BoardPack";

type Pack = components["schemas"]["BoardPack"];
type Period = components["schemas"]["Period"];

const controller = { ...ownerSession, roles: ["controller"] } as Session;
const board = { ...ownerSession, roles: ["board_viewer"] } as Session;
const period = (id: string, starts: string, ends: string): Period => ({ id, starts_on: starts, ends_on: ends, status: "open", financial_year: 1, version: 1 });
const periods = [period("p-oct", "2026-10-01", "2026-10-31")];
const draft: Pack = { id: "22222222-2222-4222-8222-222222222222", month: "2026-10", version: 1, status: "draft", highlights: "Revenue up", commentary: "", asks: "", edit: 2, updated_at: "2026-11-04T02:00:00Z", preview: true };
const published: Pack = {
  ...draft, status: "published", preview: undefined, ledger_revision: 42, period_status: "open", edit: 3,
  figures: { pl: { kind: "pl", currency: "SGD", ledger_revision: 42, columns: [{ key: "current", label: "Oct 2026" }], blocks: [{ key: "revenue", title: "Revenue", totals: [{ amount: "6000.00", currency: "SGD" }] }] } },
};

// req: FR-RPT-06
// req: FR-EQ-23
// req: FR-EQ-24
describe("the board pack and the portal", () => {
  it("saves a draft's texts over its ETag and publishes it, then shows that an open month's figures may change; axe finds nothing", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/board-packs": { status: 200, body: { items: [draft] } },
      "GET /v1/board-packs/22222222-2222-4222-8222-222222222222": { status: 200, body: draft },
      "PUT /v1/board-packs/22222222-2222-4222-8222-222222222222": { status: 200, body: { ...draft, edit: 3, commentary: "Hosting rose" } },
      "POST /v1/board-packs/22222222-2222-4222-8222-222222222222/publish": { status: 200, body: published },
    });
    renderPage(BoardPack, { path: "/reports/board-pack", session: controller, mode: "expert" });
    await screen.findByRole("option", { name: /2026-10, version 1/ });
    fireEvent.change(screen.getByLabelText("Open a pack"), { target: { value: draft.id } });
    fireEvent.change(await screen.findByLabelText("Commentary"), { target: { value: "Hosting rose" } });
    fireEvent.click(screen.getByRole("button", { name: /Save the texts/ }));
    await waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true));
    expect(seen.find((s) => s.method === "PUT")!.headers.get("If-Match")).toBe('"2"');
    expect(seen.find((s) => s.method === "PUT")!.body).toMatchObject({ commentary: "Hosting rose" });
    await noViolations(document.body);
  });

  it("shows a Board viewer the published packs and updates sent to them, with the frozen figures and no draft", async () => {
    answer({
      "GET /v1/board-packs": { status: 200, body: { items: [{ ...published, figures: undefined }] } },
      "GET /v1/board-packs/22222222-2222-4222-8222-222222222222": { status: 200, body: published },
      "GET /v1/investor-updates": { status: 200, body: { items: [{ id: "u1", pack_id: published.id, kind: "monthly", title: "October update", highlights: "Revenue up", asks: "Introductions", recipients: ["x"], status: "sent", edit: 3, read: false }] } },
      "GET /v1/me/holding": { status: 200, body: { at: "2026-11-04", linked: false, lines: [], issued: "0", votes: "0", fully_diluted: "0" } },
    });
    renderPage(BoardPortal, { path: "/board", session: board, mode: "basic" });
    expect(await screen.findByText("October update")).toBeTruthy();
    expect(screen.getByText("New")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: /2026-10, version 1/ }));
    expect(await screen.findByText("Revenue")).toBeTruthy();
    expect(screen.getByText("6,000.00")).toBeTruthy();
    expect(screen.getByText(/month was not closed/)).toBeTruthy();
    expect(screen.getByText(/do not sign in as a shareholder/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("builds an investor update from a published pack and sends it to the members chosen", async () => {
    const update = { id: "u1", pack_id: published.id, kind: "monthly", title: "October update", highlights: "", asks: "", recipients: [], status: "draft", edit: 1 };
    answer({
      "GET /v1/investor-updates": { status: 200, body: { items: [update] } },
      "GET /v1/board-packs": { status: 200, body: { items: [{ ...published, figures: undefined }] } },
      "GET /v1/members": { status: 200, body: { items: [{ id: "m1", user_id: "11111111-1111-4111-8111-111111111111", display_name: "Ivy", roles: ["board_viewer"] }] } },
      "POST /v1/investor-updates/u1/send": { status: 200, body: { ...update, status: "sent", recipients: ["11111111-1111-4111-8111-111111111111"], edit: 2 } },
    });
    renderPage(InvestorUpdates, { path: "/equity/updates", session: controller, mode: "expert" });
    fireEvent.click(await screen.findByRole("button", { name: "October update" }));
    fireEvent.change(await screen.findByLabelText("Send to"), { target: { value: "11111111-1111-4111-8111-111111111111" } });
    const to = screen.getByLabelText("Send to") as HTMLSelectElement;
    Array.from(to.options).forEach((o) => (o.selected = true));
    fireEvent.change(to);
    fireEvent.click(screen.getByRole("button", { name: /Send it/ }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/investor-updates/u1/send")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/investor-updates/u1/send")!.headers.get("If-Match")).toBe('"1"');
  });
});
