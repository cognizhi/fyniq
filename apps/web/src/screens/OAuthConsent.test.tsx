import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { answer } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { leave, OAuthConsent } from "./OAuthConsent";

const screenOf = (over = {}) => ({
  client: { id: "claude-code", name: "Claude Code", class: "agent", publisher: "Anthropic", verified: true, proposal_only: true },
  requested_scopes: ["openid", "fyniq.read.reports", "fyniq.read.sales", "fyniq.read.payroll"],
  decision: { scopes: ["openid", "fyniq.read.reports", "fyniq.read.sales"], refused_scopes: ["fyniq.read.payroll"], audiences: ["http://localhost:8080/mcp"] },
  data_categories: [
    { scope: "openid", reaches: "Who you are, so the app acts as you" },
    { scope: "fyniq.read.reports", reaches: "Reports and key figures" },
    { scope: "fyniq.read.sales", reaches: "Invoices, quotes and customers" },
  ],
  expiry: { consent_valid_days: 90, access_token_minutes: 60 },
  ...over,
});

function at(challenge: string | null) {
  window.history.replaceState({}, "", challenge === null ? "/auth/consent" : `/auth/consent?consent_challenge=${challenge}`);
}

afterEach(() => vi.restoreAllMocks());

// req: AI-37
// req: AI-36
describe("The consent screen", () => {
  it("names the app and its publisher, says what each area reaches, marks what cannot be granted, and axe finds nothing", async () => {
    answer({ "GET /v1/oauth/consent": { status: 200, body: screenOf() } });
    at("abc");
    renderPage(OAuthConsent, { path: "/auth/consent" });
    expect(await screen.findByText("Claude Code wants to connect to Fyniq")).toBeTruthy();
    expect(screen.getByText(/Anthropic, verified publisher/)).toBeTruthy();
    expect(screen.getByText("Agent: proposal-only")).toBeTruthy();
    expect(screen.getByLabelText("Reports and key figures")).toHaveProperty("checked", true);
    expect(screen.getByText("Not granted")).toBeTruthy();
    expect(screen.getByText(/You allow it for 90 days/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("shows where an app that registered itself sends you, and says Fyniq has not verified it", async () => {
    answer({ "GET /v1/oauth/consent": { status: 200, body: screenOf({ client: { id: "mcp-1", name: "Acme Books", class: "agent", verified: false, proposal_only: true, redirect_uris: ["https://acme.example/cb"] } }) } });
    at("abc");
    renderPage(OAuthConsent, { path: "/auth/consent" });
    expect(await screen.findByText("Fyniq has not verified who publishes this app")).toBeTruthy();
    expect(screen.getByText("https://acme.example/cb")).toBeTruthy();
    expect(screen.getByText("Not verified")).toBeTruthy();
  });

  it("allows only the areas kept, and sends the browser where the API says", async () => {
    const seen = answer({
      "GET /v1/oauth/consent": { status: 200, body: screenOf() },
      "POST /v1/oauth/consent": { status: 200, body: { redirect_to: "http://localhost:8080/oauth/oauth2/auth?consent_verifier=v" } },
    });
    const to = vi.spyOn(leave, "to").mockImplementation(() => undefined);
    at("abc");
    renderPage(OAuthConsent, { path: "/auth/consent" });
    fireEvent.click(await screen.findByLabelText("Invoices, quotes and customers"));
    fireEvent.click(screen.getByRole("button", { name: "Allow Claude Code" }));
    await waitFor(() => expect(to).toHaveBeenCalledWith("http://localhost:8080/oauth/oauth2/auth?consent_verifier=v"));
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ consent_challenge: "abc", decision: "accept", scopes: ["openid", "fyniq.read.reports"] });
  });

  it("denies, and cannot allow with nothing kept", async () => {
    const seen = answer({
      "GET /v1/oauth/consent": { status: 200, body: screenOf() },
      "POST /v1/oauth/consent": { status: 200, body: { redirect_to: "http://localhost:8080/oauth/oauth2/auth?consent_verifier=d" } },
    });
    const to = vi.spyOn(leave, "to").mockImplementation(() => undefined);
    at("abc");
    renderPage(OAuthConsent, { path: "/auth/consent" });
    fireEvent.click(await screen.findByLabelText("Reports and key figures"));
    fireEvent.click(screen.getByLabelText("Invoices, quotes and customers"));
    expect(screen.getByRole("button", { name: "Allow Claude Code" })).toHaveProperty("disabled", true);
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await waitFor(() => expect(to).toHaveBeenCalled());
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ consent_challenge: "abc", decision: "deny" });
  });

  it("holds Allow back with the reason and offers the second factor when a gate refuses", async () => {
    answer({ "GET /v1/oauth/consent": { status: 200, body: screenOf({ decision: { scopes: ["openid", "fyniq.read.reports"], refused_scopes: [], audiences: [], code: "AUTH-FLOW-08", problem: "Confirm it's you with your second factor first." } }) } });
    at("abc");
    renderPage(OAuthConsent, { path: "/auth/consent" });
    expect(await screen.findByText("Confirm it's you with your second factor first.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Allow Claude Code" })).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "Confirm it's you" })).toBeTruthy();
  });

  it("says what to do when the request is gone or there is none", async () => {
    answer({ "GET /v1/oauth/consent": { status: 404, body: { code: "MCP-CONSENT-01", title: "gone", status: 404, detail: "The request to connect expired or is not known.", fix: "Start again from the app" } } });
    at("old");
    const first = renderPage(OAuthConsent, { path: "/auth/consent" });
    expect(await screen.findByText(/expired or is not known/)).toBeTruthy();
    first.unmount();
    at(null);
    renderPage(OAuthConsent, { path: "/auth/consent" });
    expect(await screen.findByText(/opens from an app that asks to connect/)).toBeTruthy();
  });
});
