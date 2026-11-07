import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, calls, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { domainsOf, SignInProviders } from "./SignInProviders";

const superAdmin = { ...ownerSession, super_admin: true } as Session;
const google = { provider: "google", name: "Google", enabled: true, allowed_domains: [], sign_ins: 412, last_for_super_admin: true, version: 0 };
const zoho = { provider: "zoho", name: "Zoho", enabled: true, allowed_domains: ["cognizhi.com"], sign_ins: 96, last_for_super_admin: false, version: 1, changed_at: "2026-11-01T02:00:00Z" };

function page(extra = {}) {
  return answer({ "GET /v1/installation/sign-in-methods": { status: 200, body: { items: [google, zoho] } }, ...extra });
}

// req: FR-ADM-08
// req: FR-ADM-07
describe("Sign-in providers", () => {
  it("lists each provider with its switch, the last way in locked, and axe finds nothing", async () => {
    page();
    renderPage(SignInProviders, { path: "/settings/sign-in-providers", session: superAdmin });
    const list = await screen.findByRole("list", { name: "Providers" });
    expect(within(list).getByText("Last way in for a Super admin")).toBeTruthy();
    expect(within(list).getByText(/only cognizhi.com/)).toBeTruthy();
    const locked = within(list).getByRole("switch", { name: /Google sign-in, on\. Can't be turned off/ });
    expect(locked.getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(locked);
    expect(screen.queryByRole("dialog")).toBeNull();
    await noViolations(document.body);
  });

  it("turns a provider off with its version, and says when it waits for the other Super admin", async () => {
    const seen = page({ "PATCH /v1/installation/sign-in-methods/zoho": { status: 202, body: { id: "c-1", step: "sign_in", connector: "sign-in:zoho", status: "pending" } } });
    renderPage(SignInProviders, { path: "/settings/sign-in-providers", session: superAdmin });
    fireEvent.click(await screen.findByRole("switch", { name: "Zoho sign-in, on" }));
    const dialog = await screen.findByRole("dialog", { name: "Turn Zoho off?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByText("The change of Zoho waits for the other Super admin's approval.")).toBeTruthy();
    const sent = seen.find((s) => s.method === "PATCH");
    expect(sent?.body).toEqual({ enabled: false, allowed_domains: ["cognizhi.com"], end_sessions: true });
    expect(sent?.headers.get("If-Match")).toBe('"1"');
  });

  it("sets the domains, and shows Fyniq's refusal in plain words", async () => {
    page({ "PATCH /v1/installation/sign-in-methods/google": problem(409, "STORAGE-SIGNIN-01", "That leaves a Super admin no way to sign in.") });
    renderPage(SignInProviders, { path: "/settings/sign-in-providers", session: superAdmin });
    fireEvent.click(await screen.findByRole("button", { name: "Domains of Google" }));
    const dialog = await screen.findByRole("dialog", { name: "Email domains for Google" });
    fireEvent.change(within(dialog).getByLabelText("Allowed email domains"), { target: { value: "Cognizhi.com, example.com" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText(/no way to sign in/)).toBeTruthy();
    await waitFor(() => expect(calls()).toContain("PATCH /v1/installation/sign-in-methods/google"));
  });

  it("reads typed domains as a set, in lower case", () => {
    expect(domainsOf(" Cognizhi.com, example.com\nexample.com ;b.sg")).toEqual(["cognizhi.com", "example.com", "b.sg"]);
  });
});
