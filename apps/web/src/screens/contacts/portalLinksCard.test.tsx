import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PortalLinksCard } from "./PortalLinksCard";

const link = (extra: Record<string, unknown> = {}) => ({
  id: "l1",
  contact_id: "c1",
  label: "Sent to ap@acme.example",
  status: "live",
  created_at: "2026-11-20T09:30:00Z",
  expires_at: "2026-12-20T09:30:00Z",
  ...extra,
});
const Card = (mayMake: boolean) => () => <PortalLinksCard contactId="c1" mayMake={mayMake} />;

// req: FR-AR-14
describe("a customer's portal links", () => {
  it("makes a link and shows its address once, lists links without any token, and axe finds nothing", async () => {
    const sent = answer({
      "GET /v1/contacts/c1/portal-links": { status: 200, body: { links: [] } },
      "POST /v1/contacts/c1/portal-links": { status: 201, body: { ...link(), token: "t".repeat(43), path: `/portal/#${"t".repeat(43)}` } },
    });
    const { container } = renderPage(Card(true), { session: undefined });
    fireEvent.change(await screen.findByLabelText("A note about who it is for"), { target: { value: "Sent to ap@acme.example" } });
    fireEvent.click(screen.getByRole("button", { name: "Make a portal link" }));
    expect(await screen.findByText("Copy it now: it is shown only once")).toBeTruthy();
    expect((screen.getByLabelText("The portal link") as HTMLInputElement).value).toContain(`/portal/#${"t".repeat(43)}`);
    expect(sent.find((s) => s.method === "POST")?.body).toEqual({ label: "Sent to ap@acme.example" });
    await noViolations(container);
  });

  it("revokes a live link, and offers no way to make or revoke one to a reader", async () => {
    const sent = answer({
      "GET /v1/contacts/c1/portal-links": { status: 200, body: { links: [link({ first_opened_at: "2026-11-21T02:00:00Z" })] } },
      "POST /v1/portal-links/l1/revoke": { status: 200, body: link({ status: "revoked" }) },
    });
    renderPage(Card(true), { session: undefined });
    expect(await screen.findByText(/first opened/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/portal-links/l1/revoke")).toBe(true));
  });

  it("offers no way to make or revoke a link to a reader", async () => {
    answer({ "GET /v1/contacts/c1/portal-links": { status: 200, body: { links: [link()] } } });
    renderPage(Card(false), { session: undefined });
    expect(await screen.findAllByText(/Live/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Make a portal link" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Revoke" })).toBeNull();
  });
});
