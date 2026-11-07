import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls, problem } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { AskToJoin, JoinDomains, JoinRequests } from "./Join";

const ann = { id: "r-1", display_name: "Ann Tan", email: "ann@cognizhi.com", provider: "google", requested_at: "2026-11-07T01:00:00Z", status: "pending" };
const dan = { ...ann, id: "r-0", display_name: "Dan Lee", email: "dan@cognizhi.com", status: "rejected", decided_at: "2026-11-06T01:00:00Z" };

// req: FR-ADM-08
describe("Joining by email domain", () => {
  it("offers a person without an account to ask, then says it waits for an Owner", async () => {
    const seen = answer({
      "GET /v1/session/join-offer": { status: 200, body: { organisation_name: "Cognizhi Pte. Ltd.", domain: "cognizhi.com", email: "ann@cognizhi.com" } },
      "POST /v1/session/join-requests": { status: 201, body: ann },
    });
    renderPage(AskToJoin);
    expect(await screen.findByRole("heading", { name: "Ask to join Cognizhi Pte. Ltd." })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText("Your name"), { target: { value: " Ann Tan " } });
    fireEvent.click(screen.getByRole("button", { name: "Ask to join" }));
    expect(await screen.findByText(/Your request waits for an Owner/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ display_name: "Ann Tan" });
  });

  it("shows nothing when no organisation takes a request", async () => {
    answer({ "GET /v1/session/join-offer": problem(404, "API-NOT-FOUND-01", "No organisation takes a request to join from this sign-in.") });
    const { container } = renderPage(AskToJoin);
    await waitFor(() => expect(calls()).toContain("GET /v1/session/join-offer"));
    expect(container.querySelector("form")).toBeNull();
  });

  it("lets an Owner approve a pending request, and lists decided ones", async () => {
    answer({
      "GET /v1/members/join-requests": { status: 200, body: { items: [ann, dan] } },
      "POST /v1/members/join-requests/r-1/approve": { status: 200, body: { ...ann, status: "approved", user_id: "u-9" } },
    });
    renderPage(JoinRequests);
    const list = await screen.findByRole("list", { name: "Requests to join" });
    expect(within(list).getByText("Rejected")).toBeTruthy();
    fireEvent.click(within(list).getByRole("button", { name: "Approve Ann Tan" }));
    expect(await screen.findByText("Ann Tan is a member now, as an Employee.")).toBeTruthy();
  });

  it("lets an Owner claim and release the organisation's domains", async () => {
    answer({
      "GET /v1/organisation/join-domains": { status: 200, body: { items: [{ domain: "cognizhi.com", claimed_at: "2026-11-07T01:00:00Z" }] } },
      "PUT /v1/organisation/join-domains/example.com": { status: 200, body: { domain: "example.com", claimed_at: "2026-11-07T02:00:00Z" } },
      "DELETE /v1/organisation/join-domains/cognizhi.com": { status: 200, body: { domain: "cognizhi.com", claimed_at: "2026-11-07T01:00:00Z" } },
    });
    renderPage(() => <JoinDomains owner />);
    expect(await screen.findByText("cognizhi.com")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Email domain"), { target: { value: "Example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Claim" }));
    await waitFor(() => expect(calls()).toContain("PUT /v1/organisation/join-domains/example.com"));
    fireEvent.click(screen.getByRole("button", { name: "Release cognizhi.com" }));
    await waitFor(() => expect(calls()).toContain("DELETE /v1/organisation/join-domains/cognizhi.com"));
  });
});
